package service

import (
	"errors"
	"fmt"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
)

// AutoPickupAfterCredit 审核入袋后立即按自取「使用」生成待核销+核销码（跳过用户点使用）。
// 幂等：若本单已有 use 流水则跳过。须在已 credit 的同一事务内调用。
func (s *InventoryService) AutoPickupAfterCredit(tx *gorm.DB, accountID, orderID uint64, usageMerchantID uint64) error {
	if accountID == 0 || orderID == 0 {
		return nil
	}
	var used int64
	if err := query.NotDeleted(tx.Model(&model.UserInventoryLog{})).
		Where("order_id = ? AND event_type = ?", orderID, model.InventoryEventUse).
		Count(&used).Error; err != nil {
		return err
	}
	if used > 0 {
		return nil
	}

	var credits []model.UserInventoryLog
	if err := query.NotDeleted(tx).
		Where("order_id = ? AND event_type = ? AND delta_qty > 0", orderID, model.InventoryEventOrderCredit).
		Order("id ASC").
		Find(&credits).Error; err != nil {
		return err
	}
	if len(credits) == 0 {
		return nil
	}

	type key struct {
		ProductID uint64
		Spec      string
	}
	qtyBy := map[key]uint32{}
	orderKeys := make([]key, 0)
	for _, lg := range credits {
		k := key{ProductID: lg.ProductID, Spec: lg.Spec}
		if _, ok := qtyBy[k]; !ok {
			orderKeys = append(orderKeys, k)
		}
		qtyBy[k] += uint32(lg.DeltaQty)
	}

	oid := orderID
	for _, k := range orderKeys {
		qty := qtyBy[k]
		if qty == 0 {
			continue
		}
		var inv model.UserInventory
		if err := query.NotDeleted(tx).
			Where("account_id = ? AND product_id = ? AND spec = ?", accountID, k.ProductID, k.Spec).
			Preload("Product", "is_deleted = ?", model.NotDeleted).
			First(&inv).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("%w: 入袋后无库存记录 product=%d", ErrInventoryNotFound, k.ProductID)
			}
			return err
		}
		if inv.Quantity < qty {
			return fmt.Errorf("%w: 自动自取数量不足 product=%d need=%d have=%d", ErrInventoryInsufficient, k.ProductID, qty, inv.Quantity)
		}
		if inv.Product.ID == 0 {
			return ErrProductNotFound
		}
		ownerMerchantID := inv.Product.MerchantID
		umID := usageMerchantID
		if umID == 0 {
			umID = ownerMerchantID
		}

		primaryOID, err := s.deductInventoryUseFIFO(tx, accountID, inv.ProductID, inv.Spec, qty)
		if err != nil {
			return err
		}
		srcOID := primaryOID
		if srcOID == nil {
			srcOID = &oid
		}

		optSnap, optStatus, err := applyOptionSelectionsForUsage(
			tx, inv.Product, model.DeliveryTypePickup, qty, nil, nil,
		)
		if err != nil {
			return err
		}

		expireAt, err := resolveUsageExpireAt(tx, inv.ProductID, srcOID, time.Now())
		if err != nil {
			return err
		}

		usage := model.UserInventoryUsage{
			AccountID:          accountID,
			InventoryID:        inv.ID,
			ProductID:          inv.ProductID,
			MerchantID:         ownerMerchantID,
			UsageMerchantID:    umID,
			SourceOrderID:      srcOID,
			Quantity:           qty,
			DeliveryType:       model.DeliveryTypePickup,
			Status:             model.InventoryUsagePendingVerify,
			PackageSelectStatus: model.PackageSelectNone,
			OptionSelections:   optSnap,
			OptionSelectStatus: optStatus,
			ExpireAt:           expireAt,
		}
		if err := tx.Create(&usage).Error; err != nil {
			return err
		}
		var useLogIDs []uint64
		if err := tx.Model(&model.UserInventoryLog{}).
			Where("account_id = ? AND product_id = ? AND spec = ? AND event_type = ? AND usage_id IS NULL",
				accountID, inv.ProductID, inv.Spec, model.InventoryEventUse).
			Order("id DESC").Limit(int(qty)+4).
			Pluck("id", &useLogIDs).Error; err != nil {
			return err
		}
		if len(useLogIDs) > 0 {
			if err := tx.Model(&model.UserInventoryLog{}).Where("id IN ?", useLogIDs).
				Update("usage_id", usage.ID).Error; err != nil {
				return err
			}
		}
		if _, err := createVerificationCodeForUsage(tx, accountID, usage.ID, expireAt); err != nil {
			return err
		}
	}
	return nil
}

func resolveUsageMerchantID(order *model.Order) uint64 {
	if order == nil {
		return 0
	}
	if order.UsageMerchantID != nil && *order.UsageMerchantID > 0 {
		return *order.UsageMerchantID
	}
	return order.MerchantID
}
