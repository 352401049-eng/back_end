package service

import (
	"errors"
	"fmt"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
)

var (
	ErrCartItemNotFound   = errors.New("cart item not found")
	ErrCartProductInvalid = errors.New("cart product invalid")
)

type CartService struct {
	DB *gorm.DB
}

type AddCartInput struct {
	ProductID      uint64
	Quantity       uint32
	Spec           *string
	PurchaseType   uint8
	GroupBuyID     *uint64
	GroupBuyTeamID *uint64
}

type UpdateCartInput struct {
	Quantity *uint32
	Selected *uint8
}

func (s *CartService) Add(accountID uint64, input AddCartInput) (*model.CartItem, error) {
	if input.Quantity == 0 {
		input.Quantity = 1
	}
	if input.PurchaseType == 0 {
		input.PurchaseType = model.PurchaseTypeSolo
	}

	var product model.Product
	if err := query.NotDeleted(s.DB).
		Where("id = ? AND status = ?", input.ProductID, model.ProductStatusOn).
		First(&product).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrCartProductInvalid
		}
		return nil, err
	}

	if input.PurchaseType == model.PurchaseTypeGroup {
		if product.EnableGroupBuy != 1 {
			return nil, ErrInvalidProductArg
		}
		if input.GroupBuyID == nil {
			var gb model.GroupBuy
			if err := query.NotDeleted(s.DB).Where("product_id = ? AND status = 1", product.ID).First(&gb).Error; err != nil {
				return nil, ErrInvalidProductArg
			}
			input.GroupBuyID = &gb.ID
		}
	} else {
		input.GroupBuyID = nil
		input.GroupBuyTeamID = nil
	}

	spec := ""
	if input.Spec != nil {
		spec = *input.Spec
	}

	var existing model.CartItem
	err := query.NotDeleted(s.DB).Where(
		"account_id = ? AND product_id = ? AND purchase_type = ? AND IFNULL(spec,'') = ? AND IFNULL(group_buy_id,0) = ? AND IFNULL(group_buy_team_id,0) = ?",
		accountID, input.ProductID, input.PurchaseType, spec,
		ptrUint64(input.GroupBuyID), ptrUint64(input.GroupBuyTeamID),
	).First(&existing).Error

	if err == nil {
		newQty := existing.Quantity + input.Quantity
		if err := s.DB.Model(&existing).Update("quantity", newQty).Error; err != nil {
			return nil, err
		}
		existing.Quantity = newQty
		return &existing, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	item := model.CartItem{
		AccountID:      accountID,
		ProductID:      input.ProductID,
		PurchaseType:   input.PurchaseType,
		GroupBuyID:     input.GroupBuyID,
		GroupBuyTeamID: input.GroupBuyTeamID,
		Quantity:       input.Quantity,
		Selected:       1,
	}
	if input.Spec != nil && *input.Spec != "" {
		item.Spec = input.Spec
	}
	if err := s.DB.Create(&item).Error; err != nil {
		return nil, fmt.Errorf("加购失败: %w", err)
	}
	return &item, nil
}

func (s *CartService) Update(accountID, id uint64, input UpdateCartInput) (*model.CartItem, error) {
	item, err := s.getItem(accountID, id)
	if err != nil {
		return nil, err
	}
	updates := map[string]interface{}{}
	if input.Quantity != nil {
		if *input.Quantity == 0 {
			return nil, ErrInvalidProductArg
		}
		updates["quantity"] = *input.Quantity
	}
	if input.Selected != nil {
		updates["selected"] = *input.Selected
	}
	if len(updates) == 0 {
		return item, nil
	}
	if err := s.DB.Model(item).Updates(updates).Error; err != nil {
		return nil, err
	}
	return s.getItem(accountID, id)
}

func (s *CartService) Delete(accountID, id uint64) error {
	item, err := s.getItem(accountID, id)
	if err != nil {
		return err
	}
	return query.SoftDelete(s.DB, item, "id = ? AND account_id = ?", id, accountID).Error
}

func (s *CartService) getItem(accountID, id uint64) (*model.CartItem, error) {
	var item model.CartItem
	err := query.NotDeleted(s.DB).Where("id = ? AND account_id = ?", id, accountID).First(&item).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrCartItemNotFound
	}
	if err != nil {
		return nil, err
	}
	return &item, nil
}

func ptrUint64(p *uint64) uint64 {
	if p == nil {
		return 0
	}
	return *p
}
