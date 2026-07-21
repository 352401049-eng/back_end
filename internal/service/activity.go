package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

var (
	ErrActivityNotFound           = errors.New("activity not found")
	ErrActivityForbidden          = errors.New("activity forbidden")
	ErrActivityNotActive          = errors.New("activity not active")
	ErrActivityProductNotFound    = errors.New("activity product not found")
	ErrActivityProductDuplicate   = errors.New("activity product duplicate")
	ErrActivityLimitExceeded      = errors.New("activity purchase limit exceeded")
	ErrActivityRegisterWindow     = errors.New("not in register purchase window")
)

type ActivityService struct {
	DB *gorm.DB
}

type ActivityInput struct {
	MerchantID   uint64
	Name         string
	Description  *string
	CoverURL     *string
	BannerImages []string
	StartAt      time.Time
	EndAt        time.Time
	Status       uint8
	EnableCoupon uint8
	SortOrder    int
}

type ActivityProductInput struct {
	ProductID               uint64
	ActivityPrice           float64
	ActivityStock           uint32
	PerUserMaxQty           uint32
	PerUserMaxOrders        uint32
	DailyMax                uint32
	WeeklyMax               uint32
	MonthlyMax              uint32
	ActivityMax             uint32
	RegisterHours           uint32
	RegisterMax             uint32
	EnableGroupBuy          uint8
	GroupBuyPrice           *float64
	GroupBuyTargetCount     *uint32
	GroupBuyAllowRepeat     uint8
	GroupBuyMaxJoinsPerUser uint32
	EnableCoupon            uint8
	SortOrder               int
	Status                  uint8
}

// UpdateActivityProductPatch 活动商品部分更新。
type UpdateActivityProductPatch struct {
	ActivityPrice           *float64
	ActivityStock           *uint32
	PerUserMaxQty           *uint32
	PerUserMaxOrders        *uint32
	DailyMax                *uint32
	WeeklyMax               *uint32
	MonthlyMax              *uint32
	ActivityMax             *uint32
	RegisterHours           *uint32
	RegisterMax             *uint32
	EnableGroupBuy          *uint8
	GroupBuyPrice           *float64
	GroupBuyTargetCount     *uint32
	GroupBuyAllowRepeat     *uint8
	GroupBuyMaxJoinsPerUser *uint32
	EnableCoupon            *uint8
	SortOrder               *int
	Status                  *uint8
}

type ActivityListFilter struct {
	MerchantID *uint64
	Status     *uint8
	ActiveOnly bool
}

type ActivityStoreView struct {
	model.Activity
	IsActive             bool  `json:"is_active"`
	EnableGroupBuy       uint8 `json:"enable_group_buy"`
	GroupBuyProductCount int64 `json:"group_buy_product_count"`
}

type ActivityDetailView struct {
	ActivityStoreView
	Products []ActivityProductItemView `json:"products,omitempty"`
}

type ActivityPublicDetailView struct {
	ActivityStoreView
	Products []ActivityProductStoreView `json:"products,omitempty"`
}

type ActivityProductItemView struct {
	model.ActivityProduct
	ProductName string `json:"product_name,omitempty"`
	ProductCover string `json:"product_cover,omitempty"`
	CanGroupBuy  bool   `json:"can_group_buy"`
	CanUseCoupon bool   `json:"can_use_coupon"`
}

type ActivityProductStoreView struct {
	model.ActivityProduct
	MerchantID     uint64             `json:"merchant_id"`
	ProductName    string             `json:"product_name"`
	ProductCover   string             `json:"product_cover"`
	OriginalPrice  float64            `json:"original_price"`
	AvailableStock uint32             `json:"available_stock"`
	CanGroupBuy    bool               `json:"can_group_buy"`
	CanUseCoupon   bool               `json:"can_use_coupon"`
	SaleOptions    ProductSaleOptions `json:"sale_options"`
}

type ActivityOrderContext struct {
	Activity        *model.Activity
	ActivityProduct *model.ActivityProduct
	Product         model.Product
	UnitPrice       float64
	EnableCoupon    bool
	GroupBuyConfig  *ActivityGroupBuyConfig
}

type ActivityGroupBuyConfig struct {
	EnableGroupBuy          uint8
	GroupBuyPrice           float64
	GroupBuyTargetCount     uint32
	GroupBuyAllowRepeat     uint8
	GroupBuyMaxJoinsPerUser uint32
}

func (s *ActivityService) List(page, pageSize int, filter ActivityListFilter) ([]model.Activity, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 50 {
		pageSize = 50
	}

	q := query.NotDeleted(s.DB.Model(&model.Activity{}))
	if filter.MerchantID != nil {
		q = q.Where("merchant_id = ?", *filter.MerchantID)
	}
	if filter.Status != nil {
		q = q.Where("status = ?", *filter.Status)
	}
	if filter.ActiveOnly {
		now := time.Now()
		q = q.Where("status = ? AND start_at <= ? AND end_at >= ?", model.ActivityStatusOn, now, now)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var list []model.Activity
	if err := q.Order("sort_order ASC, id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (s *ActivityService) GetByID(id uint64, merchantID *uint64) (*model.Activity, error) {
	var act model.Activity
	q := query.NotDeleted(s.DB).Where("id = ?", id)
	if merchantID != nil {
		q = q.Where("merchant_id = ?", *merchantID)
	}
	if err := q.First(&act).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrActivityNotFound
		}
		return nil, err
	}
	return &act, nil
}

func (s *ActivityService) GetDetail(id uint64, merchantID *uint64) (*model.Activity, error) {
	act, err := s.GetByID(id, merchantID)
	if err != nil {
		return nil, err
	}
	var products []model.ActivityProduct
	if err := query.NotDeleted(s.DB).
		Preload("Product", "is_deleted = ?", model.NotDeleted).
		Where("activity_id = ?", id).
		Order("sort_order ASC, id ASC").
		Find(&products).Error; err != nil {
		return nil, err
	}
	act.Products = products
	return act, nil
}

func (s *ActivityService) ListProducts(activityID uint64, merchantID *uint64) ([]model.ActivityProduct, error) {
	if _, err := s.GetByID(activityID, merchantID); err != nil {
		return nil, err
	}
	var products []model.ActivityProduct
	if err := query.NotDeleted(s.DB).
		Preload("Product", "is_deleted = ?", model.NotDeleted).
		Where("activity_id = ?", activityID).
		Order("sort_order ASC, id ASC").
		Find(&products).Error; err != nil {
		return nil, err
	}
	return products, nil
}

func (s *ActivityService) Create(input ActivityInput) (*model.Activity, error) {
	if err := validateActivityInput(input); err != nil {
		return nil, err
	}
	act := model.Activity{
		MerchantID: input.MerchantID, Name: strings.TrimSpace(input.Name),
		Description: input.Description, CoverURL: input.CoverURL,
		BannerImages: input.BannerImages, StartAt: input.StartAt, EndAt: input.EndAt,
		Status: input.Status, EnableCoupon: normalizeEnableCoupon(input.EnableCoupon),
		SortOrder: input.SortOrder,
	}
	if act.Status == 0 {
		act.Status = model.ActivityStatusDraft
	}
	if err := s.DB.Create(&act).Error; err != nil {
		return nil, err
	}
	return &act, nil
}

func (s *ActivityService) Update(id uint64, input ActivityInput, merchantID *uint64) (*model.Activity, error) {
	act, err := s.GetByID(id, merchantID)
	if err != nil {
		return nil, err
	}
	if err := validateActivityInput(input); err != nil {
		return nil, err
	}
	updates := map[string]interface{}{
		"name": input.Name, "description": input.Description, "cover_url": input.CoverURL,
		"banner_images": toJSONColumn(input.BannerImages),
		"start_at": input.StartAt, "end_at": input.EndAt, "status": input.Status,
		"enable_coupon": normalizeEnableCoupon(input.EnableCoupon), "sort_order": input.SortOrder,
	}
	if err := s.DB.Model(act).Updates(updates).Error; err != nil {
		return nil, err
	}
	return s.GetByID(id, merchantID)
}

func (s *ActivityService) Delete(id uint64, merchantID *uint64) error {
	act, err := s.GetByID(id, merchantID)
	if err != nil {
		return err
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := query.SoftDelete(tx, &model.ActivityProduct{}, "activity_id = ?", id).Error; err != nil {
			return err
		}
		return query.SoftDelete(tx, act).Error
	})
}

func (s *ActivityService) AddProduct(activityID uint64, input ActivityProductInput, merchantID *uint64) (*model.ActivityProduct, error) {
	act, err := s.GetByID(activityID, merchantID)
	if err != nil {
		return nil, err
	}
	if err := validateActivityProductInput(input); err != nil {
		return nil, err
	}
	var product model.Product
	if err := query.NotDeleted(s.DB).
		Where("id = ? AND merchant_id = ?", input.ProductID, act.MerchantID).
		First(&product).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrProductNotFound
		}
		return nil, err
	}

	maxJoins := input.GroupBuyMaxJoinsPerUser
	if maxJoins == 0 {
		maxJoins = 1
	}
	status := input.Status
	if status == 0 {
		status = 1
	}

	existing, found, err := s.findActivityProductPair(activityID, input.ProductID)
	if err != nil {
		return nil, err
	}
	if found {
		if existing.IsDeleted == model.NotDeleted {
			return nil, ErrActivityProductDuplicate
		}
		if err := s.restoreActivityProduct(&existing, input, maxJoins, status); err != nil {
			return nil, err
		}
		return s.GetActivityProduct(existing.ID, merchantID)
	}

	ap := model.ActivityProduct{
		ActivityID: activityID, ProductID: input.ProductID,
		ActivityPrice: input.ActivityPrice, ActivityStock: input.ActivityStock,
		PerUserMaxQty: input.PerUserMaxQty, PerUserMaxOrders: input.PerUserMaxOrders,
		DailyMax: input.DailyMax, WeeklyMax: input.WeeklyMax, MonthlyMax: input.MonthlyMax,
		ActivityMax: input.ActivityMax, RegisterHours: input.RegisterHours, RegisterMax: input.RegisterMax,
		EnableGroupBuy: input.EnableGroupBuy, GroupBuyPrice: input.GroupBuyPrice,
		GroupBuyTargetCount: input.GroupBuyTargetCount,
		GroupBuyAllowRepeat: input.GroupBuyAllowRepeat,
		GroupBuyMaxJoinsPerUser: maxJoins,
		EnableCoupon: normalizeEnableCoupon(input.EnableCoupon),
		SortOrder: input.SortOrder, Status: status,
	}
	if err := s.DB.Create(&ap).Error; err != nil {
		if isMySQLDuplicateKey(err) {
			existing, found, findErr := s.findActivityProductPair(activityID, input.ProductID)
			if findErr != nil {
				return nil, findErr
			}
			if found {
				if existing.IsDeleted == model.NotDeleted {
					return nil, ErrActivityProductDuplicate
				}
				if restoreErr := s.restoreActivityProduct(&existing, input, maxJoins, status); restoreErr != nil {
					return nil, restoreErr
				}
				return s.GetActivityProduct(existing.ID, merchantID)
			}
		}
		return nil, fmt.Errorf("添加活动商品失败: %w", err)
	}
	return s.GetActivityProduct(ap.ID, merchantID)
}

func (s *ActivityService) findActivityProductPair(activityID, productID uint64) (model.ActivityProduct, bool, error) {
	var existing model.ActivityProduct
	err := s.DB.Unscoped().
		Where("activity_id = ? AND product_id = ?", activityID, productID).
		First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.ActivityProduct{}, false, nil
	}
	if err != nil {
		return model.ActivityProduct{}, false, err
	}
	return existing, true, nil
}

func (s *ActivityService) restoreActivityProduct(existing *model.ActivityProduct, input ActivityProductInput, maxJoins uint32, status uint8) error {
	updates := activityProductUpdates(input, maxJoins, status)
	updates["is_deleted"] = model.NotDeleted
	if err := s.DB.Unscoped().Model(existing).Updates(updates).Error; err != nil {
		return fmt.Errorf("恢复活动商品失败: %w", err)
	}
	return nil
}

func isMySQLDuplicateKey(err error) bool {
	var me *mysql.MySQLError
	return errors.As(err, &me) && me.Number == 1062
}

func activityProductUpdates(input ActivityProductInput, maxJoins uint32, status uint8) map[string]interface{} {
	return map[string]interface{}{
		"activity_price": input.ActivityPrice, "activity_stock": input.ActivityStock,
		"per_user_max_qty": input.PerUserMaxQty, "per_user_max_orders": input.PerUserMaxOrders,
		"daily_max": input.DailyMax, "weekly_max": input.WeeklyMax, "monthly_max": input.MonthlyMax,
		"activity_max": input.ActivityMax, "register_hours": input.RegisterHours, "register_max": input.RegisterMax,
		"enable_group_buy": input.EnableGroupBuy, "group_buy_price": input.GroupBuyPrice,
		"group_buy_target_count": input.GroupBuyTargetCount,
		"group_buy_allow_repeat": input.GroupBuyAllowRepeat,
		"group_buy_max_joins_per_user": maxJoins,
		"enable_coupon": normalizeEnableCoupon(input.EnableCoupon),
		"sort_order": input.SortOrder, "status": status,
	}
}

func (s *ActivityService) UpdateProduct(apID uint64, input ActivityProductInput, merchantID *uint64) (*model.ActivityProduct, error) {
	return s.UpdateProductInActivity(0, apID, activityProductInputToPatch(input), merchantID)
}

func (s *ActivityService) UpdateProductInActivity(activityID, apID uint64, patch UpdateActivityProductPatch, merchantID *uint64) (*model.ActivityProduct, error) {
	ap, err := s.GetActivityProduct(apID, merchantID)
	if err != nil {
		return nil, err
	}
	if activityID > 0 && ap.ActivityID != activityID {
		return nil, ErrActivityProductNotFound
	}
	if !patch.hasField() {
		return nil, ErrInvalidProductArg
	}
	merged := mergeActivityProductPatch(ap, patch)
	if err := validateActivityProductInput(merged); err != nil {
		return nil, err
	}
	maxJoins := merged.GroupBuyMaxJoinsPerUser
	if maxJoins == 0 {
		maxJoins = 1
	}
	status := merged.Status
	if status == 0 {
		status = 1
	}
	updates := activityProductUpdates(merged, maxJoins, status)
	if merged.EnableGroupBuy != 1 {
		updates["group_buy_price"] = nil
		updates["group_buy_target_count"] = nil
	}
	if err := s.DB.Model(ap).Updates(updates).Error; err != nil {
		return nil, err
	}
	return s.GetActivityProduct(apID, merchantID)
}

func (p UpdateActivityProductPatch) hasField() bool {
	return p.ActivityPrice != nil || p.ActivityStock != nil || p.PerUserMaxQty != nil ||
		p.PerUserMaxOrders != nil || p.DailyMax != nil || p.WeeklyMax != nil ||
		p.MonthlyMax != nil || p.ActivityMax != nil || p.RegisterHours != nil ||
		p.RegisterMax != nil || p.EnableGroupBuy != nil || p.GroupBuyPrice != nil ||
		p.GroupBuyTargetCount != nil || p.GroupBuyAllowRepeat != nil ||
		p.GroupBuyMaxJoinsPerUser != nil || p.EnableCoupon != nil || p.SortOrder != nil || p.Status != nil
}

func activityProductInputToPatch(input ActivityProductInput) UpdateActivityProductPatch {
	patch := UpdateActivityProductPatch{
		ActivityPrice:           &input.ActivityPrice,
		ActivityStock:           &input.ActivityStock,
		PerUserMaxQty:           &input.PerUserMaxQty,
		PerUserMaxOrders:        &input.PerUserMaxOrders,
		DailyMax:                &input.DailyMax,
		WeeklyMax:               &input.WeeklyMax,
		MonthlyMax:              &input.MonthlyMax,
		ActivityMax:             &input.ActivityMax,
		RegisterHours:           &input.RegisterHours,
		RegisterMax:             &input.RegisterMax,
		EnableGroupBuy:          &input.EnableGroupBuy,
		GroupBuyPrice:           input.GroupBuyPrice,
		GroupBuyTargetCount:     input.GroupBuyTargetCount,
		GroupBuyAllowRepeat:     &input.GroupBuyAllowRepeat,
		GroupBuyMaxJoinsPerUser: &input.GroupBuyMaxJoinsPerUser,
		EnableCoupon:            &input.EnableCoupon,
		SortOrder:               &input.SortOrder,
		Status:                  &input.Status,
	}
	return patch
}

func mergeActivityProductPatch(ap *model.ActivityProduct, patch UpdateActivityProductPatch) ActivityProductInput {
	merged := ActivityProductInput{
		ProductID:               ap.ProductID,
		ActivityPrice:           ap.ActivityPrice,
		ActivityStock:           ap.ActivityStock,
		PerUserMaxQty:           ap.PerUserMaxQty,
		PerUserMaxOrders:        ap.PerUserMaxOrders,
		DailyMax:                ap.DailyMax,
		WeeklyMax:               ap.WeeklyMax,
		MonthlyMax:              ap.MonthlyMax,
		ActivityMax:             ap.ActivityMax,
		RegisterHours:           ap.RegisterHours,
		RegisterMax:             ap.RegisterMax,
		EnableGroupBuy:          ap.EnableGroupBuy,
		GroupBuyPrice:           ap.GroupBuyPrice,
		GroupBuyTargetCount:     ap.GroupBuyTargetCount,
		GroupBuyAllowRepeat:     ap.GroupBuyAllowRepeat,
		GroupBuyMaxJoinsPerUser: ap.GroupBuyMaxJoinsPerUser,
		EnableCoupon:            ap.EnableCoupon,
		SortOrder:               ap.SortOrder,
		Status:                  ap.Status,
	}
	if patch.ActivityPrice != nil {
		merged.ActivityPrice = *patch.ActivityPrice
	}
	if patch.ActivityStock != nil {
		merged.ActivityStock = *patch.ActivityStock
	}
	if patch.PerUserMaxQty != nil {
		merged.PerUserMaxQty = *patch.PerUserMaxQty
	}
	if patch.PerUserMaxOrders != nil {
		merged.PerUserMaxOrders = *patch.PerUserMaxOrders
	}
	if patch.DailyMax != nil {
		merged.DailyMax = *patch.DailyMax
	}
	if patch.WeeklyMax != nil {
		merged.WeeklyMax = *patch.WeeklyMax
	}
	if patch.MonthlyMax != nil {
		merged.MonthlyMax = *patch.MonthlyMax
	}
	if patch.ActivityMax != nil {
		merged.ActivityMax = *patch.ActivityMax
	}
	if patch.RegisterHours != nil {
		merged.RegisterHours = *patch.RegisterHours
	}
	if patch.RegisterMax != nil {
		merged.RegisterMax = *patch.RegisterMax
	}
	if patch.EnableGroupBuy != nil {
		merged.EnableGroupBuy = *patch.EnableGroupBuy
	}
	if patch.GroupBuyPrice != nil {
		merged.GroupBuyPrice = patch.GroupBuyPrice
	}
	if patch.GroupBuyTargetCount != nil {
		merged.GroupBuyTargetCount = patch.GroupBuyTargetCount
	}
	if patch.GroupBuyAllowRepeat != nil {
		merged.GroupBuyAllowRepeat = *patch.GroupBuyAllowRepeat
	}
	if patch.GroupBuyMaxJoinsPerUser != nil {
		merged.GroupBuyMaxJoinsPerUser = *patch.GroupBuyMaxJoinsPerUser
	}
	if patch.EnableCoupon != nil {
		merged.EnableCoupon = *patch.EnableCoupon
	}
	if patch.SortOrder != nil {
		merged.SortOrder = *patch.SortOrder
	}
	if patch.Status != nil {
		merged.Status = *patch.Status
	}
	return merged
}

func (s *ActivityService) GetProductInActivity(activityID, apID uint64, merchantID *uint64) (*model.ActivityProduct, error) {
	ap, err := s.GetActivityProduct(apID, merchantID)
	if err != nil {
		return nil, err
	}
	if ap.ActivityID != activityID {
		return nil, ErrActivityProductNotFound
	}
	return ap, nil
}

func (s *ActivityService) RemoveProductInActivity(activityID, apID uint64, merchantID *uint64) error {
	ap, err := s.GetProductInActivity(activityID, apID, merchantID)
	if err != nil {
		return err
	}
	return query.SoftDelete(s.DB, ap).Error
}

func (s *ActivityService) RemoveProduct(apID uint64, merchantID *uint64) error {
	ap, err := s.GetActivityProduct(apID, merchantID)
	if err != nil {
		return err
	}
	return query.SoftDelete(s.DB, ap).Error
}

func (s *ActivityService) GetActivityProduct(apID uint64, merchantID *uint64) (*model.ActivityProduct, error) {
	var ap model.ActivityProduct
	q := query.NotDeleted(s.DB).Preload("Product", "is_deleted = ?", model.NotDeleted).Where("id = ?", apID)
	if err := q.First(&ap).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrActivityProductNotFound
		}
		return nil, err
	}
	if merchantID != nil {
		if _, err := s.GetByID(ap.ActivityID, merchantID); err != nil {
			return nil, err
		}
	}
	return &ap, nil
}

func (s *ActivityService) ToStoreView(act *model.Activity, publicOnly bool) ActivityStoreView {
	return s.ToStoreViews([]model.Activity{*act}, publicOnly)[0]
}

func (s *ActivityService) ToStoreViews(activities []model.Activity, publicOnly bool) []ActivityStoreView {
	if len(activities) == 0 {
		return nil
	}
	ids := make([]uint64, 0, len(activities))
	for i := range activities {
		ids = append(ids, activities[i].ID)
	}
	counts := s.countGroupBuyProductsByActivity(ids, publicOnly)
	now := time.Now()
	views := make([]ActivityStoreView, 0, len(activities))
	for i := range activities {
		cnt := counts[activities[i].ID]
		views = append(views, ActivityStoreView{
			Activity:             activities[i],
			IsActive:             activities[i].IsActiveNow(now),
			EnableGroupBuy:       boolToUint8(cnt > 0),
			GroupBuyProductCount: cnt,
		})
	}
	return views
}

func (s *ActivityService) ToDetailView(act *model.Activity, products []model.ActivityProduct, publicOnly bool) ActivityDetailView {
	store := s.ToStoreView(act, publicOnly)
	items := make([]ActivityProductItemView, 0, len(products))
	for i := range products {
		if publicOnly {
			if products[i].Status != 1 {
				continue
			}
			if products[i].Product == nil || products[i].Product.Status != model.ProductStatusOn {
				continue
			}
		}
		items = append(items, toActivityProductItemView(act, &products[i]))
	}
	return ActivityDetailView{ActivityStoreView: store, Products: items}
}

func (s *ActivityService) GetStoreDetail(id uint64) (*ActivityPublicDetailView, error) {
	act, err := s.GetByID(id, nil)
	if err != nil {
		return nil, err
	}
	if !act.IsActiveNow(time.Now()) {
		return nil, ErrActivityNotActive
	}
	products, err := s.ListStoreProducts(id, false)
	if err != nil {
		return nil, err
	}
	return &ActivityPublicDetailView{
		ActivityStoreView: s.ToStoreView(act, true),
		Products:          products,
	}, nil
}

func (s *ActivityService) GetDetailView(id uint64, merchantID *uint64) (*ActivityDetailView, error) {
	act, err := s.GetByID(id, merchantID)
	if err != nil {
		return nil, err
	}
	var products []model.ActivityProduct
	if err := query.NotDeleted(s.DB).
		Preload("Product", "is_deleted = ?", model.NotDeleted).
		Where("activity_id = ?", id).
		Order("sort_order ASC, id ASC").
		Find(&products).Error; err != nil {
		return nil, err
	}
	view := s.ToDetailView(act, products, false)
	return &view, nil
}

func (s *ActivityService) ListProductItemViews(activityID uint64, merchantID *uint64, publicOnly bool) ([]ActivityProductItemView, error) {
	act, err := s.GetByID(activityID, merchantID)
	if err != nil {
		return nil, err
	}
	if publicOnly && !act.IsActiveNow(time.Now()) {
		return nil, ErrActivityNotActive
	}
	q := query.NotDeleted(s.DB).
		Preload("Product", "is_deleted = ?", model.NotDeleted).
		Where("activity_id = ?", activityID)
	if publicOnly {
		q = q.Where("status = 1")
	}
	var products []model.ActivityProduct
	if err := q.Order("sort_order ASC, id ASC").Find(&products).Error; err != nil {
		return nil, err
	}
	views := make([]ActivityProductItemView, 0, len(products))
	for i := range products {
		if publicOnly {
			if products[i].Product == nil || products[i].Product.Status != model.ProductStatusOn {
				continue
			}
		}
		views = append(views, toActivityProductItemView(act, &products[i]))
	}
	return views, nil
}

func toActivityProductItemView(act *model.Activity, ap *model.ActivityProduct) ActivityProductItemView {
	view := ActivityProductItemView{
		ActivityProduct: *ap,
		CanGroupBuy:     activityProductCanGroupBuy(ap),
		CanUseCoupon:    act.EnableCoupon == 1 && ap.EnableCoupon == 1,
	}
	if ap.Product != nil {
		view.ProductName = ap.Product.Name
		view.ProductCover = ap.Product.CoverURL
	}
	return view
}

func activityProductCanGroupBuy(ap *model.ActivityProduct) bool {
	return ap.EnableGroupBuy == 1 && ap.GroupBuyPrice != nil && *ap.GroupBuyPrice > 0 &&
		ap.GroupBuyTargetCount != nil && *ap.GroupBuyTargetCount >= 2 && *ap.GroupBuyPrice < ap.ActivityPrice
}

func (s *ActivityService) countGroupBuyProductsByActivity(activityIDs []uint64, publicOnly bool) map[uint64]int64 {
	out := make(map[uint64]int64, len(activityIDs))
	if len(activityIDs) == 0 {
		return out
	}
	q := query.NotDeleted(s.DB.Model(&model.ActivityProduct{})).
		Select("activity_id, COUNT(*) AS cnt").
		Where("activity_id IN ?", activityIDs).
		Where("enable_group_buy = 1").
		Where("group_buy_price IS NOT NULL AND group_buy_target_count >= 2").
		Where("group_buy_price < activity_price")
	if publicOnly {
		q = q.Joins("JOIN product ON product.id = activity_product.product_id AND product.is_deleted = ? AND product.status = ?",
			model.NotDeleted, model.ProductStatusOn).
			Where("activity_product.status = 1")
	}
	type row struct {
		ActivityID uint64
		Cnt        int64
	}
	var rows []row
	if err := q.Group("activity_id").Scan(&rows).Error; err != nil {
		return out
	}
	for _, r := range rows {
		out[r.ActivityID] = r.Cnt
	}
	return out
}

func boolToUint8(v bool) uint8 {
	if v {
		return 1
	}
	return 0
}

func (s *ActivityService) GetProductItemView(activityID, apID uint64, merchantID *uint64) (*ActivityProductItemView, error) {
	act, err := s.GetByID(activityID, merchantID)
	if err != nil {
		return nil, err
	}
	ap, err := s.GetProductInActivity(activityID, apID, merchantID)
	if err != nil {
		return nil, err
	}
	view := toActivityProductItemView(act, ap)
	return &view, nil
}

func (s *ActivityService) ListStoreProducts(activityID uint64, groupBuyOnly bool) ([]ActivityProductStoreView, error) {
	act, err := s.GetByID(activityID, nil)
	if err != nil {
		return nil, err
	}
	if !act.IsActiveNow(time.Now()) {
		return nil, ErrActivityNotActive
	}

	var items []model.ActivityProduct
	if err := query.NotDeleted(s.DB).
		Preload("Product", "is_deleted = ? AND status = ?", model.NotDeleted, model.ProductStatusOn).
		Where("activity_id = ? AND status = 1", activityID).
		Order("sort_order ASC, id ASC").
		Find(&items).Error; err != nil {
		return nil, err
	}

	views := make([]ActivityProductStoreView, 0, len(items))
	for i := range items {
		if items[i].Product == nil || items[i].Product.ID == 0 {
			continue
		}
		view := buildActivityProductStoreView(act, &items[i], items[i].Product)
		if groupBuyOnly && !view.CanGroupBuy {
			continue
		}
		views = append(views, view)
	}
	return views, nil
}

func (s *ActivityService) GetStoreProduct(activityID, activityProductID uint64) (*ActivityProductStoreView, error) {
	act, err := s.GetByID(activityID, nil)
	if err != nil {
		return nil, err
	}
	if !act.IsActiveNow(time.Now()) {
		return nil, ErrActivityNotActive
	}
	ap, err := s.GetActivityProduct(activityProductID, nil)
	if err != nil {
		return nil, err
	}
	if ap.ActivityID != activityID {
		return nil, ErrActivityProductNotFound
	}
	if ap.Status != 1 || ap.Product == nil || ap.Product.Status != model.ProductStatusOn {
		return nil, ErrActivityProductNotFound
	}
	view := buildActivityProductStoreView(act, ap, ap.Product)
	return &view, nil
}

func buildActivityProductStoreView(act *model.Activity, ap *model.ActivityProduct, p *model.Product) ActivityProductStoreView {
	avail := availableActivityStock(ap, p)
	canCoupon := act.EnableCoupon == 1 && ap.EnableCoupon == 1
	canGroup := activityProductCanGroupBuy(ap)

	cover := ""
	if p.CoverURL != "" {
		cover = p.CoverURL
	}
	solo := PurchaseOption{
		Available:    avail > 0,
		Price:        ap.ActivityPrice,
		CanUseCoupon: canCoupon,
	}
	groupPrice := ap.ActivityPrice
	if ap.GroupBuyPrice != nil {
		groupPrice = *ap.GroupBuyPrice
	}
	group := GroupPurchaseOption{
		PurchaseOption: PurchaseOption{
			Available:    canGroup && avail > 0,
			Price:        groupPrice,
			CanUseCoupon: canCoupon,
		},
		TargetCount:     ap.GroupBuyTargetCount,
		AllowRepeatJoin: ap.GroupBuyAllowRepeat,
	}

	return ActivityProductStoreView{
		ActivityProduct: *ap,
		MerchantID:      act.MerchantID,
		ProductName:     p.Name,
		ProductCover:    cover,
		OriginalPrice:   p.Price,
		AvailableStock:  avail,
		CanGroupBuy:     canGroup,
		CanUseCoupon:    canCoupon,
		SaleOptions: ProductSaleOptions{
			Solo:  solo,
			Group: group,
		},
	}
}

func availableActivityStock(ap *model.ActivityProduct, product *model.Product) uint32 {
	if ap.ActivityStock == 0 {
		return product.Stock
	}
	remain := uint32(0)
	if ap.ActivityStock > ap.SoldCount {
		remain = ap.ActivityStock - ap.SoldCount
	}
	if product.Stock < remain {
		return product.Stock
	}
	return remain
}

func (s *ActivityService) ResolveForOrder(accountID uint64, activityProductID uint64, merchantID uint64, quantity uint32, purchaseType uint8) (*ActivityOrderContext, error) {
	var ap model.ActivityProduct
	if err := query.NotDeleted(s.DB).Preload("Product", "is_deleted = ?", model.NotDeleted).
		Where("id = ?", activityProductID).First(&ap).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrActivityProductNotFound
		}
		return nil, err
	}

	act, err := s.GetByID(ap.ActivityID, nil)
	if err != nil {
		return nil, err
	}
	if act.MerchantID != merchantID {
		return nil, ErrActivityForbidden
	}
	if !act.IsActiveNow(time.Now()) {
		return nil, ErrActivityNotActive
	}
	if ap.Status != 1 {
		return nil, ErrActivityProductNotFound
	}
	if ap.Product == nil || ap.Product.Status != model.ProductStatusOn {
		return nil, ErrProductNotFound
	}

	product := *ap.Product
	avail := availableActivityStock(&ap, &product)
	if quantity == 0 {
		quantity = 1
	}
	if avail < quantity {
		return nil, ErrInsufficientStock
	}

	if err := s.checkUserLimits(s.DB, accountID, &ap, quantity); err != nil {
		return nil, err
	}

	unitPrice := ap.ActivityPrice
	enableCoupon := act.EnableCoupon == 1 && ap.EnableCoupon == 1
	var gbConfig *ActivityGroupBuyConfig

	if purchaseType == model.PurchaseTypeGroup {
		if ap.EnableGroupBuy != 1 || ap.GroupBuyPrice == nil {
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
		unitPrice = *ap.GroupBuyPrice
		gbConfig = &ActivityGroupBuyConfig{
			EnableGroupBuy:          1,
			GroupBuyPrice:           *ap.GroupBuyPrice,
			GroupBuyTargetCount:     target,
			GroupBuyAllowRepeat:     ap.GroupBuyAllowRepeat,
			GroupBuyMaxJoinsPerUser: maxJoins,
		}
	}

	return &ActivityOrderContext{
		Activity: act, ActivityProduct: &ap, Product: product,
		UnitPrice: unitPrice, EnableCoupon: enableCoupon, GroupBuyConfig: gbConfig,
	}, nil
}

// checkUserLimits enforces per-user qty / calendar / register windows against db.
// Pass the transaction handle inside OrderService.Create to close the TOCTOU window
// between ResolveForOrder's pre-check and order insert.
func (s *ActivityService) checkUserLimits(db *gorm.DB, accountID uint64, ap *model.ActivityProduct, quantity uint32) error {
	if db == nil {
		db = s.DB
	}
	if ap.PerUserMaxQty > 0 {
		var bought uint32
		err := query.NotDeleted(db).
			Table("order_item oi").
			Select("COALESCE(SUM(oi.quantity), 0)").
			Joins("JOIN `order` o ON o.id = oi.order_id AND o.is_deleted = ?", model.NotDeleted).
			Where("o.account_id = ? AND oi.activity_product_id = ? AND oi.is_deleted = ?", accountID, ap.ID, model.NotDeleted).
			Where("o.status <> ?", model.OrderStatusCancelled).
			Scan(&bought).Error
		if err != nil {
			return err
		}
		if bought+quantity > ap.PerUserMaxQty {
			return ErrActivityLimitExceeded
		}
	}

	now := time.Now()

	if ap.RegisterHours > 0 {
		var account model.Account
		if err := query.NotDeleted(db).Select("id", "created_at").First(&account, accountID).Error; err != nil {
			return err
		}
		if !inRegisterWindow(account.CreatedAt, now, ap.RegisterHours) {
			return ErrActivityRegisterWindow
		}
		if ap.RegisterMax > 0 {
			start := account.CreatedAt
			end := registerDeadline(account.CreatedAt, ap.RegisterHours)
			n, err := countOrders(db, accountID, ap.ID, start, end)
			if err != nil {
				return err
			}
			if uint32(n) >= ap.RegisterMax {
				return ErrActivityLimitExceeded
			}
		}
	}

	type orderLimit struct {
		max  uint32
		unit string // "" = unbounded (activity_max)
	}
	limits := []orderLimit{
		{ap.DailyMax, "day"},
		{ap.WeeklyMax, "week"},
		{ap.MonthlyMax, "month"},
	}
	activityMax := ap.ActivityMax
	if activityMax == 0 && ap.PerUserMaxOrders > 0 {
		activityMax = ap.PerUserMaxOrders
	}
	limits = append(limits, orderLimit{activityMax, ""})

	for _, lim := range limits {
		if lim.max == 0 {
			continue
		}
		var start, end time.Time
		if lim.unit != "" {
			start, end = calendarWindow(now, lim.unit)
		}
		n, err := countOrders(db, accountID, ap.ID, start, end)
		if err != nil {
			return err
		}
		if uint32(n) >= lim.max {
			return ErrActivityLimitExceeded
		}
	}
	return nil
}

func (s *ActivityService) CreditSoldInTx(tx *gorm.DB, activityProductID uint64, quantity uint32) error {
	if activityProductID == 0 {
		return nil
	}
	var ap model.ActivityProduct
	if err := query.NotDeleted(tx).First(&ap, activityProductID).Error; err != nil {
		return err
	}
	if ap.ActivityStock > 0 {
		result := tx.Model(&ap).
			Where("id = ? AND sold_count + ? <= activity_stock", activityProductID, quantity).
			Update("sold_count", gorm.Expr("sold_count + ?", quantity))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrInsufficientStock
		}
	}
	return nil
}

func (s *ActivityService) RollbackSoldInTx(tx *gorm.DB, orderID uint64) error {
	var items []model.OrderItem
	if err := query.NotDeleted(tx).Where("order_id = ? AND activity_product_id IS NOT NULL", orderID).Find(&items).Error; err != nil {
		return err
	}
	for _, it := range items {
		if it.ActivityProductID == nil {
			continue
		}
		if err := tx.Model(&model.ActivityProduct{}).
			Where("id = ? AND sold_count >= ?", *it.ActivityProductID, it.Quantity).
			Update("sold_count", gorm.Expr("sold_count - ?", it.Quantity)).Error; err != nil {
			return err
		}
	}
	return nil
}

func validateActivityInput(input ActivityInput) error {
	if input.MerchantID == 0 || strings.TrimSpace(input.Name) == "" {
		return ErrInvalidProductArg
	}
	if !input.EndAt.After(input.StartAt) {
		return ErrInvalidProductArg
	}
	return nil
}

func validateActivityProductInput(input ActivityProductInput) error {
	if input.ProductID == 0 || input.ActivityPrice <= 0 {
		return ErrInvalidProductArg
	}
	if input.EnableGroupBuy == 1 {
		if input.GroupBuyPrice == nil || *input.GroupBuyPrice <= 0 {
			return ErrInvalidProductArg
		}
		if input.GroupBuyTargetCount == nil || *input.GroupBuyTargetCount < 2 {
			return ErrInvalidProductArg
		}
		if *input.GroupBuyPrice >= input.ActivityPrice {
			return ErrInvalidProductArg
		}
	}
	return nil
}
