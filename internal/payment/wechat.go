package payment

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/payment/wechatv3"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// WeChatProvider 微信支付 V3 实现。
type WeChatProvider struct {
	DB        *gorm.DB
	AppID     string
	MchID     string
	APIKey    string
	NotifyURL string
	Enabled   bool
	Client    *wechatv3.Client // V3 API 客户端
	// OnPaidInTx 支付成功后推进入包订单状态（含自动审核入背包）。由 OrderService 注入。
	OnPaidInTx func(tx *gorm.DB, orderID uint64) error
	// OnSubjectPaidInTx 支付成功后推进业务状态（外卖/跑腿等）。由对应 Service 注入。
	OnSubjectPaidInTx func(tx *gorm.DB, sub PaySubject) error
}

func (p *WeChatProvider) Name() string          { return "wechat" }
func (p *WeChatProvider) ImmediateSettle() bool { return false }

// SettlePaidInTx 微信渠道禁止业务层直接"记已付"，必须经回调/查单确认。
func (p *WeChatProvider) SettlePaidInTx(tx *gorm.DB, orderID uint64, payAmount float64, at time.Time) error {
	return fmt.Errorf("%w: wechat settle must go through notify", ErrNotSupported)
}

// SettleSubjectPaidInTx 微信渠道禁止业务层直接"记已付"，必须经回调/查单确认。
func (p *WeChatProvider) SettleSubjectPaidInTx(tx *gorm.DB, sub PaySubject, at time.Time) error {
	return fmt.Errorf("%w: wechat settle must go through notify", ErrNotSupported)
}

// CreatePrepay 发起 JSAPI 预支付，返回 wx.requestPayment 所需参数（入包订单包装器）。
func (p *WeChatProvider) CreatePrepay(orderID uint64, accountID uint64) (*PrepayResult, error) {
	sub, err := OrderSubjectFromID(p.DB, orderID, accountID)
	if err != nil {
		return nil, err
	}
	return p.CreatePrepayForSubject(sub)
}

// CreatePrepayForSubject 为支付主体发起 JSAPI 预支付。
func (p *WeChatProvider) CreatePrepayForSubject(sub PaySubject) (*PrepayResult, error) {
	if !p.Enabled || p.Client == nil {
		return nil, ErrNotConfigured
	}
	if err := sub.Validate(); err != nil {
		return nil, err
	}

	switch sub.Type {
	case model.PaySubjectOrder:
		return p.createOrderPrepay(sub)
	case model.PaySubjectTakeout:
		return p.createTakeoutPrepay(sub)
	case model.PaySubjectDeliveryFee:
		return nil, fmt.Errorf("%w: delivery_fee prepay not wired yet", ErrNotSupported)
	default:
		return nil, ErrInvalidState
	}
}

func (p *WeChatProvider) createOrderPrepay(sub PaySubject) (*PrepayResult, error) {
	orderID := sub.ID
	accountID := sub.AccountID

	// 1. 查订单，校验归属和状态
	var order model.Order
	if err := query.NotDeleted(p.DB).
		Where("id = ? AND account_id = ?", orderID, accountID).
		First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: 订单不存在", ErrNotSupported)
		}
		return nil, err
	}
	// 直购待支付 / 拼团待成团且未付均可发起预支付
	switch order.Status {
	case model.OrderStatusPendingPay, model.OrderStatusPendingGroup:
	default:
		return nil, ErrInvalidState
	}
	if order.PayStatus != model.PayStatusUnpaid {
		return &PrepayResult{Provider: p.Name(), AlreadyPaid: true, NeedPay: false,
			Message: "订单已支付"}, nil
	}

	// 2. 幂等：已有成功流水则直接返回（含迁移前 subject_id=0 的 legacy 行）
	var existingTx model.PaymentTransaction
	err := paidTransactionBySubject(p.DB, model.PaySubjectOrder, orderID).
		First(&existingTx).Error
	if err == nil {
		return &PrepayResult{Provider: p.Name(), AlreadyPaid: true, NeedPay: false,
			Message: "订单已支付"}, nil
	}

	// 3. 获取用户 openid
	var account model.Account
	if err := query.NotDeleted(p.DB).Select("id", "openid").First(&account, accountID).Error; err != nil {
		return nil, fmt.Errorf("获取用户 openid 失败: %w", err)
	}
	if account.OpenID == nil || *account.OpenID == "" {
		return nil, fmt.Errorf("用户未绑定微信 openid")
	}

	// 4. 取首个商品名作为支付描述（限 127 字节）
	desc := "雨季新江商品"
	var item model.OrderItem
	if err := query.NotDeleted(p.DB).Where("order_id = ?", orderID).First(&item).Error; err == nil {
		if len(item.ProductName) > 0 {
			desc = truncateRunes(item.ProductName, 40)
		}
	}

	// 5. 调用微信 JSAPI 统一下单
	expireTime := ""
	if order.PayExpireAt != nil {
		expireTime = order.PayExpireAt.Format(time.RFC3339)
	}
	prepayResp, err := p.Client.CreateJSAPIPrepay(&wechatv3.CreateJSAPIPrepayRequest{
		AppID:       p.AppID,
		MchID:       p.MchID,
		Description: desc,
		OutTradeNo:  order.OrderNo,
		NotifyURL:   p.NotifyURL,
		Amount: wechatv3.PrepayAmount{
			Total:    wechatv3.YuanToFen(order.PayAmount),
			Currency: "CNY",
		},
		Payer: wechatv3.PrepayPayer{
			OpenID: *account.OpenID,
		},
		TimeExpire: expireTime,
	})
	if err != nil {
		return nil, fmt.Errorf("微信下单失败: %w", err)
	}

	// 6. 保存支付流水
	prepayID := prepayResp.PrepayID
	pt := model.PaymentTransaction{
		SubjectType: model.PaySubjectOrder,
		SubjectID:   orderID,
		OrderID:     orderID,
		OrderNo:     order.OrderNo,
		PrepayID:    &prepayID,
		PayAmount:   order.PayAmount,
		Status:      model.PayTxStatusPrepay,
	}
	if err := p.DB.Create(&pt).Error; err != nil {
		// 唯一索引冲突：并发创建了同 prepay_id 流水，重查即可
		if isDuplicateKey(err) {
			return p.CreatePrepayForSubject(sub)
		}
		return nil, err
	}

	// 7. 更新订单 prepay_id
	_ = p.DB.Model(&order).Update("prepay_id", prepayID).Error

	// 8. 生成 wx.requestPayment 二次签名
	params, err := p.Client.Signer().SignPrepay(p.AppID, prepayID)
	if err != nil {
		return nil, fmt.Errorf("生成支付签名失败: %w", err)
	}

	return &PrepayResult{
		Provider: p.Name(),
		NeedPay:  true,
		Params:   params,
		Message:  "请调起微信支付",
	}, nil
}

func (p *WeChatProvider) createTakeoutPrepay(sub PaySubject) (*PrepayResult, error) {
	takeoutID := sub.ID
	accountID := sub.AccountID

	var takeout model.TakeoutOrder
	if err := query.NotDeleted(p.DB).
		Where("id = ? AND account_id = ?", takeoutID, accountID).
		First(&takeout).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("%w: 外卖订单不存在", ErrNotSupported)
		}
		return nil, err
	}
	if takeout.Status != model.TakeoutStatusPendingPay {
		return nil, ErrInvalidState
	}
	if takeout.PayStatus != model.PayStatusUnpaid {
		return &PrepayResult{Provider: p.Name(), AlreadyPaid: true, NeedPay: false,
			Message: "订单已支付"}, nil
	}

	var existingTx model.PaymentTransaction
	err := paidTransactionBySubject(p.DB, model.PaySubjectTakeout, takeoutID).
		First(&existingTx).Error
	if err == nil {
		return &PrepayResult{Provider: p.Name(), AlreadyPaid: true, NeedPay: false,
			Message: "订单已支付"}, nil
	}

	var account model.Account
	if err := query.NotDeleted(p.DB).Select("id", "openid").First(&account, accountID).Error; err != nil {
		return nil, fmt.Errorf("获取用户 openid 失败: %w", err)
	}
	if account.OpenID == nil || *account.OpenID == "" {
		return nil, fmt.Errorf("用户未绑定微信 openid")
	}

	desc := "雨季新江外卖"
	var item model.TakeoutOrderItem
	if err := query.NotDeleted(p.DB).Where("takeout_order_id = ?", takeoutID).First(&item).Error; err == nil {
		if len(item.ProductName) > 0 {
			desc = truncateRunes(item.ProductName, 40)
		}
	}

	expireTime := ""
	if takeout.PayExpireAt != nil {
		expireTime = takeout.PayExpireAt.Format(time.RFC3339)
	}
	prepayResp, err := p.Client.CreateJSAPIPrepay(&wechatv3.CreateJSAPIPrepayRequest{
		AppID:       p.AppID,
		MchID:       p.MchID,
		Description: desc,
		OutTradeNo:  takeout.OrderNo,
		NotifyURL:   p.NotifyURL,
		Amount: wechatv3.PrepayAmount{
			Total:    wechatv3.YuanToFen(takeout.PayAmount),
			Currency: "CNY",
		},
		Payer: wechatv3.PrepayPayer{
			OpenID: *account.OpenID,
		},
		TimeExpire: expireTime,
	})
	if err != nil {
		return nil, fmt.Errorf("微信下单失败: %w", err)
	}

	prepayID := prepayResp.PrepayID
	pt := model.PaymentTransaction{
		SubjectType: model.PaySubjectTakeout,
		SubjectID:   takeoutID,
		OrderID:     0,
		OrderNo:     takeout.OrderNo,
		PrepayID:    &prepayID,
		PayAmount:   takeout.PayAmount,
		Status:      model.PayTxStatusPrepay,
	}
	if err := p.DB.Create(&pt).Error; err != nil {
		if isDuplicateKey(err) {
			return p.CreatePrepayForSubject(sub)
		}
		return nil, err
	}

	params, err := p.Client.Signer().SignPrepay(p.AppID, prepayID)
	if err != nil {
		return nil, fmt.Errorf("生成支付签名失败: %w", err)
	}

	return &PrepayResult{
		Provider: p.Name(),
		NeedPay:  true,
		Params:   params,
		Message:  "请调起微信支付",
	}, nil
}

// HandleNotify 处理微信支付回调：验签 → 解密 → 按事件类型分流。
func (p *WeChatProvider) HandleNotify(headers map[string]string, body []byte) (*NotifyResult, error) {
	if !p.Enabled || p.Client == nil {
		return nil, ErrNotConfigured
	}

	eventType, plaintext, err := p.Client.ParseAndDecryptNotify(headers, body)
	if err != nil {
		log.Printf("[wechat notify] verify/decrypt failed: %v", err)
		// 解密失败时，尝试主动查微信支付状态
		if et := parseEventTypeSafely(body); et == wechatv3.EventPaySuccess {
			return p.retrieveAndSettlePayments()
		}
		return nil, fmt.Errorf("回调验证失败: %w", err)
	}

	switch eventType {
	case wechatv3.EventPaySuccess:
		return p.handlePaySuccess(plaintext)
	case wechatv3.EventRefundSuccess:
		return p.handleRefundSuccess(plaintext)
	default:
		log.Printf("[wechat notify] unhandled event type: %s", eventType)
		return &NotifyResult{Paid: false, RawAck: `{"code":"SUCCESS"}`}, nil
	}
}

// handlePaySuccess 处理支付成功回调。
func (p *WeChatProvider) handlePaySuccess(data []byte) (*NotifyResult, error) {
	notify, err := wechatv3.UnmarshalPaySuccess(data)
	if err != nil {
		return nil, err
	}

	if notify.TradeState != "SUCCESS" {
		log.Printf("[wechat notify] trade_state=%s, out_trade_no=%s, skipped", notify.TradeState, notify.OutTradeNo)
		return &NotifyResult{RawAck: `{"code":"SUCCESS"}`}, nil
	}

	txID := notify.TransactionID
	if txID == "" {
		return nil, fmt.Errorf("回调缺少 transaction_id")
	}

	var result NotifyResult
	err = p.DB.Transaction(func(tx *gorm.DB) error {
		sub, err := p.upsertPaidTransaction(tx, notify.OutTradeNo, txID, float64(notify.Amount.Total)/100.0, data)
		if err != nil {
			return err
		}
		now := time.Now()
		if err := p.markSubjectPaidInTx(tx, sub, now); err != nil {
			return err
		}
		if err := p.invokeSubjectPaidCallback(tx, sub); err != nil {
			return err
		}
		result = NotifyResult{
			SubjectType: sub.Type,
			SubjectID:   sub.ID,
			OrderNo:     sub.OrderNo,
			Paid:        true,
			RawAck:      `{"code":"SUCCESS"}`,
		}
		if sub.Type == model.PaySubjectOrder {
			result.OrderID = sub.ID
		}
		return nil
	})
	if err != nil {
		if isDuplicateKey(err) {
			return &NotifyResult{Paid: true, RawAck: `{"code":"SUCCESS"}`}, nil
		}
		return nil, err
	}

	return &result, nil
}

func (p *WeChatProvider) upsertPaidTransaction(tx *gorm.DB, orderNo, txID string, payAmount float64, raw []byte) (PaySubject, error) {
	var pt model.PaymentTransaction
	dbErr := tx.Where("order_no = ?", orderNo).First(&pt).Error
	if errors.Is(dbErr, gorm.ErrRecordNotFound) {
		sub, err := ResolveSubjectByOrderNo(tx, orderNo)
		if err != nil {
			return PaySubject{}, err
		}
		pt = model.PaymentTransaction{
			SubjectType:   sub.Type,
			SubjectID:     sub.ID,
			OrderID:       paymentTransactionOrderID(sub),
			OrderNo:       orderNo,
			TransactionID: &txID,
			PayAmount:     payAmount,
			Status:        model.PayTxStatusPaid,
		}
		if len(raw) > 0 {
			rawJSON := string(raw)
			pt.WechatRaw = &rawJSON
		}
		if err := tx.Create(&pt).Error; err != nil {
			if isDuplicateKey(err) {
				return sub, nil
			}
			return PaySubject{}, err
		}
		return sub, nil
	}
	if dbErr != nil {
		return PaySubject{}, dbErr
	}
	if pt.Status != model.PayTxStatusPaid {
		updates := map[string]interface{}{
			"transaction_id": txID,
			"status":         model.PayTxStatusPaid,
		}
		if len(raw) > 0 {
			rawJSON := string(raw)
			updates["wechat_raw"] = &rawJSON
		}
		if err := tx.Model(&pt).Updates(updates).Error; err != nil {
			return PaySubject{}, err
		}
	}
	sub := PaySubject{
		Type:      pt.SubjectType,
		ID:        pt.SubjectID,
		OrderNo:   pt.OrderNo,
		Amount:    pt.PayAmount,
	}
	if sub.Type == "" || sub.ID == 0 {
		resolved, err := ResolveSubjectByOrderNo(tx, orderNo)
		if err != nil {
			return PaySubject{}, err
		}
		sub = resolved
		backfill := map[string]interface{}{
			"subject_type": sub.Type,
			"subject_id":   sub.ID,
		}
		if oid := paymentTransactionOrderID(sub); oid > 0 {
			backfill["order_id"] = oid
		}
		if err := tx.Model(&pt).Updates(backfill).Error; err != nil {
			return PaySubject{}, err
		}
	}
	return sub, nil
}

func (p *WeChatProvider) markSubjectPaidInTx(tx *gorm.DB, sub PaySubject, at time.Time) error {
	switch sub.Type {
	case model.PaySubjectOrder:
		res := query.NotDeleted(tx.Model(&model.Order{})).
			Where("id = ? AND pay_status = ?", sub.ID, model.PayStatusUnpaid).
			Updates(map[string]interface{}{
				"pay_status": model.PayStatusPaid,
				"paid_at":    at,
				"prepay_id":  nil,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			var o model.Order
			if err := query.NotDeleted(tx).Select("pay_status").First(&o, sub.ID).Error; err != nil {
				return err
			}
			if o.PayStatus != model.PayStatusPaid {
				return ErrInvalidState
			}
		}
		return nil
	case model.PaySubjectTakeout:
		res := query.NotDeleted(tx.Model(&model.TakeoutOrder{})).
			Where("id = ? AND pay_status = ?", sub.ID, model.PayStatusUnpaid).
			Updates(map[string]interface{}{
				"pay_status": model.PayStatusPaid,
				"paid_at":    at,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			var to model.TakeoutOrder
			if err := query.NotDeleted(tx).Select("pay_status").First(&to, sub.ID).Error; err != nil {
				return err
			}
			if to.PayStatus != model.PayStatusPaid {
				return ErrInvalidState
			}
		}
		return nil
	case model.PaySubjectDeliveryFee:
		return fmt.Errorf("%w: delivery_fee settle not wired yet", ErrNotSupported)
	default:
		return ErrInvalidState
	}
}

func (p *WeChatProvider) invokeSubjectPaidCallback(tx *gorm.DB, sub PaySubject) error {
	if p.OnSubjectPaidInTx != nil {
		return p.OnSubjectPaidInTx(tx, sub)
	}
	if sub.Type == model.PaySubjectOrder {
		if p.OnPaidInTx != nil {
			return p.OnPaidInTx(tx, sub.ID)
		}
		return tx.Model(&model.Order{}).
			Where("id = ? AND status = ?", sub.ID, model.OrderStatusPendingPay).
			Updates(map[string]interface{}{
				"status":                model.OrderStatusPendingFulfill,
				"merchant_review_stage": model.MerchantReviewPending,
				"pay_expire_at":         nil,
			}).Error
	}
	return nil
}

// handleRefundSuccess 处理退款成功回调：确认入账 refunded_amount，释放 pending。
func (p *WeChatProvider) handleRefundSuccess(data []byte) (*NotifyResult, error) {
	notify, err := wechatv3.UnmarshalRefundSuccess(data)
	if err != nil {
		return nil, err
	}
	if notify.RefundStatus != "SUCCESS" {
		return &NotifyResult{RawAck: `{"code":"SUCCESS"}`}, nil
	}

	refundYuan := wechatv3.FenToYuan(notify.Amount.Refund)
	if refundYuan < 0 {
		refundYuan = 0
	}

	err = p.DB.Transaction(func(tx *gorm.DB) error {
		var pt model.PaymentTransaction
		if err := tx.Where("order_no = ?", notify.OutTradeNo).First(&pt).Error; err != nil {
			return err
		}
		subjectType := pt.SubjectType
		if subjectType == "" {
			subjectType = SubjectTypeFromOrderNo(notify.OutTradeNo)
		}
		switch subjectType {
		case model.PaySubjectTakeout:
			return p.applyTakeoutRefundInTx(tx, pt, refundYuan)
		case model.PaySubjectDeliveryFee:
			return fmt.Errorf("%w: delivery_fee refund not wired yet", ErrNotSupported)
		default:
			return p.applyOrderRefundInTx(tx, pt, refundYuan)
		}
	})
	if err != nil {
		return nil, err
	}
	return &NotifyResult{
		OrderNo: notify.OutTradeNo,
		Paid:    true,
		RawAck:  `{"code":"SUCCESS"}`,
	}, nil
}

func (p *WeChatProvider) applyOrderRefundInTx(tx *gorm.DB, pt model.PaymentTransaction, refundYuan float64) error {
	var order model.Order
	if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
		Select("id", "pay_status", "pay_amount", "refunded_amount", "refund_pending_amount").
		First(&order, pt.OrderID).Error; err != nil {
		return err
	}

	newRefunded := order.RefundedAmount + refundYuan
	if newRefunded > order.PayAmount {
		newRefunded = order.PayAmount
	}
	newPending := order.RefundPendingAmount - refundYuan
	if newPending < 0 {
		newPending = 0
	}
	status := model.PayStatusPartialRefunded
	switch {
	case newRefunded+0.0001 >= order.PayAmount:
		status = model.PayStatusRefunded
		newPending = 0
	case newPending > 0:
		status = model.PayStatusRefunding
	}

	if err := query.NotDeleted(tx.Model(&model.Order{})).Where("id = ?", order.ID).Updates(map[string]interface{}{
		"refunded_amount":       newRefunded,
		"refund_pending_amount": newPending,
		"pay_status":            status,
	}).Error; err != nil {
		return err
	}
	if status == model.PayStatusRefunded {
		_ = query.NotDeleted(tx.Model(&model.Order{})).Where("id = ?", order.ID).
			Update("status", model.OrderStatusRefunded).Error
	}
	if pt.Status != model.PayTxStatusRefunded && status == model.PayStatusRefunded {
		_ = tx.Model(&pt).Update("status", model.PayTxStatusRefunded).Error
	}
	return nil
}

func (p *WeChatProvider) applyTakeoutRefundInTx(tx *gorm.DB, pt model.PaymentTransaction, refundYuan float64) error {
	var takeout model.TakeoutOrder
	if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
		Select("id", "pay_status", "pay_amount", "refunded_amount").
		First(&takeout, pt.SubjectID).Error; err != nil {
		return err
	}
	newRefunded := takeout.RefundedAmount + refundYuan
	if newRefunded > takeout.PayAmount {
		newRefunded = takeout.PayAmount
	}
	status := model.PayStatusPartialRefunded
	if newRefunded+0.0001 >= takeout.PayAmount {
		status = model.PayStatusRefunded
	}
	if err := query.NotDeleted(tx.Model(&model.TakeoutOrder{})).Where("id = ?", takeout.ID).Updates(map[string]interface{}{
		"refunded_amount": newRefunded,
		"pay_status":      status,
	}).Error; err != nil {
		return err
	}
	if status == model.PayStatusRefunded {
		_ = query.NotDeleted(tx.Model(&model.TakeoutOrder{})).Where("id = ?", takeout.ID).
			Update("status", model.TakeoutStatusCancelled).Error
	}
	if pt.Status != model.PayTxStatusRefunded && status == model.PayStatusRefunded {
		_ = tx.Model(&pt).Update("status", model.PayTxStatusRefunded).Error
	}
	return nil
}

// RefundInTx 在事务内预留全额退款，事务提交后再异步发起微信退款。
func (p *WeChatProvider) RefundInTx(tx *gorm.DB, orderID uint64) error {
	return p.RefundAmountInTx(tx, orderID, 0, "用户取消")
}

// RefundAmountInTx 按金额预留退款（不提前记入 refunded_amount）。amount<=0 表示退剩余全部。
// 真正的微信 CreateRefund 在事务 Commit 之后执行；失败会释放 pending。
func (p *WeChatProvider) RefundAmountInTx(tx *gorm.DB, orderID uint64, amount float64, reason string) error {
	sub, err := OrderSubjectFromID(tx, orderID, 0)
	if err != nil {
		return err
	}
	return p.RefundSubjectAmountInTx(tx, sub, amount, reason)
}

func (p *WeChatProvider) RefundSubjectAmountInTx(tx *gorm.DB, sub PaySubject, amount float64, reason string) error {
	if !p.Enabled || p.Client == nil {
		return ErrNotConfigured
	}
	if err := sub.Validate(); err != nil {
		return err
	}
	switch sub.Type {
	case model.PaySubjectOrder:
		return p.refundOrderAmountInTx(tx, sub, amount, reason)
	case model.PaySubjectTakeout:
		return p.refundTakeoutAmountInTx(tx, sub, amount, reason)
	case model.PaySubjectDeliveryFee:
		return fmt.Errorf("%w: delivery_fee refund not wired yet", ErrNotSupported)
	default:
		return ErrInvalidState
	}
}

func (p *WeChatProvider) refundOrderAmountInTx(tx *gorm.DB, sub PaySubject, amount float64, reason string) error {
	var order model.Order
	if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
		Select("id", "order_no", "pay_status", "pay_amount", "refunded_amount", "refund_pending_amount").
		First(&order, sub.ID).Error; err != nil {
		return err
	}

	switch order.PayStatus {
	case model.PayStatusUnpaid:
		return nil
	case model.PayStatusRefunded:
		return nil
	case model.PayStatusPaid, model.PayStatusRefunding, model.PayStatusPartialRefunded:
		remain := refundableRemain(order)
		refund := amount
		if refund <= 0 || refund >= remain {
			refund = remain
		}
		if refund <= 0 {
			if amount > 0 {
				return fmt.Errorf("%w: no refundable balance", ErrInvalidState)
			}
			return nil
		}
		refund = roundMoney(refund)
		newPending := roundMoney(order.RefundPendingAmount + refund)
		res := optimisticRefundWhere(
			query.NotDeleted(tx.Model(&model.Order{})),
			order.ID, order.PayStatus, order.RefundedAmount, order.RefundPendingAmount,
		).Updates(map[string]interface{}{
			"pay_status":            model.PayStatusRefunding,
			"refund_pending_amount": newPending,
			"status":                model.OrderStatusRefunding,
		})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return fmtRefundConflict(order.ID)
		}
		refundReason := reason
		if refundReason == "" {
			refundReason = "用户退款"
		}
		if err := enqueueWeChatRefund(tx, RefundJob{
			Provider:    p,
			SubjectType: model.PaySubjectOrder,
			SubjectID:   order.ID,
			OrderID:     order.ID,
			OrderNo:     order.OrderNo,
			OutRefundNo: fmt.Sprintf("RF%s%d", order.OrderNo, time.Now().UnixNano()%1e12),
			PayAmount:   order.PayAmount,
			RefundAmt:   refund,
			Reason:      refundReason,
		}); err != nil {
			return err
		}
		return nil
	default:
		return ErrInvalidState
	}
}

func (p *WeChatProvider) refundTakeoutAmountInTx(tx *gorm.DB, sub PaySubject, amount float64, reason string) error {
	var takeout model.TakeoutOrder
	if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
		Select("id", "order_no", "pay_status", "pay_amount", "refunded_amount").
		First(&takeout, sub.ID).Error; err != nil {
		return err
	}
	switch takeout.PayStatus {
	case model.PayStatusUnpaid, model.PayStatusRefunded:
		return nil
	case model.PayStatusPaid, model.PayStatusRefunding, model.PayStatusPartialRefunded:
		remain := takeout.PayAmount - takeout.RefundedAmount
		if remain < 0 {
			remain = 0
		}
		refund := amount
		if refund <= 0 || refund >= remain {
			refund = remain
		}
		if refund <= 0 {
			if amount > 0 {
				return fmt.Errorf("%w: no refundable balance", ErrInvalidState)
			}
			return nil
		}
		refund = roundMoney(refund)
		newPending := roundMoney(refund)
		res := query.NotDeleted(tx.Model(&model.TakeoutOrder{})).
			Where("id = ? AND pay_status = ? AND refunded_amount = ?", takeout.ID, takeout.PayStatus, takeout.RefundedAmount).
			Updates(map[string]interface{}{
				"pay_status": model.PayStatusRefunding,
			})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("%w: takeout %d refund conflict", ErrInvalidState, takeout.ID)
		}
		refundReason := reason
		if refundReason == "" {
			refundReason = "用户退款"
		}
		return enqueueWeChatRefund(tx, RefundJob{
			Provider:    p,
			SubjectType: model.PaySubjectTakeout,
			SubjectID:   takeout.ID,
			OrderNo:     takeout.OrderNo,
			OutRefundNo: fmt.Sprintf("RF%s%d", takeout.OrderNo, time.Now().UnixNano()%1e12),
			PayAmount:   takeout.PayAmount,
			RefundAmt:   newPending,
			Reason:      refundReason,
		})
	default:
		return ErrInvalidState
	}
}

// --- helper ---

func truncateRunes(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen])
}

// isDuplicateKey 判断是否为 MySQL 唯一索引冲突。
func isDuplicateKey(err error) bool {
	return errors.Is(err, gorm.ErrDuplicatedKey)
}

// parseEventTypeSafely 从回调 JSON 中提取 event_type（不依赖 resource 解密）。
func parseEventTypeSafely(body []byte) string {
	var cb struct {
		EventType string `json:"event_type"`
	}
	if json.Unmarshal(body, &cb) == nil {
		return cb.EventType
	}
	return ""
}

// retrieveAndSettlePayments 主动查微信支付单状态，处理所有待支付订单。
func (p *WeChatProvider) retrieveAndSettlePayments() (*NotifyResult, error) {
	if p.Client == nil {
		return nil, ErrNotConfigured
	}
	var orders []model.Order
	if err := query.NotDeleted(p.DB).
		Where("status IN ? AND pay_status = ?",
			[]int{int(model.OrderStatusPendingPay), int(model.OrderStatusPendingGroup)},
			model.PayStatusUnpaid).
		Find(&orders).Error; err != nil {
		return nil, err
	}
	for _, o := range orders {
		tradeState, transactionID, err := p.Client.QueryOrderByOutTradeNo(o.OrderNo)
		if err != nil {
			log.Printf("[wechat retrieve] 查询订单 %s 失败: %v", o.OrderNo, err)
			continue
		}
		if tradeState == "SUCCESS" && o.PrepayID != nil {
			_ = p.DB.Transaction(func(tx *gorm.DB) error {
				txID := transactionID
				now := time.Now()
				tx.Model(&model.PaymentTransaction{}).Where("order_id = ?", o.ID).
					Updates(map[string]interface{}{"status": model.PayTxStatusPaid, "transaction_id": txID})
				res := query.NotDeleted(tx.Model(&model.Order{})).
					Where("id = ? AND pay_status = ?", o.ID, model.PayStatusUnpaid).
					Updates(map[string]interface{}{
						"pay_status": model.PayStatusPaid,
						"paid_at":    now,
						"prepay_id":  nil,
					})
				if res.RowsAffected == 0 {
					return nil
				}
				// 推进订单 + 自动审核
				if p.OnPaidInTx != nil {
					return p.OnPaidInTx(tx, o.ID)
				}
				// 仅直购待支付推进；拼团单保持 PendingGroup，等成团逻辑处理
				return tx.Model(&model.Order{}).Where("id = ? AND status = ?", o.ID, model.OrderStatusPendingPay).
					Updates(map[string]interface{}{
						"status":                model.OrderStatusPendingFulfill,
						"merchant_review_stage": model.MerchantReviewPending,
						"pay_expire_at":         nil,
					}).Error
			})
			log.Printf("[wechat retrieve] 订单 %s 支付成功，已处理", o.OrderNo)
		}
	}
	return &NotifyResult{Paid: true, RawAck: `{"code":"SUCCESS"}`}, nil
}
