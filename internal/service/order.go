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
	"gorm.io/gorm/clause"
)

var (
	ErrOrderNotFound      = errors.New("order not found")
	ErrOrderForbidden     = errors.New("order forbidden")
	ErrOrderStatusInvalid = errors.New("order status invalid")
	ErrInsufficientStock  = errors.New("insufficient stock")
	ErrGroupBuyInvalid       = errors.New("group buy invalid")
	ErrGroupBuyAlreadyJoined = errors.New("group buy already joined")
	ErrAddressRequired    = errors.New("address required")
	ErrInvalidDeliveryType = errors.New("invalid delivery type")
)

type OrderService struct {
	DB           *gorm.DB
	InventorySvc *InventoryService
	CouponSvc    *CouponService
	ActivitySvc  *ActivityService
	ZoneSvc      *DeliveryZoneService
}

type CreateOrderInput struct {
	ProductID         uint64
	MerchantID        uint64
	Quantity          uint32
	PurchaseType      uint8
	GroupBuyID        *uint64
	GroupBuyTeamID    *uint64
	ActivityProductID *uint64
	DeliveryType      uint8
	AddressID         *uint64
	DeliveryLatitude  *float64
	DeliveryLongitude *float64
	Remark            *string
	CartItemID        *uint64
	UserCouponID      *uint64
}

type RequestUseInput struct {
	DeliveryType      uint8
	AddressID         *uint64
	DeliveryLatitude  *float64
	DeliveryLongitude *float64
	Remark            *string
}

type BuyerBrief struct {
	AccountID uint64  `json:"account_id"`
	Nickname  *string `json:"nickname,omitempty"`
	Phone     *string `json:"phone,omitempty"`
}

type OrderView struct {
	model.Order
	StatusText       string            `json:"status_text"`
	StatusCode       string            `json:"status_code"`
	VerifyCode       *string           `json:"verify_code,omitempty"`
	Buyer            *BuyerBrief       `json:"buyer,omitempty"`
	GroupBuyProgress *GroupBuyProgress `json:"group_buy_progress,omitempty"`
}

type GroupBuyProgress struct {
	TeamID          uint64  `json:"team_id"`
	TargetCount     uint32  `json:"target_count"`
	CurrentCount    uint32  `json:"current_count"`
	RemainingCount  uint32  `json:"remaining_count"`
	Status          uint8   `json:"status"`
	StatusText      string  `json:"status_text"`
	ExpireAt        string  `json:"expire_at"`
	GroupPrice      float64 `json:"group_price"`
	AllowRepeatJoin uint8   `json:"allow_repeat_join"`
	UserJoined      bool    `json:"user_joined"`
	UserJoinCount   uint32  `json:"user_join_count"`
	IsLeader        bool    `json:"is_leader"`
}

func (s *OrderService) Create(accountID uint64, input CreateOrderInput) (*OrderView, error) {
	if input.Quantity == 0 {
		input.Quantity = 1
	}
	if input.PurchaseType == 0 {
		input.PurchaseType = model.PurchaseTypeSolo
	}
	if input.DeliveryType == 0 {
		input.DeliveryType = model.DeliveryTypePickup
	}
	if _, err := normalizeDeliveryType(input.DeliveryType); err != nil {
		return nil, err
	}
	if input.DeliveryType == model.DeliveryTypeDelivery && input.AddressID == nil {
		return nil, ErrAddressRequired
	}
	coordIn := DeliveryCoordinateInput{
		AddressID: input.AddressID, DeliveryLatitude: input.DeliveryLatitude, DeliveryLongitude: input.DeliveryLongitude,
	}
	if s.ZoneSvc != nil {
		if err := s.ZoneSvc.ValidateDelivery(accountID, input.MerchantID, input.DeliveryType, coordIn); err != nil {
			return nil, err
		}
	}

	var product model.Product
	var unitPrice float64
	var actCtx *ActivityOrderContext
	var activityID *uint64
	var activityProductID *uint64
	var actGB *ActivityGroupBuyConfig

	if input.ActivityProductID != nil && s.ActivitySvc != nil {
		ctx, err := s.ActivitySvc.ResolveForOrder(accountID, *input.ActivityProductID, input.MerchantID, input.Quantity, input.PurchaseType)
		if err != nil {
			return nil, err
		}
		actCtx = ctx
		product = ctx.Product
		unitPrice = ctx.UnitPrice
		activityID = &ctx.Activity.ID
		activityProductID = &ctx.ActivityProduct.ID
		actGB = ctx.GroupBuyConfig
		input.ProductID = product.ID
		if !ctx.EnableCoupon {
			if input.UserCouponID != nil {
				return nil, ErrCouponNotApplicable
			}
		}
	} else {
		if err := query.NotDeleted(s.DB).
			Where("id = ? AND merchant_id = ? AND status = ?", input.ProductID, input.MerchantID, model.ProductStatusOn).
			First(&product).Error; err != nil {
			return nil, ErrProductNotFound
		}
		if product.ItemType == model.ProductItemTypePackage {
			return nil, fmt.Errorf("%w: 套餐请使用套餐下单接口", ErrInvalidProductArg)
		}
		if product.Stock < input.Quantity {
			return nil, ErrInsufficientStock
		}
		unitPrice = product.Price
		if input.PurchaseType == model.PurchaseTypeGroup {
			if product.EnableGroupBuy != 1 || product.GroupBuyPrice == nil {
				return nil, ErrGroupBuyInvalid
			}
			unitPrice = *product.GroupBuyPrice
		} else {
			input.GroupBuyTeamID = nil
		}
	}

	if input.PurchaseType == model.PurchaseTypeGroup {
		if actGB == nil {
			if product.EnableGroupBuy != 1 || product.GroupBuyPrice == nil {
				return nil, ErrGroupBuyInvalid
			}
		} else if actGB.EnableGroupBuy != 1 {
			return nil, ErrGroupBuyInvalid
		}
	} else {
		input.GroupBuyTeamID = nil
	}

	if actCtx == nil && product.Stock < input.Quantity {
		return nil, ErrInsufficientStock
	}

	var groupBuyID *uint64
	var groupBuyTeamID *uint64

	subtotal := unitPrice * float64(input.Quantity)
	couponCtx := OrderCouponContext{
		AccountID: accountID, MerchantID: input.MerchantID, Product: product,
		Subtotal: subtotal, PurchaseType: input.PurchaseType,
	}
	var discountAmount float64
	if input.UserCouponID != nil {
		if s.CouponSvc == nil {
			return nil, ErrCouponNotApplicable
		}
		d, err := s.CouponSvc.EvaluateForOrder(*input.UserCouponID, couponCtx)
		if err != nil {
			return nil, err
		}
		discountAmount = d
	}
	payAmount := roundMoney(subtotal - discountAmount)

	now := time.Now()
	orderNo := genOrderNo()

	var gb model.GroupBuy
	if input.PurchaseType == model.PurchaseTypeGroup {
		ensured, err := s.ensureActiveGroupBuy(product, actGB)
		if err != nil {
			return nil, err
		}
		gb = *ensured
		if input.GroupBuyID != nil && *input.GroupBuyID != gb.ID {
			var byID model.GroupBuy
			if err := query.NotDeleted(s.DB).First(&byID, *input.GroupBuyID).Error; err != nil {
				return nil, ErrGroupBuyInvalid
			}
			gb = byID
		}
		groupBuyID = &gb.ID
	}

	var order model.Order
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		// Re-check activity purchase limits inside the tx (ResolveForOrder is a fast-fail
		// pre-check only). Lock activity_product first so concurrent creates serialize
		// before counting — same TOCTOU class CreditSoldInTx already closes for stock.
		if s.ActivitySvc != nil && activityProductID != nil && actCtx != nil && actCtx.ActivityProduct != nil {
			var apLock model.ActivityProduct
			if err := query.NotDeleted(tx).Clauses(clause.Locking{Strength: "UPDATE"}).
				First(&apLock, *activityProductID).Error; err != nil {
				return err
			}
			if err := s.ActivitySvc.checkUserLimits(tx, accountID, actCtx.ActivityProduct, input.Quantity); err != nil {
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
		if input.DeliveryType == model.DeliveryTypeDelivery && input.AddressID != nil {
			var addr model.UserAddress
			if err := query.NotDeleted(tx).Where("id = ? AND account_id = ?", *input.AddressID, accountID).First(&addr).Error; err != nil {
				return ErrAddressRequired
			}
			addrSnap = AddressSnapshotFromUserAddress(&addr)
		}

		order = model.Order{
			OrderNo:             orderNo,
			AccountID:           accountID,
			MerchantID:          input.MerchantID,
			ActivityID:          activityID,
			Status:              status,
			MerchantReviewStage: reviewStage,
			DeliveryType:        input.DeliveryType,
			AddressSnapshot:     addrSnap,
			TotalAmount:         subtotal,
			DiscountAmount:      discountAmount,
			UserCouponID:        input.UserCouponID,
			PayAmount:           payAmount,
			PayStatus:           model.PayStatusPaid, // 暂无支付，直接视为已支付
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

		spec := (*string)(nil)
		if input.CartItemID != nil {
			var cart model.CartItem
			if err := query.NotDeleted(tx).Where("id = ? AND account_id = ?", *input.CartItemID, accountID).First(&cart).Error; err == nil {
				spec = cart.Spec
			}
		}

		item := model.OrderItem{
			OrderID: order.ID, ProductID: product.ID,
			ActivityID: activityID, ActivityProductID: activityProductID,
			PurchaseType: input.PurchaseType,
			GroupBuyID: groupBuyID, ProductName: product.Name, ProductImage: &product.CoverURL,
			Spec: spec, UnitPrice: unitPrice, Quantity: input.Quantity, Subtotal: subtotal,
		}

		if input.PurchaseType == model.PurchaseTypeGroup {
			teamID, err := s.joinOrCreateTeam(tx, accountID, order.ID, product, gb, input.GroupBuyTeamID, actGB, activityID)
			if err != nil {
				return err
			}
			groupBuyTeamID = &teamID
			item.GroupBuyTeamID = groupBuyTeamID
		}

		if err := tx.Create(&item).Error; err != nil {
			return err
		}

		if err := deductProductStockInTx(tx, product.ID, input.Quantity); err != nil {
			return err
		}

		if s.ActivitySvc != nil && activityProductID != nil {
			if err := s.ActivitySvc.CreditSoldInTx(tx, *activityProductID, input.Quantity); err != nil {
				return err
			}
		}

		if input.CartItemID != nil {
			_ = query.SoftDelete(tx, &model.CartItem{}, "id = ? AND account_id = ?", *input.CartItemID, accountID).Error
		}

		return s.tryCompleteGroup(tx, groupBuyTeamID, product, actGB)
	})
	if err != nil {
		return nil, err
	}
	return s.GetView(accountID, order.ID, nil)
}

func (s *OrderService) joinOrCreateTeam(tx *gorm.DB, accountID, orderID uint64, product model.Product, gb model.GroupBuy, teamID *uint64, actGB *ActivityGroupBuyConfig, activityID *uint64) (uint64, error) {
	target := uint32(2)
	allowRepeat := product.GroupBuyAllowRepeat
	maxJoins := uint32(1)
	if actGB != nil {
		if actGB.GroupBuyTargetCount >= 2 {
			target = actGB.GroupBuyTargetCount
		}
		allowRepeat = actGB.GroupBuyAllowRepeat
		maxJoins = actGB.GroupBuyMaxJoinsPerUser
		if maxJoins == 0 {
			maxJoins = 1
		}
	} else if product.GroupBuyTargetCount != nil && *product.GroupBuyTargetCount >= 2 {
		target = *product.GroupBuyTargetCount
	} else if gb.TargetCount >= 2 {
		target = gb.TargetCount
	}
	if allowRepeat != 1 {
		maxJoins = 1
	}

	expire := time.Now().Add(24 * time.Hour)

	resolveTeamID := teamID
	if resolveTeamID == nil && allowRepeat != 1 {
		existing, err := findUserPendingTeamInGroupBuy(tx, accountID, gb.ID, activityID)
		if err != nil {
			return 0, err
		}
		if existing != nil {
			return 0, ErrGroupBuyAlreadyJoined
		}
	}

	if resolveTeamID != nil {
		var team model.GroupBuyTeam
		if err := query.NotDeleted(tx).Where("id = ? AND group_buy_id = ? AND status = ?", *resolveTeamID, gb.ID, model.GroupBuyTeamPending).First(&team).Error; err != nil {
			return 0, ErrGroupBuyInvalid
		}
		joinCount, err := countUserTeamJoins(tx, accountID, team.ID, activityID)
		if err != nil {
			return 0, err
		}
		if err := validateTeamJoinLimit(joinCount, allowRepeat, maxJoins); err != nil {
			return 0, err
		}
		if err := tx.Model(&team).Update("current_count", gorm.Expr("current_count + 1")).Error; err != nil {
			return 0, err
		}
		if err := ensureGroupBuyMember(tx, team.ID, orderID, accountID, false); err != nil {
			return 0, err
		}
		return team.ID, nil
	}

	team := model.GroupBuyTeam{
		GroupBuyID: gb.ID, LeaderID: accountID, TargetCount: target,
		CurrentCount: 1, Status: model.GroupBuyTeamPending, ExpireAt: expire,
	}
	if err := tx.Create(&team).Error; err != nil {
		return 0, err
	}
	if err := ensureGroupBuyMember(tx, team.ID, orderID, accountID, true); err != nil {
		return 0, err
	}
	return team.ID, nil
}

// ensureActiveGroupBuy 保证商品有可用的 group_buy 行（活动拼团也可能未同步商品拼团配置）。
func (s *OrderService) ensureActiveGroupBuy(product model.Product, actGB *ActivityGroupBuyConfig) (*model.GroupBuy, error) {
	target := uint32(2)
	price := 0.0
	if actGB != nil && actGB.EnableGroupBuy == 1 {
		if actGB.GroupBuyTargetCount >= 2 {
			target = actGB.GroupBuyTargetCount
		}
		price = actGB.GroupBuyPrice
	} else if product.EnableGroupBuy == 1 && product.GroupBuyPrice != nil {
		price = *product.GroupBuyPrice
		if product.GroupBuyTargetCount != nil && *product.GroupBuyTargetCount >= 2 {
			target = *product.GroupBuyTargetCount
		}
	} else {
		return nil, ErrGroupBuyInvalid
	}
	if price <= 0 {
		return nil, ErrGroupBuyInvalid
	}

	now := time.Now()
	endAt := now.AddDate(10, 0, 0)
	var gb model.GroupBuy
	err := query.NotDeleted(s.DB).Where("product_id = ?", product.ID).First(&gb).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		gb = model.GroupBuy{
			ProductID: product.ID, TargetCount: target, GroupPrice: price,
			StartAt: now, EndAt: endAt, Status: 1,
		}
		if err := s.DB.Create(&gb).Error; err != nil {
			return nil, err
		}
		return &gb, nil
	}
	if err != nil {
		return nil, err
	}
	if err := s.DB.Model(&gb).Updates(map[string]interface{}{
		"target_count": target,
		"group_price":  price,
		"status":       1,
		"end_at":       endAt,
	}).Error; err != nil {
		return nil, err
	}
	gb.TargetCount = target
	gb.GroupPrice = price
	gb.Status = 1
	return &gb, nil
}

func ensureGroupBuyMember(tx *gorm.DB, teamID, orderID, accountID uint64, isLeader bool) error {
	var existing model.GroupBuyMember
	err := query.NotDeleted(tx).
		Where("team_id = ? AND account_id = ? AND order_id = ?", teamID, accountID, orderID).
		First(&existing).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	leader := uint8(0)
	if isLeader {
		leader = 1
	}
	m := model.GroupBuyMember{
		TeamID: teamID, OrderID: orderID, AccountID: accountID,
		IsLeader: leader, JoinedAt: time.Now(),
	}
	return tx.Create(&m).Error
}

func validateTeamJoinLimit(existingJoins int64, allowRepeat uint8, maxJoins uint32) error {
	if allowRepeat != 1 {
		if existingJoins > 0 {
			return ErrGroupBuyAlreadyJoined
		}
		return nil
	}
	if maxJoins > 0 && uint32(existingJoins) >= maxJoins {
		return ErrGroupBuyAlreadyJoined
	}
	return nil
}

func (s *OrderService) tryCompleteGroup(tx *gorm.DB, teamID *uint64, product model.Product, actGB *ActivityGroupBuyConfig) error {
	if teamID == nil {
		return nil
	}
	var team model.GroupBuyTeam
	if err := query.NotDeleted(tx).First(&team, *teamID).Error; err != nil {
		return err
	}
	if team.CurrentCount < team.TargetCount {
		return nil
	}

	allowRepeat := product.GroupBuyAllowRepeat
	if actGB != nil {
		allowRepeat = actGB.GroupBuyAllowRepeat
	}
	if allowRepeat != 1 {
		distinct, err := countDistinctTeamParticipants(tx, team.ID)
		if err != nil {
			return err
		}
		if distinct < team.TargetCount {
			return nil
		}
	}

	now := time.Now()
	if err := tx.Model(&team).Updates(map[string]interface{}{
		"status": model.GroupBuyTeamSuccess, "success_at": now,
	}).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.Order{}).
		Where("status = ? AND id IN (SELECT order_id FROM order_item WHERE group_buy_team_id = ? AND is_deleted = 0)", model.OrderStatusPendingGroup, team.ID).
		Updates(map[string]interface{}{
			"status":                model.OrderStatusPendingFulfill,
			"merchant_review_stage": model.MerchantReviewPending,
		}).Error; err != nil {
		return err
	}
	return nil
}

func (s *OrderService) Cancel(accountID, orderID uint64) error {
	order, err := s.getUserOrder(accountID, orderID)
	if err != nil {
		return err
	}
	isPackageParent := order.PackageProductID != nil && order.ParentOrderID == nil && order.MerchantID == 0
	if order.Status != model.OrderStatusPendingPay && order.Status != model.OrderStatusPendingGroup {
		if order.Status == model.OrderStatusPendingFulfill &&
			(order.MerchantReviewStage == model.MerchantReviewPending ||
				(isPackageParent && order.MerchantReviewStage == model.MerchantReviewNone)) {
			// allow cancel before merchant review / 套餐父单
		} else {
			return ErrOrderStatusInvalid
		}
	}
	// 子单不可单独取消，须取消父单级联
	if order.ParentOrderID != nil {
		return fmt.Errorf("%w: 请取消套餐父订单", ErrOrderStatusInvalid)
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := rollbackGroupTeamForOrder(tx, orderID); err != nil {
			return err
		}
		if s.CouponSvc != nil {
			if err := s.CouponSvc.ReleaseByOrderInTx(tx, order); err != nil {
				return err
			}
		}
		if s.InventorySvc != nil {
			if err := s.InventorySvc.RollbackOrderCredit(tx, orderID); err != nil {
				return err
			}
		}
		if isPackageParent {
			if err := cancelPackageChildrenInTx(tx, orderID, s.InventorySvc, s.CouponSvc); err != nil {
				return err
			}
		} else if err := restoreProductStockForOrder(tx, orderID); err != nil {
			return err
		}
		if s.ActivitySvc != nil {
			if err := s.ActivitySvc.RollbackSoldInTx(tx, orderID); err != nil {
				return err
			}
		}
		return tx.Model(order).Update("status", model.OrderStatusCancelled).Error
	})
}

func (s *OrderService) RequestUse(accountID, orderID uint64, input RequestUseInput) (*OrderView, error) {
	order, err := s.getUserOrder(accountID, orderID)
	if err != nil {
		return nil, err
	}
	if order.Status != model.OrderStatusPendingFulfill || order.MerchantReviewStage != model.MerchantReviewApproved {
		return nil, ErrOrderStatusInvalid
	}
	deliveryType, err := normalizeDeliveryType(input.DeliveryType)
	if err != nil {
		return nil, err
	}
	if deliveryType == model.DeliveryTypeDelivery && input.AddressID == nil {
		return nil, ErrAddressRequired
	}
	coordIn := DeliveryCoordinateInput{
		AddressID: input.AddressID, DeliveryLatitude: input.DeliveryLatitude, DeliveryLongitude: input.DeliveryLongitude,
	}
	if s.ZoneSvc != nil {
		if err := s.ZoneSvc.ValidateDelivery(accountID, order.MerchantID, deliveryType, coordIn); err != nil {
			return nil, err
		}
	}

	updates := map[string]interface{}{
		"delivery_type":         deliveryType,
		"merchant_review_stage": model.MerchantReviewPendingUse,
	}
	if deliveryType == model.DeliveryTypeDelivery {
		if input.AddressID == nil {
			return nil, ErrAddressRequired
		}
		var addr model.UserAddress
		if err := query.NotDeleted(s.DB).Where("id = ? AND account_id = ?", *input.AddressID, accountID).First(&addr).Error; err != nil {
			return nil, ErrAddressRequired
		}
		updates["address_snapshot"] = AddressSnapshotFromUserAddress(&addr)
	}
	if input.Remark != nil {
		updates["remark"] = *input.Remark
	}
	if err := s.DB.Model(order).Updates(updates).Error; err != nil {
		return nil, err
	}
	return s.GetView(accountID, orderID, nil)
}

func (s *OrderService) ConfirmPickup(accountID, orderID uint64) (*OrderView, error) {
	order, err := s.getUserOrder(accountID, orderID)
	if err != nil {
		return nil, err
	}
	if order.Status != model.OrderStatusPendingVerify {
		return nil, ErrOrderStatusInvalid
	}
	if err := s.DB.Model(order).Update("status", model.OrderStatusCompleted).Error; err != nil {
		return nil, err
	}
	return s.GetView(accountID, orderID, nil)
}

func (s *OrderService) GetView(accountID, orderID uint64, merchantID *uint64) (*OrderView, error) {
	order, err := s.getOrderScoped(accountID, orderID, merchantID)
	if err != nil {
		return nil, err
	}
	if err := query.NotDeleted(s.DB).
		Preload("Items", "is_deleted = ?", model.NotDeleted).
		Preload("Children", "is_deleted = ?", model.NotDeleted).
		Preload("Children.Items", "is_deleted = ?", model.NotDeleted).
		First(order, order.ID).Error; err != nil {
		return nil, err
	}
	view := toOrderView(order)
	s.attachVerifyCode(&view)
	s.attachGroupBuyProgress(&view, accountID)
	if merchantID != nil || accountID == 0 {
		s.enrichBuyer(&view)
	}
	return &view, nil
}

func (s *OrderService) List(accountID uint64, merchantID *uint64, page, pageSize int, status *uint8, statusCode string, buyerAccountID *uint64) ([]OrderView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	offset := (page - 1) * pageSize

	q := query.NotDeleted(s.DB.Model(&model.Order{}))
	if merchantID != nil {
		q = q.Where("merchant_id = ?", *merchantID)
	} else if accountID > 0 {
		q = q.Where("account_id = ?", accountID)
		// 用户端只展示顶层订单（套餐父单 / 普通单），子单挂在父单 children
		q = q.Where("parent_order_id IS NULL")
	}
	if buyerAccountID != nil {
		q = q.Where("account_id = ?", *buyerAccountID)
	}
	if status != nil {
		q = q.Where("status = ?", *status)
	}
	applyStatusCodeFilter(q, statusCode)

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var orders []model.Order
	if err := q.Preload("Items", "is_deleted = ?", model.NotDeleted).
		Preload("Children", "is_deleted = ?", model.NotDeleted).
		Preload("Children.Items", "is_deleted = ?", model.NotDeleted).
		Order("id DESC").Offset(offset).Limit(pageSize).Find(&orders).Error; err != nil {
		return nil, 0, err
	}
	views := make([]OrderView, 0, len(orders))
	for i := range orders {
		view := toOrderView(&orders[i])
		s.attachVerifyCode(&view)
		s.attachGroupBuyProgress(&view, accountID)
		views = append(views, view)
	}
	if merchantID != nil || accountID == 0 {
		s.enrichBuyers(views)
	}
	return views, total, nil
}

func (s *OrderService) enrichBuyer(view *OrderView) {
	var acc model.Account
	if err := query.NotDeleted(s.DB).Select("id", "nickname", "phone").
		First(&acc, view.AccountID).Error; err != nil {
		return
	}
	view.Buyer = &BuyerBrief{AccountID: acc.ID, Nickname: acc.Nickname, Phone: acc.Phone}
}

func (s *OrderService) enrichBuyers(views []OrderView) {
	if len(views) == 0 {
		return
	}
	ids := make([]uint64, 0, len(views))
	seen := make(map[uint64]struct{}, len(views))
	for i := range views {
		id := views[i].AccountID
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	var accounts []model.Account
	if err := query.NotDeleted(s.DB).Select("id", "nickname", "phone").
		Where("id IN ?", ids).Find(&accounts).Error; err != nil {
		return
	}
	byID := make(map[uint64]BuyerBrief, len(accounts))
	for _, acc := range accounts {
		byID[acc.ID] = BuyerBrief{AccountID: acc.ID, Nickname: acc.Nickname, Phone: acc.Phone}
	}
	for i := range views {
		if b, ok := byID[views[i].AccountID]; ok {
			bCopy := b
			views[i].Buyer = &bCopy
		}
	}
}

func applyStatusCodeFilter(q *gorm.DB, code string) {
	switch code {
	case "pending_group":
		q.Where("status = ?", model.OrderStatusPendingGroup)
	case "pending_merchant":
		q.Where("status = ? AND merchant_review_stage = ?", model.OrderStatusPendingFulfill, model.MerchantReviewPending)
	case "approved":
		q.Where("status = ? AND merchant_review_stage = ?", model.OrderStatusPendingFulfill, model.MerchantReviewApproved)
	case "pending_use_merchant":
		q.Where("status = ? AND merchant_review_stage = ?", model.OrderStatusPendingFulfill, model.MerchantReviewPendingUse)
	case "ready_pickup":
		q.Where("status = ?", model.OrderStatusPendingVerify)
	case "pending_rider":
		q.Where("status = ?", model.OrderStatusPendingShip)
	case "delivering":
		q.Where("status = ?", model.OrderStatusShipping)
	case "completed":
		q.Where("status = ?", model.OrderStatusCompleted)
	}
}

func (s *OrderService) MerchantReview(merchantID, orderID uint64, approve bool, rejectReason *string) (*OrderView, error) {
	order, err := s.getOrderScoped(0, orderID, &merchantID)
	if err != nil {
		return nil, err
	}
	if order.Status != model.OrderStatusPendingFulfill || order.MerchantReviewStage != model.MerchantReviewPending {
		return nil, ErrOrderStatusInvalid
	}
	if !approve {
		err := s.DB.Transaction(func(tx *gorm.DB) error {
			if s.CouponSvc != nil {
				if err := s.CouponSvc.ReleaseByOrderInTx(tx, order); err != nil {
					return err
				}
			}
			if s.InventorySvc != nil {
				if err := s.InventorySvc.RollbackOrderCredit(tx, orderID); err != nil {
					return err
				}
			}
			if err := restoreProductStockForOrder(tx, orderID); err != nil {
				return err
			}
			if s.ActivitySvc != nil {
				if err := s.ActivitySvc.RollbackSoldInTx(tx, orderID); err != nil {
					return err
				}
			}
			return tx.Model(order).Updates(map[string]interface{}{
				"merchant_review_stage": model.MerchantReviewRejected,
				"status":                model.OrderStatusCancelled,
				"remark":                rejectReason,
			}).Error
		})
		if err != nil {
			return nil, err
		}
		return s.GetView(0, orderID, &merchantID)
	}
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		var items []model.OrderItem
		if err := query.NotDeleted(tx).Where("order_id = ?", orderID).Find(&items).Error; err != nil {
			return err
		}
		if err := s.creditOrderInventory(tx, order.AccountID, orderID, items); err != nil {
			return err
		}
		return tx.Model(order).Update("merchant_review_stage", model.MerchantReviewApproved).Error
	})
	if err != nil {
		return nil, err
	}
	return s.GetView(0, orderID, &merchantID)
}

func (s *OrderService) MerchantUseReview(merchantID, orderID uint64, approve bool) (*OrderView, error) {
	order, err := s.getOrderScoped(0, orderID, &merchantID)
	if err != nil {
		return nil, err
	}
	if order.Status != model.OrderStatusPendingFulfill || order.MerchantReviewStage != model.MerchantReviewPendingUse {
		return nil, ErrOrderStatusInvalid
	}
	if !approve {
		return nil, s.DB.Model(order).Update("merchant_review_stage", model.MerchantReviewApproved).Error
	}

	var items []model.OrderItem
	query.NotDeleted(s.DB).Where("order_id = ?", orderID).Find(&items)

	err = s.DB.Transaction(func(tx *gorm.DB) error {
		if err := s.creditOrderInventory(tx, order.AccountID, orderID, items); err != nil {
			return err
		}

		var product model.Product
		if len(items) > 0 {
			if err := query.NotDeleted(tx).First(&product, items[0].ProductID).Error; err != nil {
				return ErrProductNotFound
			}
		}

		deliveryType, err := normalizeDeliveryType(order.DeliveryType)
		if err != nil {
			return err
		}
		if deliveryType != order.DeliveryType {
			if err := tx.Model(order).Update("delivery_type", deliveryType).Error; err != nil {
				return err
			}
		}

		if deliveryType == model.DeliveryTypeDelivery && product.ItemType == model.ProductItemTypePhysical {
			if s.ZoneSvc != nil {
				if err := s.ZoneSvc.ValidateDelivery(order.AccountID, order.MerchantID, deliveryType, DeliveryCoordinateInput{
					AddressSnapshot: order.AddressSnapshot,
				}); err != nil {
					return err
				}
			}
		}

		// 购买时已入背包，此处仅扣减商家库存并完结购买订单；自提/配送核销走背包使用单
		nextStatus := model.OrderStatusCompleted
		if deliveryType == model.DeliveryTypeDelivery && product.ItemType == model.ProductItemTypePhysical {
			nextStatus = model.OrderStatusPendingShip
			orderID := order.ID
			d := model.DeliveryOrder{OrderID: &orderID, Status: model.DeliveryPendingAccept}
			if err := tx.Create(&d).Error; err != nil {
				return err
			}
		}

		return tx.Model(order).Updates(map[string]interface{}{
			"status":                nextStatus,
			"merchant_review_stage": model.MerchantReviewUseApproved,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return s.GetView(0, orderID, &merchantID)
}

func (s *OrderService) GetGroupProgress(accountID, productID uint64, teamID *uint64) (*GroupBuyProgress, error) {
	var product model.Product
	if err := query.NotDeleted(s.DB).First(&product, productID).Error; err != nil {
		return nil, ErrProductNotFound
	}

	var gb model.GroupBuy
	if err := query.NotDeleted(s.DB).Where("product_id = ? AND status = 1", productID).First(&gb).Error; err != nil {
		return nil, ErrGroupBuyInvalid
	}

	resolvedTeamID := teamID
	if resolvedTeamID == nil && accountID > 0 {
		if userTeamID, err := s.findUserPendingTeamID(accountID, productID); err != nil {
			return nil, err
		} else if userTeamID != nil {
			resolvedTeamID = userTeamID
		}
	}

	var team model.GroupBuyTeam
	q := query.NotDeleted(s.DB).Where("group_buy_id = ?", gb.ID)
	if resolvedTeamID != nil {
		q = q.Where("id = ?", *resolvedTeamID)
	} else {
		q = q.Where("status = ?", model.GroupBuyTeamPending)
	}
	if err := q.Order("id DESC").First(&team).Error; err != nil {
		return nil, ErrGroupBuyInvalid
	}

	return s.buildGroupBuyProgress(&product, &gb, &team, accountID, nil)
}

func (s *OrderService) GetActivityGroupProgress(accountID, activityID, activityProductID uint64, teamID *uint64) (*GroupBuyProgress, error) {
	if s.ActivitySvc == nil {
		return nil, ErrGroupBuyInvalid
	}
	view, err := s.ActivitySvc.GetStoreProduct(activityID, activityProductID)
	if err != nil {
		return nil, err
	}
	if !view.CanGroupBuy {
		return nil, ErrGroupBuyInvalid
	}
	ap := view.ActivityProduct
	var prod model.Product
	if err := query.NotDeleted(s.DB).First(&prod, ap.ProductID).Error; err != nil {
		return nil, ErrProductNotFound
	}
	var gb model.GroupBuy
	if err := query.NotDeleted(s.DB).Where("product_id = ? AND status = 1", ap.ProductID).First(&gb).Error; err != nil {
		return nil, ErrGroupBuyInvalid
	}

	target := uint32(2)
	if ap.GroupBuyTargetCount != nil && *ap.GroupBuyTargetCount >= 2 {
		target = *ap.GroupBuyTargetCount
	}
	maxJoins := ap.GroupBuyMaxJoinsPerUser
	if maxJoins == 0 {
		maxJoins = 1
	}
	groupPrice := ap.ActivityPrice
	if ap.GroupBuyPrice != nil {
		groupPrice = *ap.GroupBuyPrice
	}
	actGB := &ActivityGroupBuyConfig{
		EnableGroupBuy:          1,
		GroupBuyPrice:           groupPrice,
		GroupBuyTargetCount:     target,
		GroupBuyAllowRepeat:     ap.GroupBuyAllowRepeat,
		GroupBuyMaxJoinsPerUser: maxJoins,
	}

	resolvedTeamID := teamID
	if resolvedTeamID == nil && accountID > 0 {
		if userTeamID, err := findUserPendingTeamInGroupBuy(s.DB, accountID, gb.ID, &activityID); err != nil {
			return nil, err
		} else if userTeamID != nil {
			resolvedTeamID = userTeamID
		}
	}
	if resolvedTeamID == nil {
		if latestTeamID, err := findLatestActivityPendingTeam(s.DB, gb.ID, activityID); err != nil {
			return nil, err
		} else if latestTeamID != nil {
			resolvedTeamID = latestTeamID
		}
	}

	var team model.GroupBuyTeam
	q := query.NotDeleted(s.DB).Where("group_buy_id = ?", gb.ID)
	if resolvedTeamID != nil {
		q = q.Where("id = ?", *resolvedTeamID)
	} else {
		q = q.Where("status = ?", model.GroupBuyTeamPending)
	}
	if err := q.Order("id DESC").First(&team).Error; err != nil {
		return nil, ErrGroupBuyInvalid
	}

	progress, err := s.buildGroupBuyProgress(&prod, &gb, &team, accountID, actGB)
	if err != nil {
		return nil, err
	}
	if accountID > 0 {
		count, err := countUserTeamJoins(s.DB, accountID, team.ID, &activityID)
		if err != nil {
			return nil, err
		}
		progress.UserJoinCount = uint32(count)
		progress.UserJoined = count > 0
	}
	return progress, nil
}

func (s *OrderService) attachGroupBuyProgress(view *OrderView, accountID uint64) {
	if view.Status != model.OrderStatusPendingGroup {
		return
	}
	var item model.OrderItem
	for i := range view.Items {
		if view.Items[i].GroupBuyTeamID != nil {
			item = view.Items[i]
			break
		}
	}
	if item.GroupBuyTeamID == nil {
		return
	}

	var product model.Product
	if err := query.NotDeleted(s.DB).First(&product, item.ProductID).Error; err != nil {
		return
	}
	var gb model.GroupBuy
	if item.GroupBuyID != nil {
		if err := query.NotDeleted(s.DB).First(&gb, *item.GroupBuyID).Error; err != nil {
			return
		}
	} else if err := query.NotDeleted(s.DB).Where("product_id = ? AND status = 1", item.ProductID).First(&gb).Error; err != nil {
		return
	}
	var team model.GroupBuyTeam
	if err := query.NotDeleted(s.DB).First(&team, *item.GroupBuyTeamID).Error; err != nil {
		return
	}
	var actGB *ActivityGroupBuyConfig
	if item.ActivityProductID != nil && s.ActivitySvc != nil {
		if ap, err := s.ActivitySvc.GetActivityProduct(*item.ActivityProductID, nil); err == nil && ap.EnableGroupBuy == 1 && ap.GroupBuyPrice != nil {
			target := uint32(2)
			if ap.GroupBuyTargetCount != nil && *ap.GroupBuyTargetCount >= 2 {
				target = *ap.GroupBuyTargetCount
			}
			maxJoins := ap.GroupBuyMaxJoinsPerUser
			if maxJoins == 0 {
				maxJoins = 1
			}
			actGB = &ActivityGroupBuyConfig{
				EnableGroupBuy:          1,
				GroupBuyPrice:           *ap.GroupBuyPrice,
				GroupBuyTargetCount:     target,
				GroupBuyAllowRepeat:     ap.GroupBuyAllowRepeat,
				GroupBuyMaxJoinsPerUser: maxJoins,
			}
		}
	}
	progress, err := s.buildGroupBuyProgress(&product, &gb, &team, accountID, actGB)
	if err != nil {
		return
	}
	view.GroupBuyProgress = progress
}

func (s *OrderService) buildGroupBuyProgress(product *model.Product, gb *model.GroupBuy, team *model.GroupBuyTeam, accountID uint64, actGB *ActivityGroupBuyConfig) (*GroupBuyProgress, error) {
	text := "拼团中"
	switch team.Status {
	case model.GroupBuyTeamSuccess:
		text = "已成团"
	case model.GroupBuyTeamFailed:
		text = "已失败"
	}

	allowRepeat := product.GroupBuyAllowRepeat
	groupPrice := gb.GroupPrice
	target := team.TargetCount
	if actGB != nil {
		allowRepeat = actGB.GroupBuyAllowRepeat
		groupPrice = actGB.GroupBuyPrice
		if target == 0 && actGB.GroupBuyTargetCount >= 2 {
			target = actGB.GroupBuyTargetCount
		}
	}
	if target == 0 {
		if product.GroupBuyTargetCount != nil && *product.GroupBuyTargetCount >= 2 {
			target = *product.GroupBuyTargetCount
		} else if gb.TargetCount >= 2 {
			target = gb.TargetCount
		} else {
			target = 2
		}
	}

	current := team.CurrentCount
	if allowRepeat != 1 {
		distinct, err := countDistinctTeamParticipants(s.DB, team.ID)
		if err != nil {
			return nil, err
		}
		current = distinct
	}

	remaining := uint32(0)
	if current < target {
		remaining = target - current
	}

	progress := &GroupBuyProgress{
		TeamID:          team.ID,
		TargetCount:     target,
		CurrentCount:    current,
		RemainingCount:  remaining,
		Status:          team.Status,
		StatusText:      text,
		ExpireAt:        team.ExpireAt.Format(time.RFC3339),
		GroupPrice:      groupPrice,
		AllowRepeatJoin: allowRepeat,
	}
	if accountID > 0 {
		count, err := countUserTeamOrders(s.DB, accountID, team.ID)
		if err != nil {
			return nil, err
		}
		progress.UserJoinCount = uint32(count)
		progress.UserJoined = count > 0
		progress.IsLeader = team.LeaderID == accountID
	}
	return progress, nil
}

func (s *OrderService) findUserPendingTeamID(accountID, productID uint64) (*uint64, error) {
	var teamID uint64
	err := s.DB.
		Table("order_item oi").
		Select("oi.group_buy_team_id").
		Joins("JOIN `order` o ON o.id = oi.order_id AND o.is_deleted = ?", model.NotDeleted).
		Joins("JOIN group_buy_team t ON t.id = oi.group_buy_team_id AND t.is_deleted = ?", model.NotDeleted).
		Where("o.account_id = ? AND oi.product_id = ? AND oi.group_buy_team_id IS NOT NULL AND oi.is_deleted = ?", accountID, productID, model.NotDeleted).
		Where("o.status = ? AND t.status = ?", model.OrderStatusPendingGroup, model.GroupBuyTeamPending).
		Order("o.id DESC").
		Limit(1).
		Scan(&teamID).Error
	if err != nil {
		return nil, err
	}
	if teamID == 0 {
		return nil, nil
	}
	return &teamID, nil
}

func countUserTeamJoins(db *gorm.DB, accountID, teamID uint64, activityID *uint64) (int64, error) {
	q := db.
		Table("order_item oi").
		Joins("JOIN `order` o ON o.id = oi.order_id AND o.is_deleted = ?", model.NotDeleted).
		Where("o.account_id = ? AND oi.is_deleted = ?", accountID, model.NotDeleted).
		Where("o.status <> ?", model.OrderStatusCancelled)
	if teamID > 0 {
		q = q.Where("oi.group_buy_team_id = ?", teamID)
	}
	if activityID != nil {
		q = q.Where("oi.activity_id = ?", *activityID)
	} else {
		q = q.Where("oi.activity_id IS NULL")
	}
	var count int64
	return count, q.Count(&count).Error
}

func countDistinctTeamParticipants(db *gorm.DB, teamID uint64) (uint32, error) {
	var distinct int64
	err := db.Raw(`
		SELECT COUNT(DISTINCT o.account_id)
		FROM order_item oi
		INNER JOIN `+"`order`"+` o ON o.id = oi.order_id AND o.is_deleted = 0
		WHERE oi.group_buy_team_id = ? AND oi.is_deleted = 0 AND o.status = ?
	`, teamID, model.OrderStatusPendingGroup).Scan(&distinct).Error
	return uint32(distinct), err
}

func findLatestActivityPendingTeam(db *gorm.DB, groupBuyID, activityID uint64) (*uint64, error) {
	var teamID uint64
	err := db.
		Table("group_buy_team t").
		Select("t.id").
		Joins("JOIN order_item oi ON oi.group_buy_team_id = t.id AND oi.is_deleted = ?", model.NotDeleted).
		Joins("JOIN `order` o ON o.id = oi.order_id AND o.is_deleted = ?", model.NotDeleted).
		Where("t.is_deleted = ? AND t.group_buy_id = ? AND t.status = ?", model.NotDeleted, groupBuyID, model.GroupBuyTeamPending).
		Where("oi.activity_id = ? AND o.status = ?", activityID, model.OrderStatusPendingGroup).
		Order("t.id DESC").
		Limit(1).
		Scan(&teamID).Error
	if err != nil {
		return nil, err
	}
	if teamID == 0 {
		return nil, nil
	}
	return &teamID, nil
}

func findUserPendingTeamInGroupBuy(tx *gorm.DB, accountID, groupBuyID uint64, activityID *uint64) (*uint64, error) {
	q := tx.
		Table("order_item oi").
		Select("oi.group_buy_team_id").
		Joins("JOIN `order` o ON o.id = oi.order_id AND o.is_deleted = ?", model.NotDeleted).
		Joins("JOIN group_buy_team t ON t.id = oi.group_buy_team_id AND t.is_deleted = ? AND t.status = ?", model.NotDeleted, model.GroupBuyTeamPending).
		Where("o.account_id = ? AND oi.group_buy_id = ? AND oi.is_deleted = ?", accountID, groupBuyID, model.NotDeleted).
		Where("o.status = ?", model.OrderStatusPendingGroup)
	if activityID != nil {
		q = q.Where("oi.activity_id = ?", *activityID)
	} else {
		q = q.Where("oi.activity_id IS NULL")
	}
	var teamID uint64
	if err := q.Order("o.id DESC").Limit(1).Scan(&teamID).Error; err != nil {
		return nil, err
	}
	if teamID == 0 {
		return nil, nil
	}
	return &teamID, nil
}

func countUserTeamOrders(db *gorm.DB, accountID, teamID uint64) (int64, error) {
	return countUserTeamJoins(db, accountID, teamID, nil)
}

func rollbackGroupTeamForOrder(tx *gorm.DB, orderID uint64) error {
	var item model.OrderItem
	if err := query.NotDeleted(tx).
		Where("order_id = ? AND group_buy_team_id IS NOT NULL AND purchase_type = ?", orderID, model.PurchaseTypeGroup).
		First(&item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}

	teamID := *item.GroupBuyTeamID
	var team model.GroupBuyTeam
	if err := query.NotDeleted(tx).First(&team, teamID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if team.CurrentCount == 0 {
		return nil
	}

	if err := tx.Model(&team).Update("current_count", gorm.Expr("current_count - 1")).Error; err != nil {
		return err
	}

	if err := query.NotDeleted(tx).First(&team, teamID).Error; err != nil {
		return err
	}
	if team.Status == model.GroupBuyTeamSuccess && team.CurrentCount < team.TargetCount {
		return tx.Model(&team).Updates(map[string]interface{}{
			"status":     model.GroupBuyTeamPending,
			"success_at": nil,
		}).Error
	}
	return nil
}

func (s *OrderService) getUserOrder(accountID, orderID uint64) (*model.Order, error) {
	return s.getOrderScoped(accountID, orderID, nil)
}

func (s *OrderService) getOrderScoped(accountID, orderID uint64, merchantID *uint64) (*model.Order, error) {
	var order model.Order
	q := query.NotDeleted(s.DB).Where("id = ?", orderID)
	if merchantID != nil {
		q = q.Where("merchant_id = ?", *merchantID)
	} else if accountID > 0 {
		q = q.Where("account_id = ?", accountID)
	}
	if err := q.First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrOrderNotFound
		}
		return nil, err
	}
	return &order, nil
}

func toOrderView(o *model.Order) OrderView {
	return OrderView{
		Order: *o, StatusText: model.OrderStatusText(o.Status),
		StatusCode: model.OrderStatusCode(o.Status, o.MerchantReviewStage),
	}
}

func normalizeDeliveryType(deliveryType uint8) (uint8, error) {
	if deliveryType == 0 {
		return model.DeliveryTypePickup, nil
	}
	if deliveryType != model.DeliveryTypePickup && deliveryType != model.DeliveryTypeDelivery {
		return 0, ErrInvalidDeliveryType
	}
	return deliveryType, nil
}

func (s *OrderService) attachVerifyCode(view *OrderView) {
	if view.Status != model.OrderStatusPendingVerify {
		return
	}
	code, err := s.ensureVerifyCode(&view.Order)
	if err != nil || code == "" {
		return
	}
	view.VerifyCode = &code
}

func (s *OrderService) ensureVerifyCode(order *model.Order) (string, error) {
	var vc model.VerificationCode
	err := query.NotDeleted(s.DB).
		Where("order_id = ? AND status = ?", order.ID, model.VerificationCodeUnused).
		Order("id DESC").
		First(&vc).Error
	if err == nil {
		return vc.Code, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}
	created, err := createVerificationCode(s.DB, order)
	if err != nil {
		return "", err
	}
	return created.Code, nil
}

func getOrCreateVerificationCode(tx *gorm.DB, order *model.Order) (*model.VerificationCode, error) {
	var existing model.VerificationCode
	err := query.NotDeleted(tx).
		Where("order_id = ? AND status = ?", order.ID, model.VerificationCodeUnused).
		First(&existing).Error
	if err == nil {
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return createVerificationCode(tx, order)
}

func genOrderNo() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("YJ%s%s", time.Now().Format("20060102150405"), hex.EncodeToString(b))
}

func (s *OrderService) creditOrderInventory(tx *gorm.DB, accountID, orderID uint64, items []model.OrderItem) error {
	if s.InventorySvc == nil || len(items) == 0 {
		return nil
	}
	return s.InventorySvc.CreditFromOrder(tx, accountID, orderID, items)
}

// deductProductStockInTx 下单时扣减商品库存（需 stock >= quantity）。
func deductProductStockInTx(tx *gorm.DB, productID uint64, quantity uint32) error {
	if quantity == 0 {
		return nil
	}
	result := query.NotDeleted(tx.Model(&model.Product{})).
		Where("id = ? AND stock >= ?", productID, quantity).
		Update("stock", gorm.Expr("stock - ?", quantity))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return ErrInsufficientStock
	}
	return nil
}

// restoreProductStockForOrder 取消/拒单时回退订单商品库存。
func restoreProductStockForOrder(tx *gorm.DB, orderID uint64) error {
	var items []model.OrderItem
	if err := query.NotDeleted(tx).Where("order_id = ?", orderID).Find(&items).Error; err != nil {
		return err
	}
	for _, it := range items {
		if it.Quantity == 0 {
			continue
		}
		if err := tx.Model(&model.Product{}).Where("id = ?", it.ProductID).
			Update("stock", gorm.Expr("stock + ?", it.Quantity)).Error; err != nil {
			return err
		}
	}
	return nil
}

func genVerifyCode() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return fmt.Sprintf("V%s", hex.EncodeToString(b))
}

func createVerificationCode(tx *gorm.DB, order *model.Order) (*model.VerificationCode, error) {
	code := genVerifyCode()
	orderID := order.ID
	vc := model.VerificationCode{
		OrderID: &orderID, AccountID: order.AccountID, Code: code, Status: model.VerificationCodeUnused,
	}
	exp := time.Now().AddDate(0, 0, 30)
	vc.ExpiredAt = &exp
	if err := tx.Create(&vc).Error; err != nil {
		return nil, err
	}
	return &vc, nil
}
