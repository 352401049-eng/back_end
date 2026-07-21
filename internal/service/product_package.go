package service

import (
	"fmt"
	"strings"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
)

type PackageItemInput struct {
	ProductID uint64 `json:"product_id"`
	MaxQty    uint32 `json:"max_qty"`
}

type PackageGroupInput struct {
	Name        string             `json:"name"`
	SelectCount uint32             `json:"select_count"`
	Items       []PackageItemInput `json:"items"`
}

type PackageItemView struct {
	ID           uint64  `json:"id"`
	ProductID    uint64  `json:"product_id"`
	MerchantID   uint64  `json:"merchant_id"`
	MerchantName string  `json:"merchant_name"`
	MaxQty       uint32  `json:"max_qty"`
	SortOrder    int     `json:"sort_order"`
	Name         string  `json:"name"`
	CoverURL     string  `json:"cover_url"`
	Price        float64 `json:"price"`
	Stock        uint32  `json:"stock"`
	Status       uint8   `json:"status"`
	ItemType     uint8   `json:"item_type"`
}

type PackageGroupView struct {
	ID          uint64            `json:"id"`
	Name        string            `json:"name"`
	SelectCount uint32            `json:"select_count"`
	SortOrder   int               `json:"sort_order"`
	Items       []PackageItemView `json:"items"`
}

func validatePackageGroupsInput(groups []PackageGroupInput) error {
	if len(groups) == 0 {
		return fmt.Errorf("%w: 套餐至少需要 1 个分组", ErrInvalidProductArg)
	}
	for i, g := range groups {
		name := strings.TrimSpace(g.Name)
		if name == "" {
			return fmt.Errorf("%w: 第 %d 组请填写名称", ErrInvalidProductArg, i+1)
		}
		if len(g.Items) == 0 {
			return fmt.Errorf("%w: 分组「%s」至少 1 个候选商品", ErrInvalidProductArg, name)
		}
		var maxSum uint32
		seen := map[uint64]struct{}{}
		for _, it := range g.Items {
			if it.ProductID == 0 {
				return fmt.Errorf("%w: 分组「%s」候选商品无效", ErrInvalidProductArg, name)
			}
			if _, ok := seen[it.ProductID]; ok {
				return fmt.Errorf("%w: 分组「%s」候选商品重复", ErrInvalidProductArg, name)
			}
			seen[it.ProductID] = struct{}{}
			mq := it.MaxQty
			if mq == 0 {
				mq = 1
			}
			maxSum += mq
		}
		sc := g.SelectCount
		if sc == 0 {
			sc = 1
		}
		if sc > maxSum {
			return fmt.Errorf("%w: 分组「%s」选 %d 份超过候选上限之和 %d", ErrInvalidProductArg, name, sc, maxSum)
		}
	}
	return nil
}

func (s *ProductService) replacePackageGroups(tx *gorm.DB, packageProductID uint64, groups []PackageGroupInput) error {
	if err := validatePackageGroupsInput(groups); err != nil {
		return err
	}

	var oldGroups []model.ProductPackageGroup
	if err := query.NotDeleted(tx).Where("package_product_id = ?", packageProductID).Find(&oldGroups).Error; err != nil {
		return err
	}
	for _, g := range oldGroups {
		if err := query.SoftDelete(tx, &model.ProductPackageItem{}, "group_id = ?", g.ID).Error; err != nil {
			return err
		}
		if err := query.SoftDelete(tx, &g).Error; err != nil {
			return err
		}
	}

	for gi, g := range groups {
		sc := g.SelectCount
		if sc == 0 {
			sc = 1
		}
		group := model.ProductPackageGroup{
			PackageProductID: packageProductID,
			Name:             strings.TrimSpace(g.Name),
			SelectCount:      sc,
			SortOrder:        gi,
		}
		if err := tx.Create(&group).Error; err != nil {
			return fmt.Errorf("创建套餐分组失败: %w", err)
		}
		for ii, it := range g.Items {
			var p model.Product
			if err := query.NotDeleted(tx).First(&p, it.ProductID).Error; err != nil {
				return fmt.Errorf("%w: 候选商品 %d 不存在", ErrInvalidProductArg, it.ProductID)
			}
			if p.ItemType == model.ProductItemTypePackage {
				return fmt.Errorf("%w: 不能将套餐嵌套进套餐", ErrInvalidProductArg)
			}
			if p.Status != model.ProductStatusOn {
				return fmt.Errorf("%w: 候选商品「%s」未上架", ErrInvalidProductArg, p.Name)
			}
			mq := it.MaxQty
			if mq == 0 {
				mq = 1
			}
			row := model.ProductPackageItem{
				GroupID:    group.ID,
				ProductID:  p.ID,
				MerchantID: p.MerchantID,
				MaxQty:     mq,
				SortOrder:  ii,
			}
			if err := tx.Create(&row).Error; err != nil {
				return fmt.Errorf("创建套餐候选失败: %w", err)
			}
		}
	}
	return nil
}

func (s *ProductService) LoadPackageGroups(packageProductID uint64) ([]PackageGroupView, error) {
	var groups []model.ProductPackageGroup
	if err := query.NotDeleted(s.DB).
		Where("package_product_id = ?", packageProductID).
		Order("sort_order ASC, id ASC").
		Find(&groups).Error; err != nil {
		return nil, err
	}
	out := make([]PackageGroupView, 0, len(groups))
	for _, g := range groups {
		var items []model.ProductPackageItem
		if err := query.NotDeleted(s.DB).
			Where("group_id = ?", g.ID).
			Preload("Product", "is_deleted = ?", model.NotDeleted).
			Order("sort_order ASC, id ASC").
			Find(&items).Error; err != nil {
			return nil, err
		}
		gv := PackageGroupView{
			ID: g.ID, Name: g.Name, SelectCount: g.SelectCount, SortOrder: g.SortOrder,
			Items: make([]PackageItemView, 0, len(items)),
		}
		for _, it := range items {
			iv := PackageItemView{
				ID: it.ID, ProductID: it.ProductID, MerchantID: it.MerchantID,
				MaxQty: it.MaxQty, SortOrder: it.SortOrder,
			}
			if it.Product != nil {
				iv.Name = it.Product.Name
				iv.CoverURL = it.Product.CoverURL
				iv.Price = it.Product.Price
				iv.Stock = it.Product.Stock
				iv.Status = it.Product.Status
				iv.ItemType = it.Product.ItemType
				iv.MerchantID = it.Product.MerchantID
			}
			if iv.MerchantID > 0 {
				var m model.MerchantProfile
				if err := query.NotDeleted(s.DB).Select("id", "shop_name").First(&m, iv.MerchantID).Error; err == nil {
					iv.MerchantName = m.ShopName
				}
			}
			gv.Items = append(gv.Items, iv)
		}
		out = append(out, gv)
	}
	return out, nil
}
