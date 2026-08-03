package service

import (
	"errors"
	"fmt"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ErrGroupCloseNeedsConfirm 关闭商品拼团通道时仍有待成团订单，需管理端二次确认。
var ErrGroupCloseNeedsConfirm = errors.New("group close needs confirm")

// GroupCloseNeedsConfirmError 携带待成团规模，供前端弹窗提示。
type GroupCloseNeedsConfirmError struct {
	ProductID         uint64
	PendingTeamCount  int64
	PendingOrderCount int64
}

func (e *GroupCloseNeedsConfirmError) Error() string {
	return fmt.Sprintf("%s: 仍有 %d 笔待成团订单（%d 个进行中的团），关闭拼团将退款并解散这些团",
		ErrGroupCloseNeedsConfirm.Error(), e.PendingOrderCount, e.PendingTeamCount)
}

func (e *GroupCloseNeedsConfirmError) Unwrap() error { return ErrGroupCloseNeedsConfirm }

// CountPendingProductChannelGroups 统计商品普通拼团（非活动）待成团团数与已支付订单数。
func (s *OrderService) CountPendingProductChannelGroups(productID uint64) (teamCount, orderCount int64, err error) {
	if productID == 0 {
		return 0, 0, nil
	}
	err = s.DB.Table("`order` AS o").
		Joins("INNER JOIN order_item oi ON oi.order_id = o.id AND oi.is_deleted = ?", model.NotDeleted).
		Where("o.is_deleted = ? AND o.status = ? AND o.pay_status = ? AND oi.product_id = ? AND oi.activity_id IS NULL AND oi.group_buy_team_id IS NOT NULL",
			model.NotDeleted, model.OrderStatusPendingGroup, model.PayStatusPaid, productID).
		Distinct("o.id").
		Count(&orderCount).Error
	if err != nil {
		return 0, 0, err
	}
	err = s.DB.Table("group_buy_team AS t").
		Joins("INNER JOIN order_item oi ON oi.group_buy_team_id = t.id AND oi.is_deleted = ?", model.NotDeleted).
		Joins("INNER JOIN `order` o ON o.id = oi.order_id AND o.is_deleted = ?", model.NotDeleted).
		Where("t.is_deleted = ? AND t.status = ? AND oi.product_id = ? AND oi.activity_id IS NULL AND o.status = ? AND o.pay_status = ?",
			model.NotDeleted, model.GroupBuyTeamPending, productID, model.OrderStatusPendingGroup, model.PayStatusPaid).
		Distinct("t.id").
		Count(&teamCount).Error
	return teamCount, orderCount, err
}

// FailPendingProductChannelGroups 立即失败商品普通拼团下所有进行中的团并退款（不等待 expire_at）。
func (s *OrderService) FailPendingProductChannelGroups(productID uint64) (int, error) {
	if productID == 0 {
		return 0, nil
	}
	var teamIDs []uint64
	err := s.DB.Table("group_buy_team AS t").
		Select("DISTINCT t.id").
		Joins("INNER JOIN order_item oi ON oi.group_buy_team_id = t.id AND oi.is_deleted = ?", model.NotDeleted).
		Joins("INNER JOIN `order` o ON o.id = oi.order_id AND o.is_deleted = ?", model.NotDeleted).
		Where("t.is_deleted = ? AND t.status = ? AND oi.product_id = ? AND oi.activity_id IS NULL AND o.status = ? AND o.pay_status = ?",
			model.NotDeleted, model.GroupBuyTeamPending, productID, model.OrderStatusPendingGroup, model.PayStatusPaid).
		Pluck("t.id", &teamIDs).Error
	if err != nil {
		return 0, err
	}
	n := 0
	var firstErr error
	for _, id := range teamIDs {
		if err := s.failOneGroupTeam(id, false); err != nil {
			if firstErr == nil {
				firstErr = fmt.Errorf("fail group team %d: %w", id, err)
			}
			continue
		}
		n++
	}
	return n, firstErr
}

// ExpireStaleGroupTeams 超时未成团：团失败 + 订单 GroupFailed + 退款 + 回滚库存/券/销量。
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
		if err := s.failOneGroupTeam(teams[i].ID, true); err != nil {
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
	if team == nil {
		return nil
	}
	return s.failOneGroupTeam(team.ID, true)
}

// failOneGroupTeam 将进行中的团标为失败并退款待成团订单。
// requireExpired=true 时仅处理已过 expire_at 的团（定时任务）；false 时立即解散（关闭通道）。
func (s *OrderService) failOneGroupTeam(teamID uint64, requireExpired bool) error {
	return s.runTx(func(tx *gorm.DB) error {
		var locked model.GroupBuyTeam
		if err := query.NotDeleted(tx).Clauses(clause.Locking{Strength: "UPDATE"}).
			First(&locked, teamID).Error; err != nil {
			return err
		}
		if locked.Status != model.GroupBuyTeamPending {
			return nil
		}
		if requireExpired && !locked.ExpireAt.Before(time.Now()) {
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
