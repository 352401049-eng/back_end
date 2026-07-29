package service

import (
	"errors"
	"fmt"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
)

var (
	ErrPackageSelectionRequired = errors.New("package selection required")
	ErrPackageSelectionPending  = errors.New("package selection pending")
	ErrPickupNotAllowed         = errors.New("product does not allow pickup")
)

// ConfirmPackageSelection 商家核销套餐后确认选配，并扣减组件库存。
func (s *InventoryService) ConfirmPackageSelection(merchantID, usageID uint64, selections []PackageSelectionInput) (*InventoryUsageView, error) {
	var usage model.UserInventoryUsage
	if err := query.NotDeleted(s.DB).
		Where("id = ? AND merchant_id = ?", usageID, merchantID).
		Preload("Product", "is_deleted = ?", model.NotDeleted).
		First(&usage).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrInventoryUsageNotFound
		}
		return nil, err
	}
	if usage.Status != model.InventoryUsageCompleted {
		return nil, ErrInventoryUsageInvalid
	}
	if usage.PackageSelectStatus != model.PackageSelectPending {
		return nil, ErrInventoryUsageInvalid
	}
	if usage.Product == nil || usage.Product.ItemType != model.ProductItemTypePackage {
		return nil, ErrInventoryUsageInvalid
	}

	lines, err := ResolvePackageSelections(s.DB, usage.ProductID, selections)
	if err != nil {
		return nil, err
	}
	groups, err := (&ProductService{DB: s.DB}).LoadPackageGroups(usage.ProductID)
	if err != nil {
		return nil, err
	}
	snap := buildPackageSelectionSnapshot(groups, selections, lines)

	err = s.DB.Transaction(func(tx *gorm.DB) error {
		for _, ln := range lines {
			qty := ln.Qty * usage.Quantity
			if err := deductProductStockInTx(tx, ln.Product.ID, qty); err != nil {
				return err
			}
		}
		return tx.Model(&usage).Updates(map[string]interface{}{
			"package_selections":    snap,
			"package_select_status": model.PackageSelectDone,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return s.GetUsageView(0, usageID)
}

// ListPendingPackageSelections 商家端：核销后待选配的套餐使用记录。
func (s *InventoryService) ListPendingPackageSelections(merchantID uint64, page, pageSize int) ([]InventoryUsageView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	offset := (page - 1) * pageSize
	q := query.NotDeleted(s.DB.Model(&model.UserInventoryUsage{})).
		Where("merchant_id = ? AND package_select_status = ? AND status = ?",
			merchantID, model.PackageSelectPending, model.InventoryUsageCompleted)
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

func (s *InventoryService) applyPackageSelectionsForDelivery(
	tx *gorm.DB, product model.Product, quantity uint32, selections []PackageSelectionInput,
) (model.PackageSelectionSnapshot, uint8, error) {
	if product.ItemType != model.ProductItemTypePackage {
		return nil, model.PackageSelectNone, nil
	}
	// 允许空 selections：全固定分组套餐由 ResolvePackageSelections 展开固定项
	lines, err := ResolvePackageSelections(tx, product.ID, selections)
	if err != nil {
		return nil, 0, err
	}
	if len(lines) == 0 {
		return nil, 0, ErrPackageSelectionRequired
	}
	groups, err := (&ProductService{DB: tx}).LoadPackageGroups(product.ID)
	if err != nil {
		return nil, 0, err
	}
	for _, ln := range lines {
		qty := ln.Qty * quantity
		if err := deductProductStockInTx(tx, ln.Product.ID, qty); err != nil {
			return nil, 0, err
		}
	}
	snap := buildPackageSelectionSnapshot(groups, selections, lines)
	return snap, model.PackageSelectUserSet, nil
}

func restorePackageComponentStock(tx *gorm.DB, usage *model.UserInventoryUsage) error {
	if usage == nil || len(usage.PackageSelections) == 0 {
		return nil
	}
	if usage.PackageSelectStatus != model.PackageSelectDone && usage.PackageSelectStatus != model.PackageSelectUserSet {
		return nil
	}
	for _, g := range usage.PackageSelections {
		for _, it := range g.Items {
			qty := it.Qty * usage.Quantity
			if qty == 0 {
				continue
			}
			if err := tx.Model(&model.Product{}).Where("id = ?", it.ProductID).
				Update("stock", gorm.Expr("stock + ?", qty)).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func enrichUsageViewPackage(view *InventoryUsageView) {
	if view == nil {
		return
	}
	view.PackageSelectStatusText = model.PackageSelectStatusText(view.PackageSelectStatus)
	view.PackageSelectionText = view.PackageSelections.SummaryText()
	if view.Product != nil && view.Product.ItemType == model.ProductItemTypePackage {
		view.IsPackage = true
	}
}

func validateFulfillmentFlags(product model.Product, deliveryType uint8) error {
	if product.ItemType == model.ProductItemTypeVirtual && deliveryType == model.DeliveryTypeDelivery {
		return ErrVirtualNotDeliverable
	}
	if deliveryType == model.DeliveryTypeDelivery && product.AllowDelivery != 1 {
		return ErrDeliveryNotAllowed
	}
	if deliveryType == model.DeliveryTypePickup && product.AllowPickup != 1 {
		return ErrPickupNotAllowed
	}
	return nil
}

func fmtErrPackage(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, ErrInvalidProductArg) || errors.Is(err, ErrInsufficientStock) {
		return err
	}
	return fmt.Errorf("%w", err)
}
