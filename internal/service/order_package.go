package service

import (
	"fmt"
	"math"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
)

type PackageSelectionItemInput struct {
	ProductID uint64 `json:"product_id"`
	Qty       uint32 `json:"qty"`
}

type PackageSelectionInput struct {
	GroupID uint64                      `json:"group_id"`
	Items   []PackageSelectionItemInput `json:"items"`
}

type CreatePackageOrderInput struct {
	ProductID         uint64
	PackageSelections []PackageSelectionInput
	DeliveryType      uint8
	AddressID         *uint64
	DeliveryLatitude  *float64
	DeliveryLongitude *float64
	Remark            *string
}

type resolvedPackageLine struct {
	Product    model.Product
	MerchantID uint64
	Qty        uint32
	UnitPrice  float64
	LineTotal  float64
}

// CreatePackage 平台套餐下单：父单固定价 + 按店拆子单并分摊金额。
func (s *OrderService) CreatePackage(accountID uint64, input CreatePackageOrderInput) (*OrderView, error) {
	if input.ProductID == 0 {
		return nil, fmt.Errorf("%w: 请指定套餐商品", ErrInvalidProductArg)
	}
	if input.DeliveryType == 0 {
		input.DeliveryType = model.DeliveryTypePickup
	}
	deliveryType, err := normalizeDeliveryType(input.DeliveryType)
	if err != nil {
		return nil, err
	}
	// 一期：套餐统一到店履约入口，子单各自申请使用时再校验配送范围
	if deliveryType == model.DeliveryTypeDelivery {
		return nil, fmt.Errorf("%w: 套餐请先到店自取/稍后选择，各店子单再单独申请配送", ErrInvalidDeliveryType)
	}

	var pkg model.Product
	if err := query.NotDeleted(s.DB).
		Where("id = ? AND merchant_id = 0 AND item_type = ? AND status = ?",
			input.ProductID, model.ProductItemTypePackage, model.ProductStatusOn).
		First(&pkg).Error; err != nil {
		return nil, ErrProductNotFound
	}

	lines, err := s.resolvePackageSelections(pkg.ID, input.PackageSelections)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("%w: 请完成套餐选配", ErrInvalidProductArg)
	}

	payAmount := roundMoney(pkg.Price)
	byMerchant := groupPackageLinesByMerchant(lines)
	merchantPays := splitPackagePayByMerchant(payAmount, byMerchant)

	now := time.Now()
	var parent model.Order
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		pkgID := pkg.ID
		parent = model.Order{
			OrderNo:             genOrderNo(),
			AccountID:           accountID,
			MerchantID:          0,
			PackageProductID:    &pkgID,
			Status:              model.OrderStatusPendingFulfill,
			MerchantReviewStage: model.MerchantReviewNone,
			DeliveryType:        model.DeliveryTypePickup,
			TotalAmount:         payAmount,
			DiscountAmount:      0,
			PayAmount:           payAmount,
			PayStatus:           model.PayStatusPaid,
			PaidAt:              &now,
			Remark:              input.Remark,
		}
		if err := tx.Create(&parent).Error; err != nil {
			return err
		}
		parentItem := model.OrderItem{
			OrderID: parent.ID, ProductID: pkg.ID, PurchaseType: model.PurchaseTypeSolo,
			ProductName: pkg.Name, ProductImage: &pkg.CoverURL,
			UnitPrice: payAmount, Quantity: 1, Subtotal: payAmount,
		}
		if err := tx.Create(&parentItem).Error; err != nil {
			return err
		}

		for merchantID, mLines := range byMerchant {
			childPay := merchantPays[merchantID]
			childTotal := 0.0
			for _, ln := range mLines {
				childTotal += ln.LineTotal
			}
			childTotal = roundMoney(childTotal)
			parentID := parent.ID
			child := model.Order{
				ParentOrderID:       &parentID,
				OrderNo:             genOrderNo(),
				AccountID:           accountID,
				MerchantID:          merchantID,
				PackageProductID:    &pkgID,
				Status:              model.OrderStatusPendingFulfill,
				MerchantReviewStage: model.MerchantReviewPending,
				DeliveryType:        model.DeliveryTypePickup,
				TotalAmount:         childTotal,
				DiscountAmount:      0,
				PayAmount:           childPay,
				PayStatus:           model.PayStatusPaid,
				PaidAt:              &now,
				Remark:              input.Remark,
			}
			if err := tx.Create(&child).Error; err != nil {
				return err
			}
			for _, ln := range mLines {
				item := model.OrderItem{
					OrderID: child.ID, ProductID: ln.Product.ID, PurchaseType: model.PurchaseTypeSolo,
					ProductName: ln.Product.Name, ProductImage: &ln.Product.CoverURL,
					UnitPrice: ln.UnitPrice, Quantity: ln.Qty, Subtotal: roundMoney(ln.LineTotal),
				}
				if err := tx.Create(&item).Error; err != nil {
					return err
				}
				if err := deductProductStockInTx(tx, ln.Product.ID, ln.Qty); err != nil {
					return err
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetView(accountID, parent.ID, nil)
}

func (s *OrderService) resolvePackageSelections(packageProductID uint64, selections []PackageSelectionInput) ([]resolvedPackageLine, error) {
	groups, err := (&ProductService{DB: s.DB}).LoadPackageGroups(packageProductID)
	if err != nil {
		return nil, err
	}
	if len(groups) == 0 {
		return nil, fmt.Errorf("%w: 套餐未配置分组", ErrInvalidProductArg)
	}
	selByGroup := map[uint64][]PackageSelectionItemInput{}
	for _, sel := range selections {
		selByGroup[sel.GroupID] = append(selByGroup[sel.GroupID], sel.Items...)
	}

	var out []resolvedPackageLine
	for _, g := range groups {
		items := selByGroup[g.ID]
		cand := map[uint64]PackageItemView{}
		for _, it := range g.Items {
			cand[it.ProductID] = it
		}
		var totalQty uint32
		seen := map[uint64]struct{}{}
		for _, it := range items {
			if it.Qty == 0 {
				continue
			}
			c, ok := cand[it.ProductID]
			if !ok {
				return nil, fmt.Errorf("%w: 分组「%s」不含商品 %d", ErrInvalidProductArg, g.Name, it.ProductID)
			}
			if _, dup := seen[it.ProductID]; dup {
				return nil, fmt.Errorf("%w: 分组「%s」商品重复选择", ErrInvalidProductArg, g.Name)
			}
			seen[it.ProductID] = struct{}{}
			if it.Qty > c.MaxQty {
				return nil, fmt.Errorf("%w: 「%s」最多选 %d 份", ErrInvalidProductArg, c.Name, c.MaxQty)
			}
			if c.Status != model.ProductStatusOn {
				return nil, fmt.Errorf("%w: 「%s」已下架", ErrInvalidProductArg, c.Name)
			}
			if c.ItemType == model.ProductItemTypePackage {
				return nil, fmt.Errorf("%w: 套餐不可嵌套", ErrInvalidProductArg)
			}
			if c.Stock < it.Qty {
				return nil, ErrInsufficientStock
			}
			totalQty += it.Qty
			out = append(out, resolvedPackageLine{
				Product: model.Product{
					ID: c.ProductID, MerchantID: c.MerchantID, Name: c.Name,
					CoverURL: c.CoverURL, Price: c.Price, Stock: c.Stock, Status: c.Status, ItemType: c.ItemType,
				},
				MerchantID: c.MerchantID,
				Qty:        it.Qty,
				UnitPrice:  c.Price,
				LineTotal:  c.Price * float64(it.Qty),
			})
		}
		if totalQty != g.SelectCount {
			return nil, fmt.Errorf("%w: 分组「%s」须选 %d 份，当前 %d", ErrInvalidProductArg, g.Name, g.SelectCount, totalQty)
		}
	}
	// 多余 group_id 忽略；缺组已由 totalQty 校验覆盖（未选则 totalQty=0）
	return out, nil
}

func groupPackageLinesByMerchant(lines []resolvedPackageLine) map[uint64][]resolvedPackageLine {
	out := map[uint64][]resolvedPackageLine{}
	for _, ln := range lines {
		out[ln.MerchantID] = append(out[ln.MerchantID], ln)
	}
	return out
}

// splitPackagePayByMerchant 按所选子商品原价占比分摊固定套餐价，末店吃误差保证总和相等。
func splitPackagePayByMerchant(packagePay float64, byMerchant map[uint64][]resolvedPackageLine) map[uint64]float64 {
	type midTotal struct {
		id    uint64
		total float64
	}
	var ordered []midTotal
	sum := 0.0
	for mid, lines := range byMerchant {
		t := 0.0
		for _, ln := range lines {
			t += ln.LineTotal
		}
		ordered = append(ordered, midTotal{id: mid, total: t})
		sum += t
	}
	out := map[uint64]float64{}
	if len(ordered) == 0 {
		return out
	}
	if sum <= 0 {
		each := roundMoney(packagePay / float64(len(ordered)))
		allocated := 0.0
		for i, m := range ordered {
			if i == len(ordered)-1 {
				out[m.id] = roundMoney(packagePay - allocated)
			} else {
				out[m.id] = each
				allocated = roundMoney(allocated + each)
			}
		}
		return out
	}
	allocated := 0.0
	for i, m := range ordered {
		if i == len(ordered)-1 {
			out[m.id] = roundMoney(packagePay - allocated)
			continue
		}
		share := roundMoney(packagePay * (m.total / sum))
		out[m.id] = share
		allocated = roundMoney(allocated + share)
	}
	// 避免负分摊
	for mid, v := range out {
		if v < 0 || math.IsNaN(v) {
			out[mid] = 0
		}
	}
	return out
}

func cancelPackageChildrenInTx(tx *gorm.DB, parentID uint64, inventorySvc *InventoryService, couponSvc *CouponService) error {
	var children []model.Order
	if err := query.NotDeleted(tx).Where("parent_order_id = ?", parentID).Find(&children).Error; err != nil {
		return err
	}
	for i := range children {
		child := &children[i]
		if child.Status == model.OrderStatusCancelled {
			continue
		}
		if couponSvc != nil {
			if err := couponSvc.ReleaseByOrderInTx(tx, child); err != nil {
				return err
			}
		}
		if inventorySvc != nil {
			if err := inventorySvc.RollbackOrderCredit(tx, child.ID); err != nil {
				return err
			}
		}
		if err := restoreProductStockForOrder(tx, child.ID); err != nil {
			return err
		}
		if err := tx.Model(child).Update("status", model.OrderStatusCancelled).Error; err != nil {
			return err
		}
	}
	return nil
}
