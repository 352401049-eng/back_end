package payment

import (
	"errors"
	"fmt"
	"log"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/payment/wechatv3"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
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
}

func (p *WeChatProvider) Name() string          { return "wechat" }
func (p *WeChatProvider) ImmediateSettle() bool { return false }

// SettlePaidInTx 微信渠道禁止业务层直接"记已付"，必须经回调/查单确认。
func (p *WeChatProvider) SettlePaidInTx(tx *gorm.DB, orderID uint64, payAmount float64, at time.Time) error {
	return fmt.Errorf("%w: wechat settle must go through notify", ErrNotSupported)
}

// CreatePrepay 发起 JSAPI 预支付，返回 wx.requestPayment 所需参数。
func (p *WeChatProvider) CreatePrepay(orderID uint64, accountID uint64) (*PrepayResult, error) {
	if !p.Enabled || p.Client == nil {
		return nil, ErrNotConfigured
	}

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
	if order.Status != model.OrderStatusPendingPay {
		return nil, ErrInvalidState
	}
	if order.PayStatus != model.PayStatusUnpaid {
		return &PrepayResult{Provider: p.Name(), AlreadyPaid: true, NeedPay: false,
			Message: "订单已支付"}, nil
	}

	// 2. 幂等：已有成功流水则直接返回
	var existingTx model.PaymentTransaction
	err := p.DB.
		Where("order_id = ? AND status = ?", orderID, model.PayTxStatusPaid).
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
		OrderID:   orderID,
		OrderNo:   order.OrderNo,
		PrepayID:  &prepayID,
		PayAmount: order.PayAmount,
		Status:    model.PayTxStatusPrepay,
	}
	if err := p.DB.Create(&pt).Error; err != nil {
		// 唯一索引冲突：并发创建了同 prepay_id 流水，重查即可
		if isDuplicateKey(err) {
			return p.CreatePrepay(orderID, accountID)
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

// HandleNotify 处理微信支付回调：验签 → 解密 → 按事件类型分流。
func (p *WeChatProvider) HandleNotify(headers map[string]string, body []byte) (*NotifyResult, error) {
	if !p.Enabled || p.Client == nil {
		return nil, ErrNotConfigured
	}

	eventType, plaintext, err := p.Client.ParseAndDecryptNotify(headers, body)
	if err != nil {
		log.Printf("[wechat notify] verify/decrypt failed: %v", err)
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

	// 幂等：transaction_id 唯一索引保证
	txID := notify.TransactionID
	if txID == "" {
		return nil, fmt.Errorf("回调缺少 transaction_id")
	}

	err = p.DB.Transaction(func(tx *gorm.DB) error {
		// 1. 更新/创建支付流水
		var pt model.PaymentTransaction
		dbErr := tx.Where("order_no = ?", notify.OutTradeNo).First(&pt).Error
		if errors.Is(dbErr, gorm.ErrRecordNotFound) {
			// 回调先于 CreatePrepay 返回的情况，直接插入
			pt = model.PaymentTransaction{
				OrderNo:       notify.OutTradeNo,
				TransactionID: &txID,
				PayAmount:     float64(notify.Amount.Total) / 100.0,
				Status:        model.PayTxStatusPaid,
			}
			// 反查 order 填充 order_id
			var o model.Order
			if err := query.NotDeleted(tx).Select("id").Where("order_no = ?", notify.OutTradeNo).First(&o).Error; err == nil {
				pt.OrderID = o.ID
			}
			if err := tx.Create(&pt).Error; err != nil {
				if isDuplicateKey(err) {
					return nil // 并发已处理
				}
				return err
			}
		} else if dbErr != nil {
			return dbErr
		} else {
			// 已有流水，更新状态
			if pt.Status == model.PayTxStatusPaid {
				return nil // 已处理
			}
			rawJSON := string(data)
			if err := tx.Model(&pt).Updates(map[string]interface{}{
				"transaction_id": txID,
				"status":         model.PayTxStatusPaid,
				"wechat_raw":     &rawJSON,
			}).Error; err != nil {
				return err
			}
		}

		// 2. 更新订单支付状态
		orderID := pt.OrderID
		if orderID == 0 {
			var o model.Order
			if err := query.NotDeleted(tx).Select("id").Where("order_no = ?", notify.OutTradeNo).First(&o).Error; err != nil {
				return err
			}
			orderID = o.ID
		}
		now := time.Now()
		res := query.NotDeleted(tx.Model(&model.Order{})).
			Where("id = ? AND pay_status = ?", orderID, model.PayStatusUnpaid).
			Updates(map[string]interface{}{
				"pay_status": model.PayStatusPaid,
				"paid_at":    now,
				"prepay_id":  nil,
			})
		if res.Error != nil {
			return res.Error
		}
		// 如果已是 Paid（幂等），也算成功
		if res.RowsAffected == 0 {
			var o model.Order
			if err := query.NotDeleted(tx).Select("pay_status").First(&o, orderID).Error; err != nil {
				return err
			}
			if o.PayStatus != model.PayStatusPaid {
				return ErrInvalidState
			}
		}

		// 3. 推进订单状态：PendingPay → PendingFulfill（拼团单 PendingGroup 不动）
		if err := tx.Model(&model.Order{}).
			Where("id = ? AND status = ?", orderID, model.OrderStatusPendingPay).
			Updates(map[string]interface{}{
				"status":                model.OrderStatusPendingFulfill,
				"merchant_review_stage": model.MerchantReviewPending,
				"pay_expire_at":         nil,
			}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		if isDuplicateKey(err) {
			return &NotifyResult{Paid: true, RawAck: `{"code":"SUCCESS"}`}, nil
		}
		return nil, err
	}

	return &NotifyResult{
		OrderNo: notify.OutTradeNo,
		Paid:    true,
		RawAck:  `{"code":"SUCCESS"}`,
	}, nil
}

// handleRefundSuccess 处理退款成功回调。
func (p *WeChatProvider) handleRefundSuccess(data []byte) (*NotifyResult, error) {
	notify, err := wechatv3.UnmarshalRefundSuccess(data)
	if err != nil {
		return nil, err
	}
	if notify.RefundStatus != "SUCCESS" {
		return &NotifyResult{RawAck: `{"code":"SUCCESS"}`}, nil
	}

	err = p.DB.Transaction(func(tx *gorm.DB) error {
		var pt model.PaymentTransaction
		if err := tx.Where("order_no = ?", notify.OutTradeNo).First(&pt).Error; err != nil {
			return err
		}
		if pt.Status == model.PayTxStatusRefunded {
			return nil
		}
		if err := tx.Model(&pt).Update("status", model.PayTxStatusRefunded).Error; err != nil {
			return err
		}
		return query.NotDeleted(tx.Model(&model.Order{})).
			Where("id = ? AND pay_status = ?", pt.OrderID, model.PayStatusRefunding).
			Update("pay_status", model.PayStatusRefunded).Error
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

// RefundInTx 在事务内将订单标记为退款中，事务外异步发起微信退款。
func (p *WeChatProvider) RefundInTx(tx *gorm.DB, orderID uint64) error {
	if !p.Enabled || p.Client == nil {
		return ErrNotConfigured
	}

	var order model.Order
	if err := query.NotDeleted(tx).Select("id", "order_no", "pay_status", "pay_amount").
		First(&order, orderID).Error; err != nil {
		return err
	}

	switch order.PayStatus {
	case model.PayStatusUnpaid:
		return nil // 无需退款
	case model.PayStatusRefunded, model.PayStatusPartialRefunded:
		return nil
	case model.PayStatusPaid, model.PayStatusRefunding:
			// 标记退款中，防并发重复退款
			res := query.NotDeleted(tx.Model(&model.Order{})).
				Where("id = ? AND pay_status = ?", orderID, order.PayStatus).
				Update("pay_status", model.PayStatusRefunding)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return nil // 并发已处理
			}
			// 异步发起微信退款（事务提交后执行）
			client := p.Client
			notifyURL := p.NotifyURL
			orderNo := order.OrderNo
			payAmount := order.PayAmount
			go func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[wechat refund] panic recovered: %v", r)
					}
				}()
			_, err := client.CreateRefund(&wechatv3.CreateRefundRequest{
				OutTradeNo: orderNo,
				OutRefundNo: "RF" + orderNo,
				Reason:      "用户取消",
				NotifyURL:   notifyURL,
				Amount: wechatv3.RefundAmount{
					Refund:   wechatv3.YuanToFen(payAmount),
					Total:    wechatv3.YuanToFen(payAmount),
					Currency: "CNY",
				},
			})
			if err != nil {
				log.Printf("[wechat refund] order %s refund failed: %v", orderNo, err)
			}
		}()
		return nil
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
