package service

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
)

type DashboardService struct {
	DB *gorm.DB
}

// invalidOrderStatusInts 勿用 []uint8 绑定 IN/NOT IN，GORM 会当成 binary。
// 销售额口径：排除未成团/待支付及终态失败单（Mock 下拼团单虽已付，未成团不计入）。
var invalidOrderStatusInts = []int{
	int(model.OrderStatusPendingPay),
	int(model.OrderStatusPendingGroup),
	int(model.OrderStatusCancelled),
	int(model.OrderStatusGroupFailed),
	int(model.OrderStatusRefunding),
	int(model.OrderStatusRefunded),
	int(model.OrderStatusClosed),
}

type DailyStat struct {
	Date        string  `json:"date"`
	OrderCount  int64   `json:"order_count"`
	SalesAmount float64 `json:"sales_amount"`
}

type ProductSalesRank struct {
	ProductID   uint64 `json:"product_id"`
	ProductName string `json:"product_name"`
	MerchantID  uint64 `json:"merchant_id,omitempty"`
	CoverURL    string `json:"cover_url,omitempty"`
	SalesCount  uint32 `json:"sales_count"`
}

type AdminDashboard struct {
	OrderCount           int64              `json:"order_count"`
	CompletedOrderCount  int64              `json:"completed_order_count"`
	VerificationCount    int64              `json:"verification_count"`
	PendingRiderApps     int64              `json:"pending_rider_apps"`
	MerchantCount        int64              `json:"merchant_count"`
	ProductCount         int64              `json:"product_count"`
	LowStockProductCount int64              `json:"low_stock_product_count"`
	TotalSales           float64            `json:"total_sales"`
	UserCount            int64              `json:"user_count"`
	OrderTrend           []DailyStat        `json:"order_trend"`
	TopProducts          []ProductSalesRank `json:"top_products"`
}

type MerchantDashboard struct {
	ProductCount           int64              `json:"product_count"`
	PendingOrderReview     int64              `json:"pending_order_review"`
	PendingUseReview       int64              `json:"pending_use_review"`
	TodayVerificationCount int64              `json:"today_verification_count"`
	LowStockCount          int64              `json:"low_stock_count"`
	OrderTrend             []DailyStat        `json:"order_trend"`
	TopProducts            []ProductSalesRank `json:"top_products"`
	Sales                  SalesReport        `json:"sales"`
}

// SalesReport 销售额报表（含核销次数）。
type SalesReport struct {
	MerchantID        *uint64 `json:"merchant_id,omitempty"`
	MerchantName      string  `json:"merchant_name,omitempty"`
	ValidOrderCount   int64   `json:"valid_order_count"`
	TotalSalesAmount  float64 `json:"total_sales_amount"`
	VerificationCount int64   `json:"verification_count"`
	StartDate         string  `json:"start_date,omitempty"`
	EndDate           string  `json:"end_date,omitempty"`
}

type SalesReportFilter struct {
	MerchantID *uint64
	StartDate  *time.Time // inclusive, day start
	EndDate    *time.Time // exclusive, day after end
}

func (s *DashboardService) freshDB() *gorm.DB {
	// NewDB 切断与 s.DB 的 Statement 共享，避免并发/链式复用导致 Where 条件串台
	return s.DB.Session(&gorm.Session{NewDB: true})
}

func (s *DashboardService) Admin() (*AdminDashboard, error) {
	d := &AdminDashboard{}
	s.freshDB().Model(&model.Order{}).Where("is_deleted = ?", model.NotDeleted).Count(&d.OrderCount)
	s.freshDB().Model(&model.Order{}).Where("is_deleted = ? AND status = ?", model.NotDeleted, model.OrderStatusCompleted).Count(&d.CompletedOrderCount)
	s.freshDB().Model(&model.VerificationRecord{}).Where("is_deleted = ?", model.NotDeleted).Count(&d.VerificationCount)
	s.freshDB().Model(&model.RiderApplication{}).Where("is_deleted = ? AND status = ?", model.NotDeleted, model.RiderApplicationPending).Count(&d.PendingRiderApps)
	s.freshDB().Model(&model.MerchantProfile{}).Where("is_deleted = ?", model.NotDeleted).Count(&d.MerchantCount)
	s.freshDB().Model(&model.Product{}).Where("is_deleted = ?", model.NotDeleted).Count(&d.ProductCount)
	s.freshDB().Model(&model.Product{}).Where("is_deleted = ? AND stock <= ?", model.NotDeleted, 10).Count(&d.LowStockProductCount)
	s.freshDB().Model(&model.Account{}).Where("is_deleted = ? AND type = ?", model.NotDeleted, model.AccountTypeUser).Count(&d.UserCount)

	allTimeSales, err := s.SalesReport(SalesReportFilter{MerchantID: nil})
	if err != nil {
		return nil, err
	}
	d.TotalSales = allTimeSales.TotalSalesAmount

	var err2 error
	d.OrderTrend, err2 = s.orderTrend(nil, 7)
	if err2 != nil {
		return nil, err2
	}
	d.TopProducts, err2 = s.topProducts(nil, 10)
	if err2 != nil {
		return nil, err2
	}
	return d, nil
}

func (s *DashboardService) Merchant(merchantID uint64) (*MerchantDashboard, error) {
	d := &MerchantDashboard{}
	s.freshDB().Model(&model.Product{}).Where("is_deleted = ? AND merchant_id = ?", model.NotDeleted, merchantID).Count(&d.ProductCount)
	s.freshDB().Model(&model.Order{}).Where(
		"is_deleted = ? AND merchant_id = ? AND status = ? AND merchant_review_stage = ?",
		model.NotDeleted, merchantID, model.OrderStatusPendingFulfill, model.MerchantReviewPending,
	).Count(&d.PendingOrderReview)
	s.freshDB().Model(&model.Order{}).Where(
		"is_deleted = ? AND merchant_id = ? AND status = ? AND merchant_review_stage = ?",
		model.NotDeleted, merchantID, model.OrderStatusPendingFulfill, model.MerchantReviewPendingUse,
	).Count(&d.PendingUseReview)

	start, end := todayRange()
	s.freshDB().Model(&model.VerificationRecord{}).Where(
		"is_deleted = ? AND merchant_id = ? AND verified_at >= ? AND verified_at < ?",
		model.NotDeleted, merchantID, start, end,
	).Count(&d.TodayVerificationCount)

	s.freshDB().Model(&model.Product{}).Where(
		"is_deleted = ? AND merchant_id = ? AND stock <= ?", model.NotDeleted, merchantID, 10,
	).Count(&d.LowStockCount)

	var err error
	d.OrderTrend, err = s.orderTrend(&merchantID, 7)
	if err != nil {
		return nil, err
	}
	d.TopProducts, err = s.topProducts(&merchantID, 10)
	if err != nil {
		return nil, err
	}
	sales, err := s.SalesReport(SalesReportFilter{MerchantID: &merchantID})
	if err != nil {
		return nil, err
	}
	d.Sales = *sales
	return d, nil
}

// SalesReport 统计有效订单销售额及核销次数。
func (s *DashboardService) SalesReport(filter SalesReportFilter) (*SalesReport, error) {
	report := &SalesReport{MerchantID: filter.MerchantID}
	if filter.StartDate != nil {
		report.StartDate = filter.StartDate.Format("2006-01-02")
	}
	if filter.EndDate != nil {
		report.EndDate = filter.EndDate.Add(-24 * time.Hour).Format("2006-01-02")
	}
	if filter.MerchantID != nil {
		var mp model.MerchantProfile
		if err := s.freshDB().Model(&model.MerchantProfile{}).
			Select("shop_name").
			Where("is_deleted = ? AND id = ?", model.NotDeleted, *filter.MerchantID).
			First(&mp).Error; err == nil {
			report.MerchantName = mp.ShopName
		}
	}

	orderQ := s.validSalesOrderQuery(filter.MerchantID)
	orderQ = applySalesTimeRange(orderQ, filter.StartDate, filter.EndDate)

	if err := orderQ.Count(&report.ValidOrderCount).Error; err != nil {
		return nil, err
	}
	// Count 后再聚合金额：另起查询，避免复用已执行过的 statement
	sumQ := applySalesTimeRange(s.validSalesOrderQuery(filter.MerchantID), filter.StartDate, filter.EndDate)
	if err := sumQ.Select("COALESCE(SUM(pay_amount),0)").Scan(&report.TotalSalesAmount).Error; err != nil {
		return nil, err
	}

	vrQ := s.freshDB().Model(&model.VerificationRecord{}).Where("is_deleted = ?", model.NotDeleted)
	if filter.MerchantID != nil {
		vrQ = vrQ.Where("merchant_id = ?", *filter.MerchantID)
	}
	vrQ = applyVerifiedTimeRange(vrQ, filter.StartDate, filter.EndDate)
	if err := vrQ.Count(&report.VerificationCount).Error; err != nil {
		return nil, err
	}

	report.TotalSalesAmount = roundMoney(report.TotalSalesAmount)
	return report, nil
}

func (s *DashboardService) validSalesOrderQuery(merchantID *uint64) *gorm.DB {
	q := query.NotDeleted(s.freshDB().Model(&model.Order{})).
		Where("pay_status = ?", model.PayStatusPaid).
		Where("status NOT IN ?", invalidOrderStatusInts).
		Where("merchant_review_stage != ?", model.MerchantReviewRejected)
	if merchantID != nil {
		q = q.Where("merchant_id = ?", *merchantID)
	}
	return q
}

func applySalesTimeRange(q *gorm.DB, start, end *time.Time) *gorm.DB {
	if start != nil {
		q = q.Where("COALESCE(paid_at, created_at) >= ?", *start)
	}
	if end != nil {
		q = q.Where("COALESCE(paid_at, created_at) < ?", *end)
	}
	return q
}

func applyVerifiedTimeRange(q *gorm.DB, start, end *time.Time) *gorm.DB {
	if start != nil {
		q = q.Where("verified_at >= ?", *start)
	}
	if end != nil {
		q = q.Where("verified_at < ?", *end)
	}
	return q
}

func ParseSalesDateRange(startRaw, endRaw string) (start, end *time.Time, err error) {
	return parseSalesDateRange(startRaw, endRaw)
}

func parseSalesDateRange(startRaw, endRaw string) (start, end *time.Time, err error) {
	if startRaw == "" && endRaw == "" {
		return nil, nil, nil
	}
	loc := time.Local
	if startRaw != "" {
		t, parseErr := time.ParseInLocation("2006-01-02", startRaw, loc)
		if parseErr != nil {
			return nil, nil, parseErr
		}
		start = &t
	}
	if endRaw != "" {
		t, parseErr := time.ParseInLocation("2006-01-02", endRaw, loc)
		if parseErr != nil {
			return nil, nil, parseErr
		}
		endDay := t.Add(24 * time.Hour)
		end = &endDay
	} else if start != nil {
		endDay := start.Add(24 * time.Hour)
		end = &endDay
	}
	return start, end, nil
}

func todayRange() (time.Time, time.Time) {
	now := time.Now()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return start, start.Add(24 * time.Hour)
}

func (s *DashboardService) orderTrend(merchantID *uint64, days int) ([]DailyStat, error) {
	if days < 1 {
		days = 7
	}
	now := time.Now()
	loc := now.Location()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(days - 1))

	type row struct {
		Day         string
		OrderCount  int64
		SalesAmount float64
	}
	// 用 DATE_FORMAT 直接产出 YYYY-MM-DD 字符串，避免 parseTime 下 DATE() 扫成带时区的时间串导致按日对齐失败。
	dayExpr := "DATE_FORMAT(COALESCE(paid_at, created_at), '%Y-%m-%d')"
	q := query.NotDeleted(s.freshDB().Model(&model.Order{})).
		Where("pay_status = ?", model.PayStatusPaid).
		Where("status NOT IN ?", invalidOrderStatusInts).
		Where("merchant_review_stage != ?", model.MerchantReviewRejected).
		Select(dayExpr+" AS day, COUNT(*) AS order_count, COALESCE(SUM(pay_amount), 0) AS sales_amount").
		Where("COALESCE(paid_at, created_at) >= ?", start)
	if merchantID != nil {
		q = q.Where("merchant_id = ?", *merchantID)
	}
	var rows []row
	if err := q.Group(dayExpr).Order("day ASC").Scan(&rows).Error; err != nil {
		return nil, err
	}
	byDay := make(map[string]DailyStat, len(rows))
	for _, r := range rows {
		day := normalizeTrendDay(r.Day)
		if day == "" {
			continue
		}
		byDay[day] = DailyStat{
			Date:        day,
			OrderCount:  r.OrderCount,
			SalesAmount: roundMoney(r.SalesAmount),
		}
	}
	out := make([]DailyStat, 0, days)
	for i := 0; i < days; i++ {
		d := start.AddDate(0, 0, i).Format("2006-01-02")
		if stat, ok := byDay[d]; ok {
			out = append(out, stat)
		} else {
			out = append(out, DailyStat{Date: d})
		}
	}
	return out, nil
}

// normalizeTrendDay 统一为 YYYY-MM-DD，兼容驱动偶发返回带时间的 day 字段。
func normalizeTrendDay(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return ""
	}
	if len(s) >= 10 && s[4] == '-' && s[7] == '-' {
		return s[:10]
	}
	if t, err := time.ParseInLocation("2006-01-02", s, time.Local); err == nil {
		return t.Format("2006-01-02")
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.In(time.Local).Format("2006-01-02")
	}
	return ""
}

func (s *DashboardService) topProducts(merchantID *uint64, limit int) ([]ProductSalesRank, error) {
	if limit < 1 {
		limit = 10
	}
	type row struct {
		ProductID   uint64
		ProductName string
		MerchantID  uint64
		CoverURL    string
		SalesCount  uint32
	}
	q := s.freshDB().Model(&model.OrderItem{}).
		Select("order_item.product_id, product.name AS product_name, product.merchant_id, product.cover_url, SUM(order_item.quantity) AS sales_count").
		Joins("JOIN `order` ON `order`.id = order_item.order_id AND `order`.is_deleted = 0").
		Joins("JOIN product ON product.id = order_item.product_id AND product.is_deleted = 0").
		Where("order_item.is_deleted = 0").
		Where("`order`.pay_status = ?", model.PayStatusPaid).
		Where("`order`.status NOT IN ?", invalidOrderStatusInts).
		Where("`order`.merchant_review_stage != ?", model.MerchantReviewRejected)
	if merchantID != nil {
		q = q.Where("product.merchant_id = ?", *merchantID)
	}
	var rows []row
	if err := q.Group("order_item.product_id, product.name, product.merchant_id, product.cover_url").
		Order("sales_count DESC").Limit(limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ProductSalesRank, 0, len(rows))
	for _, r := range rows {
		out = append(out, ProductSalesRank{
			ProductID: r.ProductID, ProductName: r.ProductName,
			MerchantID: r.MerchantID, CoverURL: r.CoverURL, SalesCount: r.SalesCount,
		})
	}
	return out, nil
}

type CategoryService struct {
	DB *gorm.DB
}

func (s *CategoryService) List() ([]model.ProductCategory, error) {
	return s.ListByMerchant(0, true)
}

// ListByMerchant 某店铺的商品分类；visibleOnly=true 时仅返回 status=1。
func (s *CategoryService) ListByMerchant(merchantID uint64, visibleOnly bool) ([]model.ProductCategory, error) {
	q := query.NotDeleted(s.DB).Where("merchant_id = ?", merchantID).Order("sort_order ASC, id ASC")
	if visibleOnly {
		q = q.Where("status = 1")
	}
	var list []model.ProductCategory
	if err := q.Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (s *CategoryService) ListAll(status *uint8) ([]model.ProductCategory, error) {
	return s.ListAllScoped(nil, status)
}

func (s *CategoryService) ListAllScoped(merchantID *uint64, status *uint8) ([]model.ProductCategory, error) {
	q := query.NotDeleted(s.DB.Model(&model.ProductCategory{})).Order("sort_order ASC, id ASC")
	if merchantID != nil {
		q = q.Where("merchant_id = ?", *merchantID)
	}
	if status != nil {
		q = q.Where("status = ?", *status)
	}
	var list []model.ProductCategory
	if err := q.Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (s *CategoryService) GetByID(id uint64) (*model.ProductCategory, error) {
	var cat model.ProductCategory
	if err := query.NotDeleted(s.DB).First(&cat, id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrCategoryNotFound
		}
		return nil, err
	}
	return &cat, nil
}

func (s *CategoryService) GetByIDForMerchant(id, merchantID uint64) (*model.ProductCategory, error) {
	cat, err := s.GetByID(id)
	if err != nil {
		return nil, err
	}
	if cat.MerchantID != merchantID {
		return nil, ErrCategoryForbidden
	}
	return cat, nil
}

type CreateCategoryInput struct {
	MerchantID uint64
	ParentID   uint64
	Name       string
	IconURL    *string
	SortOrder  int
	Status     uint8
}

type UpdateCategoryInput struct {
	Name      *string
	IconURL   *string
	SortOrder *int
	Status    *uint8
}

func (s *CategoryService) Create(input CreateCategoryInput) (*model.ProductCategory, error) {
	name := strings.TrimSpace(input.Name)
	if name == "" || input.MerchantID == 0 {
		return nil, ErrInvalidProductArg
	}
	if utf8.RuneCountInString(name) > 64 {
		return nil, ErrInvalidProductArg
	}
	if input.ParentID > 0 {
		parent, err := s.GetByIDForMerchant(input.ParentID, input.MerchantID)
		if err != nil {
			return nil, err
		}
		if parent.ParentID != 0 {
			return nil, ErrInvalidProductArg
		}
	}
	status := input.Status
	if status == 0 {
		status = 1
	}
	cat := model.ProductCategory{
		MerchantID: input.MerchantID,
		ParentID:   input.ParentID, Name: name, IconURL: input.IconURL,
		SortOrder: input.SortOrder, Status: status,
	}
	if err := s.DB.Create(&cat).Error; err != nil {
		return nil, err
	}
	return &cat, nil
}

func (s *CategoryService) Update(id uint64, input UpdateCategoryInput) (*model.ProductCategory, error) {
	return s.UpdateForMerchant(id, 0, input, false)
}

func (s *CategoryService) UpdateForMerchant(id, merchantID uint64, input UpdateCategoryInput, scoped bool) (*model.ProductCategory, error) {
	var cat *model.ProductCategory
	var err error
	if scoped {
		cat, err = s.GetByIDForMerchant(id, merchantID)
	} else {
		cat, err = s.GetByID(id)
	}
	if err != nil {
		return nil, err
	}
	updates := map[string]interface{}{}
	if input.Name != nil {
		name := strings.TrimSpace(*input.Name)
		if name == "" {
			return nil, ErrInvalidProductArg
		}
		updates["name"] = name
	}
	if input.IconURL != nil {
		updates["icon_url"] = *input.IconURL
	}
	if input.SortOrder != nil {
		updates["sort_order"] = *input.SortOrder
	}
	if input.Status != nil {
		updates["status"] = *input.Status
	}
	if len(updates) == 0 {
		return cat, nil
	}
	if err := s.DB.Model(cat).Updates(updates).Error; err != nil {
		return nil, err
	}
	return s.GetByID(id)
}

func (s *CategoryService) Delete(id uint64) error {
	return s.DeleteForMerchant(id, 0, false)
}

func (s *CategoryService) DeleteForMerchant(id, merchantID uint64, scoped bool) error {
	var cat *model.ProductCategory
	var err error
	if scoped {
		cat, err = s.GetByIDForMerchant(id, merchantID)
	} else {
		cat, err = s.GetByID(id)
	}
	if err != nil {
		return err
	}
	return query.SoftDelete(s.DB, cat).Error
}

// FindOrCreateByName 按商家与名称查找一级分类，不存在则自动创建。
// merchantID=0 表示平台分类（套餐等）。
func (s *CategoryService) FindOrCreateByName(merchantID uint64, name string) (*model.ProductCategory, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrInvalidProductArg
	}
	if utf8.RuneCountInString(name) > 64 {
		return nil, ErrInvalidProductArg
	}

	var cat model.ProductCategory
	err := query.NotDeleted(s.DB).
		Where("merchant_id = ? AND parent_id = 0 AND name = ?", merchantID, name).
		First(&cat).Error
	if err == nil {
		return &cat, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	cat = model.ProductCategory{
		MerchantID: merchantID,
		ParentID:   0,
		Name:       name,
		Status:     1,
	}
	if err := s.DB.Create(&cat).Error; err != nil {
		return nil, err
	}
	return &cat, nil
}

// EnsureBelongsToMerchant 校验分类属于指定商家。
func (s *CategoryService) EnsureBelongsToMerchant(categoryID, merchantID uint64) error {
	cat, err := s.GetByID(categoryID)
	if err != nil {
		return err
	}
	if cat.MerchantID != merchantID {
		return ErrCategoryForbidden
	}
	return nil
}
