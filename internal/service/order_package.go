package service

import (
	"fmt"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
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
	MerchantID        uint64
	PackageSelections []PackageSelectionInput
	PurchaseType      uint8
	GroupBuyID        *uint64
	GroupBuyTeamID    *uint64
	ActivityProductID *uint64
	DeliveryType      uint8
	AddressID         *uint64
	DeliveryLatitude  *float64
	DeliveryLongitude *float64
	Remark            *string
	UserCouponID      *uint64
}

type resolvedPackageLine struct {
	Product   model.Product
	Qty       uint32
	UnitPrice float64
	LineTotal float64
}

func (s *OrderService) IsProductPackage(productID uint64) bool {
	var p model.Product
	if err := query.NotDeleted(s.DB).Select("id", "item_type").First(&p, productID).Error; err != nil {
		return false
	}
	return p.ItemType == model.ProductItemTypePackage
}

func (s *OrderService) IsActivityProductPackage(activityProductID uint64) bool {
	var ap model.ActivityProduct
	if err := query.NotDeleted(s.DB).Select("id", "product_id").First(&ap, activityProductID).Error; err != nil {
		return false
	}
	return s.IsProductPackage(ap.ProductID)
}

// CreatePackage 店内套餐下单：一店一单，实付套餐价（或活动/拼团价）。
func (s *OrderService) CreatePackage(accountID uint64, input CreatePackageOrderInput) (*OrderView, error) {
	if input.ProductID == 0 && (input.ActivityProductID == nil || *input.ActivityProductID == 0) {
		return nil, fmt.Errorf("%w: 请指定套餐商品", ErrInvalidProductArg)
	}
	if input.PurchaseType == 0 {
		input.PurchaseType = model.PurchaseTypeSolo
	}
	if input.DeliveryType == 0 {
		input.DeliveryType = model.DeliveryTypePickup
	}
	deliveryType, err := normalizeDeliveryType(input.DeliveryType)
	if err != nil {
		return nil, err
	}
	if deliveryType == model.DeliveryTypeDelivery && input.AddressID == nil {
		return nil, ErrAddressRequired
	}

	var pkg model.Product
	var unitPrice float64
	var actCtx *ActivityOrderContext
	var activityID *uint64
	var activityProductID *uint64
	var actGB *ActivityGroupBuyConfig

	if input.ActivityProductID != nil && s.ActivitySvc != nil {
		merchantHint := input.MerchantID
		if merchantHint == 0 {
			// 先解析活动商品拿到店
			var ap model.ActivityProduct
			if err := query.NotDeleted(s.DB).Select("id", "product_id").First(&ap, *input.ActivityProductID).Error; err != nil {
				return nil, ErrActivityProductNotFound
			}
			var p model.Product
			if err := query.NotDeleted(s.DB).Select("id", "merchant_id").First(&p, ap.ProductID).Error; err != nil {
				return nil, ErrProductNotFound
			}
			merchantHint = p.MerchantID
		}
		ctx, err := s.ActivitySvc.ResolveForOrder(accountID, *input.ActivityProductID, merchantHint, 1, input.PurchaseType)
		if err != nil {
			return nil, err
		}
		if ctx.Product.ItemType != model.ProductItemTypePackage {
			return nil, fmt.Errorf("%w: 活动商品不是套餐", ErrInvalidProductArg)
		}
		actCtx = ctx
		pkg = ctx.Product
		unitPrice = ctx.UnitPrice
		activityID = &ctx.Activity.ID
		activityProductID = &ctx.ActivityProduct.ID
		actGB = ctx.GroupBuyConfig
		input.MerchantID = pkg.MerchantID
		input.ProductID = pkg.ID
		if !ctx.EnableCoupon && input.UserCouponID != nil {
			return nil, ErrCouponNotApplicable
		}
	} else {
		if input.MerchantID == 0 {
			return nil, fmt.Errorf("%w: 请指定 merchant_id", ErrInvalidProductArg)
		}
		if err := query.NotDeleted(s.DB).
			Where("id = ? AND merchant_id = ? AND item_type = ? AND status = ?",
				input.ProductID, input.MerchantID, model.ProductItemTypePackage, model.ProductStatusOn).
			First(&pkg).Error; err != nil {
			return nil, ErrProductNotFound
		}
		unitPrice = pkg.Price
		if input.PurchaseType == model.PurchaseTypeGroup {
			if pkg.EnableGroupBuy != 1 || pkg.GroupBuyPrice == nil {
				return nil, ErrGroupBuyInvalid
			}
			unitPrice = *pkg.GroupBuyPrice
		} else {
			input.GroupBuyTeamID = nil
		}
	}

	if input.PurchaseType == model.PurchaseTypeGroup {
		if actGB == nil {
			if pkg.EnableGroupBuy != 1 || pkg.GroupBuyPrice == nil {
				return nil, ErrGroupBuyInvalid
			}
		} else if actGB.EnableGroupBuy != 1 {
			return nil, ErrGroupBuyInvalid
		}
	} else {
		input.GroupBuyTeamID = nil
	}

	coordIn := DeliveryCoordinateInput{
		AddressID: input.AddressID, DeliveryLatitude: input.DeliveryLatitude, DeliveryLongitude: input.DeliveryLongitude,
	}
	if s.ZoneSvc != nil {
		if err := s.ZoneSvc.ValidateDelivery(accountID, pkg.MerchantID, deliveryType, coordIn); err != nil {
			return nil, err
		}
	}

	lines, err := s.resolvePackageSelections(pkg.ID, input.PackageSelections)
	if err != nil {
		return nil, err
	}
	if len(lines) == 0 {
		return nil, fmt.Errorf("%w: 请完成套餐选配", ErrInvalidProductArg)
	}
	// 套餐本体库存：与活动库存取交后仍须扣减 package 商品自身 stock
	if pkg.Stock < 1 {
		return nil, ErrInsufficientStock
	}

	payAmount := roundMoney(unitPrice)
	discountAmount := 0.0
	couponCtx := OrderCouponContext{
		AccountID: accountID, MerchantID: pkg.MerchantID, Product: pkg,
		Subtotal: payAmount, PurchaseType: input.PurchaseType,
	}
	if input.UserCouponID != nil {
		if s.CouponSvc == nil {
			return nil, ErrCouponNotApplicable
		}
		d, err := s.CouponSvc.EvaluateForOrder(*input.UserCouponID, couponCtx)
		if err != nil {
			return nil, err
		}
		discountAmount = d
		payAmount = roundMoney(payAmount - discountAmount)
	}

	now := time.Now()
	var groupBuyID *uint64
	var groupBuyTeamID *uint64
	var gb model.GroupBuy
	if input.PurchaseType == model.PurchaseTypeGroup {
		ensured, err := s.ensureActiveGroupBuy(pkg, actGB)
		if err != nil {
			return nil, err
		}
		gb = *ensured
		resolved, err := resolveClientGroupBuy(s.DB, pkg.ID, gb, input.GroupBuyID)
		if err != nil {
			return nil, err
		}
		gb = resolved
		groupBuyID = &gb.ID
	}

	var order model.Order
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		if s.ActivitySvc != nil && activityProductID != nil && actCtx != nil && actCtx.ActivityProduct != nil {
			var apLock model.ActivityProduct
			if err := query.NotDeleted(tx).Clauses(clause.Locking{Strength: "UPDATE"}).
				First(&apLock, *activityProductID).Error; err != nil {
				return err
			}
			if err := s.ActivitySvc.checkUserLimits(tx, accountID, actCtx.ActivityProduct, 1); err != nil {
				return err
			}
		}

		status := model.OrderStatusPendingFulfill
		reviewStage := model.MerchantReviewPending
		if input.PurchaseType == model.PurchaseTypeGroup {
			status = model.OrderStatusPendingGroup
			reviewStage = model.MerchantReviewNone
		}

		var addrSnap *model.AddressSnapshot
		if deliveryType == model.DeliveryTypeDelivery && input.AddressID != nil {
			var addr model.UserAddress
			if err := query.NotDeleted(tx).Where("id = ? AND account_id = ?", *input.AddressID, accountID).First(&addr).Error; err != nil {
				return ErrAddressRequired
			}
			addrSnap = AddressSnapshotFromUserAddress(&addr)
		}

		pkgID := pkg.ID
		order = model.Order{
			OrderNo:             genOrderNo(),
			AccountID:           accountID,
			MerchantID:          pkg.MerchantID,
			PackageProductID:    &pkgID,
			ActivityID:          activityID,
			Status:              status,
			MerchantReviewStage: reviewStage,
			DeliveryType:        deliveryType,
			AddressSnapshot:     addrSnap,
			TotalAmount:         roundMoney(unitPrice),
			DiscountAmount:      discountAmount,
			UserCouponID:        input.UserCouponID,
			PayAmount:           payAmount,
			PayStatus:           model.PayStatusPaid,
			PaidAt:              &now,
			Remark:              input.Remark,
		}
		if err := tx.Create(&order).Error; err != nil {
			return err
		}

		if input.UserCouponID != nil && s.CouponSvc != nil {
			if _, err := s.CouponSvc.ApplyForOrderInTx(tx, *input.UserCouponID, order.ID, couponCtx); err != nil {
				return err
			}
		}

		if input.PurchaseType == model.PurchaseTypeGroup {
			teamID, err := s.joinOrCreateTeam(tx, accountID, order.ID, pkg, gb, input.GroupBuyTeamID, actGB, activityID)
			if err != nil {
				return err
			}
			groupBuyTeamID = &teamID
		}

		// 套餐头行：qty=1、挂活动/拼团；组件行不含 activity_product_id，避免限购/回滚按组件份数放大
		pkgCover := pkg.CoverURL
		pkgItem := model.OrderItem{
			OrderID: order.ID, ProductID: pkg.ID,
			ActivityID: activityID, ActivityProductID: activityProductID,
			PurchaseType: input.PurchaseType,
			GroupBuyID: groupBuyID, GroupBuyTeamID: groupBuyTeamID,
			ProductName: pkg.Name, ProductImage: &pkgCover,
			UnitPrice: unitPrice, Quantity: 1, Subtotal: roundMoney(unitPrice),
		}
		if err := tx.Create(&pkgItem).Error; err != nil {
			return err
		}
		if err := deductProductStockInTx(tx, pkg.ID, 1); err != nil {
			return err
		}

		for _, ln := range lines {
			item := model.OrderItem{
				OrderID: order.ID, ProductID: ln.Product.ID,
				PurchaseType: input.PurchaseType,
				ProductName:  ln.Product.Name, ProductImage: &ln.Product.CoverURL,
				UnitPrice: ln.UnitPrice, Quantity: ln.Qty, Subtotal: roundMoney(ln.LineTotal),
			}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
			if err := deductProductStockInTx(tx, ln.Product.ID, ln.Qty); err != nil {
				return err
			}
		}

		if s.ActivitySvc != nil && activityProductID != nil {
			if err := s.ActivitySvc.CreditSoldInTx(tx, *activityProductID, 1); err != nil {
				return err
			}
		}

		return s.tryCompleteGroup(tx, groupBuyTeamID, pkg, actGB)
	})
	if err != nil {
		return nil, err
	}
	return s.GetView(accountID, order.ID, nil)
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
		cand := map[uint64]PackageItemView{}
		for _, it := range g.Items {
			cand[it.ProductID] = it
		}

		if g.GroupType == model.PackageGroupTypeFixed {
			for _, c := range g.Items {
				qty := c.MaxQty
				if qty == 0 {
					qty = 1
				}
				if c.Status != model.ProductStatusOn {
					return nil, fmt.Errorf("%w: 「%s」已下架", ErrInvalidProductArg, c.Name)
				}
				if c.Stock < qty {
					return nil, ErrInsufficientStock
				}
				out = append(out, resolvedPackageLine{
					Product: model.Product{
						ID: c.ProductID, MerchantID: c.MerchantID, Name: c.Name,
						CoverURL: c.CoverURL, Price: c.Price, Stock: c.Stock, Status: c.Status, ItemType: c.ItemType,
					},
					Qty: qty, UnitPrice: c.Price, LineTotal: c.Price * float64(qty),
				})
			}
			continue
		}

		items := selByGroup[g.ID]
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
				Qty: it.Qty, UnitPrice: c.Price, LineTotal: c.Price * float64(it.Qty),
			})
		}
		need := g.SelectCount
		if need == 0 {
			need = 1
		}
		if totalQty != need {
			return nil, fmt.Errorf("%w: %s须选 %d 份，当前 %d", ErrInvalidProductArg, g.Label, need, totalQty)
		}
	}
	return out, nil
}

// cancelPackageChildrenInTx 兼容历史跨店父单：级联取消子单。
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
