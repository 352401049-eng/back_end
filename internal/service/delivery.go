package service

import (
	"crypto/rand"
	"errors"
	"fmt"
	"strings"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/payment"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrDeliveryNotFound      = errors.New("delivery order not found")
	ErrDeliveryTaken         = errors.New("delivery order already taken")
	ErrDeliveryForbidden     = errors.New("delivery order forbidden")
	ErrDeliveryStatusInvalid = errors.New("delivery status invalid")
)

// genPickupCode ?? 4 ???????????????????????
func genPickupCode(tx *gorm.DB, merchantID uint64) string {
	for i := 0; i < 8; i++ {
		var b [2]byte
		if _, err := rand.Read(b[:]); err != nil {
			continue
		}
		n := uint16(b[0])<<8 | uint16(b[1])
		code := n % 10000
		if code == 0 {
			continue
		}
		candidate := fmt.Sprintf("%04d", code)
		// ???????????????
		var existing int64
		tx.Model(&model.DeliveryOrder{}).
			Where("pickup_code = ? AND is_deleted = ? AND "+
				"(EXISTS (SELECT 1 FROM `order` o WHERE o.id = delivery_order.order_id AND o.is_deleted = 0 AND o.merchant_id = ?) OR "+
				"EXISTS (SELECT 1 FROM user_inventory_usage u WHERE u.id = delivery_order.inventory_usage_id AND u.is_deleted = 0 AND u.merchant_id = ?) OR "+
				"EXISTS (SELECT 1 FROM takeout_order t WHERE t.id = delivery_order.takeout_order_id AND t.is_deleted = 0 AND t.merchant_id = ?))",
				candidate, model.NotDeleted, merchantID, merchantID, merchantID).
			Count(&existing)
		if existing == 0 {
			return candidate
		}
	}
	return "1000"
}

type CompleteDeliveryInput struct {
	Remark *string
	Photos []string
}

type DeliveryUsageLine struct {
	UsageID              uint64                         `json:"usage_id"`
	ProductID            uint64                         `json:"product_id"`
	ProductName          string                         `json:"product_name"`
	Quantity             uint32                         `json:"quantity"`
	IsPackage            bool                           `json:"is_package"`
	PackageSelectionText string                         `json:"package_selection_text,omitempty"`
	OptionSelectionText  string                         `json:"option_selection_text,omitempty"`
	OptionSelections     model.OptionSelectionSnapshot  `json:"option_selections,omitempty"`
}

type DeliveryView struct {
	model.DeliveryOrder
	StatusText           string                 `json:"status_text"`
	AddressSnapshot      *model.AddressSnapshot `json:"address_snapshot,omitempty"`
	UsageItems           []DeliveryUsageLine    `json:"usage_items,omitempty"`
	VerifyCode           *string                `json:"verify_code,omitempty"`
	DeliveryTimeRemark   string                 `json:"delivery_time_remark,omitempty"`
}

type DeliveryService struct {
	DB                *gorm.DB
	InventorySvc      *InventoryService
	DeliveryFeePaySvc *DeliveryFeePayService
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
		// ??????????????????merchant_prepared=0)??????
		q = q.Where("status = ? AND rider_id IS NULL AND merchant_prepared = 1", model.DeliveryPendingAccept)
	case "active":
		q = q.Where("rider_id = ? AND status IN ?", riderID, []int{
			int(model.DeliveryAccepted), int(model.DeliveryPicking), int(model.DeliveryDelivering),
		})
	case "history":
		q = q.Where("rider_id = ? AND status IN ?", riderID, []int{
			int(model.DeliveryDelivered), int(model.DeliveryConfirmed),
		})
	default:
		q = q.Where("status = ? AND merchant_prepared = 1", model.DeliveryPendingAccept)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.DeliveryOrder
	if err := q.Preload("Order", "is_deleted = ?", model.NotDeleted).
		Preload("Order.Items", "is_deleted = ?", model.NotDeleted).
		Preload("Order.MerchantProfile", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage.Product", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage.MerchantProfile", "is_deleted = ?", model.NotDeleted).
		Order("id DESC").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return toRiderDeliveryViews(s.DB, list), total, nil
}

// ListForUser scope: active=??? pending_confirm=??????history=????
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
		q = q.Where("status IN ?", []int{
			int(model.DeliveryConfirmed),
			int(model.DeliveryCancelled),
			int(model.DeliveryException),
		})
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
	return toDeliveryViews(s.DB, list), total, nil
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
	view := toDeliveryView(s.DB, d)
	return &view, nil
}

func (s *DeliveryService) Accept(riderID, deliveryID uint64) (*DeliveryView, error) {
	var d model.DeliveryOrder
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := query.NotDeleted(tx).Where("id = ? AND status = ? AND merchant_prepared = 1", deliveryID, model.DeliveryPendingAccept).First(&d).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrDeliveryNotFound
			}
			return err
		}
		if d.InventoryUsageID != nil || d.OrderID == nil {
			// ???????? usage ?????? usage ???????????????
			var active int64
			_ = query.NotDeleted(tx.Model(&model.UserInventoryUsage{})).
				Where("delivery_order_id = ? AND status NOT IN ?", d.ID, []int{
					int(model.InventoryUsageCancelled), int(model.InventoryUsageCancelPending),
				}).Count(&active)
			if active == 0 && d.InventoryUsageID != nil {
				var usage model.UserInventoryUsage
				if err := query.NotDeleted(tx).First(&usage, *d.InventoryUsageID).Error; err != nil {
					return ErrDeliveryNotFound
				}
				if usage.Status == model.InventoryUsageCancelPending || usage.Status == model.InventoryUsageCancelled {
					return ErrDeliveryNotFound
				}
				active = 1
			}
			if active == 0 && d.OrderID == nil {
				if d.TakeoutOrderID == nil {
					return ErrDeliveryNotFound
				}
				var to model.TakeoutOrder
				if err := query.NotDeleted(tx).Select("status").First(&to, *d.TakeoutOrderID).Error; err != nil {
					return ErrDeliveryNotFound
				}
				if to.Status == model.TakeoutStatusCancelled {
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
	return s.getRiderViewByID(deliveryID)
}

func (s *DeliveryService) Start(riderID, deliveryID uint64) (*DeliveryView, error) {
	var d model.DeliveryOrder
	if err := query.NotDeleted(s.DB).Where("id = ? AND rider_id = ?", deliveryID, riderID).First(&d).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDeliveryNotFound
		}
		return nil, err
	}
	if d.Status != model.DeliveryAccepted || d.MerchantPrepared != 1 {
		// ?????????????????pending ??????
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
	return s.getRiderViewByID(deliveryID)
}

// MarkPrepared ??????????(merchant_prepared=0) -> ????1)??
// ????????Preparing ????PendingShip(????????
func (s *DeliveryService) MarkPrepared(merchantID, deliveryID uint64) (*DeliveryView, error) {
	var d model.DeliveryOrder
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
			First(&d, deliveryID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrDeliveryNotFound
			}
			return err
		}
		if d.Status != model.DeliveryPendingAccept || d.MerchantPrepared != 0 {
			// ????(merchant_prepared=0)???????????????
			return ErrDeliveryStatusInvalid
		}
		// ??????
		if !deliveryBelongsToMerchant(tx, &d, merchantID) {
			return ErrDeliveryForbidden
		}
		now := time.Now()
		if err := tx.Model(&d).Updates(map[string]interface{}{
			"merchant_prepared": 1,
			"prepared_at":        now,
		}).Error; err != nil {
			return err
		}
		// ????????????????
		if d.OrderID != nil {
			return tx.Model(&model.Order{}).
				Where("id = ? AND status = ?", *d.OrderID, model.OrderStatusPreparing).
				Update("status", model.OrderStatusPendingShip).Error
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return s.getViewByID(deliveryID)
}

// RejectPrepare 商家在备餐中拒绝出餐：取消配送、商品回退用户背包，并记录拒绝原因。
func (s *DeliveryService) RejectPrepare(merchantID, deliveryID uint64, reason string) (*DeliveryView, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return nil, fmt.Errorf("%w: 请填写拒绝原因", ErrDeliveryStatusInvalid)
	}
	reasonText := "商家拒单：" + reason

	var d model.DeliveryOrder
	var jobs []payment.RefundJob
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		payment.AttachRefundCollector(tx, &jobs)
		if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
			First(&d, deliveryID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrDeliveryNotFound
			}
			return err
		}
		// 仅备餐中（待接单且未出餐、未指派骑手）可拒
		if d.Status != model.DeliveryPendingAccept || d.MerchantPrepared != 0 || d.RiderID != nil {
			return ErrDeliveryStatusInvalid
		}
		if !deliveryBelongsToMerchant(tx, &d, merchantID) {
			return ErrDeliveryForbidden
		}

		var usages []model.UserInventoryUsage
		if err := query.NotDeleted(tx).
			Where("delivery_order_id = ? AND status IN ?", deliveryID,
				[]int{int(model.InventoryUsagePendingShip), int(model.InventoryUsageCancelPending)}).
			Find(&usages).Error; err != nil {
			return err
		}
		if len(usages) == 0 && d.InventoryUsageID != nil {
			var u model.UserInventoryUsage
			if err := query.NotDeleted(tx).First(&u, *d.InventoryUsageID).Error; err == nil {
				// 仅允许备餐中的 usage 回退，避免已取消/已完成重复入账
				if u.Status == model.InventoryUsagePendingShip || u.Status == model.InventoryUsageCancelPending {
					usages = append(usages, u)
				}
			}
		}

		for i := range usages {
			u := usages[i]
			if u.Status != model.InventoryUsagePendingShip && u.Status != model.InventoryUsageCancelPending {
				continue
			}
			if s.InventorySvc == nil {
				return fmt.Errorf("inventory service unavailable")
			}
			var inv model.UserInventory
			if err := query.NotDeleted(tx).First(&inv, u.InventoryID).Error; err != nil {
				return err
			}
			if err := s.InventorySvc.restoreInventoryUseCancel(tx, &u, inv.Spec, &reasonText); err != nil {
				return err
			}
			if err := restorePackageComponentStock(tx, &u); err != nil {
				return err
			}
			if err := tx.Model(&u).Updates(map[string]interface{}{
				"status":        model.InventoryUsageCancelled,
				"cancel_reason": reasonText,
				"remark":        reasonText,
			}).Error; err != nil {
				return err
			}
			// 来源购买单备注拒因，便于用户在「全部订单」查看
			if u.SourceOrderID != nil {
				_ = tx.Model(&model.Order{}).Where("id = ?", *u.SourceOrderID).
					Update("remark", reasonText).Error
			}
		}

		if err := tx.Model(&d).Updates(map[string]interface{}{
			"status":           model.DeliveryCancelled,
			"exception_reason": reasonText,
		}).Error; err != nil {
			return err
		}
		if s.DeliveryFeePaySvc != nil {
			if err := s.DeliveryFeePaySvc.RefundForDeliveryOrderInTx(tx, deliveryID, "取消配送退配送费"); err != nil {
				return err
			}
		}

		// 购买订单路径：回到待履约（已入背包），备注中保留拒单原因
		if d.OrderID != nil {
			_ = tx.Model(&model.Order{}).
				Where("id = ? AND status IN ?", *d.OrderID,
					[]int{int(model.OrderStatusPreparing), int(model.OrderStatusPendingShip)}).
				Updates(map[string]interface{}{
					"status":                model.OrderStatusPendingFulfill,
					"merchant_review_stage": model.MerchantReviewApproved,
					"remark":                reasonText,
				}).Error
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	payment.DispatchRefundJobs(jobs)
	return s.getViewByID(deliveryID)
}

// ReportException ?????????
func (s *DeliveryService) ReportException(riderID, deliveryID uint64, reason string) (*DeliveryView, error) {
	var d model.DeliveryOrder
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := query.NotDeleted(tx.Clauses(clause.Locking{Strength: "UPDATE"})).
			Where("id = ? AND rider_id = ?", deliveryID, riderID).First(&d).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return ErrDeliveryNotFound
			}
			return err
		}
		if d.Status != model.DeliveryAccepted && d.Status != model.DeliveryDelivering {
			return ErrDeliveryStatusInvalid
		}
		reasonPtr := reason
		return tx.Model(&d).Updates(map[string]interface{}{
			"status":           model.DeliveryException,
			"exception_reason": reasonPtr,
		}).Error
	})
	if err != nil {
		return nil, err
	}
	return s.getRiderViewByID(deliveryID)
}

// deliveryBelongsToMerchant ?????????????Order.MerchantID ??InventoryUsage.MerchantID???
func deliveryBelongsToMerchant(tx *gorm.DB, d *model.DeliveryOrder, merchantID uint64) bool {
	if d.OrderID != nil {
		var o model.Order
		if err := tx.Select("merchant_id").First(&o, *d.OrderID).Error; err == nil {
			return o.MerchantID == merchantID
		}
	}
	if d.InventoryUsageID != nil {
		var u model.UserInventoryUsage
		if err := tx.Select("merchant_id").First(&u, *d.InventoryUsageID).Error; err == nil {
			return u.MerchantID == merchantID
		}
	}
	return false
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
	return s.getRiderViewByID(deliveryID)
}

func (s *DeliveryService) ConfirmReceipt(accountID, deliveryID uint64) (*DeliveryView, error) {
	var d model.DeliveryOrder
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		// FOR UPDATE ??????????????????
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
		if d.TakeoutOrderID != nil {
			if err := tx.Model(&model.TakeoutOrder{}).Where("id = ?", *d.TakeoutOrderID).
				Update("status", model.TakeoutStatusCompleted).Error; err != nil {
				return err
			}
		}
		// ?????????????? usage???????
		usageQ := tx.Model(&model.UserInventoryUsage{}).
			Where("status = ?", model.InventoryUsagePendingShip)
		if d.InventoryUsageID != nil {
			usageQ = usageQ.Where("delivery_order_id = ? OR id = ?", d.ID, *d.InventoryUsageID)
		} else {
			usageQ = usageQ.Where("delivery_order_id = ?", d.ID)
		}
		if err := usageQ.Update("status", model.InventoryUsageCompleted).Error; err != nil {
			return err
		}
		// ???????????????? delivery_order ??????????
		// ???????????? > 0 ????????????????
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

// resolveDeliveryMerchantIDInTx ???? order ??inventory_usage ???? ID?delivery_order ????merchant_id???
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
	if d.TakeoutOrderID != nil {
		var to model.TakeoutOrder
		if err := tx.Select("merchant_id").First(&to, *d.TakeoutOrderID).Error; err == nil {
			return to.MerchantID
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
		OR inventory_usage_id IN (SELECT id FROM user_inventory_usage WHERE account_id = ? AND is_deleted = 0)
		OR id IN (SELECT delivery_order_id FROM user_inventory_usage WHERE account_id = ? AND delivery_order_id IS NOT NULL AND is_deleted = 0)
		OR takeout_order_id IN (SELECT id FROM takeout_order WHERE account_id = ? AND is_deleted = 0)`,
		accountID, accountID, accountID, accountID,
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
	if d.TakeoutOrderID != nil {
		var to model.TakeoutOrder
		if err := query.NotDeleted(tx).Where("id = ? AND account_id = ?", *d.TakeoutOrderID, accountID).First(&to).Error; err != nil {
			return ErrDeliveryForbidden
		}
		return nil
	}
	if d.InventoryUsageID != nil {
		var usage model.UserInventoryUsage
		if err := query.NotDeleted(tx).Where("id = ? AND account_id = ?", *d.InventoryUsageID, accountID).First(&usage).Error; err != nil {
			// ??????? usage ????
			var n int64
			if err2 := query.NotDeleted(tx.Model(&model.UserInventoryUsage{})).
				Where("delivery_order_id = ? AND account_id = ?", d.ID, accountID).Count(&n).Error; err2 != nil || n == 0 {
				return ErrDeliveryForbidden
			}
			return nil
		}
		return nil
	}
	var n int64
	if err := query.NotDeleted(tx.Model(&model.UserInventoryUsage{})).
		Where("delivery_order_id = ? AND account_id = ?", d.ID, accountID).Count(&n).Error; err != nil || n == 0 {
		return ErrDeliveryForbidden
	}
	return nil
}

// ListPreparingForMerchant ????????????merchant_prepared=0??
// ????????order_id?????????inventory_usage_id???
func (s *DeliveryService) ListPreparingForMerchant(merchantID uint64, page, pageSize int) ([]DeliveryView, int64, error) {
	return s.listDeliveriesForMerchant(merchantID, page, pageSize, 0)
}

// ListPreparedForMerchant ???????????????merchant_prepared=1, status=PendingAccept???
func (s *DeliveryService) ListPreparedForMerchant(merchantID uint64, page, pageSize int) ([]DeliveryView, int64, error) {
	return s.listDeliveriesForMerchant(merchantID, page, pageSize, 1)
}

func (s *DeliveryService) listDeliveriesForMerchant(merchantID uint64, page, pageSize int, merchantPrepared uint8) ([]DeliveryView, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	offset := (page - 1) * pageSize

	q := query.NotDeleted(s.DB.Model(&model.DeliveryOrder{})).
		Where("status = ? AND merchant_prepared = ?", model.DeliveryPendingAccept, merchantPrepared).
		Where(
			"EXISTS (SELECT 1 FROM `order` o WHERE o.id = delivery_order.order_id AND o.is_deleted = 0 AND o.merchant_id = ?) OR "+
				"EXISTS (SELECT 1 FROM user_inventory_usage u WHERE u.id = delivery_order.inventory_usage_id AND u.is_deleted = 0 AND u.merchant_id = ?) OR "+
				"EXISTS (SELECT 1 FROM user_inventory_usage u2 WHERE u2.delivery_order_id = delivery_order.id AND u2.is_deleted = 0 AND u2.merchant_id = ?)",
			merchantID, merchantID, merchantID,
		)

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
	return toDeliveryViews(s.DB, list), total, nil
}

func (s *DeliveryService) getViewByID(id uint64) (*DeliveryView, error) {
	d, err := s.getByID(id)
	if err != nil {
		return nil, err
	}
	view := toDeliveryView(s.DB, *d)
	return &view, nil
}

func (s *DeliveryService) getRiderViewByID(id uint64) (*DeliveryView, error) {
	d, err := s.getByID(id)
	if err != nil {
		return nil, err
	}
	view := toRiderDeliveryView(s.DB, *d)
	return &view, nil
}

// GetForRider ???????????????????????????????
func (s *DeliveryService) GetForRider(riderID, deliveryID uint64) (*DeliveryView, error) {
	d, err := s.getByID(deliveryID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrDeliveryNotFound
		}
		return nil, err
	}
	// ??????rider_id)????????????????
	if d.RiderID != nil && *d.RiderID != riderID {
		return nil, ErrDeliveryForbidden
	}
	view := toRiderDeliveryView(s.DB, *d)
	return &view, nil
}

func (s *DeliveryService) getByID(id uint64) (*model.DeliveryOrder, error) {
	var d model.DeliveryOrder
	if err := query.NotDeleted(s.DB).
		Preload("Order", "is_deleted = ?", model.NotDeleted).
		Preload("Order.Items", "is_deleted = ?", model.NotDeleted).
		Preload("Order.MerchantProfile", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage.Product", "is_deleted = ?", model.NotDeleted).
		Preload("InventoryUsage.MerchantProfile", "is_deleted = ?", model.NotDeleted).
		First(&d, id).Error; err != nil {
		return nil, err
	}
	return &d, nil
}

func toDeliveryViews(db *gorm.DB, list []model.DeliveryOrder) []DeliveryView {
	views := make([]DeliveryView, 0, len(list))
	for i := range list {
		views = append(views, toDeliveryView(db, list[i]))
	}
	return views
}

func toDeliveryView(db *gorm.DB, d model.DeliveryOrder) DeliveryView {
	view := DeliveryView{
		DeliveryOrder: d,
		StatusText:    model.DeliveryStatusText(d.Status),
	}
	attachDeliveryUsageItems(db, &view)
	if view.TakeoutOrderID != nil {
		attachTakeoutDeliveryData(db, &view)
	}
	return view
}

func toRiderDeliveryViews(db *gorm.DB, list []model.DeliveryOrder) []DeliveryView {
	views := make([]DeliveryView, 0, len(list))
	for i := range list {
		views = append(views, toRiderDeliveryView(db, list[i]))
	}
	return views
}

func toRiderDeliveryView(db *gorm.DB, d model.DeliveryOrder) DeliveryView {
	view := toDeliveryView(db, d)
	attachRiderVerifyCode(db, &view)
	return view
}

// attachRiderVerifyCode 背包跑腿配送单在骑手接单后附带核销码；外卖单永不返回。
func attachRiderVerifyCode(db *gorm.DB, view *DeliveryView) {
	if db == nil || view == nil {
		return
	}
	if view.TakeoutOrderID != nil || view.InventoryUsageID == nil || view.RiderID == nil {
		return
	}
	usageIDs := make([]uint64, 0, 1+len(view.UsageItems))
	usageIDs = append(usageIDs, *view.InventoryUsageID)
	seen := map[uint64]struct{}{*view.InventoryUsageID: {}}
	for _, line := range view.UsageItems {
		if line.UsageID == 0 {
			continue
		}
		if _, ok := seen[line.UsageID]; ok {
			continue
		}
		seen[line.UsageID] = struct{}{}
		usageIDs = append(usageIDs, line.UsageID)
	}
	var codes []string
	for _, uid := range usageIDs {
		var vc model.VerificationCode
		if err := query.NotDeleted(db).
			Where("inventory_usage_id = ? AND status = ?", uid, model.VerificationCodeUnused).
			First(&vc).Error; err != nil {
			continue
		}
		codes = append(codes, vc.Code)
	}
	if len(codes) == 0 {
		return
	}
	joined := strings.Join(codes, "、")
	view.VerifyCode = &joined
}

func attachDeliveryUsageItems(db *gorm.DB, view *DeliveryView) {
	if db == nil || view == nil {
		return
	}
	var usages []model.UserInventoryUsage
	q := query.NotDeleted(db).
		Preload("Product", "is_deleted = ?", model.NotDeleted).
		Where("delivery_order_id = ?", view.ID).
		Order("id ASC")
	if err := q.Find(&usages).Error; err != nil || len(usages) == 0 {
		// ???? inventory_usage_id ????
		if view.InventoryUsageID != nil {
			var u model.UserInventoryUsage
			if err := query.NotDeleted(db).
				Preload("Product", "is_deleted = ?", model.NotDeleted).
				First(&u, *view.InventoryUsageID).Error; err == nil {
				usages = []model.UserInventoryUsage{u}
			}
		}
	}
	if len(usages) == 0 {
		return
	}
	if view.DeliveryTimeRemark == "" && usages[0].Remark != "" {
		view.DeliveryTimeRemark = usages[0].Remark
	}
	lines := make([]DeliveryUsageLine, 0, len(usages))
	for i := range usages {
		u := usages[i]
		name := ""
		isPkg := false
		if u.Product != nil {
			name = u.Product.Name
			isPkg = u.Product.ItemType == model.ProductItemTypePackage
		}
		lines = append(lines, DeliveryUsageLine{
			UsageID: u.ID, ProductID: u.ProductID, ProductName: name,
			Quantity: u.Quantity, IsPackage: isPkg,
			PackageSelectionText: u.PackageSelections.SummaryText(),
			OptionSelectionText:  u.OptionSelections.SummaryText(),
			OptionSelections:     u.OptionSelections,
		})
	}
	view.UsageItems = lines
}

func attachTakeoutDeliveryData(db *gorm.DB, view *DeliveryView) {
	if db == nil || view == nil || view.TakeoutOrderID == nil {
		return
	}
	var to model.TakeoutOrder
	if err := query.NotDeleted(db).
		Preload("Items", "is_deleted = ?", model.NotDeleted).
		First(&to, *view.TakeoutOrderID).Error; err != nil {
		return
	}
	view.AddressSnapshot = to.AddressSnapshot
	view.DeliveryTimeRemark = to.DeliveryTimeRemark

	_, _, selLines := buildTakeoutSelectionDisplay(db, &to)
	if len(selLines) == 0 {
		return
	}
	lines := make([]DeliveryUsageLine, 0, len(selLines))
	for _, ln := range selLines {
		lines = append(lines, DeliveryUsageLine{
			ProductID:            ln.ProductID,
			ProductName:          ln.ProductName,
			Quantity:             ln.Quantity,
			IsPackage:            ln.IsPackage,
			PackageSelectionText: ln.PackageSelectionText,
			OptionSelectionText:  ln.OptionSelectionText,
		})
	}
	view.UsageItems = lines
}

// ListForAdmin ??????????????
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
	return toDeliveryViews(s.DB, list), total, nil
}
