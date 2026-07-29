package service

import (
	"errors"
	"fmt"
	"math"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/payment"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrInventoryRefundInvalid = errors.New("inventory refund invalid")
)

type InventoryRefundView struct {
	InventoryID  uint64  `json:"inventory_id"`
	ProductID    uint64  `json:"product_id"`
	Quantity     uint32  `json:"quantity"`
	RefundAmount float64 `json:"refund_amount"`
	OrderID      uint64  `json:"order_id"`
	RemainQty    uint32  `json:"remain_qty"`
}

// RefundInventory 退还未使用的背包商品：先预留/发起退款，成功记账后再扣库存。
func (s *OrderService) RefundInventory(accountID, inventoryID uint64, quantity uint32) (*InventoryRefundView, error) {
	if quantity == 0 {
		return nil, fmt.Errorf("%w: 退款数量须大于 0", ErrInventoryRefundInvalid)
	}
	if s.InventorySvc == nil {
		return nil, fmt.Errorf("inventory service unavailable")
	}

	var result InventoryRefundView
	err := s.runTx(func(tx *gorm.DB) error {
		var inv model.UserInventory
		if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
			Where("id = ? AND account_id = ?", inventoryID, accountID).
			First(&inv).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrInventoryNotFound
			}
			return err
		}
		if inv.Quantity < quantity {
			return ErrInventoryInsufficient
		}

		allocs, err := allocateInventoryRefund(tx, accountID, inv.ProductID, inv.Spec, quantity)
		if err != nil {
			return err
		}
		if len(allocs) == 0 {
			return fmt.Errorf("%w: 无可退款来源订单，请联系客服", ErrInventoryRefundInvalid)
		}

		var totalRefund float64
		var primaryOrderID uint64
		var totalQty uint32
		remark := "背包未使用退款"
		for _, a := range allocs {
			if primaryOrderID == 0 {
				primaryOrderID = a.OrderID
			}
			// 先支付退款（失败则整单回滚，库存不动）
			if err := s.refundAmountInTx(tx, a.OrderID, a.Amount, "背包未使用退款"); err != nil {
				if errors.Is(err, payment.ErrInvalidState) {
					return fmt.Errorf("%w: 退款冲突或余额不足，请稍后重试", ErrInventoryRefundInvalid)
				}
				return err
			}
			oid := a.OrderID
			if err := s.InventorySvc.adjustQuantity(tx, accountID, inv.ProductID, inv.Spec,
				-int32(a.Quantity), &oid, nil, model.InventoryEventRefund, &remark); err != nil {
				return err
			}
			if err := tx.Model(&model.Product{}).Where("id = ?", inv.ProductID).
				Update("stock", gorm.Expr("stock + ?", a.Quantity)).Error; err != nil {
				return err
			}
			totalRefund += a.Amount
			totalQty += a.Quantity
		}

		var after model.UserInventory
		_ = query.NotDeleted(tx).First(&after, inventoryID)
		result = InventoryRefundView{
			InventoryID:  inventoryID,
			ProductID:    inv.ProductID,
			Quantity:     totalQty,
			RefundAmount: math.Round(totalRefund*100) / 100,
			OrderID:      primaryOrderID,
			RemainQty:    after.Quantity,
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &result, nil
}

type refundAlloc struct {
	OrderID  uint64
	Quantity uint32
	Amount   float64
}

// allocateInventoryRefund 按入账流水 FIFO，把退款数量分摊到仍有余额的来源订单。
func allocateInventoryRefund(tx *gorm.DB, accountID, productID uint64, spec string, quantity uint32) ([]refundAlloc, error) {
	var logs []model.UserInventoryLog
	if err := query.NotDeleted(tx).
		Where("account_id = ? AND product_id = ? AND spec = ? AND order_id IS NOT NULL", accountID, productID, spec).
		Order("id ASC").
		Find(&logs).Error; err != nil {
		return nil, err
	}

	type orderBal struct {
		qty int32
	}
	orderSeq := make([]uint64, 0)
	bal := map[uint64]*orderBal{}
	for _, lg := range logs {
		if lg.OrderID == nil {
			continue
		}
		oid := *lg.OrderID
		b, ok := bal[oid]
		if !ok {
			b = &orderBal{}
			bal[oid] = b
			orderSeq = append(orderSeq, oid)
		}
		b.qty += lg.DeltaQty
	}

	need := int32(quantity)
	var out []refundAlloc
	for _, oid := range orderSeq {
		if need <= 0 {
			break
		}
		remain := bal[oid].qty
		if remain <= 0 {
			continue
		}
		take := remain
		if take > need {
			take = need
		}
		takeQty, amount, err := planOrderItemRefund(tx, oid, productID, spec, uint32(take))
		if err != nil {
			return nil, err
		}
		if takeQty == 0 || amount <= 0 {
			return nil, fmt.Errorf("%w: 订单可退余额不足", ErrInventoryRefundInvalid)
		}
		out = append(out, refundAlloc{OrderID: oid, Quantity: takeQty, Amount: amount})
		need -= int32(takeQty)
	}
	if need > 0 {
		return nil, fmt.Errorf("%w: 可退数量不足（可能部分已使用或余额不足）", ErrInventoryRefundInvalid)
	}
	return out, nil
}

// planOrderItemRefund 按单价与订单剩余可退金额共同约束数量；余额不够则缩减数量，不出现“多退货少退钱”。
func planOrderItemRefund(tx *gorm.DB, orderID, productID uint64, spec string, qty uint32) (uint32, float64, error) {
	var items []model.OrderItem
	if err := query.NotDeleted(tx).Where("order_id = ? AND product_id = ?", orderID, productID).Find(&items).Error; err != nil {
		return 0, 0, err
	}
	var matched *model.OrderItem
	for i := range items {
		it := &items[i]
		if orderItemSpec(*it) == spec {
			matched = it
			break
		}
	}
	if matched == nil && len(items) == 1 {
		matched = &items[0]
	}
	if matched == nil {
		return 0, 0, fmt.Errorf("%w: 找不到对应订单明细", ErrInventoryRefundInvalid)
	}

	var order model.Order
	if err := query.NotDeleted(tx).Select("id", "pay_amount", "refunded_amount", "refund_pending_amount", "pay_status", "delivery_fee").
		First(&order, orderID).Error; err != nil {
		return 0, 0, err
	}
	if order.PayStatus != model.PayStatusPaid && order.PayStatus != model.PayStatusPartialRefunded && order.PayStatus != model.PayStatusRefunding {
		return 0, 0, fmt.Errorf("%w: 订单支付状态不可退款", ErrInventoryRefundInvalid)
	}

	unit := matched.UnitPrice
	if matched.Quantity > 0 && matched.Subtotal > 0 {
		unit = matched.Subtotal / float64(matched.Quantity)
	}
	if unit <= 0 {
		return 0, 0, fmt.Errorf("%w: 订单单价无效", ErrInventoryRefundInvalid)
	}

	remainPay := order.PayAmount - order.RefundedAmount - order.RefundPendingAmount
	if remainPay < 0 {
		remainPay = 0
	}
	maxQty := uint32(math.Floor((remainPay + 1e-9) / unit))
	if qty > maxQty {
		qty = maxQty
	}
	if qty == 0 {
		return 0, 0, fmt.Errorf("%w: 订单可退余额不足", ErrInventoryRefundInvalid)
	}
	amount := math.Round(unit*float64(qty)*100) / 100
	if amount > remainPay {
		amount = math.Round(remainPay*100) / 100
	}
	return qty, amount, nil
}
