package service

import (
	"errors"
	"testing"

	"yujixinjiang/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupTakeoutUsageMerchantTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&model.Account{},
		&model.MerchantProfile{},
		&model.ProductCategory{},
		&model.Product{},
		&model.ProductApplicableMerchant{},
		&model.UserAddress{},
		&model.TakeoutOrder{},
		&model.TakeoutOrderItem{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

func seedTakeoutAccount(db *gorm.DB, id uint64) {
	phone := "13800000001"
	if err := db.Create(&model.Account{ID: id, Phone: &phone}).Error; err != nil {
		panic(err)
	}
}

func seedTakeoutAddress(db *gorm.DB, accountID, id uint64) {
	if err := db.Create(&model.UserAddress{
		ID: id, AccountID: accountID, ContactName: "测试", ContactPhone: "13800000001",
		Province: "河南省", City: "郑州市", District: "金水区", Detail: "测试路1号",
	}).Error; err != nil {
		panic(err)
	}
}

func seedTakeoutProduct(db *gorm.DB, merchantID uint64) *model.Product {
	cat := model.ProductCategory{MerchantID: merchantID, Name: "默认"}
	if err := db.Create(&cat).Error; err != nil {
		panic(err)
	}
	price := 12.0
	p := model.Product{
		MerchantID: merchantID, CategoryID: cat.ID, Name: "外卖商品",
		CoverURL: "https://example.com/c.jpg", Images: []string{"https://example.com/c.jpg"},
		Price: price, OriginalPrice: &price,
		EnableTakeout: 1, AllowDelivery: 1, TakeoutStock: 10,
		Status: model.ProductStatusOn, ItemType: model.ProductItemTypePhysical,
	}
	if err := db.Create(&p).Error; err != nil {
		panic(err)
	}
	return &p
}

func TestCreateTakeoutRequiresUsageMerchantID(t *testing.T) {
	db := setupTakeoutUsageMerchantTestDB(t)
	seedTakeoutAccount(db, 100)
	seedOpenMerchant(db, 1, "店A")
	p := seedTakeoutProduct(db, 1)
	seedTakeoutAddress(db, 100, 5)

	svc := &TakeoutService{DB: db}
	_, err := svc.Create(100, CreateTakeoutInput{
		MerchantID: 1, ProductID: p.ID, Quantity: 1, AddressID: 5,
	})
	if err == nil {
		t.Fatal("expected error without usage_merchant_id")
	}
	if !errors.Is(err, ErrInvalidProductArg) {
		t.Fatalf("expected ErrInvalidProductArg, got %v", err)
	}
}

func TestCreateTakeoutRejectsNonApplicableUsageMerchant(t *testing.T) {
	db := setupTakeoutUsageMerchantTestDB(t)
	seedTakeoutAccount(db, 100)
	seedOpenMerchant(db, 1, "店A")
	seedOpenMerchant(db, 2, "店B")
	seedOpenMerchant(db, 3, "店C")
	p := seedTakeoutProduct(db, 1)
	productSvc := &ProductService{DB: db}
	if err := productSvc.ReplaceApplicableMerchants(nil, p.ID, 1, []uint64{1, 2}); err != nil {
		t.Fatalf("replace applicable: %v", err)
	}
	seedTakeoutAddress(db, 100, 5)

	svc := &TakeoutService{DB: db}
	_, err := svc.Create(100, CreateTakeoutInput{
		MerchantID: 1, ProductID: p.ID, Quantity: 1, AddressID: 5, UsageMerchantID: 3,
	})
	if err == nil {
		t.Fatal("expected error for non-applicable merchant 3")
	}
	if !errors.Is(err, ErrProductForbidden) {
		t.Fatalf("expected ErrProductForbidden, got %v", err)
	}
}

func TestListForMerchantFiltersByUsageMerchantID(t *testing.T) {
	db := setupTakeoutUsageMerchantTestDB(t)
	seedOpenMerchant(db, 1, "店A")
	seedOpenMerchant(db, 2, "店B")

	for _, row := range []model.TakeoutOrder{
		{OrderNo: "T001", AccountID: 100, MerchantID: 1, UsageMerchantID: 1, Status: model.TakeoutStatusPreparing, GoodsAmount: 10, PayAmount: 10},
		{OrderNo: "T002", AccountID: 100, MerchantID: 1, UsageMerchantID: 2, Status: model.TakeoutStatusPreparing, GoodsAmount: 10, PayAmount: 10},
		{OrderNo: "T003", AccountID: 100, MerchantID: 1, UsageMerchantID: 2, Status: model.TakeoutStatusCompleted, GoodsAmount: 10, PayAmount: 10},
	} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatalf("seed takeout: %v", err)
		}
	}

	svc := &TakeoutService{DB: db}
	list, total, err := svc.ListForMerchant(2, 1, 10, "preparing")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("want 1 preparing for usage merchant 2, got total=%d len=%d", total, len(list))
	}
	if list[0].OrderNo != "T002" {
		t.Fatalf("unexpected order %q", list[0].OrderNo)
	}
}
