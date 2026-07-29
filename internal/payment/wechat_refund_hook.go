package payment

import (
	"fmt"
	"log"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/payment/wechatv3"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
)

const wechatRefundCollectorKey = "wechat_refund_collector"

// RefundJob 事务提交后待派发的微信退款任务。
type RefundJob struct {
	Provider    *WeChatProvider
	OrderID     uint64
	OrderNo     string
	OutRefundNo string
	PayAmount   float64
	RefundAmt   float64
	Reason      string
}

// AttachRefundCollector 在事务开始时绑定退款任务收集器；Commit 成功后由 DispatchRefundJobs 派发。
func AttachRefundCollector(tx *gorm.DB, jobs *[]RefundJob) {
	if tx == nil || jobs == nil {
		return
	}
	tx.InstanceSet(wechatRefundCollectorKey, jobs)
}

func enqueueWeChatRefund(tx *gorm.DB, job RefundJob) error {
	val, ok := tx.InstanceGet(wechatRefundCollectorKey)
	if !ok {
		return fmt.Errorf("%w: refund collector missing (call AttachRefundCollector)", ErrInvalidState)
	}
	ptr, ok := val.(*[]RefundJob)
	if !ok || ptr == nil {
		return fmt.Errorf("%w: refund collector invalid", ErrInvalidState)
	}
	*ptr = append(*ptr, job)
	return nil
}

// DispatchRefundJobs 仅在事务成功提交后调用，发起微信退款。
func DispatchRefundJobs(jobs []RefundJob) {
	for i := range jobs {
		job := jobs[i]
		go dispatchWeChatRefund(job)
	}
}

func dispatchWeChatRefund(job RefundJob) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[wechat refund] panic recovered: %v", r)
			releaseRefundPending(job.Provider, job.OrderID, job.RefundAmt)
		}
	}()
	if job.Provider == nil || job.Provider.Client == nil {
		releaseRefundPending(job.Provider, job.OrderID, job.RefundAmt)
		return
	}
	_, err := job.Provider.Client.CreateRefund(&wechatv3.CreateRefundRequest{
		OutTradeNo:  job.OrderNo,
		OutRefundNo: job.OutRefundNo,
		Reason:      truncateRunes(job.Reason, 80),
		NotifyURL:   job.Provider.NotifyURL,
		Amount: wechatv3.RefundAmount{
			Refund:   wechatv3.YuanToFen(job.RefundAmt),
			Total:    wechatv3.YuanToFen(job.PayAmount),
			Currency: "CNY",
		},
	})
	if err != nil {
		log.Printf("[wechat refund] order %s refund failed: %v", job.OrderNo, err)
		releaseRefundPending(job.Provider, job.OrderID, job.RefundAmt)
	}
}

// releaseRefundPending 微信退款发起失败时释放预留，避免卡死可退余额。
func releaseRefundPending(p *WeChatProvider, orderID uint64, amount float64) {
	if p == nil || p.DB == nil || orderID == 0 || amount <= 0 {
		return
	}
	err := p.DB.Transaction(func(tx *gorm.DB) error {
		var order model.Order
		if err := query.NotDeleted(tx).Select("id", "pay_status", "pay_amount", "refunded_amount", "refund_pending_amount").
			First(&order, orderID).Error; err != nil {
			return err
		}
		pending := order.RefundPendingAmount - amount
		if pending < 0 {
			pending = 0
		}
		status := order.PayStatus
		switch {
		case order.RefundedAmount+0.0001 >= order.PayAmount:
			status = model.PayStatusRefunded
		case pending > 0:
			status = model.PayStatusRefunding
		case order.RefundedAmount > 0:
			status = model.PayStatusPartialRefunded
		default:
			status = model.PayStatusPaid
		}
		return query.NotDeleted(tx.Model(&model.Order{})).Where("id = ?", orderID).Updates(map[string]interface{}{
			"refund_pending_amount": pending,
			"pay_status":            status,
		}).Error
	})
	if err != nil {
		log.Printf("[wechat refund] release pending order %d failed: %v", orderID, err)
	}
}

func refundableRemain(o model.Order) float64 {
	remain := o.PayAmount - o.RefundedAmount - o.RefundPendingAmount
	if remain < 0 {
		return 0
	}
	return remain
}

func fmtRefundConflict(orderID uint64) error {
	return fmt.Errorf("%w: order %d refund conflict", ErrInvalidState, orderID)
}
