package payment

import (
	"fmt"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
)

// MockProvider 模拟支付：下单事务内立即结算；取消/拒单事务内立即退款记账。
type MockProvider struct {
	DB *gorm.DB
}

func (p *MockProvider) Name() string          { return "mock" }
func (p *MockProvider) ImmediateSettle() bool { return true }

func (p *MockProvider) SettlePaidInTx(tx *gorm.DB, orderID uint64, payAmount float64, at time.Time) error {
	if orderID == 0 {
		return ErrInvalidState
	}
	if payAmount < 0 {
		return fmt.Errorf("%w: pay_amount invalid", ErrInvalidState)
	}
	_ = payAmount // 金额以下单时写入的 pay_amount 为准，结算只改支付态
	res := query.NotDeleted(tx.Model(&model.Order{})).
		Where("id = ? AND pay_status = ?", orderID, model.PayStatusUnpaid).
		Updates(map[string]interface{}{
			"pay_status": model.PayStatusPaid,
			"paid_at":    at,
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		// 允许幂等：已是 Paid 则视为成功
		var o model.Order
		if err := query.NotDeleted(tx).Select("id", "pay_status").First(&o, orderID).Error; err != nil {
			return err
		}
		if o.PayStatus == model.PayStatusPaid {
			return nil
		}
		return ErrInvalidState
	}
	return nil
}

func (p *MockProvider) RefundInTx(tx *gorm.DB, orderID uint64) error {
	if orderID == 0 {
		return ErrInvalidState
	}
	var o model.Order
	if err := query.NotDeleted(tx).Select("id", "pay_status").First(&o, orderID).Error; err != nil {
		return err
	}
	switch o.PayStatus {
	case model.PayStatusUnpaid:
		return nil
	case model.PayStatusRefunded, model.PayStatusPartialRefunded:
		return nil
	case model.PayStatusPaid, model.PayStatusRefunding:
		return query.NotDeleted(tx.Model(&model.Order{})).
			Where("id = ?", orderID).
			Update("pay_status", model.PayStatusRefunded).Error
	default:
		return ErrInvalidState
	}
}

func (p *MockProvider) CreatePrepay(orderID uint64, accountID uint64) (*PrepayResult, error) {
	var o model.Order
	if err := query.NotDeleted(p.DB).
		Where("id = ? AND account_id = ?", orderID, accountID).
		First(&o).Error; err != nil {
		return nil, err
	}
	if o.PayStatus == model.PayStatusPaid {
		return &PrepayResult{
			Provider: p.Name(), AlreadyPaid: true, NeedPay: false,
			Message: "模拟支付已结算",
		}, nil
	}
	if o.PayStatus != model.PayStatusUnpaid {
		return nil, ErrInvalidState
	}
	now := time.Now()
	if err := p.DB.Transaction(func(tx *gorm.DB) error {
		return p.SettlePaidInTx(tx, o.ID, o.PayAmount, now)
	}); err != nil {
		return nil, err
	}
	return &PrepayResult{
		Provider: p.Name(), AlreadyPaid: true, NeedPay: false,
		Message: "模拟支付已结算",
	}, nil
}

func (p *MockProvider) HandleNotify(headers map[string]string, body []byte) (*NotifyResult, error) {
	return nil, fmt.Errorf("%w: mock provider has no async notify", ErrNotSupported)
}
