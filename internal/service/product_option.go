package service

import (
	"errors"
	"fmt"
	"strings"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
)

var (
	ErrOptionInvalid             = errors.New("option invalid")
	ErrOptionRequired            = errors.New("option required")
	ErrOptionNotAllowedOnPackage = errors.New("option not allowed on package")
)

const (
	maxOptionGroupsPerProduct = 10
	minOptionItemsPerGroup    = 2
	maxOptionItemsPerGroup    = 20
)

type OptionGroupInput struct {
	Title     string            `json:"title"`
	SortOrder int               `json:"sort_order"`
	Items     []OptionItemInput `json:"items"`
}

type OptionItemInput struct {
	Label     string `json:"label"`
	SortOrder int    `json:"sort_order"`
}

type OptionSelectionUnitInput struct {
	UnitIndex uint32                      `json:"unit_index"`
	ProductID uint64                      `json:"product_id,omitempty"`
	Groups    []OptionSelectionGroupInput `json:"groups"`
}

type OptionSelectionGroupInput struct {
	GroupID  uint64 `json:"group_id"`
	OptionID uint64 `json:"option_id"`
}

func validateOptionGroupsForSave(itemType uint8, groups []OptionGroupInput) error {
	if itemType == model.ProductItemTypePackage && len(groups) > 0 {
		return ErrOptionNotAllowedOnPackage
	}
	if len(groups) > maxOptionGroupsPerProduct {
		return fmt.Errorf("%w: 最多 %d 个规格组", ErrOptionInvalid, maxOptionGroupsPerProduct)
	}
	for i, g := range groups {
		title := strings.TrimSpace(g.Title)
		if title == "" {
			return fmt.Errorf("%w: 第 %d 组请填写标题", ErrOptionInvalid, i+1)
		}
		if len(g.Items) < minOptionItemsPerGroup {
			return fmt.Errorf("%w: 分组「%s」至少 %d 个选项", ErrOptionInvalid, title, minOptionItemsPerGroup)
		}
		if len(g.Items) > maxOptionItemsPerGroup {
			return fmt.Errorf("%w: 分组「%s」最多 %d 个选项", ErrOptionInvalid, title, maxOptionItemsPerGroup)
		}
		seen := map[string]struct{}{}
		for _, it := range g.Items {
			label := strings.TrimSpace(it.Label)
			if label == "" {
				return fmt.Errorf("%w: 分组「%s」选项文案不能为空", ErrOptionInvalid, title)
			}
			if _, ok := seen[label]; ok {
				return fmt.Errorf("%w: 分组「%s」选项「%s」重复", ErrOptionInvalid, title, label)
			}
			seen[label] = struct{}{}
		}
	}
	return nil
}

func (s *ProductService) ListOptionGroups(productID uint64) ([]model.ProductOptionGroup, error) {
	var groups []model.ProductOptionGroup
	if err := query.NotDeleted(s.DB).
		Where("product_id = ?", productID).
		Order("sort_order ASC, id ASC").
		Find(&groups).Error; err != nil {
		return nil, err
	}
	for i := range groups {
		var items []model.ProductOptionItem
		if err := query.NotDeleted(s.DB).
			Where("group_id = ?", groups[i].ID).
			Order("sort_order ASC, id ASC").
			Find(&items).Error; err != nil {
			return nil, err
		}
		groups[i].Items = items
	}
	return groups, nil
}

func loadOptionGroupsInTx(tx *gorm.DB, productID uint64) ([]model.ProductOptionGroup, error) {
	var groups []model.ProductOptionGroup
	if err := query.NotDeleted(tx).
		Where("product_id = ?", productID).
		Order("sort_order ASC, id ASC").
		Find(&groups).Error; err != nil {
		return nil, err
	}
	for i := range groups {
		var items []model.ProductOptionItem
		if err := query.NotDeleted(tx).
			Where("group_id = ?", groups[i].ID).
			Order("sort_order ASC, id ASC").
			Find(&items).Error; err != nil {
			return nil, err
		}
		groups[i].Items = items
	}
	return groups, nil
}

func (s *ProductService) ReplaceOptionGroups(tx *gorm.DB, productID uint64, itemType uint8, groups []OptionGroupInput) error {
	if err := validateOptionGroupsForSave(itemType, groups); err != nil {
		return err
	}

	var oldGroups []model.ProductOptionGroup
	if err := query.NotDeleted(tx).Where("product_id = ?", productID).Find(&oldGroups).Error; err != nil {
		return err
	}
	for _, g := range oldGroups {
		if err := query.SoftDelete(tx, &model.ProductOptionItem{}, "group_id = ?", g.ID).Error; err != nil {
			return err
		}
		if err := query.SoftDelete(tx, &g).Error; err != nil {
			return err
		}
	}

	for gi, g := range groups {
		sortOrder := g.SortOrder
		if sortOrder == 0 {
			sortOrder = gi
		}
		group := model.ProductOptionGroup{
			ProductID: productID,
			Title:     strings.TrimSpace(g.Title),
			SortOrder: sortOrder,
		}
		if err := tx.Create(&group).Error; err != nil {
			return fmt.Errorf("创建规格组失败: %w", err)
		}
		for ii, it := range g.Items {
			itemSort := it.SortOrder
			if itemSort == 0 {
				itemSort = ii
			}
			row := model.ProductOptionItem{
				GroupID:   group.ID,
				Label:     strings.TrimSpace(it.Label),
				SortOrder: itemSort,
			}
			if err := tx.Create(&row).Error; err != nil {
				return fmt.Errorf("创建规格选项失败: %w", err)
			}
		}
	}
	return nil
}

func ProductNeedsOptions(db *gorm.DB, productID uint64) (bool, error) {
	var count int64
	if err := query.NotDeleted(db.Model(&model.ProductOptionGroup{})).
		Where("product_id = ?", productID).
		Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func ValidateAndBuildOptionSnapshot(db *gorm.DB, productID uint64, quantity uint32, units []OptionSelectionUnitInput) (model.OptionSelectionSnapshot, error) {
	groups, err := loadOptionGroupsInTx(db, productID)
	if err != nil {
		return nil, err
	}
	var productName string
	if len(groups) > 0 {
		var p model.Product
		if err := query.NotDeleted(db).Select("name").First(&p, productID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrProductNotFound
			}
			return nil, err
		}
		productName = p.Name
	}
	return validateSelectionAgainstGroups(productID, productName, groups, quantity, units)
}

func validateSelectionAgainstGroups(
	productID uint64,
	productName string,
	groups []model.ProductOptionGroup,
	quantity uint32,
	units []OptionSelectionUnitInput,
) (model.OptionSelectionSnapshot, error) {
	if len(groups) == 0 {
		if len(units) == 0 {
			return nil, nil
		}
		return nil, ErrOptionInvalid
	}
	if quantity == 0 {
		return nil, ErrOptionRequired
	}
	if uint32(len(units)) != quantity {
		return nil, fmt.Errorf("%w: 需为全部 %d 份完成规格选择", ErrOptionRequired, quantity)
	}

	groupByID := make(map[uint64]model.ProductOptionGroup, len(groups))
	for _, g := range groups {
		groupByID[g.ID] = g
	}

	seenUnit := make(map[uint32]struct{}, len(units))
	out := make(model.OptionSelectionSnapshot, 0, len(units))

	for _, u := range units {
		if u.UnitIndex < 1 || u.UnitIndex > quantity {
			return nil, fmt.Errorf("%w: unit_index %d 无效", ErrOptionInvalid, u.UnitIndex)
		}
		if _, ok := seenUnit[u.UnitIndex]; ok {
			return nil, fmt.Errorf("%w: unit_index %d 重复", ErrOptionInvalid, u.UnitIndex)
		}
		seenUnit[u.UnitIndex] = struct{}{}

		if len(u.Groups) != len(groups) {
			return nil, fmt.Errorf("%w: 第 %d 份未完成全部规格选择", ErrOptionRequired, u.UnitIndex)
		}

		seenGroup := make(map[uint64]struct{}, len(groups))
		groupSnaps := make([]model.OptionSelectionGroupSnap, 0, len(groups))

		for _, sel := range u.Groups {
			if sel.GroupID == 0 || sel.OptionID == 0 {
				return nil, ErrOptionInvalid
			}
			if _, ok := seenGroup[sel.GroupID]; ok {
				return nil, fmt.Errorf("%w: 第 %d 份规格组重复", ErrOptionInvalid, u.UnitIndex)
			}
			seenGroup[sel.GroupID] = struct{}{}

			g, ok := groupByID[sel.GroupID]
			if !ok {
				return nil, fmt.Errorf("%w: 规格组 %d 不存在", ErrOptionInvalid, sel.GroupID)
			}

			var item *model.ProductOptionItem
			for i := range g.Items {
				if g.Items[i].ID == sel.OptionID {
					item = &g.Items[i]
					break
				}
			}
			if item == nil || item.GroupID != sel.GroupID {
				return nil, fmt.Errorf("%w: 规格选项 %d 无效", ErrOptionInvalid, sel.OptionID)
			}

			groupSnaps = append(groupSnaps, model.OptionSelectionGroupSnap{
				GroupID:     g.ID,
				GroupTitle:  g.Title,
				OptionID:    item.ID,
				OptionLabel: item.Label,
			})
		}

		out = append(out, model.OptionSelectionUnitSnap{
			UnitIndex:   u.UnitIndex,
			ProductID:   productID,
			ProductName: productName,
			Groups:      groupSnaps,
		})
	}

	return out, nil
}
