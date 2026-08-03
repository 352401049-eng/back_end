package service

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
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
	ErrTakeoutForbidden     = errors.New("takeout order forbidden")
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
	PackageUnits       []PackageUnitInput
	OptionSelections   []OptionSelectionUnitInput
}

type TakeoutView struct {
	model.TakeoutOrder
	StatusText             string                 `json:"status_text"`
	StatusCode             string                 `json:"status_code"`
	RejectReason           string                 `json:"reject_reason,omitempty"`
	RefundStatusText       string                 `json:"refund_status_text,omitempty"`
	PickupCode             string                 `json:"pickup_code,omitempty"`
	PackageSelectionText   string                 `json:"package_selection_text,omitempty"`
	OptionSelectionText    string                 `json:"option_selection_text,omitempty"`
	SelectionLines         []TakeoutSelectionLine `json:"selection_lines,omitempty"`
}

// TakeoutSelectionLine 按份展示套餐选配与规格，供商家/配送端直接渲染。
type TakeoutSelectionLine struct {
	UnitIndex              int    `json:"unit_index"`
	UnitLabel              string `json:"unit_label,omitempty"`
	ProductID              uint64 `json:"product_id"`
	ProductName            string `json:"product_name"`
	Quantity               uint32 `json:"quantity"`
	IsPackage              bool   `json:"is_package"`
	PackageSelectionText   string `json:"package_selection_text,omitempty"`
	OptionSelectionText    string `json:"option_selection_text,omitempty"`
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
	view := &TakeoutView{
		TakeoutOrder: *to,
		StatusText:   text,
		StatusCode:   code,
	}
	applyTakeoutDisplayMeta(view)
	s.enrichSelectionDisplay(view)
	return view
}

// applyTakeoutDisplayMeta 商家拒单与退款进度展示（主状态保留拒单语义，退款作副文案）。
func applyTakeoutDisplayMeta(view *TakeoutView) {
	if view == nil {
		return
	}
	remark := ""
	if view.Remark != nil {
		remark = strings.TrimSpace(*view.Remark)
	}
	if view.Status == model.TakeoutStatusCancelled && strings.HasPrefix(remark, "商家拒单") {
		view.StatusText = "商家拒单"
		view.StatusCode = "merchant_rejected"
		view.RejectReason = remark
	}
	switch view.PayStatus {
	case model.PayStatusRefunding:
		view.RefundStatusText = "退款中"
	case model.PayStatusRefunded:
		view.RefundStatusText = "已退款"
	}
}

func (s *TakeoutService) enrichSelectionDisplay(view *TakeoutView) {
	if view == nil {
		return
	}
	pkgText, optText, lines := buildTakeoutSelectionDisplay(s.DB, &view.TakeoutOrder)
	view.PackageSelectionText = pkgText
	view.OptionSelectionText = optText
	view.SelectionLines = lines
}

// buildTakeoutSelectionDisplay 生成套餐/规格可读摘要；多份套餐按份拆成明细行。
func buildTakeoutSelectionDisplay(db *gorm.DB, to *model.TakeoutOrder) (pkgText, optText string, lines []TakeoutSelectionLine) {
	if to == nil {
		return "", "", nil
	}
	var optSnap model.OptionSelectionSnapshot
	if len(to.OptionSelections) > 0 {
		_ = json.Unmarshal(to.OptionSelections, &optSnap)
	}
	optText = optSnap.SummaryText()
	optCursor := newTakeoutOptionCursor(optSnap)

	items := to.Items
	if len(items) == 0 && db != nil {
		_ = query.NotDeleted(db).Where("takeout_order_id = ?", to.ID).Find(&items)
	}
	if len(items) == 0 {
		if optText != "" {
			lines = append(lines, TakeoutSelectionLine{
				UnitIndex: 1, ProductName: "商品", Quantity: 1,
				OptionSelectionText: optText,
			})
		}
		return "", optText, lines
	}

	pkgPartsAll := make([]string, 0, 4)
	for _, item := range items {
		isPkg := false
		if db != nil {
			var product model.Product
			if err := query.NotDeleted(db).Select("id", "item_type").First(&product, item.ProductID).Error; err == nil {
				isPkg = product.ItemType == model.ProductItemTypePackage
			}
		}
		if !isPkg || db == nil || len(to.PackageSelections) == 0 {
			lines = append(lines, TakeoutSelectionLine{
				UnitIndex: 1, ProductID: item.ProductID, ProductName: item.ProductName,
				Quantity: item.Quantity, IsPackage: isPkg, OptionSelectionText: optText,
			})
			continue
		}
		units, err := decodeTakeoutPackageUnits(to.PackageSelections, item.Quantity)
		if err != nil || len(units) == 0 {
			lines = append(lines, TakeoutSelectionLine{
				UnitIndex: 1, ProductID: item.ProductID, ProductName: item.ProductName,
				Quantity: item.Quantity, IsPackage: true, OptionSelectionText: optText,
			})
			continue
		}
		for i, sels := range units {
			resolved, err := ResolvePackageSelections(db, item.ProductID, sels)
			if err != nil {
				continue
			}
			childParts := make([]string, 0, len(resolved))
			unitOptSnap := make(model.OptionSelectionSnapshot, 0, 4)
			for _, ln := range resolved {
				childParts = append(childParts, fmt.Sprintf("%s×%d", ln.Product.Name, ln.Qty))
				unitOptSnap = append(unitOptSnap, optCursor.take(ln.Product.ID, ln.Qty)...)
			}
			unitPkg := strings.Join(childParts, "、")
			if unitPkg != "" {
				pkgPartsAll = append(pkgPartsAll, unitPkg)
			}
			unitLabel := ""
			name := item.ProductName
			if len(units) > 1 {
				unitLabel = fmt.Sprintf("第%d份", i+1)
				name = fmt.Sprintf("%s（%s）", item.ProductName, unitLabel)
			}
			lines = append(lines, TakeoutSelectionLine{
				UnitIndex:            i + 1,
				UnitLabel:            unitLabel,
				ProductID:            item.ProductID,
				ProductName:          name,
				Quantity:             1,
				IsPackage:            true,
				PackageSelectionText: unitPkg,
				OptionSelectionText:  unitOptSnap.SummaryText(),
			})
		}
	}
	pkgText = strings.Join(pkgPartsAll, "；")
	return pkgText, optText, lines
}

type takeoutOptionCursor struct {
	byProduct map[uint64][]model.OptionSelectionUnitSnap
}

func newTakeoutOptionCursor(snap model.OptionSelectionSnapshot) *takeoutOptionCursor {
	by := make(map[uint64][]model.OptionSelectionUnitSnap)
	for _, u := range snap {
		by[u.ProductID] = append(by[u.ProductID], u)
	}
	for id := range by {
		sort.Slice(by[id], func(i, j int) bool {
			return by[id][i].UnitIndex < by[id][j].UnitIndex
		})
	}
	return &takeoutOptionCursor{byProduct: by}
}

func (c *takeoutOptionCursor) take(productID uint64, qty uint32) model.OptionSelectionSnapshot {
	if c == nil || qty == 0 {
		return nil
	}
	list := c.byProduct[productID]
	if len(list) == 0 {
		return nil
	}
	n := int(qty)
	if n > len(list) {
		n = len(list)
	}
	out := append(model.OptionSelectionSnapshot(nil), list[:n]...)
	c.byProduct[productID] = list[n:]
	return out
}

func (s *TakeoutService) paymentProvider() payment.Provider {
	if s.Payment != nil {
		return s.Payment
	}
	return &payment.MockProvider{DB: s.DB}
}

// runTx 包装事务：绑定微信退款收集器，提交成功后再异步发起退款。
func (s *TakeoutService) runTx(fn func(tx *gorm.DB) error) error {
	var jobs []payment.RefundJob
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		payment.AttachRefundCollector(tx, &jobs)
		return fn(tx)
	})
	if err != nil {
		return err
	}
	payment.DispatchRefundJobs(jobs)
	return nil
}

func (s *TakeoutService) payTimeoutMinutes() int {
	if s.PayTimeoutMinutes > 0 {
		return s.PayTimeoutMinutes
	}
	return 5
}

// Create 创建外卖单：校验商品/地址/配送范围/套餐选配/规格；不写背包；创建时预扣库存（超时未付回滚）。
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
	if err := validateFulfillmentFlags(product, model.DeliveryTypeDelivery); err != nil {
		return nil, err
	}
	if err := assertProductChannelPurchasable(product, productChannelTakeout); err != nil {
		return nil, err
	}
	if product.TakeoutStock < in.Quantity {
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
	var pkgUnits [][]PackageSelectionInput
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
		if hasOptional && len(in.PackageSelections) == 0 && len(in.PackageUnits) == 0 {
			return nil, ErrPackageSelectionRequired
		}
		pkgUnits, err = normalizePackageUnits(in.PackageUnits, in.PackageSelections, in.Quantity)
		if err != nil {
			return nil, err
		}
		if err := validatePackageUnitsStock(s.DB, product.ID, pkgUnits, productChannelTakeout); err != nil {
			return nil, err
		}
		stored := make([]PackageUnitInput, len(pkgUnits))
		for i, u := range pkgUnits {
			stored[i] = PackageUnitInput{PackageSelections: u}
		}
		raw, err := json.Marshal(stored)
		if err != nil {
			return nil, err
		}
		pkgSelJSON = raw
	}

	var optSnap model.OptionSelectionSnapshot
	if product.ItemType == model.ProductItemTypePackage {
		needsChildOpts, err := packageNeedsChildOptions(s.DB, product.ID, pkgUnits)
		if err != nil {
			return nil, err
		}
		if needsChildOpts {
			optSnap, err = validateOptionsForPackageUnits(s.DB, product.ID, pkgUnits, in.OptionSelections)
			if err != nil {
				return nil, err
			}
		} else if len(in.OptionSelections) > 0 {
			return nil, ErrOptionInvalid
		}
	} else {
		needsOpts, err := ProductNeedsOptions(s.DB, product.ID)
		if err != nil {
			return nil, err
		}
		if needsOpts {
			optSnap, err = ValidateAndBuildOptionSnapshot(s.DB, product.ID, in.Quantity, in.OptionSelections)
			if err != nil {
				return nil, err
			}
		} else if len(in.OptionSelections) > 0 {
			return nil, ErrOptionInvalid
		}
	}

	unitPrice, err := takeoutGoodsUnitPrice(product)
	if err != nil {
		return nil, err
	}
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
		// 预扣库存：防止待支付期间超卖导致付款后无法履约；超时关单/拒单时回滚。
		if err := deductTakeoutStockInTx(tx, &takeout); err != nil {
			return err
		}
		aid := accountID
		AppendFulfillmentEventInTx(tx, FulfillmentEventInput{
			SubjectType: model.FulfillmentSubjectTakeout,
			SubjectID:   takeout.ID,
			EventCode:   model.EventCreated,
			ActorRole:   model.FulfillmentActorUser,
			ActorID:     &aid,
			Title:       "订单已创建",
			Detail:      map[string]interface{}{"pay_amount": payAmount},
		})
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

// MarkPaidInTx 支付成功后推进到配餐中（库存已在 Create 预扣）。
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
	aid := to.AccountID
	AppendFulfillmentEventInTx(tx, FulfillmentEventInput{
		SubjectType: model.FulfillmentSubjectTakeout,
		SubjectID:   takeoutID,
		EventCode:   model.EventPaid,
		ActorRole:   model.FulfillmentActorUser,
		ActorID:     &aid,
		Title:       "支付成功，商家配餐中",
		Detail:      map[string]interface{}{"paid_at": at.Format(time.RFC3339)},
	})
	return nil
}

func decodeTakeoutPackageUnits(raw json.RawMessage, quantity uint32) ([][]PackageSelectionInput, error) {
	if len(raw) == 0 {
		return normalizePackageUnits(nil, nil, quantity)
	}
	var units []PackageUnitInput
	if err := json.Unmarshal(raw, &units); err == nil && len(units) > 0 {
		out := make([][]PackageSelectionInput, 0, len(units))
		for _, u := range units {
			out = append(out, u.PackageSelections)
		}
		if uint32(len(out)) == quantity {
			return out, nil
		}
	}
	var sels []PackageSelectionInput
	if err := json.Unmarshal(raw, &sels); err != nil {
		return nil, fmt.Errorf("%w: package_selections 无效", ErrInvalidProductArg)
	}
	return normalizePackageUnits(nil, sels, quantity)
}

// ExpireStalePendingPay 关闭超时未支付外卖单并回滚预扣库存。
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
		return s.cancelPendingPayInTx(tx, takeoutID, 0)
	})
}

// Cancel 用户取消待支付外卖单：关微信单、回滚预扣库存。
func (s *TakeoutService) Cancel(accountID, takeoutID uint64) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var to model.TakeoutOrder
		if err := query.NotDeleted(tx).Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&to, takeoutID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTakeoutNotFound
			}
			return err
		}
		if to.AccountID != accountID {
			return ErrTakeoutForbidden
		}
		if to.Status == model.TakeoutStatusCancelled {
			return nil
		}
		if to.Status != model.TakeoutStatusPendingPay || to.PayStatus == model.PayStatusPaid {
			return ErrTakeoutStatusInvalid
		}
		return s.cancelPendingPayInTx(tx, takeoutID, accountID)
	})
}

// cancelPendingPayInTx 关闭待支付外卖单；accountID>0 时校验归属。
func (s *TakeoutService) cancelPendingPayInTx(tx *gorm.DB, takeoutID, accountID uint64) error {
	var to model.TakeoutOrder
	if err := query.NotDeleted(tx).Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&to, takeoutID).Error; err != nil {
		return err
	}
	if accountID > 0 && to.AccountID != accountID {
		return ErrTakeoutForbidden
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
	if err := restoreTakeoutStockInTx(tx, &to); err != nil {
		return err
	}
	return query.NotDeleted(tx.Model(&model.TakeoutOrder{})).
		Where("id = ? AND status = ?", takeoutID, model.TakeoutStatusPendingPay).
		Updates(map[string]interface{}{
			"status":        model.TakeoutStatusCancelled,
			"pay_expire_at": nil,
		}).Error
}

func (s *TakeoutService) attachPickupCode(view *TakeoutView) {
	if view == nil || view.DeliveryOrderID == nil {
		return
	}
	var d model.DeliveryOrder
	if err := query.NotDeleted(s.DB).Select("pickup_code", "status").First(&d, *view.DeliveryOrderID).Error; err != nil {
		return
	}
	view.PickupCode = d.PickupCode
	// 配送申诉中：外卖单仍可能是 fulfilling，用户端展示为申诉中
	if d.Status == model.DeliveryException && view.Status == model.TakeoutStatusFulfilling {
		view.StatusText = "申诉中"
		view.StatusCode = "appealing"
	}
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
	view := s.toView(&to)
	s.attachPickupCode(view)
	return view, nil
}

func (s *TakeoutService) GetMerchantView(merchantID, takeoutID uint64) (*TakeoutView, error) {
	var to model.TakeoutOrder
	if err := query.NotDeleted(s.DB).
		Where("id = ? AND merchant_id = ?", takeoutID, merchantID).
		Preload("Items", "is_deleted = ?", model.NotDeleted).
		First(&to).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrTakeoutNotFound
		}
		return nil, err
	}
	view := s.toView(&to)
	s.attachPickupCode(view)
	return view, nil
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
		view := s.toView(&rows[i])
		s.attachPickupCode(view)
		out = append(out, *view)
	}
	return out, total, nil
}

func parseMerchantTakeoutStatusFilter(code string) (*uint8, error) {
	switch strings.TrimSpace(strings.ToLower(code)) {
	case "", "all":
		return nil, nil
	case "preparing":
		v := model.TakeoutStatusPreparing
		return &v, nil
	case "fulfilling":
		v := model.TakeoutStatusFulfilling
		return &v, nil
	case "completed":
		v := model.TakeoutStatusCompleted
		return &v, nil
	case "cancelled":
		v := model.TakeoutStatusCancelled
		return &v, nil
	default:
		return nil, fmt.Errorf("%w: status 无效", ErrInvalidProductArg)
	}
}

// ListForMerchant 商家外卖单列表（status=preparing 等字符串筛选）。
func (s *TakeoutService) ListForMerchant(merchantID uint64, page, pageSize int, statusCode string) ([]TakeoutView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	if pageSize > 50 {
		pageSize = 50
	}
	status, err := parseMerchantTakeoutStatusFilter(statusCode)
	if err != nil {
		return nil, 0, err
	}

	q := query.NotDeleted(s.DB.Model(&model.TakeoutOrder{})).Where("merchant_id = ?", merchantID)
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
		view := s.toView(&rows[i])
		s.attachPickupCode(view)
		out = append(out, *view)
	}
	return out, total, nil
}

func restoreTakeoutStockInTx(tx *gorm.DB, takeout *model.TakeoutOrder) error {
	var items []model.TakeoutOrderItem
	if err := query.NotDeleted(tx).Where("takeout_order_id = ?", takeout.ID).Find(&items).Error; err != nil {
		return err
	}
	for _, item := range items {
		if item.Quantity == 0 {
			continue
		}
		if err := restoreChannelStockInTx(tx, item.ProductID, item.Quantity, productChannelTakeout); err != nil {
			return err
		}
		var product model.Product
		if err := query.NotDeleted(tx).Select("id", "item_type").First(&product, item.ProductID).Error; err != nil {
			return err
		}
		if product.ItemType != model.ProductItemTypePackage {
			continue
		}
		units, err := decodeTakeoutPackageUnits(takeout.PackageSelections, item.Quantity)
		if err != nil {
			return err
		}
		for _, sels := range units {
			lines, err := ResolvePackageSelections(tx, item.ProductID, sels)
			if err != nil {
				return err
			}
			for _, ln := range lines {
				if err := restoreChannelStockInTx(tx, ln.Product.ID, ln.Qty, productChannelTakeout); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func deductTakeoutStockInTx(tx *gorm.DB, takeout *model.TakeoutOrder) error {
	var items []model.TakeoutOrderItem
	if err := query.NotDeleted(tx).Where("takeout_order_id = ?", takeout.ID).Find(&items).Error; err != nil {
		return err
	}
	for _, item := range items {
		if err := deductChannelStockInTx(tx, item.ProductID, item.Quantity, productChannelTakeout); err != nil {
			return err
		}
		var product model.Product
		if err := query.NotDeleted(tx).Select("id", "item_type").First(&product, item.ProductID).Error; err != nil {
			return err
		}
		if product.ItemType != model.ProductItemTypePackage {
			continue
		}
		units, err := decodeTakeoutPackageUnits(takeout.PackageSelections, item.Quantity)
		if err != nil {
			return err
		}
		for _, sels := range units {
			lines, err := ResolvePackageSelections(tx, item.ProductID, sels)
			if err != nil {
				return err
			}
			for _, ln := range lines {
				if err := deductChannelStockInTx(tx, ln.Product.ID, ln.Qty, productChannelTakeout); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// ConfirmPrepared 商家确认出餐：创建配送单（merchant_prepared=1）并推进到配送中。
func (s *TakeoutService) ConfirmPrepared(merchantID, takeoutID uint64) (*TakeoutView, error) {
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		var to model.TakeoutOrder
		if err := query.NotDeleted(tx).Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&to, takeoutID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTakeoutNotFound
			}
			return err
		}
		if to.MerchantID != merchantID {
			return ErrTakeoutForbidden
		}
		if to.Status != model.TakeoutStatusPreparing || to.PayStatus != model.PayStatusPaid {
			return ErrTakeoutStatusInvalid
		}
		if to.DeliveryOrderID != nil {
			return ErrTakeoutStatusInvalid
		}

		now := time.Now()
		tid := to.ID
		d := model.DeliveryOrder{
			TakeoutOrderID:   &tid,
			Status:           model.DeliveryPendingAccept,
			MerchantPrepared: 1,
			PreparedAt:       &now,
			PickupCode:       genPickupCode(tx, merchantID),
			DeliveryFee:      to.DeliveryFee,
			RiderEarnings:    to.RiderEarnings,
		}
		if err := tx.Create(&d).Error; err != nil {
			return err
		}
		res := query.NotDeleted(tx.Model(&model.TakeoutOrder{})).
			Where("id = ? AND status = ? AND pay_status = ?", takeoutID, model.TakeoutStatusPreparing, model.PayStatusPaid).
			Updates(map[string]interface{}{
				"status":            model.TakeoutStatusFulfilling,
				"delivery_order_id": d.ID,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrTakeoutStatusInvalid
		}
		mid := merchantID
		AppendFulfillmentEventInTx(tx, FulfillmentEventInput{
			SubjectType: model.FulfillmentSubjectTakeout,
			SubjectID:   takeoutID,
			EventCode:   model.EventPrepared,
			ActorRole:   model.FulfillmentActorMerchant,
			ActorID:     &mid,
			Title:       "商家已出餐，等待骑手接单",
			Detail:      map[string]interface{}{"delivery_order_id": d.ID, "pickup_code": d.PickupCode},
		})
		AppendFulfillmentEventInTx(tx, FulfillmentEventInput{
			SubjectType: model.FulfillmentSubjectDelivery,
			SubjectID:   d.ID,
			EventCode:   model.EventCreated,
			ActorRole:   model.FulfillmentActorMerchant,
			ActorID:     &mid,
			Title:       "配送单已创建",
			Detail:      map[string]interface{}{"takeout_order_id": takeoutID},
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetMerchantView(merchantID, takeoutID)
}

// Reject 商家拒单：配餐中全额退款并回滚库存；已出餐未接单可取消配送单后同样处理。
func (s *TakeoutService) Reject(merchantID, takeoutID uint64, reason string) (*TakeoutView, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, fmt.Errorf("%w: 请填写拒绝原因", ErrTakeoutStatusInvalid)
	}
	reasonText := "商家拒单：" + reason

	err := s.runTx(func(tx *gorm.DB) error {
		var to model.TakeoutOrder
		if err := query.NotDeleted(tx).Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&to, takeoutID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrTakeoutNotFound
			}
			return err
		}
		if to.MerchantID != merchantID {
			return ErrTakeoutForbidden
		}
		if to.Status == model.TakeoutStatusCancelled {
			return nil
		}

		switch to.Status {
		case model.TakeoutStatusPreparing:
			if to.PayStatus != model.PayStatusPaid {
				return ErrTakeoutStatusInvalid
			}
		case model.TakeoutStatusFulfilling:
			if to.DeliveryOrderID == nil {
				return ErrTakeoutStatusInvalid
			}
			var d model.DeliveryOrder
			if err := query.NotDeleted(tx).Clauses(clause.Locking{Strength: "UPDATE"}).
				First(&d, *to.DeliveryOrderID).Error; err != nil {
				return err
			}
			if d.Status != model.DeliveryPendingAccept || d.RiderID != nil {
				return ErrTakeoutStatusInvalid
			}
			if err := tx.Model(&d).Update("status", model.DeliveryCancelled).Error; err != nil {
				return err
			}
		default:
			return ErrTakeoutStatusInvalid
		}

		sub, err := payment.TakeoutSubjectFromID(tx, takeoutID, 0)
		if err != nil {
			return err
		}
		if err := s.paymentProvider().RefundSubjectAmountInTx(tx, sub, to.PayAmount, reasonText); err != nil {
			return err
		}
		if err := restoreTakeoutStockInTx(tx, &to); err != nil {
			return err
		}
		res := query.NotDeleted(tx.Model(&model.TakeoutOrder{})).
			Where("id = ? AND status IN ?", takeoutID, []int{int(model.TakeoutStatusPreparing), int(model.TakeoutStatusFulfilling)}).
			Updates(map[string]interface{}{
				"status": model.TakeoutStatusCancelled,
				"remark": reasonText,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrTakeoutStatusInvalid
		}
		mid := merchantID
		AppendFulfillmentEventInTx(tx, FulfillmentEventInput{
			SubjectType: model.FulfillmentSubjectTakeout,
			SubjectID:   takeoutID,
			EventCode:   model.EventMerchantRejected,
			ActorRole:   model.FulfillmentActorMerchant,
			ActorID:     &mid,
			Title:       "商家已拒单",
			Detail:      map[string]interface{}{"reason": reasonText},
		})
		AppendFulfillmentEventInTx(tx, FulfillmentEventInput{
			SubjectType: model.FulfillmentSubjectTakeout,
			SubjectID:   takeoutID,
			EventCode:   model.EventRefundRequested,
			ActorRole:   model.FulfillmentActorSystem,
			Title:       "退款已发起",
			Detail:      map[string]interface{}{"amount": to.PayAmount, "reason": reasonText},
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.GetMerchantView(merchantID, takeoutID)
}
