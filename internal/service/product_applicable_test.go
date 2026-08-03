package service

import (
	"errors"
	"testing"

	"yujixinjiang/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupApplicableProductTestDB(t *testing.T) *gorm.DB {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&model.MerchantProfile{},
		&model.ProductCategory{},
		&model.Product{},
		&model.ProductApplicableMerchant{},
		&model.GroupBuy{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func seedOpenMerchant(db *gorm.DB, id uint64, shopName string) {
	m := model.MerchantProfile{
		ID:        id,
		AccountID: id,
		ShopName:  shopName,
		Status:    model.MerchantStatusOpen,
	}
	if err := db.Create(&m).Error; err != nil {
		panic(err)
	}
}

func seedOnShelfProduct(db *gorm.DB, merchantID uint64, name string) *model.Product {
	cat := model.ProductCategory{MerchantID: merchantID, Name: "默认"}
	if err := db.Create(&cat).Error; err != nil {
		panic(err)
	}
	p := model.Product{
		MerchantID: merchantID,
		CategoryID: cat.ID,
		Name:       name,
		CoverURL:   "https://example.com/cover.jpg",
		Images:     []string{"https://example.com/cover.jpg"},
		Price:      10,
		Stock:      0,
		EnableDeal: 1,
		DealStock:  5,
		Status:     model.ProductStatusOn,
		ItemType:   model.ProductItemTypePhysical,
	}
	if err := db.Create(&p).Error; err != nil {
		panic(err)
	}
	return &p
}

func TestReplaceApplicableRequiresOwner(t *testing.T) {
	db := setupApplicableProductTestDB(t)
	seedOpenMerchant(db, 1, "店A")
	seedOpenMerchant(db, 2, "店B")
	p := seedOnShelfProduct(db, 2, "共享商品")

	svc := &ProductService{DB: db}
	err := svc.ReplaceApplicableMerchants(nil, p.ID, 1, []uint64{2})
	if err == nil {
		t.Fatal("expected error when ownerID does not match product owner")
	}
	if !errors.Is(err, ErrProductForbidden) {
		t.Fatalf("expected product forbidden, got %v", err)
	}
}

func TestListOnShelfIncludesApplicable(t *testing.T) {
	db := setupApplicableProductTestDB(t)
	seedOpenMerchant(db, 1, "店A")
	seedOpenMerchant(db, 2, "店B")
	p := seedOnShelfProduct(db, 1, "跨店商品")

	svc := &ProductService{DB: db}
	if err := svc.ReplaceApplicableMerchants(nil, p.ID, 1, []uint64{1, 2}); err != nil {
		t.Fatalf("replace applicable: %v", err)
	}

	list, total, err := svc.ListOnShelfByMerchant(2, 1, 10, ProductListFilter{})
	if err != nil {
		t.Fatalf("list on shelf: %v", err)
	}
	if total != 1 || len(list) != 1 || list[0].ID != p.ID {
		t.Fatalf("want product %d in merchant 2 list, got total=%d len=%d", p.ID, total, len(list))
	}
}

func TestReplaceNormalizesOwnerIntoApplicable(t *testing.T) {
	db := setupApplicableProductTestDB(t)
	seedOpenMerchant(db, 1, "店A")
	seedOpenMerchant(db, 2, "店B")
	p := seedOnShelfProduct(db, 1, "跨店商品")

	svc := &ProductService{DB: db}
	if err := svc.ReplaceApplicableMerchants(nil, p.ID, 1, []uint64{2}); err != nil {
		t.Fatalf("replace applicable: %v", err)
	}

	ids, err := svc.ListApplicableMerchantIDs(p.ID)
	if err != nil {
		t.Fatalf("list applicable: %v", err)
	}
	if len(ids) != 2 || ids[0] != 1 || ids[1] != 2 {
		t.Fatalf("want applicable [1 2], got %v", ids)
	}
}

func TestGetOnShelfAllowsApplicableMerchant(t *testing.T) {
	db := setupApplicableProductTestDB(t)
	seedOpenMerchant(db, 1, "店A")
	seedOpenMerchant(db, 2, "店B")
	p := seedOnShelfProduct(db, 1, "跨店商品")

	svc := &ProductService{DB: db}
	if err := svc.ReplaceApplicableMerchants(nil, p.ID, 1, []uint64{1, 2}); err != nil {
		t.Fatalf("replace applicable: %v", err)
	}

	got, err := svc.GetOnShelf(p.ID, 2)
	if err != nil {
		t.Fatalf("get on shelf: %v", err)
	}
	if got.ID != p.ID {
		t.Fatalf("got product id %d, want %d", got.ID, p.ID)
	}
}

func TestGetByIDAllowsApplicableMerchant(t *testing.T) {
	db := setupApplicableProductTestDB(t)
	seedOpenMerchant(db, 1, "店A")
	seedOpenMerchant(db, 2, "店B")
	p := seedOnShelfProduct(db, 1, "跨店商品")

	svc := &ProductService{DB: db}
	if err := svc.ReplaceApplicableMerchants(nil, p.ID, 1, []uint64{1, 2}); err != nil {
		t.Fatalf("replace applicable: %v", err)
	}

	scope := uint64(2)
	got, err := svc.GetByID(p.ID, &scope)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got.ID != p.ID {
		t.Fatalf("got product id %d, want %d", got.ID, p.ID)
	}
}

func TestUpdateStatusForbiddenForApplicableOnly(t *testing.T) {
	db := setupApplicableProductTestDB(t)
	seedOpenMerchant(db, 1, "店A")
	seedOpenMerchant(db, 2, "店B")
	p := seedOnShelfProduct(db, 1, "跨店商品")

	svc := &ProductService{DB: db}
	if err := svc.ReplaceApplicableMerchants(nil, p.ID, 1, []uint64{1, 2}); err != nil {
		t.Fatalf("replace applicable: %v", err)
	}

	scope := uint64(2)
	_, err := svc.UpdateStatus(p.ID, model.ProductStatusOff, &scope)
	if err == nil {
		t.Fatal("expected forbidden when applicable-only merchant mutates status")
	}
	if !errors.Is(err, ErrProductForbidden) {
		t.Fatalf("expected product forbidden, got %v", err)
	}
}

func TestMerchantScopedUpdateIgnoresApplicableMerchantIDs(t *testing.T) {
	db := setupApplicableProductTestDB(t)
	seedOpenMerchant(db, 1, "店A")
	seedOpenMerchant(db, 2, "店B")
	p := seedOnShelfProduct(db, 1, "商品")

	svc := &ProductService{DB: db}
	if err := svc.ReplaceApplicableMerchants(nil, p.ID, 1, []uint64{1}); err != nil {
		t.Fatalf("replace applicable: %v", err)
	}

	scope := uint64(1)
	input := ProductInput{
		Name:                     p.Name,
		CoverURL:                 p.CoverURL,
		Images:                   p.Images,
		Price:                    p.Price,
		CategoryID:               p.CategoryID,
		Status:                   p.Status,
		EnableDeal:               p.EnableDeal,
		DealStock:                p.DealStock,
		ItemType:                 p.ItemType,
		HasApplicableMerchantIDs: true,
		ApplicableMerchantIDs:    []uint64{1, 2},
	}
	if _, err := svc.Update(p.ID, input, &scope); err != nil {
		t.Fatalf("update: %v", err)
	}

	ids, err := svc.ListApplicableMerchantIDs(p.ID)
	if err != nil {
		t.Fatalf("list applicable: %v", err)
	}
	if len(ids) != 1 || ids[0] != 1 {
		t.Fatalf("want applicable [1] only, got %v", ids)
	}
}

func TestUpdateApplicableMerchantIDsViaReplace(t *testing.T) {
	db := setupApplicableProductTestDB(t)
	seedOpenMerchant(db, 1, "店A")
	seedOpenMerchant(db, 2, "店B")
	seedOpenMerchant(db, 3, "店C")
	p := seedOnShelfProduct(db, 1, "跨店商品")

	svc := &ProductService{DB: db}
	if err := svc.ReplaceApplicableMerchants(nil, p.ID, 1, []uint64{1, 2}); err != nil {
		t.Fatalf("replace applicable: %v", err)
	}
	if err := svc.ReplaceApplicableMerchants(nil, p.ID, 1, []uint64{1, 3}); err != nil {
		t.Fatalf("replace applicable patch: %v", err)
	}

	ids, err := svc.ListApplicableMerchantIDs(p.ID)
	if err != nil {
		t.Fatalf("list applicable: %v", err)
	}
	if len(ids) != 2 || ids[0] != 1 || ids[1] != 3 {
		t.Fatalf("want applicable [1 3], got %v", ids)
	}
}
