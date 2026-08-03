package service

import (
	"errors"
	"fmt"

	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/query"

	"gorm.io/gorm"
)

// ApplicableMerchantBrief 适用店面摘要（用户端展示）。
type ApplicableMerchantBrief struct {
	ID       uint64 `json:"id"`
	ShopName string `json:"shop_name"`
}

func normalizeApplicableIDs(ownerID uint64, ids []uint64) ([]uint64, error) {
	if ownerID == 0 {
		return nil, fmt.Errorf("%w: 请指定所属商家", ErrInvalidProductArg)
	}
	seen := map[uint64]struct{}{}
	out := []uint64{ownerID}
	seen[ownerID] = struct{}{}
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out, nil
}

func (s *ProductService) ReplaceApplicableMerchants(tx *gorm.DB, productID, ownerID uint64, ids []uint64) error {
	ids, err := normalizeApplicableIDs(ownerID, ids)
	if err != nil {
		return err
	}
	db := tx
	if db == nil {
		db = s.DB
	}
	var product model.Product
	if err := query.NotDeleted(db).Select("merchant_id").First(&product, productID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrProductNotFound
		}
		return err
	}
	if product.MerchantID != ownerID {
		return ErrProductForbidden
	}
	for _, mid := range ids {
		if err := s.ensureMerchantExists(mid); err != nil {
			return err
		}
	}
	if err := db.Where("product_id = ?", productID).Delete(&model.ProductApplicableMerchant{}).Error; err != nil {
		return err
	}
	rows := make([]model.ProductApplicableMerchant, 0, len(ids))
	for _, mid := range ids {
		rows = append(rows, model.ProductApplicableMerchant{ProductID: productID, MerchantID: mid})
	}
	return db.Create(&rows).Error
}

func (s *ProductService) ListApplicableMerchantIDs(productID uint64) ([]uint64, error) {
	var rows []model.ProductApplicableMerchant
	if err := s.DB.Where("product_id = ?", productID).Order("merchant_id ASC").Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]uint64, 0, len(rows))
	for i := range rows {
		out = append(out, rows[i].MerchantID)
	}
	return out, nil
}

func (s *ProductService) AssertMerchantApplicable(productID, merchantID uint64) error {
	var product model.Product
	if err := query.NotDeleted(s.DB).Select("merchant_id").First(&product, productID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrProductNotFound
		}
		return err
	}
	if product.MerchantID == merchantID {
		return nil
	}
	var count int64
	if err := s.DB.Model(&model.ProductApplicableMerchant{}).
		Where("product_id = ? AND merchant_id = ?", productID, merchantID).
		Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		return ErrProductForbidden
	}
	return nil
}

func (s *ProductService) loadApplicableMerchantBriefs(productIDs []uint64) (map[uint64][]ApplicableMerchantBrief, map[uint64][]uint64, error) {
	briefs := make(map[uint64][]ApplicableMerchantBrief)
	idsMap := make(map[uint64][]uint64)
	if len(productIDs) == 0 {
		return briefs, idsMap, nil
	}
	var rows []model.ProductApplicableMerchant
	if err := s.DB.Where("product_id IN ?", productIDs).Order("merchant_id ASC").Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	merchantIDs := make([]uint64, 0, len(rows))
	for i := range rows {
		merchantIDs = append(merchantIDs, rows[i].MerchantID)
	}
	shopNames := make(map[uint64]string)
	if len(merchantIDs) > 0 {
		var merchants []model.MerchantProfile
		if err := query.NotDeleted(s.DB).Select("id", "shop_name").
			Where("id IN ?", merchantIDs).Find(&merchants).Error; err != nil {
			return nil, nil, err
		}
		for i := range merchants {
			shopNames[merchants[i].ID] = merchants[i].ShopName
		}
	}
	for i := range rows {
		pid := rows[i].ProductID
		mid := rows[i].MerchantID
		idsMap[pid] = append(idsMap[pid], mid)
		briefs[pid] = append(briefs[pid], ApplicableMerchantBrief{
			ID:       mid,
			ShopName: shopNames[mid],
		})
	}
	return briefs, idsMap, nil
}

func applyApplicableMerchantsToStoreView(view *ProductStoreView, ids []uint64, briefs []ApplicableMerchantBrief) {
	if view == nil {
		return
	}
	view.ApplicableMerchantIDs = ids
	view.ApplicableMerchants = briefs
}

func merchantShelfProductScope(q *gorm.DB, merchantID uint64) *gorm.DB {
	return q.Where(
		"merchant_id = ? OR id IN (SELECT product_id FROM product_applicable_merchant WHERE merchant_id = ?)",
		merchantID, merchantID,
	)
}

func (s *ProductService) assertOwnerScope(product *model.Product, scopeMerchantID *uint64) error {
	if scopeMerchantID == nil {
		return nil
	}
	if product.MerchantID != *scopeMerchantID {
		return ErrProductForbidden
	}
	return nil
}
