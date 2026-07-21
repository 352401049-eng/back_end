package service

import (
	"fmt"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/payment"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func (s *OrderService) settlePaymentInTx(tx *gorm.DB, orderID uint64, payAmount float64, at time.Time) error {
	p := s.Payment
	if p == nil {
		p = &payment.MockProvider{DB: s.DB}
	}
	if !p.ImmediateSettle() {
		return fmt.Errorf("%w: 请使用 PAYMENT_PROVIDER=mock，或完成微信支付接入后再切换", payment.ErrNotConfigured)
	}
	return p.SettlePaidInTx(tx, orderID, payAmount, at)
}

func (s *OrderService) refundPaymentInTx(tx *gorm.DB, orderID uint64) error {
	p := s.Payment
	if p == nil {
		p = &payment.MockProvider{DB: s.DB}
	}
	return p.RefundInTx(tx, orderID)
}

func (s *OrderService) paymentProvider() payment.Provider {
	if s.Payment != nil {
		return s.Payment
	}
	return &payment.MockProvider{DB: s.DB}
}

// PaymentProviderInfo 当前支付渠道（供前端决定是否调起收银台）。
func (s *OrderService) PaymentProviderInfo() map[string]interface{} {
	p := s.paymentProvider()
	return map[string]interface{}{
		"provider":         p.Name(),
		"immediate_settle": p.ImmediateSettle(),
	}
}

// CreatePrepay 预支付。Mock：若未付则补结算；已付则幂等返回。
func (s *OrderService) CreatePrepay(accountID, orderID uint64) (*payment.PrepayResult, error) {
	return s.paymentProvider().CreatePrepay(orderID, accountID)
}

// HandlePaymentNotify 支付渠道异步回调入口（微信桩预留）。
func (s *OrderService) HandlePaymentNotify(headers map[string]string, body []byte) (*payment.NotifyResult, error) {
	return s.paymentProvider().HandleNotify(headers, body)
}

// orderHasSuccessfulGroup 成团成功后的订单：禁止用户单方面取消，避免打乱整团。
func orderHasSuccessfulGroup(tx *gorm.DB, orderID uint64) (bool, error) {
	var n int64
	// 多表 JOIN 必须带表前缀过滤 is_deleted，避免 MySQL ambiguous
	err := tx.Table("order_item oi").
		Joins("JOIN group_buy_team t ON t.id = oi.group_buy_team_id AND t.is_deleted = ?", model.NotDeleted).
		Where("oi.order_id = ? AND oi.is_deleted = ? AND t.status = ?", orderID, model.NotDeleted, model.GroupBuyTeamSuccess).
		Count(&n).Error
	return n > 0, err
}

// ExpireStaleGroupTeams 超时未成团：团失败 + 订单 GroupFailed + 模拟退款 + 回滚库存/券/销量。
// 单个团失败只记入 failed，不中断同批次其余团。
func (s *OrderService) ExpireStaleGroupTeams(now time.Time) (int, error) {
	var teams []model.GroupBuyTeam
	if err := query.NotDeleted(s.DB).
		Where("status = ? AND expire_at < ?", model.GroupBuyTeamPending, now).
		Limit(100).
		Find(&teams).Error; err != nil {
		return 0, err
	}
	n := 0
	var firstErr error
	for i := range teams {
		if err := s.expireOneGroupTeam(&teams[i]); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("expire group team %d: %w", teams[i].ID, err)
			}
			continue
		}
		n++
	}
	return n, firstErr
}

func (s *OrderService) expireOneGroupTeam(team *model.GroupBuyTeam) error {
	return s.DB.Transaction(func(tx *gorm.DB) error {
		var locked model.GroupBuyTeam
		if err := query.NotDeleted(tx).Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&locked, team.ID).Error; err != nil {
			return err
		}
		if locked.Status != model.GroupBuyTeamPending || !locked.ExpireAt.Before(time.Now()) {
			return nil
		}
		if err := tx.Model(&locked).Update("status", model.GroupBuyTeamFailed).Error; err != nil {
			return err
		}

		var orderIDs []uint64
		if err := query.NotDeleted(tx.Model(&model.OrderItem{})).
			Where("group_buy_team_id = ?", locked.ID).
			Distinct("order_id").
			Pluck("order_id", &orderIDs).Error; err != nil {
			return err
		}
		for _, oid := range orderIDs {
			var order model.Order
			if err := query.NotDeleted(tx).First(&order, oid).Error; err != nil {
				return err
			}
			if order.Status != model.OrderStatusPendingGroup {
				continue
			}
			if s.CouponSvc != nil {
				if err := s.CouponSvc.ReleaseByOrderInTx(tx, &order); err != nil {
					return err
				}
			}
			if s.InventorySvc != nil {
				if err := s.InventorySvc.RollbackOrderCredit(tx, oid); err != nil {
					return err
				}
			}
			isLegacyPackageParent := order.PackageProductID != nil && order.ParentOrderID == nil && order.MerchantID == 0
			if isLegacyPackageParent {
				if err := cancelPackageChildrenInTx(tx, oid, s.InventorySvc, s.CouponSvc); err != nil {
					return err
				}
			} else if err := restoreProductStockForOrder(tx, oid); err != nil {
				return err
			}
			if s.ActivitySvc != nil {
				if err := s.ActivitySvc.RollbackSoldInTx(tx, oid); err != nil {
					return err
				}
			}
			if err := s.refundPaymentInTx(tx, oid); err != nil {
				return err
			}
			if err := tx.Model(&order).Update("status", model.OrderStatusGroupFailed).Error; err != nil {
				return err
			}
		}
		return nil
	})
}
