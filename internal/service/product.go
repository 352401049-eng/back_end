package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
)

type ProductService struct {
	DB          *gorm.DB
	CategorySvc *CategoryService
}

type ProductInput struct {
	MerchantID     uint64
	CategoryID     uint64
	CategoryName   string
	Name           string
	Description    *string
	CoverURL            string
	Images              []string
	Price               float64
	OriginalPrice  *float64
	Stock          uint32
	IsHot               uint8
	EnableGroupBuy      uint8
	EnableCoupon        uint8
	AllowPickup         uint8
	AllowDelivery       uint8
	GroupBuyTargetCount *uint32
	GroupBuyPrice       *float64
	GroupBuyAllowRepeat uint8
	ItemType            uint8
	Status              uint8
	PackageGroups       []PackageGroupInput // item_type=套餐时必填
}

type GroupBuyConfigInput struct {
	EnableGroupBuy      uint8
	GroupBuyTargetCount *uint32
	GroupBuyPrice       *float64
	GroupBuyAllowRepeat *uint8
}

type ProductListFilter struct {
	MerchantID         *uint64
	CategoryID         *uint64
	Status             *uint8
	Keyword            string
	EnableGroupBuyOnly bool
	AllowPickupOnly    bool
	ExcludePackage     bool // 选品辅助：排除套餐
	ItemType           *uint8
}

// ProductDetailView 商品详情（管理端/用户端），套餐附带分组。
type ProductDetailView struct {
	model.Product
	PackageGroups []PackageGroupView `json:"package_groups,omitempty"`
	CanGroupBuy   bool               `json:"can_group_buy"`
	CanUseCoupon  bool               `json:"can_use_coupon"`
	GroupBuyID    *uint64            `json:"group_buy_id,omitempty"`
	SaleOptions   ProductSaleOptions `json:"sale_options"`
}

func (s *ProductService) Create(input ProductInput, scopeMerchantID *uint64) (*model.Product, error) {
	if err := s.validateInput(input); err != nil {
		return nil, err
	}
	isPackage := input.ItemType == model.ProductItemTypePackage
	if isPackage && len(input.PackageGroups) == 0 {
		return nil, fmt.Errorf("%w: 请配置套餐分组（固定包含或可选）", ErrInvalidProductArg)
	}
	merchantID := input.MerchantID
	if scopeMerchantID != nil {
		merchantID = *scopeMerchantID
	}
	if merchantID == 0 {
		return nil, fmt.Errorf("%w: 请指定所属商家", ErrInvalidProductArg)
	}
	if err := s.ensureMerchantExists(merchantID); err != nil {
		return nil, err
	}
	if isPackage && strings.TrimSpace(input.CategoryName) == "" && input.CategoryID == 0 {
		input.CategoryName = "套餐"
	}
	categoryID, err := s.resolveCategoryID(input, merchantID, 0)
	if err != nil {
		return nil, err
	}

	product := model.Product{
		MerchantID:          merchantID,
		CategoryID:          categoryID,
		Name:                input.Name,
		Description:         input.Description,
		CoverURL:            resolveProductCover(input.CoverURL, input.Images),
		Images:              input.Images,
		Price:               input.Price,
		OriginalPrice:       input.OriginalPrice,
		Stock:               input.Stock,
		IsHot:               input.IsHot,
		EnableGroupBuy:      input.EnableGroupBuy,
		EnableCoupon:        normalizeEnableCoupon(input.EnableCoupon),
		AllowPickup:         normalizeAllowPickup(input.AllowPickup),
		AllowDelivery:       normalizeAllowDelivery(input.AllowDelivery),
		GroupBuyTargetCount: input.GroupBuyTargetCount,
		GroupBuyPrice:       input.GroupBuyPrice,
		GroupBuyAllowRepeat: normalizeGroupBuyAllowRepeat(input.GroupBuyAllowRepeat),
		ItemType:            input.ItemType,
		Status:              input.Status,
	}
	if product.EnableGroupBuy != 1 {
		product.GroupBuyTargetCount = nil
		product.GroupBuyPrice = nil
		product.GroupBuyAllowRepeat = 0
	}
	if product.ItemType == 0 {
		product.ItemType = model.ProductItemTypePhysical
	}
	if product.Status != model.ProductStatusOff && product.Status != model.ProductStatusOn {
		product.Status = model.ProductStatusOff
	}

	err = s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&product).Error; err != nil {
			return fmt.Errorf("创建商品失败: %w", err)
		}
		if isPackage {
			if err := s.replacePackageGroups(tx, product.ID, merchantID, input.PackageGroups); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := s.syncGroupBuy(&product); err != nil {
		return nil, fmt.Errorf("同步拼团配置失败: %w", err)
	}
	return s.GetByID(product.ID, scopeMerchantID)
}

func (s *ProductService) GetByID(id uint64, scopeMerchantID *uint64) (*model.Product, error) {
	var product model.Product
	q := query.NotDeleted(s.DB).Preload("Category", "is_deleted = ?", model.NotDeleted).Preload("Merchant", "is_deleted = ?", model.NotDeleted).Where("id = ?", id)
	if scopeMerchantID != nil {
		q = q.Where("merchant_id = ?", *scopeMerchantID)
	}
	if err := q.First(&product).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			if scopeMerchantID != nil {
				return nil, ErrProductForbidden
			}
			return nil, ErrProductNotFound
		}
		return nil, err
	}
	return &product, nil
}

func (s *ProductService) List(page, pageSize int, filter ProductListFilter) ([]model.Product, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 50 {
		pageSize = 50
	}
	if filter.MerchantID != nil && filter.CategoryID != nil {
		if err := s.ensureCategoryBelongsToMerchant(*filter.CategoryID, *filter.MerchantID); err != nil {
			return nil, 0, err
		}
	}
	offset := (page - 1) * pageSize

	q := query.NotDeleted(s.DB.Model(&model.Product{}))
	if filter.MerchantID != nil {
		q = q.Where("merchant_id = ?", *filter.MerchantID)
	}
	if filter.CategoryID != nil {
		q = q.Where("category_id = ?", *filter.CategoryID)
	}
	if filter.Status != nil {
		q = q.Where("status = ?", *filter.Status)
	}
	if filter.Keyword != "" {
		q = q.Where("name LIKE ?", "%"+filter.Keyword+"%")
	}
	if filter.EnableGroupBuyOnly {
		q = q.Where("enable_group_buy = 1 AND group_buy_price IS NOT NULL AND group_buy_target_count >= 2")
	}
	if filter.AllowPickupOnly {
		q = q.Where("allow_pickup = 1")
	}
	if filter.ExcludePackage {
		q = q.Where("item_type <> ?", model.ProductItemTypePackage)
	}
	if filter.ItemType != nil {
		q = q.Where("item_type = ?", *filter.ItemType)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var list []model.Product
	if err := q.Preload("Category", "is_deleted = ?", model.NotDeleted).Preload("Merchant", "is_deleted = ?", model.NotDeleted).
		Order("id DESC").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (s *ProductService) Update(id uint64, input ProductInput, scopeMerchantID *uint64) (*model.Product, error) {
	product, err := s.GetByID(id, scopeMerchantID)
	if err != nil {
		return nil, err
	}
	if err := s.validateInput(input); err != nil {
		return nil, err
	}

	isPackage := input.ItemType == model.ProductItemTypePackage || product.ItemType == model.ProductItemTypePackage
	if product.ItemType == model.ProductItemTypePackage {
		input.ItemType = model.ProductItemTypePackage
		isPackage = true
	}
	if isPackage && input.ItemType != model.ProductItemTypePackage {
		return nil, fmt.Errorf("%w: 套餐不可改为普通商品", ErrInvalidProductArg)
	}
	if !isPackage && input.ItemType == model.ProductItemTypePackage {
		return nil, fmt.Errorf("%w: 普通商品不可改为套餐", ErrInvalidProductArg)
	}

	merchantID := product.MerchantID
	if scopeMerchantID == nil && input.MerchantID > 0 {
		merchantID = input.MerchantID
		if err := s.ensureMerchantExists(merchantID); err != nil {
			return nil, err
		}
	}
	if isPackage && merchantID == 0 {
		return nil, fmt.Errorf("%w: 店内套餐须指定商家", ErrInvalidProductArg)
	}
	if isPackage && strings.TrimSpace(input.CategoryName) == "" && input.CategoryID == 0 {
		input.CategoryID = product.CategoryID
	}
	categoryID, err := s.resolveCategoryID(input, merchantID, product.CategoryID)
	if err != nil {
		return nil, err
	}

	images := input.Images
	if len(images) == 0 {
		images = product.Images
	}
	cover := resolveProductCover(input.CoverURL, input.Images)
	if cover == "" {
		cover = product.CoverURL
	}

	updates := map[string]interface{}{
		"merchant_id":            merchantID,
		"category_id":            categoryID,
		"name":                   input.Name,
		"description":            input.Description,
		"cover_url":              cover,
		"images":                 toJSONColumn(images),
		"price":                  input.Price,
		"original_price":         input.OriginalPrice,
		"stock":                  input.Stock,
		"is_hot":                 input.IsHot,
		"enable_group_buy":       input.EnableGroupBuy,
		"enable_coupon":          normalizeEnableCoupon(input.EnableCoupon),
		"allow_pickup":           normalizeAllowPickup(input.AllowPickup),
		"allow_delivery":         normalizeAllowDelivery(input.AllowDelivery),
		"group_buy_target_count": input.GroupBuyTargetCount,
		"group_buy_price":        input.GroupBuyPrice,
		"group_buy_allow_repeat": normalizeGroupBuyAllowRepeat(input.GroupBuyAllowRepeat),
		"item_type":              input.ItemType,
		"status":                 input.Status,
	}
	if input.EnableGroupBuy != 1 {
		updates["group_buy_target_count"] = nil
		updates["group_buy_price"] = nil
		updates["group_buy_allow_repeat"] = 0
	}
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(product).Updates(updates).Error; err != nil {
			return fmt.Errorf("更新商品失败: %w", err)
		}
		if isPackage && len(input.PackageGroups) > 0 {
			if err := s.replacePackageGroups(tx, id, merchantID, input.PackageGroups); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	product.EnableGroupBuy = input.EnableGroupBuy
	product.EnableCoupon = normalizeEnableCoupon(input.EnableCoupon)
	product.AllowPickup = normalizeAllowPickup(input.AllowPickup)
	product.AllowDelivery = normalizeAllowDelivery(input.AllowDelivery)
	product.GroupBuyTargetCount = input.GroupBuyTargetCount
	product.GroupBuyPrice = input.GroupBuyPrice
	product.GroupBuyAllowRepeat = normalizeGroupBuyAllowRepeat(input.GroupBuyAllowRepeat)
	if input.EnableGroupBuy != 1 {
		product.GroupBuyTargetCount = nil
		product.GroupBuyPrice = nil
		product.GroupBuyAllowRepeat = 0
	}
	if err := s.syncGroupBuy(product); err != nil {
		return nil, fmt.Errorf("同步拼团配置失败: %w", err)
	}
	return s.GetByID(id, scopeMerchantID)
}

func (s *ProductService) UpdateImages(id uint64, images []string, coverURL *string, scopeMerchantID *uint64) (*model.Product, error) {
	if len(images) == 0 {
		return nil, ErrInvalidProductArg
	}
	if _, err := s.GetByID(id, scopeMerchantID); err != nil {
		return nil, err
	}
	cover := images[0]
	if coverURL != nil && *coverURL != "" {
		cover = *coverURL
	}
	if err := s.DB.Model(&model.Product{}).Where("id = ?", id).Updates(map[string]interface{}{
		"images": toJSONColumn(images), "cover_url": cover,
	}).Error; err != nil {
		return nil, fmt.Errorf("更新商品图片失败: %w", err)
	}
	return s.GetByID(id, scopeMerchantID)
}

func (s *ProductService) UpdateStatus(id uint64, status uint8, scopeMerchantID *uint64) (*model.Product, error) {
	if status != model.ProductStatusOff && status != model.ProductStatusOn {
		return nil, ErrInvalidProductArg
	}
	if _, err := s.GetByID(id, scopeMerchantID); err != nil {
		return nil, err
	}
	if err := s.DB.Model(&model.Product{}).Where("id = ?", id).Update("status", status).Error; err != nil {
		return nil, err
	}
	return s.GetByID(id, scopeMerchantID)
}

func (s *ProductService) UpdatePrice(id uint64, price float64, originalPrice *float64, scopeMerchantID *uint64) (*model.Product, error) {
	if price <= 0 {
		return nil, ErrInvalidProductArg
	}
	product, err := s.GetByID(id, scopeMerchantID)
	if err != nil {
		return nil, err
	}
	if product.EnableGroupBuy == 1 && product.GroupBuyPrice != nil && price <= *product.GroupBuyPrice {
		return nil, ErrInvalidProductArg
	}
	updates := map[string]interface{}{"price": price, "original_price": originalPrice}
	if err := s.DB.Model(&model.Product{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return nil, err
	}
	return s.GetByID(id, scopeMerchantID)
}

func (s *ProductService) UpdateGroupBuy(id uint64, input GroupBuyConfigInput, scopeMerchantID *uint64) (*model.Product, error) {
	product, err := s.GetByID(id, scopeMerchantID)
	if err != nil {
		return nil, err
	}
	targetCount := input.GroupBuyTargetCount
	groupPrice := input.GroupBuyPrice
	allowRepeat := product.GroupBuyAllowRepeat
	if input.EnableGroupBuy == 1 {
		if targetCount == nil {
			targetCount = product.GroupBuyTargetCount
		}
		if groupPrice == nil {
			groupPrice = product.GroupBuyPrice
		}
		if input.GroupBuyAllowRepeat != nil {
			allowRepeat = normalizeGroupBuyAllowRepeat(*input.GroupBuyAllowRepeat)
		}
	}
	check := ProductInput{
		Price:               product.Price,
		EnableGroupBuy:      input.EnableGroupBuy,
		GroupBuyTargetCount: targetCount,
		GroupBuyPrice:       groupPrice,
		GroupBuyAllowRepeat: allowRepeat,
	}
	if err := validateGroupBuyConfig(check); err != nil {
		return nil, err
	}

	updates := map[string]interface{}{
		"enable_group_buy": input.EnableGroupBuy,
	}
	if input.EnableGroupBuy == 1 {
		updates["group_buy_target_count"] = targetCount
		updates["group_buy_price"] = groupPrice
		updates["group_buy_allow_repeat"] = allowRepeat
	} else {
		updates["group_buy_target_count"] = nil
		updates["group_buy_price"] = nil
		updates["group_buy_allow_repeat"] = 0
	}
	if err := s.DB.Model(product).Updates(updates).Error; err != nil {
		return nil, fmt.Errorf("更新拼团配置失败: %w", err)
	}
	product.EnableGroupBuy = input.EnableGroupBuy
	product.GroupBuyTargetCount = targetCount
	product.GroupBuyPrice = groupPrice
	product.GroupBuyAllowRepeat = allowRepeat
	if input.EnableGroupBuy != 1 {
		product.GroupBuyTargetCount = nil
		product.GroupBuyPrice = nil
		product.GroupBuyAllowRepeat = 0
	}
	if err := s.syncGroupBuy(product); err != nil {
		return nil, fmt.Errorf("同步拼团配置失败: %w", err)
	}
	return s.GetByID(id, scopeMerchantID)
}

func (s *ProductService) UpdateCoupon(id uint64, enableCoupon uint8, scopeMerchantID *uint64) (*model.Product, error) {
	if enableCoupon != 0 && enableCoupon != 1 {
		return nil, ErrInvalidProductArg
	}
	if _, err := s.GetByID(id, scopeMerchantID); err != nil {
		return nil, err
	}
	if err := s.DB.Model(&model.Product{}).Where("id = ?", id).
		Update("enable_coupon", normalizeEnableCoupon(enableCoupon)).Error; err != nil {
		return nil, err
	}
	return s.GetByID(id, scopeMerchantID)
}

type UpdateProductSaleInput struct {
	EnableGroupBuy      *uint8
	EnableCoupon        *uint8
	GroupBuyTargetCount *uint32
	GroupBuyPrice       *float64
	GroupBuyAllowRepeat *uint8
}

// UpdateSaleOptions 一次性更新拼团与优惠券配置（商品编辑页）。
func (s *ProductService) UpdateSaleOptions(id uint64, input UpdateProductSaleInput, scopeMerchantID *uint64) (*model.Product, error) {
	product, err := s.GetByID(id, scopeMerchantID)
	if err != nil {
		return nil, err
	}

	if input.EnableGroupBuy != nil || input.GroupBuyTargetCount != nil || input.GroupBuyPrice != nil || input.GroupBuyAllowRepeat != nil {
		enable := product.EnableGroupBuy
		if input.EnableGroupBuy != nil {
			enable = *input.EnableGroupBuy
		}
		target := input.GroupBuyTargetCount
		if target == nil {
			target = product.GroupBuyTargetCount
		}
		price := input.GroupBuyPrice
		if price == nil {
			price = product.GroupBuyPrice
		}
		var allowRepeat *uint8
		if input.GroupBuyAllowRepeat != nil {
			allowRepeat = input.GroupBuyAllowRepeat
		} else {
			v := product.GroupBuyAllowRepeat
			allowRepeat = &v
		}
		if _, err := s.UpdateGroupBuy(id, GroupBuyConfigInput{
			EnableGroupBuy:      enable,
			GroupBuyTargetCount: target,
			GroupBuyPrice:       price,
			GroupBuyAllowRepeat: allowRepeat,
		}, scopeMerchantID); err != nil {
			return nil, err
		}
	}

	if input.EnableCoupon != nil {
		if _, err := s.UpdateCoupon(id, *input.EnableCoupon, scopeMerchantID); err != nil {
			return nil, err
		}
	}

	return s.GetByID(id, scopeMerchantID)
}

func (s *ProductService) UpdateStock(id uint64, stock uint32, scopeMerchantID *uint64) (*model.Product, error) {
	if _, err := s.GetByID(id, scopeMerchantID); err != nil {
		return nil, err
	}
	if err := s.DB.Model(&model.Product{}).Where("id = ?", id).Update("stock", stock).Error; err != nil {
		return nil, err
	}
	return s.GetByID(id, scopeMerchantID)
}

func (s *ProductService) validateInput(input ProductInput) error {
	cover := resolveProductCover(input.CoverURL, input.Images)
	if input.Name == "" || cover == "" {
		return ErrInvalidProductArg
	}
	isPackage := input.ItemType == model.ProductItemTypePackage
	if !isPackage && input.CategoryID == 0 && strings.TrimSpace(input.CategoryName) == "" {
		return ErrInvalidProductArg
	}
	if input.Price <= 0 {
		return ErrInvalidProductArg
	}
	if input.ItemType != 0 &&
		input.ItemType != model.ProductItemTypePhysical &&
		input.ItemType != model.ProductItemTypeVirtual &&
		input.ItemType != model.ProductItemTypePackage {
		return ErrInvalidProductArg
	}
	if isPackage {
		// 创建时必须带分组；更新时未传则保留原分组
		if len(input.PackageGroups) > 0 {
			if err := validatePackageGroupsInput(input.PackageGroups); err != nil {
				return err
			}
		}
	}
	if input.EnableCoupon != 0 && input.EnableCoupon != 1 {
		return ErrInvalidProductArg
	}
	if input.GroupBuyAllowRepeat != 0 && input.GroupBuyAllowRepeat != 1 {
		return ErrInvalidProductArg
	}
	return validateGroupBuyConfig(input)
}

// GetDetailView 商品详情（含套餐分组）。
func (s *ProductService) GetDetailView(id uint64, scopeMerchantID *uint64) (*ProductDetailView, error) {
	product, err := s.GetByID(id, scopeMerchantID)
	if err != nil {
		return nil, err
	}
	return s.toDetailView(product)
}

// GetOnShelfPublic 上架商品详情（支持平台套餐 merchant_id=0）。
func (s *ProductService) GetOnShelfPublic(id uint64) (*ProductDetailView, error) {
	var product model.Product
	if err := query.NotDeleted(s.DB).Preload("Category", "is_deleted = ?", model.NotDeleted).
		Where("id = ? AND status = ?", id, model.ProductStatusOn).
		First(&product).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrProductNotFound
		}
		return nil, err
	}
	if product.MerchantID > 0 {
		if err := s.ensureMerchantOpen(product.MerchantID); err != nil {
			return nil, err
		}
	}
	return s.toDetailView(&product)
}

func (s *ProductService) toDetailView(product *model.Product) (*ProductDetailView, error) {
	store := s.ToStoreView(product)
	view := &ProductDetailView{
		Product:      *product,
		CanGroupBuy:  store.CanGroupBuy,
		CanUseCoupon: store.CanUseCoupon,
		GroupBuyID:   store.GroupBuyID,
		SaleOptions:  store.SaleOptions,
	}
	if product.ItemType == model.ProductItemTypePackage {
		groups, err := s.LoadPackageGroups(product.ID)
		if err != nil {
			return nil, err
		}
		view.PackageGroups = groups
	}
	return view, nil
}

func validateGroupBuyConfig(input ProductInput) error {
	if input.EnableGroupBuy == 1 {
		if input.GroupBuyTargetCount == nil || *input.GroupBuyTargetCount < 2 {
			return ErrInvalidProductArg
		}
		if input.GroupBuyPrice == nil || *input.GroupBuyPrice <= 0 {
			return ErrInvalidProductArg
		}
		if *input.GroupBuyPrice >= input.Price {
			return ErrInvalidProductArg
		}
	}
	return nil
}

func resolveProductCover(cover string, images []string) string {
	if cover != "" {
		return cover
	}
	if len(images) > 0 {
		return images[0]
	}
	return ""
}

func normalizeEnableCoupon(v uint8) uint8 {
	if v == 0 {
		return 0
	}
	return 1
}

func normalizeAllowPickup(v uint8) uint8 {
	if v == 0 {
		return 0
	}
	return 1
}

func normalizeAllowDelivery(v uint8) uint8 {
	if v == 0 {
		return 0
	}
	return 1
}

func normalizeGroupBuyAllowRepeat(v uint8) uint8 {
	if v == 1 {
		return 1
	}
	return 0
}

// syncGroupBuy 将商品拼团配置同步到 group_buy 表，供购物车/下单引用 group_buy_id。
func (s *ProductService) syncGroupBuy(product *model.Product) error {
	if product.EnableGroupBuy != 1 || product.GroupBuyTargetCount == nil || product.GroupBuyPrice == nil {
		return query.NotDeleted(s.DB.Model(&model.GroupBuy{})).
			Where("product_id = ?", product.ID).
			Update("status", 0).Error
	}

	now := time.Now()
	endAt := now.AddDate(10, 0, 0)
	var gb model.GroupBuy
	err := query.NotDeleted(s.DB).Where("product_id = ?", product.ID).First(&gb).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		gb = model.GroupBuy{
			ProductID:   product.ID,
			TargetCount: *product.GroupBuyTargetCount,
			GroupPrice:  *product.GroupBuyPrice,
			StartAt:     now,
			EndAt:       endAt,
			Status:      1,
		}
		return s.DB.Create(&gb).Error
	}
	if err != nil {
		return err
	}
	return s.DB.Model(&gb).Updates(map[string]interface{}{
		"target_count": *product.GroupBuyTargetCount,
		"group_price":  *product.GroupBuyPrice,
		"status":       1,
		"end_at":       endAt,
	}).Error
}

func (s *ProductService) ensureMerchantExists(merchantID uint64) error {
	var count int64
	if err := query.NotDeleted(s.DB.Model(&model.MerchantProfile{})).Where("id = ?", merchantID).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return ErrMerchantNotFound
	}
	return nil
}

func (s *ProductService) ensureCategoryBelongsToMerchant(categoryID, merchantID uint64) error {
	if s.CategorySvc != nil {
		return s.CategorySvc.EnsureBelongsToMerchant(categoryID, merchantID)
	}
	var cat model.ProductCategory
	if err := query.NotDeleted(s.DB).First(&cat, categoryID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrCategoryNotFound
		}
		return err
	}
	if cat.MerchantID != merchantID {
		return ErrCategoryForbidden
	}
	return nil
}

func (s *ProductService) resolveCategoryID(input ProductInput, merchantID, fallbackID uint64) (uint64, error) {
	if input.CategoryID > 0 {
		if err := s.ensureCategoryBelongsToMerchant(input.CategoryID, merchantID); err != nil {
			return 0, err
		}
		return input.CategoryID, nil
	}
	if name := strings.TrimSpace(input.CategoryName); name != "" {
		if s.CategorySvc == nil {
			return 0, ErrInvalidProductArg
		}
		cat, err := s.CategorySvc.FindOrCreateByName(merchantID, name)
		if err != nil {
			return 0, err
		}
		return cat.ID, nil
	}
	if fallbackID > 0 {
		if err := s.ensureCategoryBelongsToMerchant(fallbackID, merchantID); err != nil {
			return 0, err
		}
		return fallbackID, nil
	}
	return 0, ErrInvalidProductArg
}

// ListOnShelfByMerchant 某营业商家的上架商品列表（用户端）。
func (s *ProductService) ListOnShelfByMerchant(merchantID uint64, page, pageSize int, filter ProductListFilter) ([]model.Product, int64, error) {
	if err := s.ensureMerchantOpen(merchantID); err != nil {
		return nil, 0, err
	}
	if filter.CategoryID != nil {
		if err := s.ensureCategoryBelongsToMerchant(*filter.CategoryID, merchantID); err != nil {
			return nil, 0, err
		}
	}
	onShelf := uint8(model.ProductStatusOn)
	filter.MerchantID = &merchantID
	filter.Status = &onShelf
	return s.List(page, pageSize, filter)
}

// GetOnShelf 上架商品详情（用户端）。
func (s *ProductService) GetOnShelf(id, merchantID uint64) (*model.Product, error) {
	if err := s.ensureMerchantOpen(merchantID); err != nil {
		return nil, err
	}
	var product model.Product
	if err := query.NotDeleted(s.DB).Preload("Category", "is_deleted = ?", model.NotDeleted).
		Where("id = ? AND merchant_id = ? AND status = ?", id, merchantID, model.ProductStatusOn).
		First(&product).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrProductNotFound
		}
		return nil, err
	}
	return &product, nil
}

func (s *ProductService) ensureMerchantOpen(merchantID uint64) error {
	var count int64
	if err := query.NotDeleted(s.DB.Model(&model.MerchantProfile{})).
		Where("id = ? AND status = ?", merchantID, model.MerchantStatusOpen).
		Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return ErrMerchantNotFound
	}
	return nil
}
