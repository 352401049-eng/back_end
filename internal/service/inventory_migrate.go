package service

import (
	"log"
	"os"
	"strings"
	"time"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
)

// MigrateBagToPendingVerifyIfEnabled 启动时若 MIGRATE_BAG_TO_VERIFY=true，将存量背包批量自取入待核销。
func MigrateBagToPendingVerifyIfEnabled(db *gorm.DB) {
	flag := strings.TrimSpace(strings.ToLower(os.Getenv("MIGRATE_BAG_TO_VERIFY")))
	if flag != "1" && flag != "true" && flag != "yes" {
		return
	}
	if err := db.Exec("UPDATE product SET allow_pickup = 1 WHERE allow_pickup <> 1 AND is_deleted = 0").Error; err != nil {
		log.Printf("[migrate-bag] 强制 allow_pickup=1 失败: %v", err)
	} else {
		log.Printf("[migrate-bag] 已强制商品 allow_pickup=1")
	}
	svc := &InventoryService{DB: db}
	n, err := svc.MigrateBagToPendingVerify()
	if err != nil {
		log.Printf("[migrate-bag] 失败: %v (已处理 %d 条)", err, n)
		return
	}
	log.Printf("[migrate-bag] 完成，已迁入待核销 %d 条背包记录", n)
}

// MigrateBagToPendingVerify 将 quantity>0 的背包按自取生成待核销+核销码（幂等：已为 0 跳过）。
func (s *InventoryService) MigrateBagToPendingVerify() (int, error) {
	var rows []model.UserInventory
	if err := query.NotDeleted(s.DB).
		Where("quantity > 0").
		Preload("Product", "is_deleted = ?", model.NotDeleted).
		Order("id ASC").
		Find(&rows).Error; err != nil {
		return 0, err
	}
	done := 0
	for i := range rows {
		inv := rows[i]
		if inv.Quantity == 0 {
			continue
		}
		if inv.Product.ID == 0 {
			log.Printf("[migrate-bag] skip inventory=%d: product missing", inv.ID)
			continue
		}
		umID := inv.Product.MerchantID
		err := s.DB.Transaction(func(tx *gorm.DB) error {
			var fresh model.UserInventory
			if err := query.NotDeleted(tx).
				Where("id = ?", inv.ID).
				Preload("Product", "is_deleted = ?", model.NotDeleted).
				First(&fresh).Error; err != nil {
				return err
			}
			if fresh.Quantity == 0 {
				return nil
			}
			qty := fresh.Quantity
			ownerMerchantID := fresh.Product.MerchantID
			if ownerMerchantID == 0 {
				ownerMerchantID = umID
			}
			oid := uint64(0)
			if fresh.LastOrderID != nil {
				oid = *fresh.LastOrderID
			}
			primaryOID, err := s.deductInventoryUseFIFO(tx, fresh.AccountID, fresh.ProductID, fresh.Spec, qty)
			if err != nil {
				return err
			}
			srcOID := primaryOID
			if srcOID == nil && oid > 0 {
				srcOID = &oid
			}
			optSnap, optStatus, err := applyOptionSelectionsForUsage(
				tx, fresh.Product, model.DeliveryTypePickup, qty, nil, nil,
			)
			if err != nil {
				return err
			}
			expireAt, err := resolveUsageExpireAt(tx, fresh.ProductID, srcOID, time.Now())
			if err != nil {
				return err
			}
			usage := model.UserInventoryUsage{
				AccountID:           fresh.AccountID,
				InventoryID:         fresh.ID,
				ProductID:           fresh.ProductID,
				MerchantID:          ownerMerchantID,
				UsageMerchantID:     ownerMerchantID,
				SourceOrderID:       srcOID,
				Quantity:            qty,
				DeliveryType:        model.DeliveryTypePickup,
				Status:              model.InventoryUsagePendingVerify,
				PackageSelectStatus: model.PackageSelectNone,
				OptionSelections:    optSnap,
				OptionSelectStatus:  optStatus,
				ExpireAt:            expireAt,
			}
			if err := tx.Create(&usage).Error; err != nil {
				return err
			}
			var useLogIDs []uint64
			if err := tx.Model(&model.UserInventoryLog{}).
				Where("account_id = ? AND product_id = ? AND spec = ? AND event_type = ? AND usage_id IS NULL",
					fresh.AccountID, fresh.ProductID, fresh.Spec, model.InventoryEventUse).
				Order("id DESC").Limit(int(qty)+4).
				Pluck("id", &useLogIDs).Error; err != nil {
				return err
			}
			if len(useLogIDs) > 0 {
				if err := tx.Model(&model.UserInventoryLog{}).Where("id IN ?", useLogIDs).
					Update("usage_id", usage.ID).Error; err != nil {
					return err
				}
			}
			if _, err := createVerificationCodeForUsage(tx, fresh.AccountID, usage.ID, expireAt); err != nil {
				return err
			}
			return nil
		})
		if err != nil {
			log.Printf("[migrate-bag] inventory=%d account=%d product=%d err=%v", inv.ID, inv.AccountID, inv.ProductID, err)
			return done, err
		}
		done++
		log.Printf("[migrate-bag] inventory=%d qty=%d -> pending_verify", inv.ID, inv.Quantity)
	}
	return done, nil
}
