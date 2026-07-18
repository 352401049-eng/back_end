package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
)

var (
	ErrInventoryNotFound           = errors.New("inventory not found")
	ErrInventoryInsufficient       = errors.New("inventory insufficient")
	ErrInventoryUsageNotFound      = errors.New("inventory usage not found")
	ErrInventoryUsageInvalid       = errors.New("inventory usage invalid")
	ErrInventoryRollback           = errors.New("inventory rollback failed")
	ErrInventoryCancelPending      = errors.New("inventory cancel pending review")
	ErrVirtualNotDeliverable       = errors.New("virtual product not deliverable")
)

type InventoryService struct {
	DB      *gorm.DB
	ZoneSvc *DeliveryZoneService
}

type UseInventoryInput struct {
	Quantity          uint32
	DeliveryType      uint8
	AddressID         *uint64
	DeliveryLatitude  *float64
	DeliveryLongitude *float64
	Remark            *string
}

type InventoryUsageView struct {
	model.UserInventoryUsage
	StatusText string      `json:"status_text"`
	VerifyCode *string     `json:"verify_code,omitempty"`
	Buyer      *BuyerBrief `json:"buyer,omitempty"`
}

func (s *InventoryService) CreditFromOrder(tx *gorm.DB, accountID, orderID uint64, items []model.OrderItem) error {
	var credited int64
	if err := query.NotDeleted(tx.Model(&model.UserInventoryLog{})).
		Where("order_id = ? AND event_type = ?", orderID, model.InventoryEventOrderCredit).
		Count(&credited).Error; err != nil {
		return err
	}
	if credited > 0 {
		return nil
	}

	for _, it := range items {
		spec := orderItemSpec(it)
		if err := s.adjustQuantity(tx, accountID, it.ProductID, spec, int32(it.Quantity), &orderID, nil, model.InventoryEventOrderCredit, nil); err != nil {
			return err
		}
		if err := tx.Model(&model.UserInventory{}).
			Where("account_id = ? AND product_id = ? AND spec = ? AND is_deleted = 0", accountID, it.ProductID, spec).
			Update("last_order_id", orderID).Error; err != nil {
			return err
		}
	}
	return nil
}

func (s *InventoryService) RollbackOrderCredit(tx *gorm.DB, orderID uint64) error {
	var logs []model.UserInventoryLog
	if err := query.NotDeleted(tx).
		Where("order_id = ? AND event_type IN ?", orderID, []string{model.InventoryEventOrderCredit, model.InventoryEventOrderRollback}).
		Find(&logs).Error; err != nil {
		return err
	}
	if len(logs) == 0 {
		return nil
	}

	credited := int32(0)
	rolledBack := int32(0)
	var accountID uint64
	for _, lg := range logs {
		accountID = lg.AccountID
		if lg.EventType == model.InventoryEventOrderCredit {
			credited += lg.DeltaQty
		} else {
			rolledBack += -lg.DeltaQty
		}
	}
	remaining := credited - rolledBack
	if remaining <= 0 {
		return nil
	}

	var items []model.OrderItem
	if err := query.NotDeleted(tx).Where("order_id = ?", orderID).Find(&items).Error; err != nil {
		return err
	}
	for _, it := range items {
		spec := orderItemSpec(it)
		rollbackQty := int32(it.Quantity)
		if rollbackQty > remaining {
			rollbackQty = remaining
		}
		if rollbackQty <= 0 {
			continue
		}
		if err := s.adjustQuantity(tx, accountID, it.ProductID, spec, -rollbackQty, &orderID, nil, model.InventoryEventOrderRollback, strPtr("订单取消回滚")); err != nil {
			if errors.Is(err, ErrInventoryInsufficient) {
				return ErrInventoryRollback
			}
			return err
		}
		remaining -= rollbackQty
	}
	if remaining > 0 {
		return ErrInventoryRollback
	}
	return nil
}

func (s *InventoryService) Use(accountID, inventoryID uint64, input UseInventoryInput) (*InventoryUsageView, error) {
	if input.Quantity == 0 {
		input.Quantity = 1
	}
	deliveryType, err := normalizeDeliveryType(input.DeliveryType)
	if err != nil {
		return nil, err
	}
	if deliveryType == model.DeliveryTypeDelivery && input.AddressID == nil {
		return nil, ErrAddressRequired
	}

	var inv model.UserInventory
	if err := query.NotDeleted(s.DB).
		Where("id = ? AND account_id = ?", inventoryID, accountID).
		Preload("Product", "is_deleted = ?", model.NotDeleted).
		First(&inv).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInventoryNotFound
		}
		return nil, err
	}
	if inv.Quantity < input.Quantity {
		return nil, ErrInventoryInsufficient
	}
	if inv.Product.ID == 0 {
		return nil, ErrProductNotFound
	}
	// 虚拟商品（如电影票）只能到店核销，不支持骑手配送
	if inv.Product.ItemType == model.ProductItemTypeVirtual && deliveryType == model.DeliveryTypeDelivery {
		return nil, ErrVirtualNotDeliverable
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
	if err := zoneSvc.ValidateDelivery(accountID, inv.Product.MerchantID, deliveryType, DeliveryCoordinateInput{
		AddressID: input.AddressID, AddressSnapshot: addrSnap,
	}); err != nil {
		return nil, err
	}

	status := model.InventoryUsagePendingVerify
	if deliveryType == model.DeliveryTypeDelivery {
		status = model.InventoryUsagePendingShip
	}

	var usage model.UserInventoryUsage
	var verifyCode *string

	err = s.DB.Transaction(func(tx *gorm.DB) error {
		usageIDHolder := uint64(0)
		if err := s.adjustQuantity(tx, accountID, inv.ProductID, inv.Spec, -int32(input.Quantity), nil, nil, model.InventoryEventUse, nil); err != nil {
			return err
		}

		usage = model.UserInventoryUsage{
			AccountID: accountID, InventoryID: inv.ID, ProductID: inv.ProductID,
			MerchantID: inv.Product.MerchantID, SourceOrderID: inv.LastOrderID,
			Quantity: input.Quantity, DeliveryType: deliveryType,
			AddressSnapshot: addrSnap, Status: status, Remark: input.Remark,
		}
		if err := tx.Create(&usage).Error; err != nil {
			return err
		}
		usageIDHolder = usage.ID

		// 回填 usage_id 到最近一条 use 流水
		if err := tx.Model(&model.UserInventoryLog{}).
			Where("account_id = ? AND product_id = ? AND spec = ? AND event_type = ? AND usage_id IS NULL", accountID, inv.ProductID, inv.Spec, model.InventoryEventUse).
			Order("id DESC").Limit(1).
			Update("usage_id", usageIDHolder).Error; err != nil {
			return err
		}

		if deliveryType == model.DeliveryTypePickup {
			vc, err := createVerificationCodeForUsage(tx, accountID, usage.ID)
			if err != nil {
				return err
			}
			verifyCode = &vc.Code
		} else {
			d := model.DeliveryOrder{
				InventoryUsageID: &usage.ID,
				Status:           model.DeliveryPendingAccept,
			}
			if err := tx.Create(&d).Error; err != nil {
				return err
			}
			if err := tx.Model(&usage).Update("delivery_order_id", d.ID).Error; err != nil {
				return err
			}
			usage.DeliveryOrderID = &d.ID
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	view := &InventoryUsageView{
		UserInventoryUsage: usage,
		StatusText:         model.InventoryUsageStatusText(usage.Status),
		VerifyCode:         verifyCode,
	}
	return view, nil
}

func (s *InventoryService) RequestCancelUsage(accountID, usageID uint64, reason *string) (*InventoryUsageView, error) {
	var usage model.UserInventoryUsage
	if err := query.NotDeleted(s.DB).Where("id = ? AND account_id = ?", usageID, accountID).First(&usage).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInventoryUsageNotFound
		}
		return nil, err
	}
	if usage.Status == model.InventoryUsageCancelPending {
		return nil, ErrInventoryCancelPending
	}
	if usage.Status != model.InventoryUsagePendingVerify && usage.Status != model.InventoryUsagePendingShip {
		return nil, ErrInventoryUsageInvalid
	}

	// 自提待核销：直接取消
	if usage.Status == model.InventoryUsagePendingVerify {
		return s.finalizeCancelUsage(accountID, &usage, reason)
	}

	// 配送：未接单可直取消；骑手已接单需商家审核
	if usage.DeliveryOrderID == nil {
		return s.finalizeCancelUsage(accountID, &usage, reason)
	}
	var delivery model.DeliveryOrder
	if err := query.NotDeleted(s.DB).First(&delivery, *usage.DeliveryOrderID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return s.finalizeCancelUsage(accountID, &usage, reason)
		}
		return nil, err
	}
	if delivery.Status == model.DeliveryPendingAccept {
		return s.finalizeCancelUsage(accountID, &usage, reason)
	}
	if delivery.Status == model.DeliveryCancelled || delivery.Status == model.DeliveryConfirmed {
		return nil, ErrInventoryUsageInvalid
	}

	// 骑手已接单至用户确认收货前：提交取消申请，待商家审核
	updates := map[string]interface{}{
		"status":        model.InventoryUsageCancelPending,
		"cancel_reason": reason,
	}
	if err := s.DB.Model(&usage).Updates(updates).Error; err != nil {
		return nil, err
	}
	usage.Status = model.InventoryUsageCancelPending
	usage.CancelReason = reason
	view, err := s.GetUsageView(accountID, usageID)
	if err != nil {
		return nil, err
	}
	return view, nil
}

// CancelUsage 兼容旧调用。
func (s *InventoryService) CancelUsage(accountID, usageID uint64) (*InventoryUsageView, error) {
	return s.RequestCancelUsage(accountID, usageID, nil)
}

func (s *InventoryService) MerchantReviewCancelUsage(merchantID, usageID uint64, approve bool, rejectReason *string) (*InventoryUsageView, error) {
	var usage model.UserInventoryUsage
	if err := query.NotDeleted(s.DB).Where("id = ? AND merchant_id = ?", usageID, merchantID).First(&usage).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInventoryUsageNotFound
		}
		return nil, err
	}
	if usage.Status != model.InventoryUsageCancelPending {
		return nil, ErrInventoryUsageInvalid
	}
	if approve {
		return s.finalizeCancelUsage(usage.AccountID, &usage, usage.CancelReason)
	}
	updates := map[string]interface{}{
		"status":        model.InventoryUsagePendingShip,
		"cancel_reason": rejectReason,
	}
	if err := s.DB.Model(&usage).Updates(updates).Error; err != nil {
		return nil, err
	}
	return s.GetUsageView(0, usageID)
}

func (s *InventoryService) finalizeCancelUsage(accountID uint64, usage *model.UserInventoryUsage, reason *string) (*InventoryUsageView, error) {
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var inv model.UserInventory
		if err := query.NotDeleted(tx).First(&inv, usage.InventoryID).Error; err != nil {
			return err
		}
		if err := s.adjustQuantity(tx, accountID, usage.ProductID, inv.Spec, int32(usage.Quantity), usage.SourceOrderID, &usage.ID, model.InventoryEventUseCancel, strPtr("取消使用回滚")); err != nil {
			return err
		}

		if usage.Status == model.InventoryUsagePendingVerify || usage.DeliveryType == model.DeliveryTypePickup {
			now := time.Now()
			if err := tx.Model(&model.VerificationCode{}).
				Where("inventory_usage_id = ? AND status = ?", usage.ID, model.VerificationCodeUnused).
				Updates(map[string]interface{}{"status": model.VerificationCodeExpired, "used_at": now}).Error; err != nil {
				return err
			}
		}

		if usage.DeliveryOrderID != nil {
			if err := tx.Model(&model.DeliveryOrder{}).
				Where("id = ? AND status NOT IN ?", *usage.DeliveryOrderID, []int{int(model.DeliveryConfirmed), int(model.DeliveryCancelled)}).
				Update("status", model.DeliveryCancelled).Error; err != nil {
				return err
			}
		}

		updates := map[string]interface{}{"status": model.InventoryUsageCancelled}
		if reason != nil {
			updates["cancel_reason"] = *reason
		}
		return tx.Model(usage).Updates(updates).Error
	})
	if err != nil {
		return nil, err
	}
	return s.GetUsageView(accountID, usage.ID)
}

func (s *InventoryService) GetUsageView(accountID, usageID uint64) (*InventoryUsageView, error) {
	var usage model.UserInventoryUsage
	q := query.NotDeleted(s.DB).
		Preload("Product", "is_deleted = ?", model.NotDeleted).
		Preload("DeliveryOrder", "is_deleted = ?", model.NotDeleted).
		Where("id = ?", usageID)
	if accountID > 0 {
		q = q.Where("account_id = ?", accountID)
	}
	if err := q.First(&usage).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInventoryUsageNotFound
		}
		return nil, err
	}
	view := &InventoryUsageView{
		UserInventoryUsage: usage,
		StatusText:         model.InventoryUsageStatusText(usage.Status),
	}
	if usage.Status == model.InventoryUsagePendingVerify {
		var vc model.VerificationCode
		if err := query.NotDeleted(s.DB).
			Where("inventory_usage_id = ? AND status = ?", usage.ID, model.VerificationCodeUnused).
			First(&vc).Error; err == nil {
			view.VerifyCode = &vc.Code
		}
	}
	s.enrichUsageBuyer(view)
	return view, nil
}

func (s *InventoryService) GetUsageViewForMerchant(merchantID, usageID uint64) (*InventoryUsageView, error) {
	view, err := s.GetUsageView(0, usageID)
	if err != nil {
		return nil, err
	}
	if view.MerchantID != merchantID {
		return nil, ErrInventoryUsageNotFound
	}
	return view, nil
}

func (s *InventoryService) enrichUsageBuyer(view *InventoryUsageView) {
	var acc model.Account
	if err := query.NotDeleted(s.DB).Select("id", "nickname", "phone").
		First(&acc, view.AccountID).Error; err != nil {
		return
	}
	view.Buyer = &BuyerBrief{AccountID: acc.ID, Nickname: acc.Nickname, Phone: acc.Phone}
}

func (s *InventoryService) ListUsages(accountID uint64, page, pageSize int) ([]InventoryUsageView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 50 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize

	q := query.NotDeleted(s.DB.Model(&model.UserInventoryUsage{})).Where("account_id = ?", accountID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.UserInventoryUsage
	if err := q.Preload("Product", "is_deleted = ?", model.NotDeleted).
		Order("id DESC").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	views := make([]InventoryUsageView, 0, len(list))
	for i := range list {
		v, err := s.GetUsageView(accountID, list[i].ID)
		if err != nil {
			return nil, 0, err
		}
		views = append(views, *v)
	}
	return views, total, nil
}

func (s *InventoryService) ListUsagesForMerchant(merchantID uint64, status *uint8, page, pageSize int) ([]InventoryUsageView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 50 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize

	q := query.NotDeleted(s.DB.Model(&model.UserInventoryUsage{})).Where("merchant_id = ?", merchantID)
	if status != nil {
		q = q.Where("status = ?", *status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.UserInventoryUsage
	if err := q.Order("id DESC").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	views := make([]InventoryUsageView, 0, len(list))
	for i := range list {
		v, err := s.GetUsageView(0, list[i].ID)
		if err != nil {
			return nil, 0, err
		}
		views = append(views, *v)
	}
	return views, total, nil
}

func (s *InventoryService) ListUsagesForAdmin(merchantID *uint64, status *uint8, page, pageSize int) ([]InventoryUsageView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 50 {
		pageSize = 50
	}
	offset := (page - 1) * pageSize

	q := query.NotDeleted(s.DB.Model(&model.UserInventoryUsage{}))
	if merchantID != nil {
		q = q.Where("merchant_id = ?", *merchantID)
	}
	if status != nil {
		q = q.Where("status = ?", *status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.UserInventoryUsage
	if err := q.Order("id DESC").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	views := make([]InventoryUsageView, 0, len(list))
	for i := range list {
		v, err := s.GetUsageView(0, list[i].ID)
		if err != nil {
			return nil, 0, err
		}
		views = append(views, *v)
	}
	return views, total, nil
}

func (s *InventoryService) CompleteUsageByVerify(tx *gorm.DB, usageID uint64) error {
	return tx.Model(&model.UserInventoryUsage{}).
		Where("id = ? AND status = ?", usageID, model.InventoryUsagePendingVerify).
		Update("status", model.InventoryUsageCompleted).Error
}

func (s *InventoryService) adjustQuantity(
	tx *gorm.DB, accountID, productID uint64, spec string, delta int32,
	orderID, usageID *uint64, eventType string, remark *string,
) error {
	var inv model.UserInventory
	err := query.NotDeleted(tx).
		Where("account_id = ? AND product_id = ? AND spec = ?", accountID, productID, spec).
		First(&inv).Error

	before := uint32(0)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if delta < 0 {
			return ErrInventoryInsufficient
		}
		inv = model.UserInventory{
			AccountID: accountID, ProductID: productID, Spec: spec, Quantity: uint32(delta),
		}
		if orderID != nil {
			inv.LastOrderID = orderID
		}
		if err := tx.Create(&inv).Error; err != nil {
			return err
		}
	} else if err != nil {
		return err
	} else {
		before = inv.Quantity
		after := int64(before) + int64(delta)
		if after < 0 {
			return ErrInventoryInsufficient
		}
		updates := map[string]interface{}{"quantity": after}
		if orderID != nil && delta > 0 {
			updates["last_order_id"] = *orderID
		}
		if err := tx.Model(&inv).Updates(updates).Error; err != nil {
			return err
		}
		inv.Quantity = uint32(after)
	}

	log := model.UserInventoryLog{
		AccountID: accountID, InventoryID: &inv.ID, ProductID: productID, Spec: spec,
		OrderID: orderID, UsageID: usageID, EventType: eventType, DeltaQty: delta,
		BeforeQty: before, AfterQty: inv.Quantity, Remark: remark,
	}
	return tx.Create(&log).Error
}

func createVerificationCodeForUsage(tx *gorm.DB, accountID, usageID uint64) (*model.VerificationCode, error) {
	code := genVerifyCodeStr()
	vc := model.VerificationCode{
		AccountID: accountID, InventoryUsageID: &usageID, Code: code,
		Status: model.VerificationCodeUnused,
	}
	exp := time.Now().AddDate(0, 0, 30)
	vc.ExpiredAt = &exp
	if err := tx.Create(&vc).Error; err != nil {
		return nil, err
	}
	return &vc, nil
}

func orderItemSpec(it model.OrderItem) string {
	if it.Spec != nil {
		return *it.Spec
	}
	return ""
}

func genVerifyCodeStr() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("V%s", hex.EncodeToString(b))
}

func strPtr(s string) *string { return &s }
