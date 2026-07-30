package service

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/payment"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrTakeoutNotFound      = errors.New("takeout order not found")
	ErrTakeoutStatusInvalid = errors.New("takeout status invalid")
)

type TakeoutService struct {
	DB                *gorm.DB
	ZoneSvc           *DeliveryZoneService
	Payment           payment.Provider
	PayTimeoutMinutes int
}

type CreateTakeoutInput struct {
	MerchantID         uint64
	ProductID          uint64
	Quantity           uint32
	AddressID          uint64
	DeliveryTimeRemark string
	PackageSelections  []PackageSelectionInput
	OptionSelections   []OptionSelectionUnitInput
}

type TakeoutView struct {
	model.TakeoutOrder
	StatusText string `json:"status_text"`
	StatusCode string `json:"status_code"`
}

func computeTakeoutPayAmount(unitPrice float64, qty uint32, deliveryFee float64) float64 {
	goods := roundMoney(unitPrice * float64(qty))
	return roundMoney(goods + deliveryFee)
}

func genTakeoutOrderNo() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("T%s%s", time.Now().Format("20060102150405"), hex.EncodeToString(b))
}

func takeoutStatusMeta(status uint8) (text, code string) {
	switch status {
	case model.TakeoutStatusPendingPay:
		return "待支付", "pending_pay"
	case model.TakeoutStatusPreparing:
		return "配餐中", "preparing"
	case model.TakeoutStatusFulfilling:
		return "配送中", "fulfilling"
	case model.TakeoutStatusCompleted:
		return "已完成", "completed"
	case model.TakeoutStatusCancelled:
		return "已取消", "cancelled"
	default:
		return "未知", "unknown"
	}
}

func (s *TakeoutService) toView(to *model.TakeoutOrder) *TakeoutView {
	text, code := takeoutStatusMeta(to.Status)
	return &TakeoutView{
		TakeoutOrder: *to,
		StatusText:   text,
		StatusCode:   code,
	}
}

func (s *TakeoutService) paymentProvider() payment.Provider {
	if s.Payment != nil {
		return s.Payment
	}
	return &payment.MockProvider{DB: s.DB}
}

func (s *TakeoutService) payTimeoutMinutes() int {
	if s.PayTimeoutMinutes > 0 {
		return s.PayTimeoutMinutes
	}
	return 15
}

// Create 创建外卖单：校验商品/地址/配送范围/套餐选配/规格；不写背包；支付成功后再扣库存。
func (s *TakeoutService) Create(accountID uint64, in CreateTakeoutInput) (*TakeoutView, error) {
	if in.MerchantID == 0 || in.ProductID == 0 {
		return nil, fmt.Errorf("%w: 请指定 merchant_id 与 product_id", ErrInvalidProductArg)
	}
	if in.Quantity == 0 {
		in.Quantity = 1
	}
	if in.AddressID == 0 {
		return nil, ErrAddressRequired
	}

	var product model.Product
	if err := query.NotDeleted(s.DB).
		Where("id = ? AND merchant_id = ? AND status = ?", in.ProductID, in.MerchantID, model.ProductStatusOn).
		First(&product).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrProductNotFound
		}
		return nil, err
	}
	if product.AllowDelivery != 1 {
		return nil, fmt.Errorf("%w: 商品不支持配送", ErrInvalidProductArg)
	}
	if product.Stock < in.Quantity {
		return nil, ErrInsufficientStock
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

	var pkgSelJSON json.RawMessage
	if product.ItemType == model.ProductItemTypePackage {
		groups, err := (&ProductService{DB: s.DB}).LoadPackageGroups(product.ID)
		if err != nil {
			return nil, err
		}
		if len(groups) == 0 {
			return nil, fmt.Errorf("%w: 套餐未配置分组", ErrInvalidProductArg)
		}
		hasOptional := false
		for _, g := range groups {
			if g.GroupType != model.PackageGroupTypeFixed {
				hasOptional = true
				break
			}
		}
		if hasOptional && len(in.PackageSelections) == 0 {
			return nil, ErrPackageSelectionRequired
		}
		if _, err := ResolvePackageSelections(s.DB, product.ID, in.PackageSelections); err != nil {
			return nil, err
		}
		raw, err := json.Marshal(in.PackageSelections)
		if err != nil {
			return nil, err
		}
		pkgSelJSON = raw
	}

	needsOpts, err := ProductNeedsOptions(s.DB, product.ID)
	if err != nil {
		return nil, err
	}
	var optSnap model.OptionSelectionSnapshot
	if needsOpts {
		optSnap, err = ValidateAndBuildOptionSnapshot(s.DB, product.ID, in.Quantity, in.OptionSelections)
		if err != nil {
			return nil, err
		}
	} else if len(in.OptionSelections) > 0 {
		return nil, ErrOptionInvalid
	}

	unitPrice := product.Price
	goodsAmount := roundMoney(unitPrice * float64(in.Quantity))
	deliveryFee := roundMoney(mp.DeliveryFee)
	payAmount := computeTakeoutPayAmount(unitPrice, in.Quantity, deliveryFee)

	var addr model.UserAddress
	if err := query.NotDeleted(s.DB).Where("id = ? AND account_id = ?", in.AddressID, accountID).First(&addr).Error; err != nil {
		return nil, ErrAddressRequired
	}
	addrSnap := AddressSnapshotFromUserAddress(&addr)

	var optJSON json.RawMessage
	if len(optSnap) > 0 {
		raw, err := json.Marshal(optSnap)
		if err != nil {
			return nil, err
		}
		optJSON = raw
	}

	now := time.Now()
	expireAt := now.Add(time.Duration(s.payTimeoutMinutes()) * time.Minute)

	var takeout model.TakeoutOrder
	err = s.DB.Transaction(func(tx *gorm.DB) error {
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
			PackageSelections:  pkgSelJSON,
			OptionSelections:   optJSON,
		}
		if err := tx.Create(&takeout).Error; err != nil {
			return err
		}

		cover := product.CoverURL
		item := model.TakeoutOrderItem{
			TakeoutOrderID: takeout.ID,
			ProductID:      product.ID,
			ProductName:    product.Name,
			ProductImage:   &cover,
			UnitPrice:      unitPrice,
			Quantity:       in.Quantity,
			Subtotal:       goodsAmount,
		}
		if err := tx.Create(&item).Error; err != nil {
			return err
		}

		return s.settlePaymentInTx(tx, takeout.ID, now)
	})
	if err != nil {
		return nil, err
	}
	return s.GetView(accountID, takeout.ID)
}

func (s *TakeoutService) settlePaymentInTx(tx *gorm.DB, takeoutID uint64, at time.Time) error {
	p := s.paymentProvider()
	if !p.ImmediateSettle() {
		return nil
	}
	sub, err := payment.TakeoutSubjectFromID(tx, takeoutID, 0)
	if err != nil {
		return err
	}
	if err := p.SettleSubjectPaidInTx(tx, sub, at); err != nil {
		return err
	}
	return s.MarkPaidInTx(tx, takeoutID, at)
}

// CreatePrepay 发起外卖单预支付。Mock 结算后补调 MarkPaidInTx 扣库存。
func (s *TakeoutService) CreatePrepay(accountID, takeoutID uint64) (*payment.PrepayResult, error) {
	sub, err := payment.TakeoutSubjectFromID(s.DB, takeoutID, accountID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTakeoutNotFound
		}
		return nil, err
	}
	result, err := s.paymentProvider().CreatePrepayForSubject(sub)
	if err != nil {
		return nil, err
	}
	if result.AlreadyPaid {
		if err := s.DB.Transaction(func(tx *gorm.DB) error {
			return s.MarkPaidInTx(tx, takeoutID, time.Now())
		}); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// MarkPaidInTx 支付成功后扣库存并推进到配餐中。
func (s *TakeoutService) MarkPaidInTx(tx *gorm.DB, takeoutID uint64, at time.Time) error {
	var to model.TakeoutOrder
	if err := query.NotDeleted(tx).Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&to, takeoutID).Error; err != nil {
		return err
	}
	if to.Status == model.TakeoutStatusPreparing && to.PayStatus == model.PayStatusPaid {
		return nil
	}
	if to.Status != model.TakeoutStatusPendingPay {
		return ErrTakeoutStatusInvalid
	}

	var items []model.TakeoutOrderItem
	if err := query.NotDeleted(tx).Where("takeout_order_id = ?", takeoutID).Find(&items).Error; err != nil {
		return err
	}
	for _, item := range items {
		if err := deductProductStockInTx(tx, item.ProductID, item.Quantity); err != nil {
			return err
		}
		var product model.Product
		if err := query.NotDeleted(tx).Select("id", "item_type").First(&product, item.ProductID).Error; err != nil {
			return err
		}
		if product.ItemType != model.ProductItemTypePackage {
			continue
		}
		sels, err := decodeTakeoutPackageSelections(to.PackageSelections)
		if err != nil {
			return err
		}
		lines, err := ResolvePackageSelections(tx, item.ProductID, sels)
		if err != nil {
			return err
		}
		for _, ln := range lines {
			qty := ln.Qty * item.Quantity
			if err := deductProductStockInTx(tx, ln.Product.ID, qty); err != nil {
				return err
			}
		}
	}

	res := query.NotDeleted(tx.Model(&model.TakeoutOrder{})).
		Where("id = ? AND status = ?", takeoutID, model.TakeoutStatusPendingPay).
		Updates(map[string]interface{}{
			"status":        model.TakeoutStatusPreparing,
			"pay_expire_at": nil,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		var cur model.TakeoutOrder
		if err := query.NotDeleted(tx).Select("status").First(&cur, takeoutID).Error; err != nil {
			return err
		}
		if cur.Status == model.TakeoutStatusPreparing {
			return nil
		}
		return ErrTakeoutStatusInvalid
	}
	_ = at
	return nil
}

func decodeTakeoutPackageSelections(raw json.RawMessage) ([]PackageSelectionInput, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var sels []PackageSelectionInput
	if err := json.Unmarshal(raw, &sels); err != nil {
		return nil, fmt.Errorf("%w: package_selections 无效", ErrInvalidProductArg)
	}
	return sels, nil
}

// ExpireStalePendingPay 关闭超时未支付外卖单（未扣库存，无需回滚）。
func (s *TakeoutService) ExpireStalePendingPay(now time.Time) (int, error) {
	var orders []model.TakeoutOrder
	if err := query.NotDeleted(s.DB).
		Where("status = ? AND pay_expire_at IS NOT NULL AND pay_expire_at < ?", model.TakeoutStatusPendingPay, now).
		Limit(100).
		Find(&orders).Error; err != nil {
		return 0, err
	}
	n := 0
	var firstErr error
	for i := range orders {
		if err := s.expireOnePendingPayTakeout(orders[i].ID); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("expire pending-pay takeout %d: %w", orders[i].ID, err)
			}
			continue
		}
		n++
	}
	return n, firstErr
}

func (s *TakeoutService) expireOnePendingPayTakeout(takeoutID uint64) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var to model.TakeoutOrder
		if err := query.NotDeleted(tx).Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&to, takeoutID).Error; err != nil {
			return err
		}
		if to.Status != model.TakeoutStatusPendingPay {
			return nil
		}
		if to.PayStatus == model.PayStatusPaid {
			return s.MarkPaidInTx(tx, takeoutID, time.Now())
		}
		if wp, ok := s.Payment.(*payment.WeChatProvider); ok && wp.Client != nil {
			if err := wp.Client.CloseOrder(wp.MchID, to.OrderNo); err != nil {
				log.Printf("[pay-expire] close wechat takeout %s failed: %v", to.OrderNo, err)
			}
		}
		return query.NotDeleted(tx.Model(&model.TakeoutOrder{})).
			Where("id = ? AND status = ?", takeoutID, model.TakeoutStatusPendingPay).
			Updates(map[string]interface{}{
				"status":        model.TakeoutStatusCancelled,
				"pay_expire_at": nil,
			}).Error
	})
}

func (s *TakeoutService) GetView(accountID, takeoutID uint64) (*TakeoutView, error) {
	var to model.TakeoutOrder
	q := query.NotDeleted(s.DB).Where("id = ?", takeoutID)
	if accountID > 0 {
		q = q.Where("account_id = ?", accountID)
	}
	if err := q.Preload("Items", "is_deleted = ?", model.NotDeleted).First(&to).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTakeoutNotFound
		}
		return nil, err
	}
	return s.toView(&to), nil
}

func (s *TakeoutService) List(accountID uint64, page, pageSize int, status *uint8) ([]TakeoutView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 50 {
		pageSize = 50
	}

	q := query.NotDeleted(s.DB.Model(&model.TakeoutOrder{})).Where("account_id = ?", accountID)
	if status != nil {
		q = q.Where("status = ?", *status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var rows []model.TakeoutOrder
	if err := q.Order("id DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Preload("Items", "is_deleted = ?", model.NotDeleted).
		Find(&rows).Error; err != nil {
		return nil, 0, err
	}

	out := make([]TakeoutView, 0, len(rows))
	for i := range rows {
		out = append(out, *s.toView(&rows[i]))
	}
	return out, total, nil
}
