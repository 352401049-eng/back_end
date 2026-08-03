package service

import (
	"errors"
	"fmt"

	"yujixinjiang/backend/internal/model"

	"gorm.io/gorm"
)

// ErrPendingPayDuplicate 同一用户对同一商品存在未完成支付的入包订单，禁止重复下单。
var ErrPendingPayDuplicate = errors.New("pending pay duplicate product")

// PendingPayDuplicateError 携带待付款订单信息，供前端跳转订单列表/详情。
type PendingPayDuplicateError struct {
	OrderID   uint64
	OrderNo   string
	ProductID uint64
	Status    uint8
}

func (e *PendingPayDuplicateError) Error() string {
	return fmt.Sprintf("%s: 您有该商品的待付款订单，请前往订单查看并完成支付或取消后再购买", ErrPendingPayDuplicate.Error())
}

func (e *PendingPayDuplicateError) Unwrap() error { return ErrPendingPayDuplicate }

// StatusCode 与前端订单筛选项一致：pending_pay | pending_group。
func (e *PendingPayDuplicateError) StatusCode() string {
	if e != nil && e.Status == model.OrderStatusPendingGroup {
		return "pending_group"
	}
	return "pending_pay"
}

// assertNoUnpaidBagOrderForProducts 入包下单前拦截：存在含任一 product_id 且未支付的
// pending_pay / pending_group 订单时拒绝（外卖单不在此范围）。
func assertNoUnpaidBagOrderForProducts(db *gorm.DB, accountID uint64, productIDs []uint64) error {
	ids := uniqueUint64s(productIDs)
	if accountID == 0 || len(ids) == 0 {
		return nil
	}
	var row struct {
		ID        uint64
		OrderNo   string
		Status    uint8
		ProductID uint64
	}
	// 显式表前缀，避免 JOIN 时 is_deleted 歧义（order 为 MySQL 保留字）
	err := db.Table("`order` AS o").
		Select("o.id, o.order_no, o.status, oi.product_id").
		Joins("INNER JOIN order_item oi ON oi.order_id = o.id AND oi.is_deleted = ?", model.NotDeleted).
		// 勿用 []uint8：GORM 会当成 []byte，生成 status IN '<binary>' 导致 SQL 语法错误
		Where("o.is_deleted = ? AND o.account_id = ? AND o.pay_status = ? AND o.status IN ? AND oi.product_id IN ?",
			model.NotDeleted,
			accountID,
			model.PayStatusUnpaid,
			[]int{int(model.OrderStatusPendingPay), int(model.OrderStatusPendingGroup)},
			ids,
		).
		Order("o.id ASC").
		Limit(1).
		Take(&row).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil
		}
		return err
	}
	return &PendingPayDuplicateError{
		OrderID:   row.ID,
		OrderNo:   row.OrderNo,
		ProductID: row.ProductID,
		Status:    row.Status,
	}
}

func uniqueUint64s(in []uint64) []uint64 {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[uint64]struct{}, len(in))
	out := make([]uint64, 0, len(in))
	for _, id := range in {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
