package service

import (
	"errors"
	"testing"
	"time"

	"yujixinjiang/backend/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupOrderApplicableTestDB(t *testing.T) *gorm.DB {
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
		&model.Activity{},
		&model.ActivityProduct{},
		&model.Order{},
		&model.OrderItem{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return db
}

type orderApplicableFixture struct {
	product *model.Product
	act     *model.Activity
	ap      *model.ActivityProduct
}

func seedOrderApplicableFixture(t *testing.T, db *gorm.DB) orderApplicableFixture {
	t.Helper()
	phone := "13800000099"
	if err := db.Create(&model.Account{ID: 100, Phone: &phone}).Error; err != nil {
		t.Fatalf("account: %v", err)
	}
	seedOpenMerchant(db, 1, "店A")
	seedOpenMerchant(db, 2, "店B")
	seedOpenMerchant(db, 3, "店C")
	p := seedOnShelfProduct(db, 1, "跨店商品")
	productSvc := &ProductService{DB: db}
	if err := productSvc.ReplaceApplicableMerchants(nil, p.ID, 1, []uint64{1, 2}); err != nil {
		t.Fatalf("replace applicable: %v", err)
	}
	now := time.Now()
	act := model.Activity{
		MerchantID:   0,
		Name:         "平台活动",
		StartAt:      now.Add(-time.Hour),
		EndAt:        now.Add(time.Hour),
		Status:       model.ActivityStatusOn,
		EnableCoupon: 1,
	}
	if err := db.Create(&act).Error; err != nil {
		t.Fatalf("activity: %v", err)
	}
	ap := model.ActivityProduct{
		ActivityID:    act.ID,
		ProductID:     p.ID,
		ActivityPrice: 8,
		ActivityStock: 20,
		Status:        1,
		EnableCoupon:  1,
	}
	if err := db.Create(&ap).Error; err != nil {
		t.Fatalf("activity product: %v", err)
	}
	return orderApplicableFixture{product: p, act: &act, ap: &ap}
}

func TestResolveForOrderApplicableEntryMerchant(t *testing.T) {
	db := setupOrderApplicableTestDB(t)
	fx := seedOrderApplicableFixture(t, db)
	actSvc := &ActivityService{DB: db}

	ctx, err := actSvc.ResolveForOrder(100, fx.ap.ID, 2, 1, model.PurchaseTypeSolo)
	if err != nil {
		t.Fatalf("resolve as applicable shop B: %v", err)
	}
	if ctx.Product.MerchantID != 1 {
		t.Fatalf("product owner want 1, got %d", ctx.Product.MerchantID)
	}

	_, err = actSvc.ResolveForOrder(100, fx.ap.ID, 3, 1, model.PurchaseTypeSolo)
	if err == nil {
		t.Fatal("expected error for non-applicable shop C")
	}
	if !errors.Is(err, ErrActivityForbidden) {
		t.Fatalf("expected activity forbidden, got %v", err)
	}
}

func TestOrderCreateApplicableShopMerchantIsOwner(t *testing.T) {
	db := setupOrderApplicableTestDB(t)
	fx := seedOrderApplicableFixture(t, db)
	orderSvc := &OrderService{
		DB:          db,
		ActivitySvc: &ActivityService{DB: db},
	}

	view, err := orderSvc.Create(100, CreateOrderInput{
		ActivityProductID: &fx.ap.ID,
		MerchantID:      2,
		Quantity:          1,
		DeliveryType:      model.DeliveryTypePickup,
	})
	if err != nil {
		t.Fatalf("create from applicable shop B: %v", err)
	}
	if view.MerchantID != 1 {
		t.Fatalf("order.merchant_id want owner 1, got %d", view.MerchantID)
	}

	var order model.Order
	if err := db.First(&order, view.ID).Error; err != nil {
		t.Fatalf("reload order: %v", err)
	}
	if order.MerchantID != 1 {
		t.Fatalf("persisted merchant_id want 1, got %d", order.MerchantID)
	}
}

func TestOrderCreateNonActivityApplicableShop(t *testing.T) {
	db := setupOrderApplicableTestDB(t)
	fx := seedOrderApplicableFixture(t, db)
	orderSvc := &OrderService{DB: db}

	view, err := orderSvc.Create(100, CreateOrderInput{
		ProductID:    fx.product.ID,
		MerchantID:   2,
		Quantity:     1,
		DeliveryType: model.DeliveryTypePickup,
	})
	if err != nil {
		t.Fatalf("create non-activity from shop B: %v", err)
	}
	if view.MerchantID != 1 {
		t.Fatalf("order.merchant_id want owner 1, got %d", view.MerchantID)
	}
}
