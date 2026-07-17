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

var (
	ErrVerifyCodeNotFound      = errors.New("verification code not found")
	ErrVerifyCodeUsed          = errors.New("verification code already used")
	ErrVerifyCodeExpired       = errors.New("verification code expired")
	ErrVerifyMerchantMismatch  = errors.New("verification merchant mismatch")
)

type VerificationService struct {
	DB           *gorm.DB
	InventorySvc *InventoryService
}

// VerifyPreviewView 扫码后展示的核销信息（数量固定，不可选）。
type VerifyPreviewView struct {
	Code        string  `json:"code"`
	ProductID   uint64  `json:"product_id"`
	ProductName string  `json:"product_name"`
	CoverURL    string  `json:"cover_url,omitempty"`
	Spec        string  `json:"spec,omitempty"`
	Quantity    uint32  `json:"quantity"`
	UsageID     *uint64 `json:"usage_id,omitempty"`
	OrderID     *uint64 `json:"order_id,omitempty"`
	OrderNo     string  `json:"order_no,omitempty"`
}

type verifyResolveResult struct {
	vc         model.VerificationCode
	merchantID uint64
	product    model.Product
	spec       string
	quantity   uint32
	usageID    *uint64
	orderID    *uint64
	orderNo    string
}

// LookupByCode 扫码查询核销信息，仅商品所属商家可查看。
func (s *VerificationService) LookupByCode(merchantID uint64, code string) (*VerifyPreviewView, error) {
	resolved, err := s.resolveVerifyCode(code, false)
	if err != nil {
		return nil, err
	}
	if resolved.merchantID != merchantID {
		return nil, ErrVerifyMerchantMismatch
	}
	return toVerifyPreviewView(resolved), nil
}

// Verify 一次性核销：整单/整次使用记录完成，不支持部分数量。
func (s *VerificationService) Verify(merchantID, operatorID uint64, code string) (*model.VerificationRecord, error) {
	var vc model.VerificationCode
	var record model.VerificationRecord

	err := s.DB.Transaction(func(tx *gorm.DB) error {
		resolved, err := s.resolveVerifyCodeInTx(tx, code, true)
		if err != nil {
			return err
		}
		if resolved.merchantID != merchantID {
			return ErrVerifyMerchantMismatch
		}
		vc = resolved.vc

		now := time.Now()
		result := tx.Model(&vc).Where("status = ?", model.VerificationCodeUnused).
			Updates(map[string]interface{}{"status": model.VerificationCodeUsed, "used_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrVerifyCodeUsed
		}

		orderID := uint64(0)
		if resolved.usageID != nil {
			var usage model.UserInventoryUsage
			if err := query.NotDeleted(tx).First(&usage, *resolved.usageID).Error; err != nil {
				return ErrVerifyCodeNotFound
			}
			if usage.MerchantID != merchantID || usage.ProductID != resolved.product.ID {
				return ErrVerifyMerchantMismatch
			}
			if usage.SourceOrderID != nil {
				orderID = *usage.SourceOrderID
			}
			record = model.VerificationRecord{
				VerificationCodeID: vc.ID, OrderID: orderID,
				MerchantID: merchantID, OperatorID: operatorID, VerifiedAt: now,
			}
			if err := tx.Create(&record).Error; err != nil {
				return err
			}
			if s.InventorySvc != nil {
				return s.InventorySvc.CompleteUsageByVerify(tx, usage.ID)
			}
			return tx.Model(&usage).Update("status", model.InventoryUsageCompleted).Error
		}

		if resolved.orderID == nil {
			return ErrVerifyCodeNotFound
		}
		var order model.Order
		if err := query.NotDeleted(tx).Where("id = ? AND merchant_id = ?", *resolved.orderID, merchantID).
			First(&order).Error; err != nil {
			return ErrVerifyMerchantMismatch
		}
		record = model.VerificationRecord{
			VerificationCodeID: vc.ID, OrderID: order.ID,
			MerchantID: merchantID, OperatorID: operatorID, VerifiedAt: now,
		}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		return tx.Model(&order).Update("status", model.OrderStatusCompleted).Error
	})
	if err != nil {
		if errors.Is(err, ErrVerifyMerchantMismatch) {
			return nil, ErrVerifyMerchantMismatch
		}
		if errors.Is(err, ErrVerifyCodeNotFound) || errors.Is(err, ErrVerifyCodeUsed) || errors.Is(err, ErrVerifyCodeExpired) {
			return nil, err
		}
		return nil, fmt.Errorf("核销失败: %w", err)
	}
	return &record, nil
}

func (s *VerificationService) resolveVerifyCode(code string, forUpdate bool) (*verifyResolveResult, error) {
	if code == "" {
		return nil, ErrVerifyCodeNotFound
	}
	q := query.NotDeleted(s.DB).Where("code = ?", code)
	if forUpdate {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var vc model.VerificationCode
	if err := q.First(&vc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrVerifyCodeNotFound
		}
		return nil, err
	}
	return s.buildVerifyResolveResult(s.DB, &vc)
}

func (s *VerificationService) resolveVerifyCodeInTx(tx *gorm.DB, code string, forUpdate bool) (*verifyResolveResult, error) {
	if code == "" {
		return nil, ErrVerifyCodeNotFound
	}
	q := query.NotDeleted(tx).Where("code = ?", code)
	if forUpdate {
		q = q.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var vc model.VerificationCode
	if err := q.First(&vc).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrVerifyCodeNotFound
		}
		return nil, err
	}
	return s.buildVerifyResolveResult(tx, &vc)
}

func (s *VerificationService) buildVerifyResolveResult(db *gorm.DB, vc *model.VerificationCode) (*verifyResolveResult, error) {
	if vc.Status == model.VerificationCodeUsed {
		return nil, ErrVerifyCodeUsed
	}
	if vc.Status == model.VerificationCodeExpired {
		return nil, ErrVerifyCodeExpired
	}
	if vc.ExpiredAt != nil && vc.ExpiredAt.Before(time.Now()) {
		return nil, ErrVerifyCodeExpired
	}

	if vc.InventoryUsageID != nil {
		var usage model.UserInventoryUsage
		if err := query.NotDeleted(db).
			Preload("Product", "is_deleted = ?", model.NotDeleted).
			Preload("Inventory", "is_deleted = ?", model.NotDeleted).
			First(&usage, *vc.InventoryUsageID).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, ErrVerifyCodeNotFound
			}
			return nil, err
		}
		if usage.Status != model.InventoryUsagePendingVerify {
			return nil, ErrVerifyCodeUsed
		}
		var product model.Product
		if usage.Product != nil && usage.Product.ID != 0 {
			product = *usage.Product
		} else if err := query.NotDeleted(db).First(&product, usage.ProductID).Error; err != nil {
			return nil, ErrVerifyCodeNotFound
		}
		if product.MerchantID != usage.MerchantID {
			return nil, ErrVerifyMerchantMismatch
		}
		spec := ""
		if usage.Inventory != nil {
			spec = usage.Inventory.Spec
		}
		return &verifyResolveResult{
			vc: *vc, merchantID: product.MerchantID, product: product,
			spec: spec, quantity: usage.Quantity, usageID: vc.InventoryUsageID,
		}, nil
	}

	if vc.OrderID == nil {
		return nil, ErrVerifyCodeNotFound
	}
	var order model.Order
	if err := query.NotDeleted(db).
		Preload("Items", "is_deleted = ?", model.NotDeleted).
		First(&order, *vc.OrderID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrVerifyCodeNotFound
		}
		return nil, err
	}
	if order.Status != model.OrderStatusPendingVerify {
		return nil, ErrVerifyCodeUsed
	}
	if len(order.Items) == 0 {
		return nil, ErrVerifyCodeNotFound
	}
	item := order.Items[0]
	var product model.Product
	if err := query.NotDeleted(db).First(&product, item.ProductID).Error; err != nil {
		return nil, ErrVerifyCodeNotFound
	}
	if product.MerchantID != order.MerchantID {
		return nil, ErrVerifyMerchantMismatch
	}
	spec := ""
	if item.Spec != nil {
		spec = *item.Spec
	}
	orderID := order.ID
	return &verifyResolveResult{
		vc: *vc, merchantID: product.MerchantID, product: product,
		spec: spec, quantity: item.Quantity, orderID: &orderID, orderNo: order.OrderNo,
	}, nil
}

func toVerifyPreviewView(resolved *verifyResolveResult) *VerifyPreviewView {
	return &VerifyPreviewView{
		Code:        resolved.vc.Code,
		ProductID:   resolved.product.ID,
		ProductName: resolved.product.Name,
		CoverURL:    resolved.product.CoverURL,
		Spec:        resolved.spec,
		Quantity:    resolved.quantity,
		UsageID:     resolved.usageID,
		OrderID:     resolved.orderID,
		OrderNo:     resolved.orderNo,
	}
}

func (s *VerificationService) ListByMerchant(merchantID uint64, page, pageSize int) ([]model.VerificationRecord, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	offset := (page - 1) * pageSize
	q := query.NotDeleted(s.DB.Model(&model.VerificationRecord{})).Where("merchant_id = ?", merchantID)
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.VerificationRecord
	if err := q.Order("id DESC").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func (s *VerificationService) ListAll(page, pageSize int) ([]model.VerificationRecord, int64, error) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 10
	}
	offset := (page - 1) * pageSize
	q := query.NotDeleted(s.DB.Model(&model.VerificationRecord{}))
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.VerificationRecord
	if err := q.Order("id DESC").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}
