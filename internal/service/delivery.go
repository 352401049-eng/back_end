package service

import (
	"errors"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrDeliveryNotFound       = errors.New("delivery order not found")
	ErrDeliveryTaken          = errors.New("delivery order already taken")
	ErrDeliveryForbidden      = errors.New("delivery order forbidden")
	ErrDeliveryStatusInvalid  = errors.New("delivery status invalid")
)

type CompleteDeliveryInput struct {
	Remark *string
	Photos []string
}

type DeliveryView struct {
	model.DeliveryOrder
	StatusText string `json:"status_text"`
}

type DeliveryService struct {
	DB *gorm.DB
}

func (s *DeliveryService) ListForRider(riderID uint64, scope string, page, pageSize int) ([]DeliveryView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	offset := (page - 1) * pageSize

	q := query.NotDeleted(s.DB.Model(&model.DeliveryOrder{}))
	switch scope {
	case "pending":
		q = q.Where("status = ? AND rider_id IS NULL", model.DeliveryPendingAccept)
	case "active":
		q = q.Where("rider_id = ? AND status IN ?", riderID, []int{
			int(model.DeliveryAccepted), int(model.DeliveryPicking), int(model.DeliveryDelivering),
		})
	case "history":
		q = q.Where("rider_id = ? AND status IN ?", riderID, []int{
			int(model.DeliveryDelivered), int(model.DeliveryConfirmed),
		})
	default:
		q = q.Where("status = ?", model.DeliveryPendingAccept)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.DeliveryOrder
	if err := q.Preload("Order", "is_deleted = ?", model.NotDeleted).
		Preload("Order.Items", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage.Product", "is_deleted = ?", model.NotDeleted).
		Order("id DESC").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return toDeliveryViews(list), total, nil
}

// ListForUser scope: active=配送中 pending_confirm=待确认收货 history=已完成
func (s *DeliveryService) ListForUser(accountID uint64, scope string, page, pageSize int) ([]DeliveryView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	offset := (page - 1) * pageSize

	q := s.userDeliveryQuery(accountID)
	switch scope {
	case "pending_confirm":
		q = q.Where("status = ? AND user_confirmed = ?", model.DeliveryDelivered, 0)
	case "history":
		q = q.Where("status = ?", model.DeliveryConfirmed)
	case "active", "delivering":
		q = q.Where("status IN ?", []int{
			int(model.DeliveryPendingAccept),
			int(model.DeliveryAccepted), int(model.DeliveryPicking), int(model.DeliveryDelivering),
		})
	default:
		q = q.Where("status IN ?", []int{
			int(model.DeliveryPendingAccept),
			int(model.DeliveryAccepted), int(model.DeliveryPicking), int(model.DeliveryDelivering), int(model.DeliveryDelivered),
		})
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.DeliveryOrder
	if err := q.Preload("Order", "is_deleted = ?", model.NotDeleted).
		Preload("Order.Items", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage.Product", "is_deleted = ?", model.NotDeleted).
		Order("id DESC").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return toDeliveryViews(list), total, nil
}

func (s *DeliveryService) GetForUser(accountID, deliveryID uint64) (*DeliveryView, error) {
	var d model.DeliveryOrder
	if err := s.userDeliveryQuery(accountID).Where("id = ?", deliveryID).
		Preload("Order", "is_deleted = ?", model.NotDeleted).
		Preload("Order.Items", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage.Product", "is_deleted = ?", model.NotDeleted).
		First(&d).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDeliveryNotFound
		}
		return nil, err
	}
	view := toDeliveryView(d)
	return &view, nil
}

func (s *DeliveryService) Accept(riderID, deliveryID uint64) (*DeliveryView, error) {
	var d model.DeliveryOrder
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := query.NotDeleted(tx).Where("id = ? AND status = ?", deliveryID, model.DeliveryPendingAccept).First(&d).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrDeliveryNotFound
			}
			return err
		}
		if d.InventoryUsageID != nil {
			var usage model.UserInventoryUsage
			if err := query.NotDeleted(tx).First(&usage, *d.InventoryUsageID).Error; err == nil {
				if usage.Status == model.InventoryUsageCancelPending || usage.Status == model.InventoryUsageCancelled {
					return ErrDeliveryNotFound
				}
			}
		}
		now := time.Now()
		if err := tx.Model(&d).Updates(map[string]interface{}{
			"rider_id": riderID, "status": model.DeliveryAccepted, "accepted_at": now,
		}).Error; err != nil {
			return err
		}
		if d.OrderID != nil {
			return tx.Model(&model.Order{}).Where("id = ?", *d.OrderID).Update("status", model.OrderStatusShipping).Error
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.getViewByID(deliveryID)
}

func (s *DeliveryService) Start(riderID, deliveryID uint64) (*DeliveryView, error) {
	var d model.DeliveryOrder
	if err := query.NotDeleted(s.DB).Where("id = ? AND rider_id = ?", deliveryID, riderID).First(&d).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDeliveryNotFound
		}
		return nil, err
	}
	if d.Status != model.DeliveryAccepted && d.Status != model.DeliveryPicking {
		return nil, ErrDeliveryStatusInvalid
	}
	now := time.Now()
	updates := map[string]interface{}{
		"status": model.DeliveryDelivering, "started_at": now,
	}
	if err := s.DB.Model(&d).Updates(updates).Error; err != nil {
		return nil, err
	}
	if d.OrderID != nil {
		_ = s.DB.Model(&model.Order{}).Where("id = ?", *d.OrderID).Update("status", model.OrderStatusShipping).Error
	}
	return s.getViewByID(deliveryID)
}

// CancelByRider 骑手取消接单：仅当 status=Accepted（已接单未开始配送）时可取消。
// 取消后订单回到待接单状态（status=0，rider_id/accepted_at 清空），订单状态同步回退，
// 其他骑手可接。
func (s *DeliveryService) CancelByRider(riderID, deliveryID uint64) (*DeliveryView, error) {
	var d model.DeliveryOrder
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		// FOR UPDATE 行锁，防并发取消/开始配送竞态
		if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
			Where("id = ? AND rider_id = ?", deliveryID, riderID).First(&d).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrDeliveryNotFound
			}
			return err
		}
		// 仅已接单（未开始配送）可取消；已开始配送（Picking/Delivering）不允许
		if d.Status != model.DeliveryAccepted {
			return ErrDeliveryStatusInvalid
		}
		if err := tx.Model(&d).Updates(map[string]interface{}{
			"status":      model.DeliveryPendingAccept,
			"rider_id":    nil,
			"accepted_at": nil,
		}).Error; err != nil {
			return err
		}
		// 回滚父订单状态：接单时 Accept 推进到 Shipping(4)，取消应回退到 PendingShip(3)
		if d.OrderID != nil {
			return tx.Model(&model.Order{}).Where("id = ? AND status = ?", *d.OrderID, model.OrderStatusShipping).
				Update("status", model.OrderStatusPendingShip).Error
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.getViewByID(deliveryID)
}

func (s *DeliveryService) Complete(riderID, deliveryID uint64, input CompleteDeliveryInput) (*DeliveryView, error) {
	var d model.DeliveryOrder
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := query.NotDeleted(tx).
			Where("id = ? AND rider_id = ? AND status IN ?", deliveryID, riderID,
				[]int{int(model.DeliveryAccepted), int(model.DeliveryPicking), int(model.DeliveryDelivering)}).
			First(&d).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrDeliveryNotFound
			}
			return err
		}
		now := time.Now()
		updates := map[string]interface{}{
			"status": model.DeliveryDelivered, "delivered_at": now,
		}
		if input.Remark != nil {
			updates["deliver_remark"] = *input.Remark
		}
		if len(input.Photos) > 0 {
			updates["deliver_photos"] = toJSONColumn(input.Photos)
		}
		if err := tx.Model(&d).Updates(updates).Error; err != nil {
			return err
		}
		if d.OrderID != nil {
			return tx.Model(&model.Order{}).Where("id = ?", *d.OrderID).
				Update("status", model.OrderStatusPendingConfirm).Error
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.getViewByID(deliveryID)
}

func (s *DeliveryService) ConfirmReceipt(accountID, deliveryID uint64) (*DeliveryView, error) {
	var d model.DeliveryOrder
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		// FOR UPDATE 行锁，防止并发确认收货导致重复入账
		q := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).Where("id = ? AND status = ?", deliveryID, model.DeliveryDelivered)
		if err := q.First(&d).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrDeliveryStatusInvalid
			}
			return err
		}
		if err := s.assertDeliveryOwner(tx, accountID, &d); err != nil {
			return err
		}
		now := time.Now()
		if err := tx.Model(&d).Updates(map[string]interface{}{
			"status": model.DeliveryConfirmed, "user_confirmed": 1,
		}).Error; err != nil {
			return err
		}
		if d.OrderID != nil {
			if err := tx.Model(&model.Order{}).Where("id = ?", *d.OrderID).
				Update("status", model.OrderStatusCompleted).Error; err != nil {
				return err
			}
		}
		if d.InventoryUsageID != nil {
			if err := tx.Model(&model.UserInventoryUsage{}).
				Where("id = ? AND status = ?", *d.InventoryUsageID, model.InventoryUsagePendingShip).
				Update("status", model.InventoryUsageCompleted).Error; err != nil {
				return err
			}
		}
		// 骑手收益入账：用户确认收货时，按 delivery_order 快照金额创建收益记录
		// 仅当有骑手接单且收益金额 > 0 时入账；异常单/取消单不进此分支
		if d.RiderID != nil && d.RiderEarnings > 0 {
			merchantID := resolveDeliveryMerchantIDInTx(tx, &d)
			earning := model.RiderEarning{
				RiderID:         *d.RiderID,
				DeliveryOrderID: d.ID,
				OrderID:         d.OrderID,
				MerchantID:      merchantID,
				Amount:          d.RiderEarnings,
				Status:          model.RiderEarningPending,
				CreatedAt:       now,
			}
			if err := tx.Create(&earning).Error; err != nil {
				return err
			}
		}
		_ = now
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.getViewByID(deliveryID)
}

func (s *DeliveryService) ConfirmReceiptByOrderID(accountID, orderID uint64) (*DeliveryView, error) {
	var d model.DeliveryOrder
	if err := query.NotDeleted(s.DB).
		Where("order_id = ? AND status = ?", orderID, model.DeliveryDelivered).
		First(&d).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDeliveryNotFound
		}
		return nil, err
	}
	return s.ConfirmReceipt(accountID, d.ID)
}

// resolveDeliveryMerchantIDInTx 从关联的 order 或 inventory_usage 解析商家 ID（delivery_order 本身无 merchant_id）。
func resolveDeliveryMerchantIDInTx(tx *gorm.DB, d *model.DeliveryOrder) uint64 {
	if d.OrderID != nil {
		var o model.Order
		if err := tx.Select("merchant_id").First(&o, *d.OrderID).Error; err == nil {
			return o.MerchantID
		}
	}
	if d.InventoryUsageID != nil {
		var u model.UserInventoryUsage
		if err := tx.Select("merchant_id").First(&u, *d.InventoryUsageID).Error; err == nil {
			return u.MerchantID
		}
	}
	return 0
}

func (s *DeliveryService) CreateForOrder(orderID uint64) (*model.DeliveryOrder, error) {
	d := model.DeliveryOrder{OrderID: &orderID, Status: model.DeliveryPendingAccept}
	if err := s.DB.Create(&d).Error; err != nil {
		return nil, err
	}
	return &d, nil
}

func (s *DeliveryService) userDeliveryQuery(accountID uint64) *gorm.DB {
	return query.NotDeleted(s.DB.Model(&model.DeliveryOrder{})).Where(
		`order_id IN (SELECT id FROM `+"`order`"+` WHERE account_id = ? AND is_deleted = 0)
		OR inventory_usage_id IN (SELECT id FROM user_inventory_usage WHERE account_id = ? AND is_deleted = 0)`,
		accountID, accountID,
	)
}

func (s *DeliveryService) assertDeliveryOwner(tx *gorm.DB, accountID uint64, d *model.DeliveryOrder) error {
	if d.OrderID != nil {
		var order model.Order
		if err := query.NotDeleted(tx).Where("id = ? AND account_id = ?", *d.OrderID, accountID).First(&order).Error; err != nil {
			return ErrDeliveryForbidden
		}
		return nil
	}
	if d.InventoryUsageID != nil {
		var usage model.UserInventoryUsage
		if err := query.NotDeleted(tx).Where("id = ? AND account_id = ?", *d.InventoryUsageID, accountID).First(&usage).Error; err != nil {
			return ErrDeliveryForbidden
		}
		return nil
	}
	return ErrDeliveryForbidden
}

func (s *DeliveryService) getViewByID(id uint64) (*DeliveryView, error) {
	d, err := s.getByID(id)
	if err != nil {
		return nil, err
	}
	view := toDeliveryView(*d)
	return &view, nil
}

func (s *DeliveryService) getByID(id uint64) (*model.DeliveryOrder, error) {
	var d model.DeliveryOrder
	if err := query.NotDeleted(s.DB).
		Preload("Order", "is_deleted = ?", model.NotDeleted).
		Preload("Order.Items", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage.Product", "is_deleted = ?", model.NotDeleted).
		First(&d, id).Error; err != nil {
		return nil, err
	}
	return &d, nil
}

func toDeliveryViews(list []model.DeliveryOrder) []DeliveryView {
	views := make([]DeliveryView, 0, len(list))
	for i := range list {
		views = append(views, toDeliveryView(list[i]))
	}
	return views
}

func toDeliveryView(d model.DeliveryOrder) DeliveryView {
	return DeliveryView{
		DeliveryOrder: d,
		StatusText:    model.DeliveryStatusText(d.Status),
	}
}

// ListForAdmin 全平台或按商家筛选配送单。
func (s *DeliveryService) ListForAdmin(merchantID *uint64, status *uint8, page, pageSize int) ([]DeliveryView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	offset := (page - 1) * pageSize

	q := query.NotDeleted(s.DB.Model(&model.DeliveryOrder{}))
	if merchantID != nil {
		q = q.Where(
			"EXISTS (SELECT 1 FROM `order` o WHERE o.id = delivery_order.order_id AND o.is_deleted = 0 AND o.merchant_id = ?) OR "+
				"EXISTS (SELECT 1 FROM user_inventory_usage u WHERE u.id = delivery_order.inventory_usage_id AND u.is_deleted = 0 AND u.merchant_id = ?)",
			*merchantID, *merchantID,
		)
	}
	if status != nil {
		q = q.Where("status = ?", *status)
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.DeliveryOrder
	if err := q.Preload("Order", "is_deleted = ?", model.NotDeleted).
		Preload("Order.Items", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage.Product", "is_deleted = ?", model.NotDeleted).
		Order("id DESC").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return toDeliveryViews(list), total, nil
}
