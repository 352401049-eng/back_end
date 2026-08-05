package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ConvertDeliveryInput 待核销改为配送。
type ConvertDeliveryInput struct {
	AddressID         uint64
	UsageMerchantID   uint64
	Remark            *string
	PackageSelections []PackageSelectionInput
	OptionSelections  []OptionSelectionUnitInput
	// SkipFeeCheck 配送费已付（MarkPaid 履约）时为 true
	SkipFeeCheck bool
}

// ConvertDeliveryResult 改为配送结果：免配送费直接履约，或返回待支付配送费单。
type ConvertDeliveryResult struct {
	Usage            *InventoryUsageView  `json:"usage,omitempty"`
	DeliveryFeeOrder *DeliveryFeePayView  `json:"delivery_fee_order,omitempty"`
	NeedPayFee       bool                 `json:"need_pay_fee"`
}

// ConvertPendingVerifyToDelivery 将待核销自取改为配送；有配送费时创建预支付单。
func (s *InventoryService) ConvertPendingVerifyToDelivery(accountID, usageID uint64, input ConvertDeliveryInput) (*ConvertDeliveryResult, error) {
	if input.AddressID == 0 {
		return nil, ErrAddressRequired
	}

	var usage model.UserInventoryUsage
	if err := query.NotDeleted(s.DB).
		Where("id = ? AND account_id = ?", usageID, accountID).
		Preload("Product", "is_deleted = ?", model.NotDeleted).
		First(&usage).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInventoryUsageNotFound
		}
		return nil, err
	}
	if usage.Status != model.InventoryUsagePendingVerify {
		return nil, fmt.Errorf("%w: 仅待核销可改为配送", ErrInventoryUsageInvalid)
	}
	if usage.DeliveryType != model.DeliveryTypePickup {
		return nil, fmt.Errorf("%w: 仅自取待核销可改为配送", ErrInventoryUsageInvalid)
	}
	if usage.Product == nil || usage.Product.ID == 0 {
		return nil, ErrProductNotFound
	}
	if err := validateFulfillmentFlags(*usage.Product, model.DeliveryTypeDelivery); err != nil {
		return nil, err
	}

	usageMerchantID := input.UsageMerchantID
	if usageMerchantID == 0 {
		usageMerchantID = usage.UsageMerchantID
	}
	if usageMerchantID == 0 {
		usageMerchantID = usage.MerchantID
	}
	if usageMerchantID == 0 {
		return nil, fmt.Errorf("%w: 请指定使用门店", ErrInventoryUsageInvalid)
	}
	productSvc := &ProductService{DB: s.DB}
	if err := productSvc.AssertMerchantApplicable(usage.ProductID, usageMerchantID); err != nil {
		return nil, err
	}

	var addr model.UserAddress
	if err := query.NotDeleted(s.DB).Where("id = ? AND account_id = ?", input.AddressID, accountID).First(&addr).Error; err != nil {
		return nil, ErrAddressRequired
	}
	addrSnap := AddressSnapshotFromUserAddress(&addr)

	zoneSvc := s.ZoneSvc
	if zoneSvc == nil {
		zoneSvc = &DeliveryZoneService{DB: s.DB}
	}
	if err := zoneSvc.ValidateDelivery(accountID, usageMerchantID, model.DeliveryTypeDelivery, DeliveryCoordinateInput{
		AddressID: &input.AddressID, AddressSnapshot: addrSnap,
	}); err != nil {
		return nil, err
	}

	var mp model.MerchantProfile
	if err := query.NotDeleted(s.DB).Select("id", "delivery_fee", "rider_earnings").First(&mp, usageMerchantID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMerchantNotFound
		}
		return nil, err
	}

	// 预校验配送选配（套餐/规格）
	if usage.Product.ItemType == model.ProductItemTypePackage {
		if _, _, err := (&InventoryService{DB: s.DB}).applyPackageSelectionsForDelivery(
			s.DB, *usage.Product, usage.Quantity, input.PackageSelections,
		); err != nil {
			return nil, err
		}
	}
	if _, _, err := applyOptionSelectionsForUsage(
		s.DB, *usage.Product, model.DeliveryTypeDelivery, usage.Quantity,
		input.PackageSelections, input.OptionSelections,
	); err != nil {
		return nil, err
	}

	deliveryFee := roundMoney(mp.DeliveryFee)
	if deliveryFee > 0 && !input.SkipFeeCheck {
		if s.DeliveryFeePaySvc == nil {
			return nil, ErrDeliveryFeePaymentRequired
		}
		feeView, err := s.DeliveryFeePaySvc.CreateForConvertUsage(accountID, CreateConvertDeliveryFeeInput{
			UsageID:           usageID,
			MerchantID:        usageMerchantID,
			AddressID:         input.AddressID,
			Remark:            input.Remark,
			PackageSelections: input.PackageSelections,
			OptionSelections:  input.OptionSelections,
		})
		if err != nil {
			return nil, err
		}
		return &ConvertDeliveryResult{DeliveryFeeOrder: feeView, NeedPayFee: true}, nil
	}

	view, err := s.convertPendingVerifyInTx(s.DB, accountID, usageID, ConvertDeliveryInput{
		AddressID:         input.AddressID,
		UsageMerchantID:   usageMerchantID,
		Remark:            input.Remark,
		PackageSelections: input.PackageSelections,
		OptionSelections:  input.OptionSelections,
		SkipFeeCheck:      true,
	}, &mp)
	if err != nil {
		return nil, err
	}
	return &ConvertDeliveryResult{Usage: view, NeedPayFee: false}, nil
}

func (s *InventoryService) convertPendingVerifyInTx(
	db *gorm.DB, accountID, usageID uint64, input ConvertDeliveryInput, mp *model.MerchantProfile,
) (*InventoryUsageView, error) {
	var out *InventoryUsageView
	err := db.Transaction(func(tx *gorm.DB) error {
		var usage model.UserInventoryUsage
		if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
			Where("id = ? AND account_id = ?", usageID, accountID).
			Preload("Product", "is_deleted = ?", model.NotDeleted).
			First(&usage).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrInventoryUsageNotFound
			}
			return err
		}
		if usage.Status != model.InventoryUsagePendingVerify {
			return fmt.Errorf("%w: 仅待核销可改为配送", ErrInventoryUsageInvalid)
		}
		if usage.Product == nil || usage.Product.ID == 0 {
			return ErrProductNotFound
		}

		var addr model.UserAddress
		if err := query.NotDeleted(tx).Where("id = ? AND account_id = ?", input.AddressID, accountID).First(&addr).Error; err != nil {
			return ErrAddressRequired
		}
		addrSnap := AddressSnapshotFromUserAddress(&addr)

		var snap model.PackageSelectionSnapshot
		pkgStatus := uint8(model.PackageSelectNone)
		if usage.Product.ItemType == model.ProductItemTypePackage {
			var err error
			snap, pkgStatus, err = s.applyPackageSelectionsForDelivery(tx, *usage.Product, usage.Quantity, input.PackageSelections)
			if err != nil {
				return err
			}
		}
		optSnap, optStatus, err := applyOptionSelectionsForUsage(
			tx, *usage.Product, model.DeliveryTypeDelivery, usage.Quantity,
			input.PackageSelections, input.OptionSelections,
		)
		if err != nil {
			return err
		}

		now := time.Now()
		if err := tx.Model(&model.VerificationCode{}).
			Where("inventory_usage_id = ? AND status = ?", usage.ID, model.VerificationCodeUnused).
			Updates(map[string]interface{}{"status": model.VerificationCodeExpired, "used_at": now}).Error; err != nil {
			return err
		}

		deliveryFee := float64(0)
		riderEarnings := float64(0)
		if mp != nil {
			deliveryFee = mp.DeliveryFee
			riderEarnings = mp.RiderEarnings
		}
		d := model.DeliveryOrder{
			InventoryUsageID: &usage.ID,
			Status:           model.DeliveryPendingAdminReview,
			MerchantPrepared: 1,
			PreparedAt:       &now,
			DeliveryFee:      deliveryFee,
			RiderEarnings:    riderEarnings,
		}
		if err := tx.Create(&d).Error; err != nil {
			return err
		}

		updates := map[string]interface{}{
			"delivery_type":         model.DeliveryTypeDelivery,
			"status":                model.InventoryUsagePendingShip,
			"usage_merchant_id":     input.UsageMerchantID,
			"delivery_order_id":     d.ID,
			"package_selections":    snap,
			"package_select_status": pkgStatus,
			"option_selections":     optSnap,
			"option_select_status":  optStatus,
		}
		if input.Remark != nil {
			updates["remark"] = *input.Remark
		}
		if err := tx.Model(&usage).Updates(updates).Error; err != nil {
			return err
		}
		if err := tx.Model(&usage).Update("address_snapshot", addrSnap).Error; err != nil {
			return err
		}

		view, err := s.GetUsageView(accountID, usage.ID)
		if err != nil {
			return err
		}
		// GetUsageView 用全局 DB；事务未提交时可能读不到更新。手动拼装。
		_ = view
		usage.DeliveryType = model.DeliveryTypeDelivery
		usage.Status = model.InventoryUsagePendingShip
		usage.AddressSnapshot = addrSnap
		usage.UsageMerchantID = input.UsageMerchantID
		usage.DeliveryOrderID = &d.ID
		usage.PackageSelections = snap
		usage.PackageSelectStatus = pkgStatus
		usage.OptionSelections = optSnap
		usage.OptionSelectStatus = optStatus
		built := &InventoryUsageView{
			UserInventoryUsage: usage,
			StatusText:         model.InventoryUsageStatusText(usage.Status),
		}
		enrichUsageViewPackage(built)
		enrichUsageViewOptions(tx, built)
		out = built
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// CreateConvertDeliveryFeeInput 待核销改配送的配送费预支付。
type CreateConvertDeliveryFeeInput struct {
	UsageID           uint64
	MerchantID        uint64
	AddressID         uint64
	Remark            *string
	PackageSelections []PackageSelectionInput
	OptionSelections  []OptionSelectionUnitInput
}

func (s *DeliveryFeePayService) CreateForConvertUsage(accountID uint64, in CreateConvertDeliveryFeeInput) (*DeliveryFeePayView, error) {
	if in.UsageID == 0 || in.MerchantID == 0 || in.AddressID == 0 {
		return nil, fmt.Errorf("%w: 参数不完整", ErrInventoryUsageInvalid)
	}
	var mp model.MerchantProfile
	if err := query.NotDeleted(s.DB).Select("id", "delivery_fee", "rider_earnings").First(&mp, in.MerchantID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMerchantNotFound
		}
		return nil, err
	}
	deliveryFee := roundMoney(mp.DeliveryFee)
	if deliveryFee <= 0 {
		return nil, fmt.Errorf("%w: 该商家无需支付配送费", ErrInventoryUsageInvalid)
	}

	payload := deliveryFeePayload{
		ConvertUsageID:    in.UsageID,
		AddressID:         in.AddressID,
		Remark:            in.Remark,
		UsageMerchantID:   in.MerchantID,
		PackageSelections: in.PackageSelections,
		OptionSelections:  in.OptionSelections,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	expireAt := now.Add(time.Duration(s.payTimeoutMinutes()) * time.Minute)
	var feeOrder model.DeliveryFeeOrder
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		feeOrder = model.DeliveryFeeOrder{
			OrderNo:       genDeliveryFeeOrderNo(),
			AccountID:     accountID,
			MerchantID:    in.MerchantID,
			Status:        model.DeliveryFeeStatusPendingPay,
			Amount:        deliveryFee,
			RiderEarnings: roundMoney(mp.RiderEarnings),
			PayAmount:     deliveryFee,
			PayStatus:     model.PayStatusUnpaid,
			PayExpireAt:   &expireAt,
			Payload:       raw,
		}
		if err := tx.Create(&feeOrder).Error; err != nil {
			return err
		}
		return s.settlePaymentInTx(tx, feeOrder.ID, now)
	})
	if err != nil {
		return nil, err
	}
	return s.toView(&feeOrder), nil
}
