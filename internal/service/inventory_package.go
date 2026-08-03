package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
)

var (
	ErrPackageSelectionRequired = errors.New("package selection required")
	ErrPackageSelectionPending  = errors.New("package selection pending")
	ErrPickupNotAllowed         = errors.New("product does not allow pickup")
)

// PackageUnitInput 一份套餐的选配（核销时 quantity>1 需多份）。
type PackageUnitInput struct {
	PackageSelections []PackageSelectionInput `json:"package_selections"`
}

// validatePackageUnitsStock 汇总多份套餐组件需求并校验通道库存。
func validatePackageUnitsStock(db *gorm.DB, packageProductID uint64, units [][]PackageSelectionInput, channel string) error {
	need := map[uint64]uint32{}
	for _, sels := range units {
		lines, err := ResolvePackageSelections(db, packageProductID, sels)
		if err != nil {
			return err
		}
		if len(lines) == 0 {
			return ErrPackageSelectionRequired
		}
		for _, ln := range lines {
			need[ln.Product.ID] += ln.Qty
		}
	}
	if len(need) == 0 {
		return nil
	}
	ids := make([]uint64, 0, len(need))
	for id := range need {
		ids = append(ids, id)
	}
	var products []model.Product
	if err := query.NotDeleted(db).Select("id", "deal_stock", "group_stock", "takeout_stock").Where("id IN ?", ids).Find(&products).Error; err != nil {
		return err
	}
	stockByID := make(map[uint64]uint32, len(products))
	for _, p := range products {
		stockByID[p.ID] = productChannelStock(p, channel)
	}
	for id, qty := range need {
		if stockByID[id] < qty {
			return ErrInsufficientStock
		}
	}
	return nil
}

func normalizePackageUnits(units []PackageUnitInput, single []PackageSelectionInput, quantity uint32) ([][]PackageSelectionInput, error) {
	if quantity == 0 {
		quantity = 1
	}
	out := make([][]PackageSelectionInput, 0, quantity)
	if len(units) > 0 {
		if uint32(len(units)) != quantity {
			return nil, fmt.Errorf("%w: 需为全部 %d 份套餐完成选配", ErrPackageSelectionRequired, quantity)
		}
		for _, u := range units {
			out = append(out, u.PackageSelections)
		}
		return out, nil
	}
	if single == nil {
		single = []PackageSelectionInput{}
	}
	// 兼容旧客户端：只传一份选配时，复制到每一份
	for i := uint32(0); i < quantity; i++ {
		out = append(out, single)
	}
	return out, nil
}

func applyPackageUnitsInTx(tx *gorm.DB, productID uint64, quantity uint32, units [][]PackageSelectionInput) (model.PackageSelectionSnapshot, error) {
	if uint32(len(units)) != quantity {
		return nil, fmt.Errorf("%w: 需为全部 %d 份套餐完成选配", ErrPackageSelectionRequired, quantity)
	}
	groups, err := (&ProductService{DB: tx}).LoadPackageGroups(productID)
	if err != nil {
		return nil, err
	}
	merged := make(model.PackageSelectionSnapshot, 0)
	for i, sels := range units {
		lines, err := ResolvePackageSelections(tx, productID, sels)
		if err != nil {
			return nil, err
		}
		if len(lines) == 0 {
			return nil, ErrPackageSelectionRequired
		}
		for _, ln := range lines {
			if err := deductChannelStockInTx(tx, ln.Product.ID, ln.Qty, productChannelDeal); err != nil {
				return nil, err
			}
		}
		snap := buildPackageSelectionSnapshot(groups, sels, lines)
		if quantity > 1 {
			for j := range snap {
				prefix := fmt.Sprintf("第%d份", i+1)
				if snap[j].GroupName != "" {
					snap[j].GroupName = prefix + "·" + snap[j].GroupName
				} else {
					snap[j].GroupName = prefix
				}
			}
		}
		merged = append(merged, snap...)
	}
	return merged, nil
}

// ConfirmPackageSelection 商家核销套餐后确认选配，并扣减组件库存。
// packageUnits 优先；为空时回退 packageSelections（并按 quantity 复制）。
func (s *InventoryService) ConfirmPackageSelection(
	merchantID, usageID uint64,
	selections []PackageSelectionInput,
	packageUnits []PackageUnitInput,
) (*InventoryUsageView, error) {
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

	units, err := normalizePackageUnits(packageUnits, selections, usage.Quantity)
	if err != nil {
		return nil, err
	}

	err = s.DB.Transaction(func(tx *gorm.DB) error {
		snap, err := applyPackageUnitsInTx(tx, usage.ProductID, usage.Quantity, units)
		if err != nil {
			return err
		}
		raw, err := json.Marshal(snap)
		if err != nil {
			return err
		}
		return query.NotDeleted(tx.Model(&model.UserInventoryUsage{})).
			Where("id = ?", usageID).
			Updates(map[string]interface{}{
				"package_selections":    json.RawMessage(raw),
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
		if err := deductChannelStockInTx(tx, ln.Product.ID, qty, productChannelDeal); err != nil {
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
	expanded := packageSnapExpanded(usage.PackageSelections)
	for _, g := range usage.PackageSelections {
		for _, it := range g.Items {
			rollbackQty := it.Qty
			// 外卖用户选配 / 旧版商家一次选配：快照为单份结构，扣减时乘过 quantity
			if !expanded && usage.PackageSelectStatus != model.PackageSelectDone {
				rollbackQty = it.Qty * usage.Quantity
			} else if !expanded && usage.Quantity > 1 {
				rollbackQty = it.Qty * usage.Quantity
			}
			if rollbackQty == 0 {
				continue
			}
			if err := restoreChannelStockInTx(tx, it.ProductID, rollbackQty, productChannelDeal); err != nil {
				return err
			}
		}
	}
	return nil
}

func packageSnapExpanded(snap model.PackageSelectionSnapshot) bool {
	for _, g := range snap {
		if strings.HasPrefix(g.GroupName, "第") && strings.Contains(g.GroupName, "份") {
			return true
		}
	}
	return false
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

func enrichUsageViewOptions(db *gorm.DB, view *InventoryUsageView) {
	if view == nil {
		return
	}
	view.OptionSelectionText = view.OptionSelections.SummaryText()
	if view.Product == nil {
		return
	}
	var needs bool
	var err error
	if view.Product.ItemType == model.ProductItemTypePackage {
		needs, err = packageNeedsChildOptions(db, view.ProductID, nil)
	} else {
		needs, err = ProductNeedsOptions(db, view.ProductID)
	}
	if err == nil {
		view.HasOptions = needs
	}
}

// applyOptionSelectionsForUsage 配送时校验并落库规格；自取待核销时标记待选。
func applyOptionSelectionsForUsage(
	tx *gorm.DB,
	product model.Product,
	deliveryType uint8,
	quantity uint32,
	packageSelections []PackageSelectionInput,
	optionSelections []OptionSelectionUnitInput,
) (model.OptionSelectionSnapshot, uint8, error) {
	if product.ItemType == model.ProductItemTypePackage {
		if deliveryType != model.DeliveryTypeDelivery {
			needs, err := packageNeedsChildOptions(tx, product.ID, nil)
			if err != nil {
				return nil, 0, err
			}
			if !needs {
				return nil, model.OptionSelectNone, nil
			}
			return nil, model.OptionSelectPending, nil
		}
		units, err := normalizePackageUnits(nil, packageSelections, quantity)
		if err != nil {
			return nil, 0, err
		}
		needs, err := packageNeedsChildOptions(tx, product.ID, units)
		if err != nil {
			return nil, 0, err
		}
		if !needs {
			return nil, model.OptionSelectNone, nil
		}
		snap, err := validateOptionsForPackageUnits(tx, product.ID, units, optionSelections)
		if err != nil {
			return nil, 0, err
		}
		return snap, model.OptionSelectDone, nil
	}

	needs, err := ProductNeedsOptions(tx, product.ID)
	if err != nil {
		return nil, 0, err
	}
	if !needs {
		return nil, model.OptionSelectNone, nil
	}
	if deliveryType == model.DeliveryTypePickup {
		return nil, model.OptionSelectPending, nil
	}
	snap, err := ValidateAndBuildOptionSnapshot(tx, product.ID, quantity, optionSelections)
	if err != nil {
		return nil, 0, err
	}
	return snap, model.OptionSelectDone, nil
}

// applyOptionSelectionsForVerify 核销完成时校验规格（自取或套餐子项）。
func applyOptionSelectionsForVerify(
	tx *gorm.DB,
	product *model.Product,
	productID uint64,
	quantity uint32,
	packageUnits [][]PackageSelectionInput,
	optionSelections []OptionSelectionUnitInput,
) (model.OptionSelectionSnapshot, uint8, error) {
	if product != nil && product.ItemType == model.ProductItemTypePackage {
		needs, err := packageNeedsChildOptions(tx, productID, packageUnits)
		if err != nil {
			return nil, 0, err
		}
		if !needs {
			return nil, model.OptionSelectNone, nil
		}
		snap, err := validateOptionsForPackageUnits(tx, productID, packageUnits, optionSelections)
		if err != nil {
			return nil, 0, err
		}
		return snap, model.OptionSelectDone, nil
	}

	needs, err := ProductNeedsOptions(tx, productID)
	if err != nil {
		return nil, 0, err
	}
	if !needs {
		return nil, model.OptionSelectNone, nil
	}
	snap, err := ValidateAndBuildOptionSnapshot(tx, productID, quantity, optionSelections)
	if err != nil {
		return nil, 0, err
	}
	return snap, model.OptionSelectDone, nil
}

func packageNeedsChildOptions(tx *gorm.DB, packageProductID uint64, units [][]PackageSelectionInput) (bool, error) {
	if len(units) == 0 {
		groups, err := (&ProductService{DB: tx}).LoadPackageGroups(packageProductID)
		if err != nil {
			return false, err
		}
		for _, g := range groups {
			for _, it := range g.Items {
				needs, err := ProductNeedsOptions(tx, it.ProductID)
				if err != nil {
					return false, err
				}
				if needs {
					return true, nil
				}
			}
		}
		return false, nil
	}
	for _, sels := range units {
		lines, err := ResolvePackageSelections(tx, packageProductID, sels)
		if err != nil {
			return false, err
		}
		for _, ln := range lines {
			needs, err := ProductNeedsOptions(tx, ln.Product.ID)
			if err != nil {
				return false, err
			}
			if needs {
				return true, nil
			}
		}
	}
	return false, nil
}

// validateOptionsForPackageUnits 套餐子商品规格：按子商品汇总份数并合并快照。
func validateOptionsForPackageUnits(
	tx *gorm.DB,
	packageProductID uint64,
	units [][]PackageSelectionInput,
	optionSelections []OptionSelectionUnitInput,
) (model.OptionSelectionSnapshot, error) {
	needCount := map[uint64]uint32{}
	for _, sels := range units {
		lines, err := ResolvePackageSelections(tx, packageProductID, sels)
		if err != nil {
			return nil, err
		}
		for _, ln := range lines {
			needs, err := ProductNeedsOptions(tx, ln.Product.ID)
			if err != nil {
				return nil, err
			}
			if !needs {
				continue
			}
			needCount[ln.Product.ID] += ln.Qty
		}
	}
	if len(needCount) == 0 {
		return nil, nil
	}
	if len(needCount) > 1 {
		for _, sel := range optionSelections {
			if sel.ProductID == 0 {
				return nil, fmt.Errorf("%w: 套餐多子商品须指定 product_id", ErrOptionInvalid)
			}
		}
	}

	merged := make(model.OptionSelectionSnapshot, 0)
	for productID, qty := range needCount {
		filtered := filterOptionSelectionsForProduct(optionSelections, productID, len(needCount))
		if qty > 0 && len(filtered) == 0 {
			return nil, ErrOptionRequired
		}
		snap, err := ValidateAndBuildOptionSnapshot(tx, productID, qty, filtered)
		if err != nil {
			return nil, err
		}
		merged = append(merged, snap...)
	}
	return merged, nil
}

func filterOptionSelectionsForProduct(
	selections []OptionSelectionUnitInput,
	productID uint64,
	childCount int,
) []OptionSelectionUnitInput {
	out := make([]OptionSelectionUnitInput, 0, len(selections))
	for _, sel := range selections {
		if sel.ProductID != 0 && sel.ProductID != productID {
			continue
		}
		if sel.ProductID == 0 && childCount > 1 {
			continue
		}
		out = append(out, sel)
	}
	return out
}

func validateFulfillmentFlags(product model.Product, deliveryType uint8) error {
	// 虚拟商品仅支持到店核销（delivery_type=pickup 生成核销码），禁止骑手配送；
	// 不校验 allow_pickup（虚拟商品后台常关自取/配送开关）。
	if product.ItemType == model.ProductItemTypeVirtual {
		if deliveryType == model.DeliveryTypeDelivery {
			return ErrVirtualNotDeliverable
		}
		return nil
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
