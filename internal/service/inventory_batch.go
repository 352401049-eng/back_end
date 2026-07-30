package service

import (
	"errors"
	"fmt"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
)

type UseBatchItemInput struct {
	InventoryID       uint64                  `json:"inventory_id"`
	Quantity          uint32                  `json:"quantity"`
	PackageSelections []PackageSelectionInput `json:"package_selections"`
	OptionSelections  []OptionSelectionUnitInput `json:"option_selections"`
}

type UseBatchInput struct {
	Items             []UseBatchItemInput
	DeliveryType      uint8
	AddressID         *uint64
	DeliveryLatitude  *float64
	DeliveryLongitude *float64
	Remark            *string
}

type UseBatchResult struct {
	Usages         []InventoryUsageView `json:"usages"`
	DeliveryOrderID *uint64             `json:"delivery_order_id,omitempty"`
	PickupCode     string               `json:"pickup_code,omitempty"`
	DeliveryFee    float64              `json:"delivery_fee,omitempty"`
}

// UseBatch 同店多商品同时使用：自取各生成核销码；外卖共用一笔配送单与一次配送费。
func (s *InventoryService) UseBatch(accountID uint64, input UseBatchInput) (*UseBatchResult, error) {
	if len(input.Items) == 0 {
		return nil, fmt.Errorf("%w: 请选择商品", ErrInventoryUsageInvalid)
	}
	deliveryType, err := normalizeDeliveryType(input.DeliveryType)
	if err != nil {
		return nil, err
	}
	if deliveryType == model.DeliveryTypeDelivery && input.AddressID == nil {
		return nil, ErrAddressRequired
	}

	type loadedItem struct {
		inv     model.UserInventory
		qty     uint32
		sels    []PackageSelectionInput
		optSels []OptionSelectionUnitInput
	}
	loaded := make([]loadedItem, 0, len(input.Items))
	var merchantID uint64

	for _, it := range input.Items {
		qty := it.Quantity
		if qty == 0 {
			qty = 1
		}
		var inv model.UserInventory
		if err := query.NotDeleted(s.DB).
			Where("id = ? AND account_id = ?", it.InventoryID, accountID).
			Preload("Product", "is_deleted = ?", model.NotDeleted).
			First(&inv).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrInventoryNotFound
			}
			return nil, err
		}
		if inv.Product.ID == 0 {
			return nil, ErrProductNotFound
		}
		if inv.Quantity < qty {
			return nil, ErrInventoryInsufficient
		}
		if merchantID == 0 {
			merchantID = inv.Product.MerchantID
		} else if inv.Product.MerchantID != merchantID {
			return nil, fmt.Errorf("%w: 只能同时使用同一家店的商品", ErrInventoryUsageInvalid)
		}
		if err := validateFulfillmentFlags(inv.Product, deliveryType); err != nil {
			return nil, err
		}
		loaded = append(loaded, loadedItem{inv: inv, qty: qty, sels: it.PackageSelections, optSels: it.OptionSelections})
	}

	// 同一背包行可能拆成多份提交（每份独立套餐选配），需按合计校验库存
	needByInv := map[uint64]uint32{}
	qtyByInv := map[uint64]uint32{}
	for _, item := range loaded {
		needByInv[item.inv.ID] += item.qty
		qtyByInv[item.inv.ID] = item.inv.Quantity
	}
	for id, need := range needByInv {
		if qtyByInv[id] < need {
			return nil, ErrInventoryInsufficient
		}
	}

	var addrSnap *model.AddressSnapshot
	if deliveryType == model.DeliveryTypeDelivery {
		var addr model.UserAddress
		if err := query.NotDeleted(s.DB).Where("id = ? AND account_id = ?", *input.AddressID, accountID).First(&addr).Error; err != nil {
			return nil, ErrAddressRequired
		}
		addrSnap = AddressSnapshotFromUserAddress(&addr)
	}
	zoneSvc := s.ZoneSvc
	if zoneSvc == nil {
		zoneSvc = &DeliveryZoneService{DB: s.DB}
	}
	if err := zoneSvc.ValidateDelivery(accountID, merchantID, deliveryType, DeliveryCoordinateInput{
		AddressID: input.AddressID, AddressSnapshot: addrSnap,
	}); err != nil {
		return nil, err
	}

	status := model.InventoryUsagePendingVerify
	if deliveryType == model.DeliveryTypeDelivery {
		status = model.InventoryUsagePendingShip
	}

	result := &UseBatchResult{Usages: make([]InventoryUsageView, 0, len(loaded))}
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		var deliveryID uint64
		var pickupCode string
		var deliveryFee float64

		if deliveryType == model.DeliveryTypeDelivery {
			var merchant model.MerchantProfile
			if err := query.NotDeleted(tx).First(&merchant, merchantID).Error; err != nil {
				return ErrMerchantNotFound
			}
			deliveryFee = merchant.DeliveryFee
			d := model.DeliveryOrder{
				Status:           model.DeliveryPendingAccept,
				MerchantPrepared: 0,
				PickupCode:       genPickupCode(tx, merchantID),
				DeliveryFee:      deliveryFee,
				RiderEarnings:    merchant.RiderEarnings,
			}
			if err := tx.Create(&d).Error; err != nil {
				return err
			}
			deliveryID = d.ID
			pickupCode = d.PickupCode
			result.DeliveryOrderID = &deliveryID
			result.PickupCode = pickupCode
			result.DeliveryFee = deliveryFee
		}

		for i, item := range loaded {
			inv := item.inv
			primaryOID, err := s.deductInventoryUseFIFO(tx, accountID, inv.ProductID, inv.Spec, item.qty)
			if err != nil {
				return err
			}
			var snap model.PackageSelectionSnapshot
			pkgStatus := uint8(model.PackageSelectNone)
			if deliveryType == model.DeliveryTypeDelivery && inv.Product.ItemType == model.ProductItemTypePackage {
				var err error
				snap, pkgStatus, err = s.applyPackageSelectionsForDelivery(tx, inv.Product, item.qty, item.sels)
				if err != nil {
					return err
				}
			}

			optSnap, optStatus, err := applyOptionSelectionsForUsage(
				tx, inv.Product, deliveryType, item.qty, item.sels, item.optSels,
			)
			if err != nil {
				return err
			}

			srcOID := primaryOID
			if srcOID == nil {
				srcOID = inv.LastOrderID
			}
			usage := model.UserInventoryUsage{
				AccountID: accountID, InventoryID: inv.ID, ProductID: inv.ProductID,
				MerchantID: merchantID, SourceOrderID: srcOID,
				Quantity: item.qty, DeliveryType: deliveryType,
				AddressSnapshot: addrSnap, Status: status, Remark: input.Remark,
				PackageSelections: snap, PackageSelectStatus: pkgStatus,
				OptionSelections: optSnap, OptionSelectStatus: optStatus,
			}
			if deliveryID > 0 {
				usage.DeliveryOrderID = &deliveryID
			}
			if err := tx.Create(&usage).Error; err != nil {
				return err
			}
			var useLogIDs []uint64
			if err := tx.Model(&model.UserInventoryLog{}).
				Where("account_id = ? AND product_id = ? AND spec = ? AND event_type = ? AND usage_id IS NULL",
					accountID, inv.ProductID, inv.Spec, model.InventoryEventUse).
				Order("id DESC").Limit(int(item.qty)+4).
				Pluck("id", &useLogIDs).Error; err != nil {
				return err
			}
			if len(useLogIDs) > 0 {
				if err := tx.Model(&model.UserInventoryLog{}).Where("id IN ?", useLogIDs).
					Update("usage_id", usage.ID).Error; err != nil {
					return err
				}
			}

			var verifyCode *string
			if deliveryType == model.DeliveryTypePickup {
				vc, err := createVerificationCodeForUsage(tx, accountID, usage.ID)
				if err != nil {
					return err
				}
				verifyCode = &vc.Code
			} else if i == 0 && deliveryID > 0 {
				// 首个 usage 回填到 delivery_order.inventory_usage_id（兼容旧查询）
				if err := tx.Model(&model.DeliveryOrder{}).Where("id = ?", deliveryID).
					Update("inventory_usage_id", usage.ID).Error; err != nil {
					return err
				}
			}

			view := InventoryUsageView{
				UserInventoryUsage: usage,
				StatusText:         model.InventoryUsageStatusText(usage.Status),
				VerifyCode:         verifyCode,
			}
			view.Product = &inv.Product
			enrichUsageViewPackage(&view)
			enrichUsageViewOptions(s.DB, &view)
			result.Usages = append(result.Usages, view)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return result, nil
}
