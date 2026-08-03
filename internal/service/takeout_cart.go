package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// CartCheckoutTakeoutInput 购物车外卖合单结算。
type CartCheckoutTakeoutInput struct {
	CartItemIDs        []uint64
	MerchantID         uint64
	AddressID          uint64
	DeliveryTimeRemark string
	Lines              []CartCheckoutTakeoutLineInput
}

// CartCheckoutTakeoutLineInput 单行选配（规格等）；套餐商品请走详情页外卖下单。
type CartCheckoutTakeoutLineInput struct {
	CartItemID       uint64
	OptionSelections []OptionSelectionUnitInput
}

type cartTakeoutLine struct {
	Cart     model.CartItem
	Product  model.Product
	UnitPrice float64
	Subtotal  float64
	Opts     []OptionSelectionUnitInput
}

func validateCartAddForTakeout(purchaseType uint8, product model.Product) error {
	if purchaseType == model.PurchaseTypeGroup {
		return fmt.Errorf("%w: 拼团商品请在详情页单独下单", ErrInvalidProductArg)
	}
	if err := validateFulfillmentFlags(product, model.DeliveryTypeDelivery); err != nil {
		if errors.Is(err, ErrDeliveryNotAllowed) || errors.Is(err, ErrVirtualNotDeliverable) {
			return fmt.Errorf("%w: 该商品不支持外卖配送", ErrInvalidProductArg)
		}
		return err
	}
	return nil
}

func validateCartCheckoutTakeoutInput(in CartCheckoutTakeoutInput) error {
	if len(in.CartItemIDs) == 0 {
		return fmt.Errorf("%w: 请选择购物车商品", ErrInvalidProductArg)
	}
	if in.MerchantID == 0 {
		return fmt.Errorf("%w: 请指定商家", ErrInvalidProductArg)
	}
	if in.AddressID == 0 {
		return ErrAddressRequired
	}
	return nil
}

func lineSelectionsByCartID(lines []CartCheckoutTakeoutLineInput) map[uint64][]OptionSelectionUnitInput {
	out := make(map[uint64][]OptionSelectionUnitInput, len(lines))
	for _, ln := range lines {
		if ln.CartItemID == 0 {
			continue
		}
		out[ln.CartItemID] = ln.OptionSelections
	}
	return out
}

// CreateFromCart 将同店、可外卖的购物车行合并为一笔外卖单。
func (s *TakeoutService) CreateFromCart(accountID uint64, in CartCheckoutTakeoutInput) (*TakeoutView, error) {
	if err := validateCartCheckoutTakeoutInput(in); err != nil {
		return nil, err
	}

	var cartItems []model.CartItem
	if err := query.NotDeleted(s.DB).
		Where("account_id = ? AND id IN ?", accountID, in.CartItemIDs).
		Find(&cartItems).Error; err != nil {
		return nil, err
	}
	if len(cartItems) != len(in.CartItemIDs) {
		return nil, ErrCartItemNotFound
	}

	lineOpts := lineSelectionsByCartID(in.Lines)
	seen := map[uint64]struct{}{}
	lines := make([]cartTakeoutLine, 0, len(cartItems))
	var goodsAmount float64

	for _, cart := range cartItems {
		if _, ok := seen[cart.ID]; ok {
			return nil, fmt.Errorf("%w: 购物车项重复", ErrInvalidProductArg)
		}
		seen[cart.ID] = struct{}{}
		if cart.PurchaseType == model.PurchaseTypeGroup {
			return nil, fmt.Errorf("%w: 拼团商品请在详情页单独下单", ErrInvalidProductArg)
		}
		if cart.Quantity == 0 {
			return nil, fmt.Errorf("%w: 数量无效", ErrInvalidProductArg)
		}

		var product model.Product
		if err := query.NotDeleted(s.DB).
			Where("id = ? AND merchant_id = ? AND status = ?", cart.ProductID, in.MerchantID, model.ProductStatusOn).
			First(&product).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrProductNotFound
			}
			return nil, err
		}
		if err := validateFulfillmentFlags(product, model.DeliveryTypeDelivery); err != nil {
			if errors.Is(err, ErrDeliveryNotAllowed) || errors.Is(err, ErrVirtualNotDeliverable) {
				return nil, fmt.Errorf("%w: 该商品不支持外卖配送", ErrInvalidProductArg)
			}
			return nil, err
		}
		if product.ItemType == model.ProductItemTypePackage {
			return nil, fmt.Errorf("%w: 购物车结算暂不支持套餐商品，请在商品详情页下单", ErrInvalidProductArg)
		}
		if product.TakeoutStock < cart.Quantity {
			return nil, ErrInsufficientStock
		}
		if err := assertProductChannelPurchasable(product, productChannelTakeout); err != nil {
			return nil, err
		}

		unit, err := takeoutGoodsUnitPrice(product)
		if err != nil {
			return nil, err
		}
		sub := roundMoney(unit * float64(cart.Quantity))
		goodsAmount = roundMoney(goodsAmount + sub)
		opts := lineOpts[cart.ID]
		if len(opts) == 0 {
			fromCart, err := cartOptionSelectionsFromItem(cart)
			if err != nil {
				return nil, err
			}
			opts = fromCart
		}
		lines = append(lines, cartTakeoutLine{
			Cart: cart, Product: product, UnitPrice: unit, Subtotal: sub,
			Opts: opts,
		})
	}

	addrID := in.AddressID
	coordIn := DeliveryCoordinateInput{AddressID: &addrID}
	if s.ZoneSvc != nil {
		if err := s.ZoneSvc.ValidateDelivery(accountID, in.MerchantID, model.DeliveryTypeDelivery, coordIn); err != nil {
			return nil, err
		}
	}

	var mp model.MerchantProfile
	if err := query.NotDeleted(s.DB).Select("id", "delivery_fee", "rider_earnings").First(&mp, in.MerchantID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMerchantNotFound
		}
		return nil, err
	}

	var mergedOptSnap model.OptionSelectionSnapshot
	for _, ln := range lines {
		needsOpts, err := ProductNeedsOptions(s.DB, ln.Product.ID)
		if err != nil {
			return nil, err
		}
		if needsOpts {
			snap, err := ValidateAndBuildOptionSnapshot(s.DB, ln.Product.ID, ln.Cart.Quantity, ln.Opts)
			if err != nil {
				return nil, err
			}
			mergedOptSnap = append(mergedOptSnap, snap...)
		} else if len(ln.Opts) > 0 {
			return nil, ErrOptionInvalid
		}
	}

	var addr model.UserAddress
	if err := query.NotDeleted(s.DB).Where("id = ? AND account_id = ?", in.AddressID, accountID).First(&addr).Error; err != nil {
		return nil, ErrAddressRequired
	}
	addrSnap := AddressSnapshotFromUserAddress(&addr)

	var optJSON json.RawMessage
	if len(mergedOptSnap) > 0 {
		raw, err := json.Marshal(mergedOptSnap)
		if err != nil {
			return nil, err
		}
		optJSON = raw
	}

	deliveryFee := roundMoney(mp.DeliveryFee)
	payAmount := roundMoney(goodsAmount + deliveryFee)
	now := time.Now()
	expireAt := now.Add(time.Duration(s.payTimeoutMinutes()) * time.Minute)

	var takeout model.TakeoutOrder
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		for i := range lines {
			var p model.Product
			if err := query.NotDeleted(tx).Clauses(clause.Locking{Strength: "UPDATE"}).
				First(&p, lines[i].Product.ID).Error; err != nil {
				return err
			}
			if p.Status != model.ProductStatusOn || p.MerchantID != in.MerchantID {
				return ErrProductNotFound
			}
			if p.TakeoutStock < lines[i].Cart.Quantity {
				return ErrInsufficientStock
			}
			if err := assertProductChannelPurchasable(p, productChannelTakeout); err != nil {
				return err
			}
			lines[i].Product = p
			unit, err := takeoutGoodsUnitPrice(p)
			if err != nil {
				return err
			}
			lines[i].UnitPrice = unit
			lines[i].Subtotal = roundMoney(unit * float64(lines[i].Cart.Quantity))
		}
		recalc := 0.0
		for _, ln := range lines {
			recalc += ln.Subtotal
		}
		goodsAmount = roundMoney(recalc)
		payAmount = roundMoney(goodsAmount + deliveryFee)

		takeout = model.TakeoutOrder{
			OrderNo:            genTakeoutOrderNo(),
			AccountID:          accountID,
			MerchantID:         in.MerchantID,
			Status:             model.TakeoutStatusPendingPay,
			GoodsAmount:        goodsAmount,
			DeliveryFee:        deliveryFee,
			RiderEarnings:      roundMoney(mp.RiderEarnings),
			PayAmount:          payAmount,
			PayStatus:          model.PayStatusUnpaid,
			PayExpireAt:        &expireAt,
			AddressSnapshot:    addrSnap,
			DeliveryTimeRemark: in.DeliveryTimeRemark,
			OptionSelections:   optJSON,
		}
		if err := tx.Create(&takeout).Error; err != nil {
			return err
		}

		for _, ln := range lines {
			cover := ln.Product.CoverURL
			item := model.TakeoutOrderItem{
				TakeoutOrderID: takeout.ID,
				ProductID:      ln.Product.ID,
				ProductName:    ln.Product.Name,
				ProductImage:   &cover,
				UnitPrice:      ln.UnitPrice,
				Quantity:       ln.Cart.Quantity,
				Subtotal:       ln.Subtotal,
			}
			if err := tx.Create(&item).Error; err != nil {
				return err
			}
			if err := query.SoftDelete(tx, &model.CartItem{}, "id = ? AND account_id = ?", ln.Cart.ID, accountID).Error; err != nil {
				return err
			}
		}
		if err := deductTakeoutStockInTx(tx, &takeout); err != nil {
			return err
		}
		return s.settlePaymentInTx(tx, takeout.ID, now)
	})
	if err != nil {
		return nil, err
	}
	return s.GetView(accountID, takeout.ID)
}
