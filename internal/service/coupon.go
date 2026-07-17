package service

import (
	"encoding/json"
	"errors"
	"math"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrCouponNotFound        = errors.New("coupon not found")
	ErrCouponUnavailable     = errors.New("coupon unavailable")
	ErrCouponQuotaExceeded   = errors.New("coupon quota exceeded")
	ErrCouponAlreadyClaimed  = errors.New("coupon already claimed")
	ErrUserCouponNotFound    = errors.New("user coupon not found")
	ErrUserCouponInvalid     = errors.New("user coupon invalid")
	ErrCouponNotApplicable   = errors.New("coupon not applicable")
)

type CouponService struct {
	DB *gorm.DB
}

type CreateCouponInput struct {
	Name           string
	Type           uint8
	MerchantID     *uint64
	MinAmount      float64
	DiscountAmount *float64
	DiscountRate   *uint8
	MaxDiscount    *float64
	TotalQuota     uint32
	ScopeType      uint8
	ScopeIDs       []uint64
	StartAt        time.Time
	EndAt          time.Time
}

type UpdateCouponInput struct {
	Name           *string
	MinAmount      *float64
	DiscountAmount *float64
	DiscountRate   *uint8
	MaxDiscount    *float64
	TotalQuota     *uint32
	ScopeType      *uint8
	ScopeIDs       *[]uint64
	StartAt        *time.Time
	EndAt          *time.Time
}

type OrderCouponContext struct {
	AccountID    uint64
	MerchantID   uint64
	Product      model.Product
	Subtotal     float64
	PurchaseType uint8
}

type ApplicableCouponView struct {
	model.UserCoupon
	StatusText     string  `json:"status_text"`
	DiscountAmount float64 `json:"discount_amount"`
	PayAmount      float64 `json:"pay_amount"`
	Applicable     bool    `json:"applicable"`
	Reason         string  `json:"reason,omitempty"`
}

// ClaimableCouponView 用户端可领取券模板。
type ClaimableCouponView struct {
	model.Coupon
	Claimed        bool   `json:"claimed"`
	CanClaim       bool   `json:"can_claim"`
	RemainingQuota uint32 `json:"remaining_quota"`
}

func (s *CouponService) Create(input CreateCouponInput) (*model.Coupon, error) {
	if input.Name == "" {
		return nil, errors.New("优惠券名称不能为空")
	}
	if err := validateCouponTemplate(input.Type, input.DiscountAmount, input.DiscountRate); err != nil {
		return nil, err
	}
	if !input.EndAt.After(input.StartAt) {
		return nil, errors.New("失效时间须晚于生效时间")
	}
	coupon := model.Coupon{
		Name:           input.Name,
		Type:           input.Type,
		MerchantID:     input.MerchantID,
		MinAmount:      input.MinAmount,
		DiscountAmount: input.DiscountAmount,
		DiscountRate:   input.DiscountRate,
		MaxDiscount:    input.MaxDiscount,
		TotalQuota:     input.TotalQuota,
		ScopeType:      input.ScopeType,
		ScopeIDs:       input.ScopeIDs,
		StartAt:        input.StartAt,
		EndAt:          input.EndAt,
		Status:         model.CouponStatusEnabled,
	}
	if err := s.DB.Create(&coupon).Error; err != nil {
		return nil, err
	}
	return &coupon, nil
}

func (s *CouponService) Update(id uint64, merchantScope *uint64, input UpdateCouponInput) (*model.Coupon, error) {
	coupon, err := s.getScoped(id, merchantScope)
	if err != nil {
		return nil, err
	}
	updates := map[string]interface{}{}
	if input.Name != nil {
		updates["name"] = *input.Name
	}
	if input.MinAmount != nil {
		updates["min_amount"] = *input.MinAmount
	}
	if input.DiscountAmount != nil {
		updates["discount_amount"] = *input.DiscountAmount
	}
	if input.DiscountRate != nil {
		updates["discount_rate"] = *input.DiscountRate
	}
	if input.MaxDiscount != nil {
		updates["max_discount"] = *input.MaxDiscount
	}
	if input.TotalQuota != nil {
		updates["total_quota"] = *input.TotalQuota
	}
	if input.ScopeType != nil {
		updates["scope_type"] = *input.ScopeType
	}
	if input.ScopeIDs != nil {
		updates["scope_ids"] = toJSONColumnUint64(*input.ScopeIDs)
	}
	if input.StartAt != nil {
		updates["start_at"] = *input.StartAt
	}
	if input.EndAt != nil {
		updates["end_at"] = *input.EndAt
	}
	if len(updates) == 0 {
		return coupon, nil
	}
	if err := s.DB.Model(coupon).Updates(updates).Error; err != nil {
		return nil, err
	}
	return s.GetByID(id)
}

func (s *CouponService) UpdateStatus(id uint64, merchantScope *uint64, status uint8) (*model.Coupon, error) {
	if status != model.CouponStatusDisabled && status != model.CouponStatusEnabled {
		return nil, errors.New("状态无效")
	}
	coupon, err := s.getScoped(id, merchantScope)
	if err != nil {
		return nil, err
	}
	if err := s.DB.Model(coupon).Update("status", status).Error; err != nil {
		return nil, err
	}
	return s.GetByID(id)
}

func (s *CouponService) GetByID(id uint64) (*model.Coupon, error) {
	var coupon model.Coupon
	if err := query.NotDeleted(s.DB).First(&coupon, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrCouponNotFound
		}
		return nil, err
	}
	return &coupon, nil
}

func (s *CouponService) List(page, pageSize int, merchantScope *uint64, status *uint8) ([]model.Coupon, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	offset := (page - 1) * pageSize
	q := query.NotDeleted(s.DB.Model(&model.Coupon{}))
	if merchantScope != nil {
		q = q.Where("merchant_id = ?", *merchantScope)
	}
	if status != nil {
		q = q.Where("status = ?", *status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.Coupon
	if err := q.Order("id DESC").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// ListClaimable 可领取的券模板（领券中心；merchantID 为空时返回平台券）。
func (s *CouponService) ListClaimable(merchantID *uint64, accountID *uint64) ([]ClaimableCouponView, error) {
	coupons, err := s.listActiveCouponTemplates(merchantID, false)
	if err != nil {
		return nil, err
	}
	return s.toClaimableViews(coupons, accountID), nil
}

// ListClaimableByMerchant 某商家店铺可领券（含平台通用券，可选）。
func (s *CouponService) ListClaimableByMerchant(merchantID uint64, accountID *uint64, includePlatform bool) ([]ClaimableCouponView, error) {
	coupons, err := s.listActiveCouponTemplates(&merchantID, includePlatform)
	if err != nil {
		return nil, err
	}
	return s.toClaimableViews(coupons, accountID), nil
}

func (s *CouponService) listActiveCouponTemplates(merchantID *uint64, includePlatform bool) ([]model.Coupon, error) {
	now := time.Now()
	q := query.NotDeleted(s.DB.Model(&model.Coupon{})).
		Where("status = ? AND start_at <= ? AND end_at >= ?", model.CouponStatusEnabled, now, now).
		Where("total_quota = 0 OR received_count < total_quota")
	if merchantID != nil {
		if includePlatform {
			q = q.Where("merchant_id IS NULL OR merchant_id = ?", *merchantID)
		} else {
			q = q.Where("merchant_id = ?", *merchantID)
		}
	} else {
		q = q.Where("merchant_id IS NULL")
	}
	var coupons []model.Coupon
	if err := q.Order("id DESC").Find(&coupons).Error; err != nil {
		return nil, err
	}
	return coupons, nil
}

func (s *CouponService) toClaimableViews(coupons []model.Coupon, accountID *uint64) []ClaimableCouponView {
	out := make([]ClaimableCouponView, 0, len(coupons))
	for _, c := range coupons {
		remaining := uint32(0)
		if c.TotalQuota > 0 {
			if c.ReceivedCount < c.TotalQuota {
				remaining = c.TotalQuota - c.ReceivedCount
			}
		}
		view := ClaimableCouponView{
			Coupon:         c,
			RemainingQuota: remaining,
			CanClaim:       c.TotalQuota == 0 || remaining > 0,
		}
		if accountID != nil {
			var count int64
			query.NotDeleted(s.DB.Model(&model.UserCoupon{})).
				Where("account_id = ? AND coupon_id = ?", *accountID, c.ID).
				Count(&count)
			view.Claimed = count > 0
			if view.Claimed {
				view.CanClaim = false
			}
		}
		out = append(out, view)
	}
	return out
}

func (s *CouponService) Claim(accountID, couponID uint64) (*model.UserCoupon, error) {
	var uc model.UserCoupon
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var coupon model.Coupon
		if err := query.NotDeleted(tx).Clauses(clause.Locking{Strength: "UPDATE"}).First(&coupon, couponID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrCouponNotFound
			}
			return err
		}
		if err := assertCouponClaimable(&coupon); err != nil {
			return err
		}
		var count int64
		if err := query.NotDeleted(tx.Model(&model.UserCoupon{})).
			Where("account_id = ? AND coupon_id = ?", accountID, couponID).
			Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrCouponAlreadyClaimed
		}
		now := time.Now()
		uc = model.UserCoupon{
			AccountID: accountID, CouponID: coupon.ID,
			Status: model.UserCouponStatusUnused, ReceivedAt: now, ExpiredAt: coupon.EndAt,
		}
		if err := tx.Create(&uc).Error; err != nil {
			return err
		}
		return tx.Model(&coupon).Update("received_count", gorm.Expr("received_count + 1")).Error
	})
	if err != nil {
		return nil, err
	}
	return s.loadUserCoupon(uc.ID, accountID)
}

func (s *CouponService) ListApplicable(accountID uint64, ctx OrderCouponContext) ([]ApplicableCouponView, error) {
	s.expireStaleUserCoupons(accountID)
	now := time.Now()
	var list []model.UserCoupon
	if err := query.NotDeleted(s.DB).Preload("Coupon", "is_deleted = ?", model.NotDeleted).
		Where("account_id = ? AND status = ? AND expired_at >= ?", accountID, model.UserCouponStatusUnused, now).
		Order("expired_at ASC").
		Find(&list).Error; err != nil {
		return nil, err
	}
	views := make([]ApplicableCouponView, 0, len(list))
	for _, uc := range list {
		discount, err := s.evaluateUserCoupon(&uc, ctx)
		view := ApplicableCouponView{
			UserCoupon: uc,
			StatusText: userCouponStatusText(uc.Status),
			Applicable: err == nil,
		}
		if err == nil {
			view.DiscountAmount = discount
			view.PayAmount = roundMoney(ctx.Subtotal - discount)
		} else {
			view.Reason = couponReasonText(err)
		}
		views = append(views, view)
	}
	return views, nil
}

// EvaluateForOrder 校验并计算优惠金额（不含事务锁）。
func (s *CouponService) EvaluateForOrder(userCouponID uint64, ctx OrderCouponContext) (float64, error) {
	var uc model.UserCoupon
	if err := query.NotDeleted(s.DB).Preload("Coupon", "is_deleted = ?", model.NotDeleted).
		Where("id = ? AND account_id = ?", userCouponID, ctx.AccountID).
		First(&uc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, ErrUserCouponNotFound
		}
		return 0, err
	}
	return s.evaluateUserCoupon(&uc, ctx)
}

// ApplyForOrderInTx 下单事务内锁定用券。
func (s *CouponService) ApplyForOrderInTx(tx *gorm.DB, userCouponID, orderID uint64, ctx OrderCouponContext) (float64, error) {
	var uc model.UserCoupon
	if err := query.NotDeleted(tx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Preload("Coupon", "is_deleted = ?", model.NotDeleted).
		Where("id = ? AND account_id = ?", userCouponID, ctx.AccountID).
		First(&uc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, ErrUserCouponNotFound
		}
		return 0, err
	}
	discount, err := s.evaluateUserCoupon(&uc, ctx)
	if err != nil {
		return 0, err
	}
	now := time.Now()
	if err := tx.Model(&uc).Updates(map[string]interface{}{
		"status": model.UserCouponStatusUsed, "order_id": orderID, "used_at": now,
	}).Error; err != nil {
		return 0, err
	}
	return discount, nil
}

// ReleaseByOrderInTx 订单取消/拒单时退还优惠券。
func (s *CouponService) ReleaseByOrderInTx(tx *gorm.DB, order *model.Order) error {
	if order.UserCouponID == nil {
		return nil
	}
	var uc model.UserCoupon
	if err := query.NotDeleted(tx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", *order.UserCouponID).
		First(&uc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	if uc.Status != model.UserCouponStatusUsed || uc.OrderID == nil || *uc.OrderID != order.ID {
		return nil
	}
	now := time.Now()
	status := model.UserCouponStatusUnused
	if !uc.ExpiredAt.After(now) {
		status = model.UserCouponStatusExpired
	}
	return tx.Model(&uc).Updates(map[string]interface{}{
		"status": status, "order_id": nil, "used_at": nil,
	}).Error
}

func (s *CouponService) getScoped(id uint64, merchantScope *uint64) (*model.Coupon, error) {
	coupon, err := s.GetByID(id)
	if err != nil {
		return nil, err
	}
	if merchantScope != nil {
		if coupon.MerchantID == nil || *coupon.MerchantID != *merchantScope {
			return nil, ErrCouponNotFound
		}
	}
	return coupon, nil
}

func (s *CouponService) loadUserCoupon(id, accountID uint64) (*model.UserCoupon, error) {
	var uc model.UserCoupon
	if err := query.NotDeleted(s.DB).Preload("Coupon", "is_deleted = ?", model.NotDeleted).
		Where("id = ? AND account_id = ?", id, accountID).
		First(&uc).Error; err != nil {
		return nil, err
	}
	return &uc, nil
}

func (s *CouponService) expireStaleUserCoupons(accountID uint64) {
	now := time.Now()
	_ = query.NotDeleted(s.DB.Model(&model.UserCoupon{})).
		Where("account_id = ? AND status = ? AND expired_at < ?", accountID, model.UserCouponStatusUnused, now).
		Update("status", model.UserCouponStatusExpired).Error
}

func (s *CouponService) evaluateUserCoupon(uc *model.UserCoupon, ctx OrderCouponContext) (float64, error) {
	if uc.Status != model.UserCouponStatusUnused {
		return 0, ErrUserCouponInvalid
	}
	now := time.Now()
	if !uc.ExpiredAt.After(now) {
		return 0, ErrUserCouponInvalid
	}
	coupon := uc.Coupon
	if coupon.ID == 0 {
		if err := query.NotDeleted(s.DB).First(&coupon, uc.CouponID).Error; err != nil {
			return 0, ErrCouponNotFound
		}
	}
	if coupon.Status != model.CouponStatusEnabled {
		return 0, ErrCouponUnavailable
	}
	if now.Before(coupon.StartAt) || now.After(coupon.EndAt) {
		return 0, ErrCouponUnavailable
	}
	if coupon.MerchantID != nil && *coupon.MerchantID != ctx.MerchantID {
		return 0, ErrCouponNotApplicable
	}
	if ctx.Product.EnableCoupon != 1 {
		return 0, ErrCouponNotApplicable
	}
	if ctx.Subtotal < coupon.MinAmount {
		return 0, ErrCouponNotApplicable
	}
	if !couponMatchesScope(&coupon, ctx.Product) {
		return 0, ErrCouponNotApplicable
	}
	return calcCouponDiscount(&coupon, ctx.Subtotal)
}

func couponMatchesScope(coupon *model.Coupon, product model.Product) bool {
	switch coupon.ScopeType {
	case model.CouponScopeAll:
		return true
	case model.CouponScopeCategory:
		return uint64InSlice(product.CategoryID, coupon.ScopeIDs)
	case model.CouponScopeProduct:
		return uint64InSlice(product.ID, coupon.ScopeIDs)
	default:
		return false
	}
}

func calcCouponDiscount(coupon *model.Coupon, subtotal float64) (float64, error) {
	var discount float64
	switch coupon.Type {
	case model.CouponTypeFixed:
		if coupon.DiscountAmount == nil || *coupon.DiscountAmount <= 0 {
			return 0, ErrCouponUnavailable
		}
		discount = *coupon.DiscountAmount
	case model.CouponTypeRate:
		if coupon.DiscountRate == nil || *coupon.DiscountRate == 0 || *coupon.DiscountRate >= 100 {
			return 0, ErrCouponUnavailable
		}
		rate := float64(*coupon.DiscountRate)
		discount = subtotal * (100 - rate) / 100
		if coupon.MaxDiscount != nil && discount > *coupon.MaxDiscount {
			discount = *coupon.MaxDiscount
		}
	default:
		return 0, ErrCouponUnavailable
	}
	if discount > subtotal {
		discount = subtotal
	}
	return roundMoney(discount), nil
}

func assertCouponClaimable(coupon *model.Coupon) error {
	now := time.Now()
	if coupon.Status != model.CouponStatusEnabled {
		return ErrCouponUnavailable
	}
	if now.Before(coupon.StartAt) || now.After(coupon.EndAt) {
		return ErrCouponUnavailable
	}
	if coupon.TotalQuota > 0 && coupon.ReceivedCount >= coupon.TotalQuota {
		return ErrCouponQuotaExceeded
	}
	return nil
}

func validateCouponTemplate(couponType uint8, discountAmount *float64, discountRate *uint8) error {
	switch couponType {
	case model.CouponTypeFixed:
		if discountAmount == nil || *discountAmount <= 0 {
			return errors.New("满减券须填写减免金额")
		}
	case model.CouponTypeRate:
		if discountRate == nil || *discountRate == 0 || *discountRate >= 100 {
			return errors.New("折扣券须填写 1-99 的折扣率")
		}
	default:
		return errors.New("优惠券类型无效")
	}
	return nil
}

func uint64InSlice(id uint64, ids []uint64) bool {
	for _, v := range ids {
		if v == id {
			return true
		}
	}
	return false
}

func roundMoney(v float64) float64 {
	return math.Round(v*100) / 100
}

func toJSONColumnUint64(v []uint64) interface{} {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(b)
}

func userCouponStatusText(status uint8) string {
	switch status {
	case model.UserCouponStatusUnused:
		return "未使用"
	case model.UserCouponStatusUsed:
		return "已使用"
	case model.UserCouponStatusExpired:
		return "已过期"
	default:
		return "未知"
	}
}

func couponReasonText(err error) string {
	switch {
	case errors.Is(err, ErrCouponNotApplicable):
		return "不满足使用条件"
	case errors.Is(err, ErrUserCouponInvalid):
		return "优惠券不可用或已过期"
	case errors.Is(err, ErrCouponUnavailable):
		return "优惠券已失效"
	default:
		return "不可用"
	}
}
