package service

import (
	"testing"
	"time"

	"yujixinjiang/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupDashboardSalesTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(
		&model.MerchantProfile{},
		&model.ProductCategory{},
		&model.Product{},
		&model.UserInventory{},
		&model.UserInventoryUsage{},
		&model.TakeoutOrder{},
		&model.TakeoutOrderItem{},
		&model.VerificationCode{},
		&model.VerificationRecord{},
		&model.OrderItem{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

// seedSharedProductSalesFixture: product owned by merchant 1, fulfilled at merchant 2.
func seedSharedProductSalesFixture(t *testing.T, db *gorm.DB) {
	t.Helper()
	seedOpenMerchant(db, 1, "店A")
	seedOpenMerchant(db, 2, "店B")
	p := seedOnShelfProduct(db, 1, "跨店商品")

	inv := model.UserInventory{AccountID: 100, ProductID: p.ID, Quantity: 1}
	if err := db.Create(&inv).Error; err != nil {
		t.Fatalf("create inventory: %v", err)
	}
	usageAtB := model.UserInventoryUsage{
		AccountID:       100,
		InventoryID:     inv.ID,
		ProductID:       p.ID,
		MerchantID:      1,
		UsageMerchantID: 2,
		Quantity:        1,
		DeliveryType:    model.DeliveryTypePickup,
		Status:          model.InventoryUsageCompleted,
	}
	if err := db.Create(&usageAtB).Error; err != nil {
		t.Fatalf("create usage at B: %v", err)
	}
	vc := model.VerificationCode{
		InventoryUsageID: &usageAtB.ID,
		AccountID:        100,
		Code:             "VERIFY-B-001",
		Status:           model.VerificationCodeUsed,
	}
	if err := db.Create(&vc).Error; err != nil {
		t.Fatalf("create verification code: %v", err)
	}
	now := time.Now()
	if err := db.Create(&model.VerificationRecord{
		VerificationCodeID: vc.ID,
		OrderID:          0,
		MerchantID:       2,
		OperatorID:       200,
		VerifiedAt:       now,
	}).Error; err != nil {
		t.Fatalf("create verification record: %v", err)
	}

	takeout := model.TakeoutOrder{
		OrderNo:         "TO-SHARED-001",
		AccountID:       100,
		MerchantID:      1,
		UsageMerchantID: 2,
		Status:          model.TakeoutStatusCompleted,
		GoodsAmount:     20,
		PayAmount:       20,
		PayStatus:       model.PayStatusPaid,
	}
	if err := db.Create(&takeout).Error; err != nil {
		t.Fatalf("create takeout: %v", err)
	}
	if err := db.Create(&model.TakeoutOrderItem{
		TakeoutOrderID: takeout.ID,
		ProductID:      p.ID,
		ProductName:    p.Name,
		UnitPrice:      20,
		Quantity:       1,
		Subtotal:       20,
	}).Error; err != nil {
		t.Fatalf("create takeout item: %v", err)
	}
}

func TestSalesReportAttributesToUsageMerchant(t *testing.T) {
	db := setupDashboardSalesTestDB(t)
	seedSharedProductSalesFixture(t, db)
	svc := &DashboardService{DB: db}

	midB := uint64(2)
	reportB, err := svc.SalesReport(SalesReportFilter{MerchantID: &midB})
	if err != nil {
		t.Fatalf("sales report merchant B: %v", err)
	}
	if reportB.CompletedItemCount != 2 {
		t.Fatalf("merchant B completed items want 2 (bag+takeout), got %d", reportB.CompletedItemCount)
	}
	if reportB.CompletedSalesAmount != 30 {
		t.Fatalf("merchant B sales want 30 (10 bag + 20 takeout), got %v", reportB.CompletedSalesAmount)
	}
	if reportB.VerificationCount != 1 {
		t.Fatalf("merchant B verification count want 1, got %d", reportB.VerificationCount)
	}

	midA := uint64(1)
	reportA, err := svc.SalesReport(SalesReportFilter{MerchantID: &midA})
	if err != nil {
		t.Fatalf("sales report merchant A: %v", err)
	}
	if reportA.CompletedItemCount != 0 {
		t.Fatalf("merchant A completed items want 0 (fulfilled at B), got %d", reportA.CompletedItemCount)
	}
	if reportA.CompletedSalesAmount != 0 {
		t.Fatalf("merchant A sales want 0, got %v", reportA.CompletedSalesAmount)
	}
	if reportA.VerificationCount != 0 {
		t.Fatalf("merchant A verification count want 0, got %d", reportA.VerificationCount)
	}
}

func TestTopProductsAttributesToUsageMerchant(t *testing.T) {
	db := setupDashboardSalesTestDB(t)
	seedSharedProductSalesFixture(t, db)
	svc := &DashboardService{DB: db}

	midB := uint64(2)
	topB, err := svc.topProducts(&midB, 10)
	if err != nil {
		t.Fatalf("top products merchant B: %v", err)
	}
	if len(topB) != 1 || topB[0].SalesCount != 2 {
		t.Fatalf("merchant B top products want 1 product with count 2, got %+v", topB)
	}

	midA := uint64(1)
	topA, err := svc.topProducts(&midA, 10)
	if err != nil {
		t.Fatalf("top products merchant A: %v", err)
	}
	if len(topA) != 0 {
		t.Fatalf("merchant A top products want empty, got %+v", topA)
	}
}
