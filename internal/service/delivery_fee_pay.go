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
	ErrDeliveryFeeOrderNotFound = errors.New("delivery fee order not found")
	ErrDeliveryFeeStatusInvalid = errors.New("delivery fee order status invalid")
)

type DeliveryFeePayService struct {
	DB                *gorm.DB
	InventorySvc      *InventoryService
	ZoneSvc           *DeliveryZoneService
	Payment           payment.Provider
	PayTimeoutMinutes int
}

type CreateDeliveryFeePayInput struct {
	MerchantID         uint64
	AddressID          uint64
	DeliveryTimeRemark string
	Items              []UseBatchItemInput
	Remark             *string
}

type DeliveryFeePayView struct {
	model.DeliveryFeeOrder
	StatusText string `json:"status_text"`
	StatusCode string `json:"status_code"`
}

type deliveryFeePayload struct {
	Items             []UseBatchItemInput        `json:"items,omitempty"`
	ConvertUsageID    uint64                     `json:"convert_usage_id,omitempty"`
	AddressID         uint64                     `json:"address_id"`
	Remark            *string                    `json:"remark,omitempty"`
	UsageMerchantID   uint64                     `json:"usage_merchant_id,omitempty"`
	PackageSelections []PackageSelectionInput    `json:"package_selections,omitempty"`
	OptionSelections  []OptionSelectionUnitInput `json:"option_selections,omitempty"`
}

func genDeliveryFeeOrderNo() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return fmt.Sprintf("F%s%s", time.Now().Format("20060102150405"), hex.EncodeToString(b))
}

func deliveryFeeStatusMeta(status uint8) (text, code string) {
	switch status {
	case model.DeliveryFeeStatusPendingPay:
		return "待支付", "pending_pay"
	case model.DeliveryFeeStatusFulfilled:
		return "已提交", "fulfilled"
	case model.DeliveryFeeStatusCancelled:
		return "已取消", "cancelled"
	default:
		return "未知", "unknown"
	}
}

func (s *DeliveryFeePayService) toView(o *model.DeliveryFeeOrder) *DeliveryFeePayView {
	text, code := deliveryFeeStatusMeta(o.Status)
	return &DeliveryFeePayView{
		DeliveryFeeOrder: *o,
		StatusText:       text,
		StatusCode:       code,
	}
}

func (s *DeliveryFeePayService) paymentProvider() payment.Provider {
	if s.Payment != nil {
		return s.Payment
	}
	return &payment.MockProvider{DB: s.DB}
}

func (s *DeliveryFeePayService) payTimeoutMinutes() int {
	if s.PayTimeoutMinutes > 0 {
		return s.PayTimeoutMinutes
	}
	return 5
}

func (s *DeliveryFeePayService) inventorySvc() *InventoryService {
	if s.InventorySvc != nil {
		return s.InventorySvc
	}
	return &InventoryService{DB: s.DB, ZoneSvc: s.ZoneSvc}
}

// Create 创建配送费预支付单（草稿 payload，不扣背包）。
func (s *DeliveryFeePayService) Create(accountID uint64, in CreateDeliveryFeePayInput) (*DeliveryFeePayView, error) {
	if in.MerchantID == 0 {
		return nil, fmt.Errorf("%w: 请指定商家", ErrInventoryUsageInvalid)
	}
	if in.AddressID == 0 {
		return nil, ErrAddressRequired
	}
	if len(in.Items) == 0 {
		return nil, fmt.Errorf("%w: 请选择商品", ErrInventoryUsageInvalid)
	}

	remark := in.Remark
	if remark == nil && in.DeliveryTimeRemark != "" {
		r := in.DeliveryTimeRemark
		remark = &r
	}

	batchIn := UseBatchInput{
		Items:                      in.Items,
		UsageMerchantID:            in.MerchantID,
		DeliveryType:                 model.DeliveryTypeDelivery,
		AddressID:                    &in.AddressID,
		Remark:                       remark,
		FulfillAfterDeliveryFeePay:   true,
	}
	if err := s.inventorySvc().validateUseBatchDraft(accountID, batchIn, in.MerchantID); err != nil {
		return nil, err
	}

	var mp model.MerchantProfile
	if err := query.NotDeleted(s.DB).Select("id", "delivery_fee", "rider_earnings").First(&mp, in.MerchantID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMerchantNotFound
		}
		return nil, err
	}
	deliveryFee := roundMoney(mp.DeliveryFee)
	if deliveryFee <= 0 {
		return nil, fmt.Errorf("%w: 该商家无需支付配送费", ErrInventoryUsageInvalid)
	}

	payload := deliveryFeePayload{
		Items:     in.Items,
		AddressID: in.AddressID,
		Remark:    remark,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	now := time.Now()
	expireAt := now.Add(time.Duration(s.payTimeoutMinutes()) * time.Minute)

	var feeOrder model.DeliveryFeeOrder
	err = s.DB.Transaction(func(tx *gorm.DB) error {
		feeOrder = model.DeliveryFeeOrder{
			OrderNo:       genDeliveryFeeOrderNo(),
			AccountID:     accountID,
			MerchantID:    in.MerchantID,
			Status:        model.DeliveryFeeStatusPendingPay,
			Amount:        deliveryFee,
			RiderEarnings: roundMoney(mp.RiderEarnings),
			PayAmount:     deliveryFee,
			PayStatus:     model.PayStatusUnpaid,
			PayExpireAt:   &expireAt,
			Payload:       raw,
		}
		if err := tx.Create(&feeOrder).Error; err != nil {
			return err
		}
		return s.settlePaymentInTx(tx, feeOrder.ID, now)
	})
	if err != nil {
		return nil, err
	}
	return s.toView(&feeOrder), nil
}

func (s *DeliveryFeePayService) settlePaymentInTx(tx *gorm.DB, feeOrderID uint64, at time.Time) error {
	p := s.paymentProvider()
	if !p.ImmediateSettle() {
		return nil
	}
	sub, err := payment.DeliveryFeeSubjectFromID(tx, feeOrderID, 0)
	if err != nil {
		return err
	}
	if err := p.SettleSubjectPaidInTx(tx, sub, at); err != nil {
		return err
	}
	return s.MarkPaidInTx(tx, feeOrderID, at)
}

// CreatePrepay 发起配送费预支付。
func (s *DeliveryFeePayService) CreatePrepay(accountID, feeOrderID uint64) (*payment.PrepayResult, error) {
	sub, err := payment.DeliveryFeeSubjectFromID(s.DB, feeOrderID, accountID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDeliveryFeeOrderNotFound
		}
		return nil, err
	}
	result, err := s.paymentProvider().CreatePrepayForSubject(sub)
	if err != nil {
		return nil, err
	}
	if result.AlreadyPaid {
		if err := s.DB.Transaction(func(tx *gorm.DB) error {
			return s.MarkPaidInTx(tx, feeOrderID, time.Now())
		}); err != nil {
			return nil, err
		}
	}
	return result, nil
}

// MarkPaidInTx 支付成功后扣背包并建 usage + delivery_order。
func (s *DeliveryFeePayService) MarkPaidInTx(tx *gorm.DB, feeOrderID uint64, at time.Time) error {
	var feeOrder model.DeliveryFeeOrder
	if err := query.NotDeleted(tx).Clauses(clause.Locking{Strength: "UPDATE"}).
		First(&feeOrder, feeOrderID).Error; err != nil {
		return err
	}
	if feeOrder.Status == model.DeliveryFeeStatusFulfilled {
		return nil
	}
	if feeOrder.Status != model.DeliveryFeeStatusPendingPay {
		return ErrDeliveryFeeStatusInvalid
	}
	if feeOrder.PayStatus != model.PayStatusPaid {
		return ErrDeliveryFeeStatusInvalid
	}

	var payload deliveryFeePayload
	if err := json.Unmarshal(feeOrder.Payload, &payload); err != nil {
		return fmt.Errorf("%w: invalid payload", ErrDeliveryFeeStatusInvalid)
	}

	base := s.inventorySvc()
	invOnTx := *base
	invOnTx.DB = tx

	var deliveryOrderID *uint64
	if payload.ConvertUsageID > 0 {
		var mp model.MerchantProfile
		if err := query.NotDeleted(tx).Select("id", "delivery_fee", "rider_earnings").First(&mp, feeOrder.MerchantID).Error; err != nil {
			return err
		}
		view, err := invOnTx.convertPendingVerifyInTx(tx, feeOrder.AccountID, payload.ConvertUsageID, ConvertDeliveryInput{
			AddressID:         payload.AddressID,
			UsageMerchantID:   payload.UsageMerchantID,
			Remark:            payload.Remark,
			PackageSelections: payload.PackageSelections,
			OptionSelections:  payload.OptionSelections,
			SkipFeeCheck:      true,
		}, &mp)
		if err != nil {
			return err
		}
		if view != nil && view.DeliveryOrderID != nil {
			deliveryOrderID = view.DeliveryOrderID
		}
	} else {
		result, err := invOnTx.UseBatch(feeOrder.AccountID, UseBatchInput{
			Items:                      payload.Items,
			UsageMerchantID:            feeOrder.MerchantID,
			DeliveryType:               model.DeliveryTypeDelivery,
			AddressID:                  &payload.AddressID,
			Remark:                     payload.Remark,
			FulfillAfterDeliveryFeePay: true,
		})
		if err != nil {
			return err
		}
		deliveryOrderID = result.DeliveryOrderID
	}

	updates := map[string]interface{}{
		"status": model.DeliveryFeeStatusFulfilled,
	}
	if deliveryOrderID != nil {
		updates["delivery_order_id"] = *deliveryOrderID
	}
	if err := tx.Model(&feeOrder).Updates(updates).Error; err != nil {
		return err
	}
	_ = at
	return nil
}

// RefundForDeliveryOrderInTx 配送单取消时退配送费（幂等）。
func (s *DeliveryFeePayService) RefundForDeliveryOrderInTx(tx *gorm.DB, deliveryOrderID uint64, reason string) error {
	var feeOrder model.DeliveryFeeOrder
	err := query.NotDeleted(tx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("delivery_order_id = ? AND status = ?", deliveryOrderID, model.DeliveryFeeStatusFulfilled).
		First(&feeOrder).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if feeOrder.PayStatus == model.PayStatusRefunded || feeOrder.PayStatus == model.PayStatusUnpaid {
		return nil
	}
	sub, err := payment.DeliveryFeeSubjectFromID(tx, feeOrder.ID, 0)
	if err != nil {
		return err
	}
	return s.paymentProvider().RefundSubjectAmountInTx(tx, sub, feeOrder.PayAmount, reason)
}

// ExpireStalePendingPay 关闭超时未支付的配送费单（未扣背包）。
func (s *DeliveryFeePayService) ExpireStalePendingPay(now time.Time) (int, error) {
	var orders []model.DeliveryFeeOrder
	if err := query.NotDeleted(s.DB).
		Where("status = ? AND pay_expire_at IS NOT NULL AND pay_expire_at < ?", model.DeliveryFeeStatusPendingPay, now).
		Limit(100).
		Find(&orders).Error; err != nil {
		return 0, err
	}
	n := 0
	var firstErr error
	for i := range orders {
		if err := s.expireOnePendingPay(orders[i].ID); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("expire pending-pay delivery fee %d: %w", orders[i].ID, err)
			}
			continue
		}
		n++
	}
	return n, firstErr
}

func (s *DeliveryFeePayService) expireOnePendingPay(feeOrderID uint64) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var feeOrder model.DeliveryFeeOrder
		if err := query.NotDeleted(tx).Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&feeOrder, feeOrderID).Error; err != nil {
			return err
		}
		if feeOrder.Status != model.DeliveryFeeStatusPendingPay {
			return nil
		}
		if feeOrder.PayStatus == model.PayStatusPaid {
			return s.MarkPaidInTx(tx, feeOrderID, time.Now())
		}
		if wp, ok := s.Payment.(*payment.WeChatProvider); ok && wp.Client != nil {
			if err := wp.Client.CloseOrder(wp.MchID, feeOrder.OrderNo); err != nil {
				log.Printf("[pay-expire] close wechat delivery fee %s failed: %v", feeOrder.OrderNo, err)
			}
		}
		return query.NotDeleted(tx.Model(&model.DeliveryFeeOrder{})).
			Where("id = ? AND status = ?", feeOrderID, model.DeliveryFeeStatusPendingPay).
			Updates(map[string]interface{}{
				"status":        model.DeliveryFeeStatusCancelled,
				"pay_expire_at": nil,
			}).Error
	})
}

func decodeDeliveryFeePayload(raw json.RawMessage) (deliveryFeePayload, error) {
	var payload deliveryFeePayload
	if len(raw) == 0 {
		return payload, fmt.Errorf("empty payload")
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return payload, err
	}
	return payload, nil
}
