package service

import (
	"fmt"
	"log"
	"math"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
)

type InventoryAttrCleanupReport struct {
	GroupsScanned  int  `json:"groups_scanned"`
	UseLogsStamped int  `json:"use_logs_stamped"`
	RefundsMoved   int  `json:"refunds_moved"`
	OrdersMoneyFix int  `json:"orders_money_fix"`
	DryRun         bool `json:"dry_run"`
}

// CleanupInventoryAttribution 回放流水，补全 use 的 order_id，并把错记到虚高旧单的退款改挂到真实来源。
// dryRun=true 只统计不写库。
func CleanupInventoryAttribution(db *gorm.DB, dryRun bool) (*InventoryAttrCleanupReport, error) {
	report := &InventoryAttrCleanupReport{DryRun: dryRun}

	type row struct {
		AccountID uint64
		ProductID uint64
		Spec      string
	}
	var keys []row
	if err := db.Model(&model.UserInventoryLog{}).
		Select("DISTINCT account_id, product_id, spec").
		Where("is_deleted = ?", model.NotDeleted).
		Find(&keys).Error; err != nil {
		return nil, err
	}

	for _, k := range keys {
		report.GroupsScanned++
		var nUse, nRefund, nMoney int
		var err error
		if dryRun {
			nUse, nRefund, nMoney, err = cleanupOneInventoryGroup(db, k.AccountID, k.ProductID, k.Spec, true)
		} else {
			err = db.Transaction(func(tx *gorm.DB) error {
				var e error
				nUse, nRefund, nMoney, e = cleanupOneInventoryGroup(tx, k.AccountID, k.ProductID, k.Spec, false)
				return e
			})
		}
		if err != nil {
			return report, fmt.Errorf("account=%d product=%d spec=%q: %w", k.AccountID, k.ProductID, k.Spec, err)
		}
		report.UseLogsStamped += nUse
		report.RefundsMoved += nRefund
		report.OrdersMoneyFix += nMoney
	}
	return report, nil
}

func cleanupOneInventoryGroup(db *gorm.DB, accountID, productID uint64, spec string, dryRun bool) (useFixed, refundMoved, moneyFixed int, err error) {
	var logs []model.UserInventoryLog
	if err = query.NotDeleted(db).
		Where("account_id = ? AND product_id = ? AND spec = ?", accountID, productID, spec).
		Order("id ASC").
		Find(&logs).Error; err != nil {
		return
	}
	if len(logs) == 0 {
		return
	}

	bal := map[uint64]int32{}
	ceiling := map[uint64]int32{}
	orderSeq := make([]uint64, 0)
	touch := func(oid uint64) {
		if _, ok := bal[oid]; !ok {
			orderSeq = append(orderSeq, oid)
		}
	}
	consumeFIFOPlan := func(need int32) []struct {
		oid uint64
		qty int32
	} {
		var plan []struct {
			oid uint64
			qty int32
		}
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
			plan = append(plan, struct {
				oid uint64
				qty int32
			}{oid: oid, qty: take})
			need -= take
		}
		return plan
	}
	applyConsume := func(plan []struct {
		oid uint64
		qty int32
	}) {
		for _, p := range plan {
			bal[p.oid] -= p.qty
		}
	}
	clampCeiling := func() {
		for oid := range bal {
			if bal[oid] < 0 {
				bal[oid] = 0
			}
			if capQty, ok := ceiling[oid]; ok && bal[oid] > capQty {
				bal[oid] = capQty
			}
		}
	}

	type moneyMove struct {
		From, To uint64
		Qty      uint32
	}
	var moneyMoves []moneyMove

	for i := range logs {
		lg := &logs[i]
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
			if lg.OrderID == nil {
				plan := consumeFIFOPlan(need)
				if len(plan) == 0 {
					continue
				}
				// 补全 / 拆分 use 流水的 order_id
				if !dryRun {
					if err = stampUseLogOrderIDs(db, lg, plan); err != nil {
						return
					}
				}
				useFixed++
				applyConsume(plan)
			} else {
				oid := *lg.OrderID
				touch(oid)
				bal[oid] += lg.DeltaQty
				if bal[oid] < 0 {
					extra := -bal[oid]
					bal[oid] = 0
					applyConsume(consumeFIFOPlan(extra))
				}
			}

		case model.InventoryEventRefund:
			need := -lg.DeltaQty
			if need <= 0 {
				continue
			}
			if lg.OrderID == nil {
				plan := consumeFIFOPlan(need)
				applyConsume(plan)
				if len(plan) > 0 && !dryRun {
					oid := plan[0].oid
					if err = db.Model(lg).Update("order_id", oid).Error; err != nil {
						return
					}
					refundMoved++
				}
				continue
			}
			oid := *lg.OrderID
			touch(oid)
			// 该单此刻无可退净余额 → 错账，改挂到 FIFO 真实来源
			if bal[oid] < need {
				plan := consumeFIFOPlan(need)
				if len(plan) > 0 {
					newOID := plan[0].oid
					if newOID != oid {
						log.Printf("[inventory-attr-cleanup] move refund log=%d %d -> %d qty=%d account=%d product=%d",
							lg.ID, oid, newOID, need, accountID, productID)
						if !dryRun {
							if err = db.Model(lg).Update("order_id", newOID).Error; err != nil {
								return
							}
						}
						refundMoved++
						moneyMoves = append(moneyMoves, moneyMove{From: oid, To: newOID, Qty: uint32(need)})
						oid = newOID
					}
					applyConsume(plan)
					ceiling[oid] -= need
					if ceiling[oid] < 0 {
						ceiling[oid] = 0
					}
					clampCeiling()
					continue
				}
			}
			bal[oid] -= need
			if bal[oid] < 0 {
				extra := -bal[oid]
				bal[oid] = 0
				applyConsume(consumeFIFOPlan(extra))
			}
			ceiling[oid] -= need
			if ceiling[oid] < 0 {
				ceiling[oid] = 0
			}

		case model.InventoryEventUseCancel, model.InventoryEventOrderRollback:
			if lg.OrderID == nil {
				if lg.DeltaQty < 0 {
					applyConsume(consumeFIFOPlan(-lg.DeltaQty))
				}
				continue
			}
			oid := *lg.OrderID
			touch(oid)
			bal[oid] += lg.DeltaQty
			clampCeiling()

		default:
			if lg.OrderID == nil {
				if lg.DeltaQty < 0 {
					applyConsume(consumeFIFOPlan(-lg.DeltaQty))
				}
				continue
			}
			oid := *lg.OrderID
			touch(oid)
			bal[oid] += lg.DeltaQty
		}
		clampCeiling()
	}

	for _, mv := range moneyMoves {
		n, e := moveInventoryRefundMoney(db, mv.From, mv.To, productID, spec, mv.Qty, dryRun)
		if e != nil {
			err = e
			return
		}
		moneyFixed += n
	}
	return
}

func stampUseLogOrderIDs(db *gorm.DB, lg *model.UserInventoryLog, plan []struct {
	oid uint64
	qty int32
}) error {
	if len(plan) == 0 {
		return nil
	}
	before := lg.BeforeQty
	for i, p := range plan {
		if p.qty <= 0 {
			continue
		}
		after := before
		if before >= uint32(p.qty) {
			after = before - uint32(p.qty)
		} else {
			after = 0
		}
		oid := p.oid
		if i == 0 {
			if err := db.Model(&model.UserInventoryLog{}).Where("id = ?", lg.ID).Updates(map[string]interface{}{
				"order_id":  oid,
				"delta_qty": -p.qty,
				"after_qty": after,
			}).Error; err != nil {
				return err
			}
		} else {
			nl := model.UserInventoryLog{
				AccountID:   lg.AccountID,
				InventoryID: lg.InventoryID,
				ProductID:   lg.ProductID,
				Spec:        lg.Spec,
				OrderID:     &oid,
				UsageID:     lg.UsageID,
				EventType:   model.InventoryEventUse,
				DeltaQty:    -p.qty,
				BeforeQty:   before,
				AfterQty:    after,
				Remark:      lg.Remark,
				CreatedAt:   lg.CreatedAt,
			}
			if err := db.Create(&nl).Error; err != nil {
				return err
			}
		}
		before = after
	}
	return nil
}

func moveInventoryRefundMoney(db *gorm.DB, fromOID, toOID, productID uint64, spec string, qty uint32, dryRun bool) (int, error) {
	if fromOID == 0 || toOID == 0 || fromOID == toOID || qty == 0 {
		return 0, nil
	}
	meta, err := loadOrderRefundMeta(db, fromOID, productID, spec)
	if err != nil {
		// 旧单可能已无明细，按目标单单价
		meta, err = loadOrderRefundMeta(db, toOID, productID, spec)
		if err != nil {
			return 0, nil // 跳过无法估价的
		}
	}
	amount := math.Round(meta.UnitPrice*float64(qty)*100) / 100
	if amount <= 0 {
		return 0, nil
	}
	if dryRun {
		return 1, nil
	}
	if err := adjustOrderRefundedAmount(db, fromOID, -amount); err != nil {
		return 0, err
	}
	if err := adjustOrderRefundedAmount(db, toOID, amount); err != nil {
		return 0, err
	}
	return 1, nil
}

func adjustOrderRefundedAmount(db *gorm.DB, orderID uint64, delta float64) error {
	var o model.Order
	if err := query.NotDeleted(db).Select("id", "pay_amount", "refunded_amount", "refund_pending_amount", "pay_status", "status").
		First(&o, orderID).Error; err != nil {
		return err
	}
	newRefunded := math.Round((o.RefundedAmount+delta)*100) / 100
	if newRefunded < 0 {
		newRefunded = 0
	}
	if newRefunded > o.PayAmount {
		newRefunded = o.PayAmount
	}
	payStatus := o.PayStatus
	orderStatus := o.Status
	switch {
	case newRefunded+0.0001 >= o.PayAmount && o.RefundPendingAmount < 0.0001:
		payStatus = model.PayStatusRefunded
		orderStatus = model.OrderStatusRefunded
	case o.RefundPendingAmount > 0.0001:
		payStatus = model.PayStatusRefunding
	case newRefunded > 0.0001:
		payStatus = model.PayStatusPartialRefunded
		if orderStatus == model.OrderStatusRefunded {
			orderStatus = model.OrderStatusRefunding
		}
	default:
		payStatus = model.PayStatusPaid
		if orderStatus == model.OrderStatusRefunded || orderStatus == model.OrderStatusRefunding {
			orderStatus = model.OrderStatusPendingFulfill
		}
	}
	return query.NotDeleted(db.Model(&model.Order{})).Where("id = ?", orderID).Updates(map[string]interface{}{
		"refunded_amount": newRefunded,
		"pay_status":      payStatus,
		"status":          orderStatus,
	}).Error
}
