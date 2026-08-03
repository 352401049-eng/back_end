package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

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
	ProductID        uint64
	Quantity         uint32
	Spec             *string
	PurchaseType     uint8
	GroupBuyID       *uint64
	GroupBuyTeamID   *uint64
	OptionSelections []OptionSelectionUnitInput
}

type UpdateCartInput struct {
	Quantity         *uint32
	Selected         *uint8
	OptionSelections []OptionSelectionUnitInput
	HasOptionUpdate  bool
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
	if err := validateCartAddForTakeout(input.PurchaseType, product); err != nil {
		return nil, err
	}
	if product.ItemType == model.ProductItemTypePackage {
		return nil, fmt.Errorf("%w: 套餐请在详情页下单", ErrInvalidProductArg)
	}
	input.GroupBuyID = nil
	input.GroupBuyTeamID = nil

	optRaw, optText, optKey, err := s.prepareCartOptions(product.ID, input.Quantity, input.OptionSelections)
	if err != nil {
		return nil, err
	}

	var existing model.CartItem
	err = query.NotDeleted(s.DB).Where(
		"account_id = ? AND product_id = ? AND purchase_type = ? AND IFNULL(option_key,'') = ? AND IFNULL(group_buy_id,0) = ? AND IFNULL(group_buy_team_id,0) = ?",
		accountID, input.ProductID, input.PurchaseType, optKey,
		ptrUint64(input.GroupBuyID), ptrUint64(input.GroupBuyTeamID),
	).First(&existing).Error

	if err == nil {
		newQty := existing.Quantity + input.Quantity
		mergedRaw, mergedText, err := s.resizeCartOptions(product.ID, existing.OptionSelections, newQty)
		if err != nil {
			return nil, err
		}
		updates := map[string]interface{}{
			"quantity":          newQty,
			"option_selections": mergedRaw,
			"option_text":       mergedText,
			"option_key":        optKey,
		}
		if err := s.DB.Model(&existing).Updates(updates).Error; err != nil {
			return nil, err
		}
		return s.getItem(accountID, existing.ID)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}

	item := model.CartItem{
		AccountID:        accountID,
		ProductID:        input.ProductID,
		PurchaseType:     input.PurchaseType,
		GroupBuyID:       input.GroupBuyID,
		GroupBuyTeamID:   input.GroupBuyTeamID,
		Quantity:         input.Quantity,
		Selected:         1,
		OptionSelections: optRaw,
		OptionText:       optText,
		OptionKey:        optKey,
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
	qty := item.Quantity
	if input.Quantity != nil {
		if *input.Quantity == 0 {
			return nil, ErrInvalidProductArg
		}
		qty = *input.Quantity
		updates["quantity"] = qty
	}
	if input.Selected != nil {
		updates["selected"] = *input.Selected
	}

	needsOpts, err := ProductNeedsOptions(s.DB, item.ProductID)
	if err != nil {
		return nil, err
	}

	if input.HasOptionUpdate {
		if !needsOpts {
			return nil, fmt.Errorf("%w: 该商品无需规格", ErrOptionInvalid)
		}
		optRaw, optText, optKey, err := s.prepareCartOptions(item.ProductID, qty, input.OptionSelections)
		if err != nil {
			return nil, err
		}
		updates["option_selections"] = optRaw
		updates["option_text"] = optText
		updates["option_key"] = optKey
	} else if input.Quantity != nil && needsOpts && len(item.OptionSelections) > 0 {
		optRaw, optText, err := s.resizeCartOptions(item.ProductID, item.OptionSelections, qty)
		if err != nil {
			return nil, err
		}
		updates["option_selections"] = optRaw
		updates["option_text"] = optText
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

func (s *CartService) prepareCartOptions(productID uint64, quantity uint32, units []OptionSelectionUnitInput) (json.RawMessage, string, string, error) {
	needs, err := ProductNeedsOptions(s.DB, productID)
	if err != nil {
		return nil, "", "", err
	}
	if !needs {
		if len(units) > 0 {
			return nil, "", "", ErrOptionInvalid
		}
		return nil, "", "", nil
	}
	if len(units) == 0 {
		return nil, "", "", fmt.Errorf("%w: 请先选择规格", ErrOptionRequired)
	}
	snap, err := ValidateAndBuildOptionSnapshot(s.DB, productID, quantity, units)
	if err != nil {
		return nil, "", "", err
	}
	raw, err := json.Marshal(unitsToStored(units, quantity))
	if err != nil {
		return nil, "", "", err
	}
	return raw, snap.SummaryText(), cartOptionMergeKey(units), nil
}

func (s *CartService) resizeCartOptions(productID uint64, existing json.RawMessage, newQty uint32) (json.RawMessage, string, error) {
	units, err := parseCartOptionSelections(existing)
	if err != nil {
		return nil, "", err
	}
	if len(units) == 0 {
		return nil, "", fmt.Errorf("%w: 请先选择规格", ErrOptionRequired)
	}
	resized := resizeOptionUnits(units, newQty)
	snap, err := ValidateAndBuildOptionSnapshot(s.DB, productID, newQty, resized)
	if err != nil {
		return nil, "", err
	}
	raw, err := json.Marshal(unitsToStored(resized, newQty))
	if err != nil {
		return nil, "", err
	}
	return raw, snap.SummaryText(), nil
}

func parseCartOptionSelections(raw json.RawMessage) ([]OptionSelectionUnitInput, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var units []OptionSelectionUnitInput
	if err := json.Unmarshal(raw, &units); err != nil {
		return nil, fmt.Errorf("%w: 规格数据无效", ErrOptionInvalid)
	}
	return units, nil
}

func unitsToStored(units []OptionSelectionUnitInput, quantity uint32) []OptionSelectionUnitInput {
	out := make([]OptionSelectionUnitInput, 0, len(units))
	for i, u := range units {
		idx := u.UnitIndex
		if idx == 0 {
			idx = uint32(i + 1)
		}
		if quantity > 0 && idx > quantity {
			continue
		}
		groups := make([]OptionSelectionGroupInput, len(u.Groups))
		copy(groups, u.Groups)
		sort.Slice(groups, func(a, b int) bool { return groups[a].GroupID < groups[b].GroupID })
		out = append(out, OptionSelectionUnitInput{
			UnitIndex: idx,
			ProductID: u.ProductID,
			Groups:    groups,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UnitIndex < out[j].UnitIndex })
	return out
}

func resizeOptionUnits(units []OptionSelectionUnitInput, newQty uint32) []OptionSelectionUnitInput {
	if newQty == 0 {
		return nil
	}
	sorted := unitsToStored(units, 0)
	if len(sorted) == 0 {
		return nil
	}
	template := sorted[0]
	out := make([]OptionSelectionUnitInput, 0, newQty)
	for i := uint32(1); i <= newQty; i++ {
		src := template
		if int(i) <= len(sorted) {
			src = sorted[i-1]
		}
		groups := make([]OptionSelectionGroupInput, len(src.Groups))
		copy(groups, src.Groups)
		out = append(out, OptionSelectionUnitInput{
			UnitIndex: i,
			ProductID: src.ProductID,
			Groups:    groups,
		})
	}
	return out
}

func cartOptionMergeKey(units []OptionSelectionUnitInput) string {
	if len(units) == 0 {
		return ""
	}
	sorted := unitsToStored(units, 0)
	type groupPick struct {
		GroupID  uint64 `json:"g"`
		OptionID uint64 `json:"o"`
	}
	type unitSig struct {
		Groups []groupPick `json:"groups"`
	}
	sigs := make([]unitSig, 0, len(sorted))
	for _, u := range sorted {
		gs := make([]groupPick, 0, len(u.Groups))
		for _, g := range u.Groups {
			gs = append(gs, groupPick{GroupID: g.GroupID, OptionID: g.OptionID})
		}
		sigs = append(sigs, unitSig{Groups: gs})
	}
	// 各份规格一致时按「模板」合并，便于同规格多次加购累加数量
	same := true
	for i := 1; i < len(sigs); i++ {
		ra, _ := json.Marshal(sigs[0])
		rb, _ := json.Marshal(sigs[i])
		if string(ra) != string(rb) {
			same = false
			break
		}
	}
	var payload interface{}
	if same {
		payload = struct {
			Mode string  `json:"mode"`
			Unit unitSig `json:"unit"`
		}{Mode: "same", Unit: sigs[0]}
	} else {
		payload = struct {
			Mode  string    `json:"mode"`
			Units []unitSig `json:"units"`
		}{Mode: "full", Units: sigs}
	}
	raw, _ := json.Marshal(payload)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:16])
}

func ptrUint64(p *uint64) uint64 {
	if p == nil {
		return 0
	}
	return *p
}

// cartOptionSelectionsFromItem 供外卖结算读取购物车内已存规格。
func cartOptionSelectionsFromItem(item model.CartItem) ([]OptionSelectionUnitInput, error) {
	units, err := parseCartOptionSelections(item.OptionSelections)
	if err != nil {
		return nil, err
	}
	if len(units) == 0 && strings.TrimSpace(item.OptionText) == "" {
		return nil, nil
	}
	return resizeOptionUnits(units, item.Quantity), nil
}
