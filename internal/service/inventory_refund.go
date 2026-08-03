package service

import (
	"errors"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/payment"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrInventoryRefundInvalid = errors.New("inventory refund invalid")
)

// errGroupBuyNoSelfRefund 拼团成团入袋后禁止用户自助退款（对齐美团：未成团可退，成团后须履约/联系商家）。
func errGroupBuyNoSelfRefund() error {
	return fmt.Errorf("%w: 拼团商品成团进入背包后不可自行退款", ErrInventoryRefundInvalid)
}

func isGroupBuyPurchaseType(purchaseType uint8) bool {
	return purchaseType == model.PurchaseTypeGroup
}

type InventoryRefundView struct {
	InventoryID   uint64  `json:"inventory_id"`
	ProductID     uint64  `json:"product_id"`
	Quantity      uint32  `json:"quantity"`
	RefundAmount  float64 `json:"refund_amount"`
	OrderID       uint64  `json:"order_id"`
	RemainQty     uint32  `json:"remain_qty"`
	RefundPending bool    `json:"refund_pending"` // 微信退款已提交、待回调到账
}

// InventoryRefundSource 背包内某一来源订单的可退批次（同商品不同成交价分开展示）。
type InventoryRefundSource struct {
	OrderID      uint64    `json:"order_id"`
	OrderNo      string    `json:"order_no"`
	Quantity     uint32    `json:"quantity"`
	UnitPrice    float64   `json:"unit_price"`
	RefundAmount float64   `json:"refund_amount"` // 退本批全部可退数量时的金额
	Label        string    `json:"label"`
	PurchaseType uint8     `json:"purchase_type"`
	ActivityID   *uint64   `json:"activity_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type InventoryRefundSourcesView struct {
	InventoryID uint64                  `json:"inventory_id"`
	ProductID   uint64                  `json:"product_id"`
	ProductName string                  `json:"product_name"`
	RemainQty   uint32                  `json:"remain_qty"`
	Sources     []InventoryRefundSource `json:"sources"`
}

// ListInventoryRefundSources 列出背包商品可退来源批次（按订单/成交价拆分）。
func (s *OrderService) ListInventoryRefundSources(accountID, inventoryID uint64) (*InventoryRefundSourcesView, error) {
	if s.InventorySvc == nil {
		return nil, fmt.Errorf("inventory service unavailable")
	}
	inv, err := s.InventorySvc.GetOwned(accountID, inventoryID)
	if err != nil {
		return nil, err
	}
	productName := ""
	var product model.Product
	if err := query.NotDeleted(s.DB).Select("id", "name").First(&product, inv.ProductID).Error; err == nil {
		productName = product.Name
	}

	batches, err := listRefundBatches(s.DB, accountID, inv.ProductID, inv.Spec, inv.Quantity, !s.paymentProvider().ImmediateSettle())
	if err != nil {
		return nil, err
	}
	sources := make([]InventoryRefundSource, 0, len(batches))
	for _, b := range batches {
		sources = append(sources, InventoryRefundSource{
			OrderID:      b.OrderID,
			OrderNo:      b.OrderNo,
			Quantity:     b.Quantity,
			UnitPrice:    b.UnitPrice,
			RefundAmount: b.Amount,
			Label:        b.Label,
			PurchaseType: b.PurchaseType,
			ActivityID:   b.ActivityID,
			CreatedAt:    b.CreatedAt,
		})
	}
	return &InventoryRefundSourcesView{
		InventoryID: inv.ID,
		ProductID:   inv.ProductID,
		ProductName: productName,
		RemainQty:   inv.Quantity,
		Sources:     sources,
	}, nil
}

// InventoryRefundItemInput 指定某一来源订单的退款数量。
type InventoryRefundItemInput struct {
	OrderID  uint64 `json:"order_id"`
	Quantity uint32 `json:"quantity"`
}

// RefundInventory 退还未使用的背包商品。
// items 非空时按多来源精确退；否则 quantity + orderID（可空）走单来源或 FIFO。
func (s *OrderService) RefundInventory(accountID, inventoryID uint64, quantity uint32, orderID *uint64, items []InventoryRefundItemInput) (*InventoryRefundView, error) {
	if s.InventorySvc == nil {
		return nil, fmt.Errorf("inventory service unavailable")
	}
	if len(items) == 0 && quantity == 0 {
		return nil, fmt.Errorf("%w: 退款数量须大于 0", ErrInventoryRefundInvalid)
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

		var allocs []refundAlloc
		var err error
		requirePayTx := !s.paymentProvider().ImmediateSettle()
		if len(items) > 0 {
			allocs, err = allocateInventoryRefundItems(tx, accountID, inv.ProductID, inv.Spec, inv.Quantity, items, requirePayTx)
		} else {
			if inv.Quantity < quantity {
				return ErrInventoryInsufficient
			}
			allocs, err = allocateInventoryRefund(tx, accountID, inv.ProductID, inv.Spec, quantity, orderID, requirePayTx)
		}
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
			if err := s.refundAmountInTx(tx, a.OrderID, a.Amount, "背包未使用退款"); err != nil {
				if errors.Is(err, payment.ErrInvalidState) {
					detail := err.Error()
					switch {
					case strings.Contains(detail, "collector"):
						return fmt.Errorf("%w: 退款服务未就绪，请稍后重试", ErrInventoryRefundInvalid)
					case strings.Contains(detail, "no refundable balance"):
						return fmt.Errorf("%w: 订单可退余额不足", ErrInventoryRefundInvalid)
					case strings.Contains(detail, "conflict"):
						return fmt.Errorf("%w: 退款冲突，请稍后重试", ErrInventoryRefundInvalid)
					default:
						return fmt.Errorf("%w: 退款冲突或余额不足，请稍后重试", ErrInventoryRefundInvalid)
					}
				}
				return err
			}
			if err := s.maybeReleaseCouponAfterOrderRefundInTx(tx, a.OrderID); err != nil {
				return err
			}
			// 微信：事务内先扣背包；CreateRefund 失败时由 AttachRestore 回滚
			payment.AttachRestoreToLastRefundJob(tx, accountID, inv.ProductID, inv.Spec, a.Quantity)
			oid := a.OrderID
			if err := s.InventorySvc.adjustQuantity(tx, accountID, inv.ProductID, inv.Spec,
				-int32(a.Quantity), &oid, nil, model.InventoryEventRefund, &remark); err != nil {
				return err
			}
			if err := tx.Model(&model.Product{}).Where("id = ?", inv.ProductID).
				Update("stock", gorm.Expr("stock + ?", a.Quantity)).Error; err != nil {
				return err
			}
			if s.ActivitySvc != nil {
				// 活动已售与平台日限均按退款件数回滚（此前只回滚日限，导致 sold_count 虚高）
				if err := s.ActivitySvc.RollbackSoldQtyOnRefundInTx(tx, a.OrderID, inv.ProductID, a.Quantity); err != nil {
					return err
				}
				if err := s.ActivitySvc.RollbackPlatformDailyOnRefundInTx(tx, a.OrderID, inv.ProductID, a.Quantity); err != nil {
					return err
				}
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
	if result.OrderID > 0 {
		var o model.Order
		if e := query.NotDeleted(s.DB).Select("pay_status").First(&o, result.OrderID).Error; e == nil {
			result.RefundPending = o.PayStatus == model.PayStatusRefunding
		}
	}
	return &result, nil
}

type refundAlloc struct {
	OrderID  uint64
	Quantity uint32
	Amount   float64
}

type refundBatch struct {
	OrderID      uint64
	OrderNo      string
	Quantity     uint32
	UnitPrice    float64
	Amount       float64
	Label        string
	PurchaseType uint8
	ActivityID   *uint64
	CreatedAt    time.Time
}

func listRefundBatches(db *gorm.DB, accountID, productID uint64, spec string, invQty uint32, requirePayTx bool) ([]refundBatch, error) {
	nets, orderSeq, err := orderNetBalances(db, accountID, productID, spec)
	if err != nil {
		return nil, err
	}
	pool := int32(invQty)
	out := make([]refundBatch, 0)
	for _, oid := range orderSeq {
		if pool <= 0 {
			break
		}
		remain := nets[oid]
		if remain <= 0 {
			continue
		}
		take := remain
		if take > pool {
			take = pool
		}
		meta, err := loadOrderRefundMeta(db, oid, productID, spec)
		if err != nil {
			return nil, err
		}
		// 拼团入袋来源不出现在可退列表
		if isGroupBuyPurchaseType(meta.PurchaseType) {
			continue
		}
		takeQty, amount, err := planOrderItemRefundWithMeta(meta, uint32(take))
		if err != nil {
			// 该来源暂不可退（支付态/余额），跳过
			continue
		}
		if requirePayTx && !orderHasPaidPaymentTx(db, oid) {
			continue
		}
		if takeQty == 0 {
			continue
		}
		out = append(out, refundBatch{
			OrderID:      oid,
			OrderNo:      meta.OrderNo,
			Quantity:     takeQty,
			UnitPrice:    meta.UnitPrice,
			Amount:       amount,
			Label:        meta.Label,
			PurchaseType: meta.PurchaseType,
			ActivityID:   meta.ActivityID,
			CreatedAt:    meta.CreatedAt,
		})
		pool -= int32(takeQty)
	}
	return out, nil
}

// allocateInventoryRefundItems 按用户勾选的多来源精确分摊。
func allocateInventoryRefundItems(tx *gorm.DB, accountID, productID uint64, spec string, invQty uint32, items []InventoryRefundItemInput, requirePayTx bool) ([]refundAlloc, error) {
	nets, _, err := orderNetBalances(tx, accountID, productID, spec)
	if err != nil {
		return nil, err
	}
	seen := map[uint64]struct{}{}
	var out []refundAlloc
	var totalQty uint32
	for _, it := range items {
		if it.OrderID == 0 || it.Quantity == 0 {
			continue
		}
		if _, dup := seen[it.OrderID]; dup {
			return nil, fmt.Errorf("%w: 同一来源订单不可重复提交", ErrInventoryRefundInvalid)
		}
		seen[it.OrderID] = struct{}{}
		remain := nets[it.OrderID]
		if remain <= 0 {
			return nil, fmt.Errorf("%w: 该来源无可退数量", ErrInventoryRefundInvalid)
		}
		if int32(it.Quantity) > remain {
			return nil, fmt.Errorf("%w: 该来源可退数量不足", ErrInventoryRefundInvalid)
		}
		if requirePayTx && !orderHasPaidPaymentTx(tx, it.OrderID) {
			return nil, fmt.Errorf("%w: 该来源订单无微信支付流水，不可退款", ErrInventoryRefundInvalid)
		}
		meta, metaErr := loadOrderRefundMeta(tx, it.OrderID, productID, spec)
		if metaErr != nil {
			return nil, metaErr
		}
		if isGroupBuyPurchaseType(meta.PurchaseType) {
			return nil, errGroupBuyNoSelfRefund()
		}
		takeQty, amount, err := planOrderItemRefundWithMeta(meta, it.Quantity)
		if err != nil {
			return nil, err
		}
		if takeQty != it.Quantity || amount <= 0 {
			return nil, fmt.Errorf("%w: 订单可退余额不足", ErrInventoryRefundInvalid)
		}
		out = append(out, refundAlloc{OrderID: it.OrderID, Quantity: takeQty, Amount: amount})
		totalQty += takeQty
	}
	if len(out) == 0 || totalQty == 0 {
		return nil, fmt.Errorf("%w: 退款数量须大于 0", ErrInventoryRefundInvalid)
	}
	if totalQty > invQty {
		return nil, ErrInventoryInsufficient
	}
	if err := validateRefundAllocs(nets, invQty, out); err != nil {
		return nil, err
	}
	return out, nil
}

// allocateInventoryRefund 分摊退款。orderID 非空时只从该订单扣；否则 FIFO，跳过暂不可退来源（与列表接口一致）。
func allocateInventoryRefund(tx *gorm.DB, accountID, productID uint64, spec string, quantity uint32, orderID *uint64, requirePayTx bool) ([]refundAlloc, error) {
	nets, orderSeq, err := orderNetBalances(tx, accountID, productID, spec)
	if err != nil {
		return nil, err
	}
	need := int32(quantity)
	var out []refundAlloc

	if orderID != nil && *orderID > 0 {
		oid := *orderID
		remain := nets[oid]
		if remain <= 0 {
			return nil, fmt.Errorf("%w: 该来源无可退数量", ErrInventoryRefundInvalid)
		}
		take := remain
		if take > need {
			take = need
		}
		if requirePayTx && !orderHasPaidPaymentTx(tx, oid) {
			return nil, fmt.Errorf("%w: 该来源订单无微信支付流水，不可退款", ErrInventoryRefundInvalid)
		}
		meta, metaErr := loadOrderRefundMeta(tx, oid, productID, spec)
		if metaErr != nil {
			return nil, metaErr
		}
		if isGroupBuyPurchaseType(meta.PurchaseType) {
			return nil, errGroupBuyNoSelfRefund()
		}
		takeQty, amount, err := planOrderItemRefundWithMeta(meta, uint32(take))
		if err != nil {
			return nil, err
		}
		if takeQty == 0 || amount <= 0 {
			return nil, fmt.Errorf("%w: 订单可退余额不足", ErrInventoryRefundInvalid)
		}
		out = append(out, refundAlloc{OrderID: oid, Quantity: takeQty, Amount: amount})
		need -= int32(takeQty)
		if need > 0 {
			return nil, fmt.Errorf("%w: 该来源可退数量不足", ErrInventoryRefundInvalid)
		}
		var inv model.UserInventory
		invQty := quantity
		if e := query.NotDeleted(tx).Select("quantity").
			Where("account_id = ? AND product_id = ? AND spec = ?", accountID, productID, spec).
			First(&inv).Error; e == nil {
			invQty = inv.Quantity
		}
		if err := validateRefundAllocs(nets, invQty, out); err != nil {
			return nil, err
		}
		return out, nil
	}

	for _, oid := range orderSeq {
		if need <= 0 {
			break
		}
		remain := nets[oid]
		if remain <= 0 {
			continue
		}
		take := remain
		if take > need {
			take = need
		}
		if requirePayTx && !orderHasPaidPaymentTx(tx, oid) {
			continue
		}
		meta, metaErr := loadOrderRefundMeta(tx, oid, productID, spec)
		if metaErr != nil || isGroupBuyPurchaseType(meta.PurchaseType) {
			// 拼团入袋或元数据异常：跳过，与列表一致
			continue
		}
		takeQty, amount, err := planOrderItemRefundWithMeta(meta, uint32(take))
		if err != nil || takeQty == 0 || amount <= 0 {
			// 与 listRefundBatches 一致：跳过暂不可退来源，继续下一笔
			continue
		}
		out = append(out, refundAlloc{OrderID: oid, Quantity: takeQty, Amount: amount})
		need -= int32(takeQty)
	}
	if need > 0 {
		return nil, fmt.Errorf("%w: 可退数量不足（拼团入袋不可退，或已使用/余额不足）", ErrInventoryRefundInvalid)
	}
	var inv model.UserInventory
	invQty := quantity
	if e := query.NotDeleted(tx).Select("quantity").
		Where("account_id = ? AND product_id = ? AND spec = ?", accountID, productID, spec).
		First(&inv).Error; e == nil {
		invQty = inv.Quantity
	}
	if err := validateRefundAllocs(nets, invQty, out); err != nil {
		return nil, err
	}
	return out, nil
}

func orderNetBalances(db *gorm.DB, accountID, productID uint64, spec string) (map[uint64]int32, []uint64, error) {
	// 必须包含 order_id 为空的 use 流水：使用时未绑定来源订单，若忽略会导致
	// 已核销的旧订单仍显示可退净余额，FIFO 退款错记到早期订单。
	var logs []model.UserInventoryLog
	if err := query.NotDeleted(db).
		Where("account_id = ? AND product_id = ? AND spec = ?", accountID, productID, spec).
		Order("id ASC").
		Find(&logs).Error; err != nil {
		return nil, nil, err
	}
	bal, orderSeq := orderNetBalancesFromLogs(logs)

	var invQty uint32
	var inv model.UserInventory
	if err := query.NotDeleted(db).
		Select("quantity").
		Where("account_id = ? AND product_id = ? AND spec = ?", accountID, productID, spec).
		First(&inv).Error; err == nil {
		invQty = inv.Quantity
	}
	if trimmed := reconcileNetBalancesToInventory(bal, orderSeq, invQty); trimmed > 0 {
		log.Printf("[inventory-attr] trimmed phantom=%d account=%d product=%d spec=%q bag=%d",
			trimmed, accountID, productID, spec, invQty)
	}
	var total int32
	for _, oid := range orderSeq {
		total += bal[oid]
	}
	if total > int32(invQty) {
		log.Printf("[inventory-attr] WARN account=%d product=%d spec=%q net_total=%d > bag=%d after reconcile",
			accountID, productID, spec, total, invQty)
	}
	return bal, orderSeq, nil
}

// orderNetBalancesFromLogs 按时间回放流水，计算各来源订单仍留在背包中的净数量。
// use（常无 order_id）按 FIFO 从最早仍有余额的入账订单扣减。
func orderNetBalancesFromLogs(logs []model.UserInventoryLog) (map[uint64]int32, []uint64) {
	bal := map[uint64]int32{}
	ceiling := map[uint64]int32{} // 入账 - 已退款，use_cancel 不可突破
	orderSeq := make([]uint64, 0)

	touch := func(oid uint64) {
		if _, ok := bal[oid]; !ok {
			orderSeq = append(orderSeq, oid)
		}
	}
	// 将 qty（正数）按 FIFO 从各来源净余额扣减
	consumeFIFO := func(need int32) {
		for _, oid := range orderSeq {
			if need <= 0 {
				break
			}
			if bal[oid] <= 0 {
				continue
			}
			take := bal[oid]
			if take > need {
				take = need
			}
			bal[oid] -= take
			need -= take
		}
	}

	for _, lg := range logs {
		switch lg.EventType {
		case model.InventoryEventOrderCredit:
			if lg.OrderID == nil || lg.DeltaQty <= 0 {
				continue
			}
			oid := *lg.OrderID
			touch(oid)
			bal[oid] += lg.DeltaQty
			ceiling[oid] += lg.DeltaQty
		case model.InventoryEventUse:
			need := -lg.DeltaQty
			if need <= 0 {
				continue
			}
			if lg.OrderID != nil {
				oid := *lg.OrderID
				touch(oid)
				bal[oid] += lg.DeltaQty // 负数
				if bal[oid] < 0 {
					// 指定来源不够时，差额继续 FIFO（容错历史脏数据）
					extra := -bal[oid]
					bal[oid] = 0
					consumeFIFO(extra)
				}
			} else {
				consumeFIFO(need)
			}
		case model.InventoryEventRefund:
			if lg.OrderID == nil {
				if lg.DeltaQty < 0 {
					consumeFIFO(-lg.DeltaQty)
				}
				continue
			}
			oid := *lg.OrderID
			touch(oid)
			bal[oid] += lg.DeltaQty
			if lg.DeltaQty < 0 {
				ceiling[oid] += lg.DeltaQty // 降低可退上限
				if ceiling[oid] < 0 {
					ceiling[oid] = 0
				}
			}
		case model.InventoryEventUseCancel, model.InventoryEventOrderRollback:
			if lg.OrderID == nil {
				if lg.DeltaQty < 0 {
					consumeFIFO(-lg.DeltaQty)
				}
				continue
			}
			oid := *lg.OrderID
			touch(oid)
			bal[oid] += lg.DeltaQty
		default:
			if lg.OrderID == nil {
				if lg.DeltaQty < 0 {
					consumeFIFO(-lg.DeltaQty)
				}
				continue
			}
			oid := *lg.OrderID
			touch(oid)
			bal[oid] += lg.DeltaQty
		}
	}

	// use 无 order_id + use_cancel 记到 LastOrderID 会造成单订单余额虚高；
	// 单订单净余额不得超过「入账 - 已退款」。
	for oid := range bal {
		if bal[oid] < 0 {
			bal[oid] = 0
			continue
		}
		if capQty, ok := ceiling[oid]; ok && bal[oid] > capQty {
			bal[oid] = capQty
		}
	}
	return bal, orderSeq
}

// reconcileNetBalancesToInventory 各来源净余额合计不得超过当前背包件数。
// 超出部分按 FIFO 从最早订单削去（消化 use/cancel 错账导致的虚高旧单余额）。
// 返回被削去的虚高件数（>0 表示存在历史错账残留）。
func reconcileNetBalancesToInventory(bal map[uint64]int32, orderSeq []uint64, invQty uint32) int32 {
	var total int32
	for _, oid := range orderSeq {
		if bal[oid] < 0 {
			bal[oid] = 0
		}
		total += bal[oid]
	}
	for oid, v := range bal {
		if v < 0 {
			bal[oid] = 0
		}
	}
	excess := total - int32(invQty)
	if excess <= 0 {
		return 0
	}
	trimmed := excess
	for _, oid := range orderSeq {
		if excess <= 0 {
			break
		}
		if bal[oid] <= 0 {
			continue
		}
		take := bal[oid]
		if take > excess {
			take = excess
		}
		bal[oid] -= take
		excess -= take
	}
	return trimmed
}

// validateRefundAllocs 退款落库前硬校验：件数不超过背包，且每笔来源净余额足够。
func validateRefundAllocs(nets map[uint64]int32, invQty uint32, allocs []refundAlloc) error {
	if len(allocs) == 0 {
		return fmt.Errorf("%w: 无可退款来源订单，请联系客服", ErrInventoryRefundInvalid)
	}
	var sum uint32
	seen := map[uint64]struct{}{}
	for _, a := range allocs {
		if a.OrderID == 0 || a.Quantity == 0 || a.Amount <= 0 {
			return fmt.Errorf("%w: 退款分摊无效", ErrInventoryRefundInvalid)
		}
		if _, dup := seen[a.OrderID]; dup {
			return fmt.Errorf("%w: 同一来源订单不可重复提交", ErrInventoryRefundInvalid)
		}
		seen[a.OrderID] = struct{}{}
		if nets[a.OrderID] < int32(a.Quantity) {
			return fmt.Errorf("%w: 来源订单 #%d 可退数量不足（已按背包校正，请刷新后重试）",
				ErrInventoryRefundInvalid, a.OrderID)
		}
		sum += a.Quantity
	}
	if sum > invQty {
		return ErrInventoryInsufficient
	}
	return nil
}

type orderRefundMeta struct {
	OrderNo      string
	UnitPrice    float64
	Label        string
	PurchaseType uint8
	ActivityID   *uint64
	CreatedAt    time.Time
	PayAmount    float64
	Refunded     float64
	Pending      float64
	PayStatus    uint8
}

func loadOrderRefundMeta(db *gorm.DB, orderID, productID uint64, spec string) (*orderRefundMeta, error) {
	var items []model.OrderItem
	if err := query.NotDeleted(db).Where("order_id = ? AND product_id = ?", orderID, productID).Find(&items).Error; err != nil {
		return nil, err
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
		return nil, fmt.Errorf("%w: 找不到对应订单明细", ErrInventoryRefundInvalid)
	}
	var order model.Order
	if err := query.NotDeleted(db).
		Select("id", "order_no", "total_amount", "discount_amount", "pay_amount", "refunded_amount", "refund_pending_amount", "pay_status", "created_at", "activity_id", "user_coupon_id").
		First(&order, orderID).Error; err != nil {
		return nil, err
	}
	catalogUnit := matched.UnitPrice
	if matched.Quantity > 0 && matched.Subtotal > 0 {
		catalogUnit = matched.Subtotal / float64(matched.Quantity)
	}
	// 按行小计占订单总额比例分摊实付，保证用券订单按优惠后单价退款
	unit := catalogUnit
	if order.TotalAmount > 0 && matched.Quantity > 0 {
		itemPayShare := matched.Subtotal / order.TotalAmount * order.PayAmount
		unit = itemPayShare / float64(matched.Quantity)
	}
	unit = math.Round(unit*100) / 100
	if unit <= 0 {
		unit = math.Round(catalogUnit*100) / 100
	}
	activityID := matched.ActivityID
	if activityID == nil {
		activityID = order.ActivityID
	}
	return &orderRefundMeta{
		OrderNo:      order.OrderNo,
		UnitPrice:    unit,
		Label:        refundSourceLabel(matched.PurchaseType, activityID, order.DiscountAmount > 0),
		PurchaseType: matched.PurchaseType,
		ActivityID:   activityID,
		CreatedAt:    order.CreatedAt,
		PayAmount:    order.PayAmount,
		Refunded:     order.RefundedAmount,
		Pending:      order.RefundPendingAmount,
		PayStatus:    order.PayStatus,
	}, nil
}

func refundSourceLabel(purchaseType uint8, activityID *uint64, usedCoupon bool) string {
	if activityID != nil && *activityID > 0 {
		return "活动价"
	}
	if purchaseType == model.PurchaseTypeGroup {
		return "拼团价"
	}
	if usedCoupon {
		return "优惠价"
	}
	return "原价"
}

// maybeReleaseCouponAfterOrderRefundInTx 订单实付已全退（含退款中预留）时退还优惠券。
func (s *OrderService) maybeReleaseCouponAfterOrderRefundInTx(tx *gorm.DB, orderID uint64) error {
	if s.CouponSvc == nil || orderID == 0 {
		return nil
	}
	var order model.Order
	if err := query.NotDeleted(tx).First(&order, orderID).Error; err != nil {
		return nil
	}
	remain := order.PayAmount - order.RefundedAmount - order.RefundPendingAmount
	if remain > 0.009 {
		return nil
	}
	return s.CouponSvc.ReleaseByOrderInTx(tx, &order)
}

func planOrderItemRefund(tx *gorm.DB, orderID, productID uint64, spec string, qty uint32) (uint32, float64, error) {
	meta, err := loadOrderRefundMeta(tx, orderID, productID, spec)
	if err != nil {
		return 0, 0, err
	}
	return planOrderItemRefundWithMeta(meta, qty)
}

// orderHasPaidPaymentTx 是否存在微信支付流水（含已退款）。历史 mock 单无此流水，不可作微信退款来源。
func orderHasPaidPaymentTx(db *gorm.DB, orderID uint64) bool {
	var n int64
	// 勿用 []uint8：GORM 会当成 []byte，生成 status IN '<binary>' 导致 SQL 语法错误
	if err := db.Model(&model.PaymentTransaction{}).
		Where("order_id = ? AND status IN ? AND transaction_id IS NOT NULL AND transaction_id <> ''",
			orderID, []int{int(model.PayTxStatusPaid), int(model.PayTxStatusRefunded)}).
		Count(&n).Error; err != nil {
		return false
	}
	return n > 0
}

func planOrderItemRefundWithMeta(meta *orderRefundMeta, qty uint32) (uint32, float64, error) {
	if meta.PayStatus != model.PayStatusPaid && meta.PayStatus != model.PayStatusPartialRefunded && meta.PayStatus != model.PayStatusRefunding {
		return 0, 0, fmt.Errorf("%w: 订单支付状态不可退款", ErrInventoryRefundInvalid)
	}
	unit := meta.UnitPrice
	if unit <= 0 {
		return 0, 0, fmt.Errorf("%w: 订单单价无效", ErrInventoryRefundInvalid)
	}
	remainPay := meta.PayAmount - meta.Refunded - meta.Pending
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
