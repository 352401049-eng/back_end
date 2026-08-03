package service

import (
	"errors"
	"testing"

	"yujixinjiang/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupUseBatchUsageMerchantTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&model.MerchantProfile{},
		&model.ProductCategory{},
		&model.Product{},
		&model.ProductApplicableMerchant{},
		&model.UserInventory{},
		&model.UserAddress{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func seedBagInventory(db *gorm.DB, accountID uint64, product *model.Product) *model.UserInventory {
	inv := model.UserInventory{AccountID: accountID, ProductID: product.ID, Quantity: 2}
	if err := db.Create(&inv).Error; err != nil {
		panic(err)
	}
	return &inv
}

func seedDeliverableProduct(db *gorm.DB, merchantID uint64) *model.Product {
	cat := model.ProductCategory{MerchantID: merchantID, Name: "默认"}
	if err := db.Create(&cat).Error; err != nil {
		panic(err)
	}
	p := model.Product{
		MerchantID: merchantID, CategoryID: cat.ID, Name: "背包商品",
		CoverURL: "https://example.com/c.jpg", Images: []string{"https://example.com/c.jpg"},
		Price: 10, EnableDeal: 1, DealStock: 5,
		Status: model.ProductStatusOn, ItemType: model.ProductItemTypePhysical,
		AllowDelivery: 1,
	}
	if err := db.Create(&p).Error; err != nil {
		panic(err)
	}
	return &p
}

func TestUseBatchDeliveryRequiresUsageMerchantID(t *testing.T) {
	db := setupUseBatchUsageMerchantTestDB(t)
	seedOpenMerchant(db, 1, "店A")
	p := seedDeliverableProduct(db, 1)
	inv := seedBagInventory(db, 100, p)
	addrID := uint64(5)
	seedTakeoutAddress(db, 100, addrID)

	svc := &InventoryService{DB: db}
	_, err := svc.UseBatch(100, UseBatchInput{
		Items:        []UseBatchItemInput{{InventoryID: inv.ID, Quantity: 1}},
		DeliveryType: model.DeliveryTypeDelivery,
		AddressID:    &addrID,
	})
	if err == nil {
		t.Fatal("expected error without usage_merchant_id")
	}
	if !errors.Is(err, ErrInvalidProductArg) {
		t.Fatalf("expected ErrInvalidProductArg, got %v", err)
	}
}

func TestUseBatchDeliveryRejectsNonApplicableUsageMerchant(t *testing.T) {
	db := setupUseBatchUsageMerchantTestDB(t)
	seedOpenMerchant(db, 1, "店A")
	seedOpenMerchant(db, 2, "店B")
	seedOpenMerchant(db, 3, "店C")
	p := seedDeliverableProduct(db, 1)
	productSvc := &ProductService{DB: db}
	if err := productSvc.ReplaceApplicableMerchants(nil, p.ID, 1, []uint64{1, 2}); err != nil {
		t.Fatalf("replace applicable: %v", err)
	}
	inv := seedBagInventory(db, 100, p)
	addrID := uint64(5)
	seedTakeoutAddress(db, 100, addrID)

	svc := &InventoryService{DB: db}
	_, err := svc.UseBatch(100, UseBatchInput{
		Items:           []UseBatchItemInput{{InventoryID: inv.ID, Quantity: 1}},
		DeliveryType:    model.DeliveryTypeDelivery,
		AddressID:       &addrID,
		UsageMerchantID: 3,
	})
	if err == nil {
		t.Fatal("expected error for non-applicable merchant 3")
	}
	if !errors.Is(err, ErrProductForbidden) {
		t.Fatalf("expected ErrProductForbidden, got %v", err)
	}
}
