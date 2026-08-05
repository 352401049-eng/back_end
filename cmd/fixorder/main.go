package main

import (
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	"yujixinjiang/backend/internal/config"
	"yujixinjiang/backend/internal/database"
	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/payment"
	"yujixinjiang/backend/internal/query"
	"yujixinjiang/backend/internal/service"
)

// 一次性运维：取消并退款指定订单（走正常 Cancel 链路含微信退款）。
// 用法: ORDER_ID=65 ./fixorder
func main() {
	orderID := uint64(65)
	if v := os.Getenv("ORDER_ID"); v != "" {
		n, err := strconv.ParseUint(v, 10, 64)
		if err != nil || n == 0 {
			log.Fatalf("invalid ORDER_ID=%q", v)
		}
		orderID = n
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	db, err := database.Connect(cfg.DB)
	if err != nil {
		log.Fatalf("db: %v", err)
	}
	payProvider, err := payment.NewProvider(cfg, db)
	if err != nil {
		log.Fatalf("payment: %v", err)
	}

	activitySvc := &service.ActivityService{DB: db}
	inventorySvc := &service.InventoryService{DB: db}
	couponSvc := &service.CouponService{DB: db}
	orderSvc := &service.OrderService{
		DB: db, InventorySvc: inventorySvc, CouponSvc: couponSvc,
		ActivitySvc: activitySvc, Payment: payProvider,
		PayTimeoutMinutes: cfg.Payment.PayTimeoutMinutes,
		AvatarPublicBase:  cfg.Upload.AvatarPublicBase,
	}

	var order model.Order
	if err := query.NotDeleted(db).First(&order, orderID).Error; err != nil {
		log.Fatalf("load order %d: %v", orderID, err)
	}
	fmt.Printf("cancel order id=%d account=%d status=%d pay_status=%d amount=%.2f\n",
		order.ID, order.AccountID, order.Status, order.PayStatus, order.PayAmount)

	if err := orderSvc.Cancel(order.AccountID, order.ID); err != nil {
		log.Fatalf("cancel failed: %v", err)
	}
	fmt.Println("cancel ok, waiting refund dispatch...")
	time.Sleep(3 * time.Second)

	_ = query.NotDeleted(db).First(&order, orderID).Error
	fmt.Printf("after: status=%d pay_status=%d refunded=%.2f pending_refund=%.2f\n",
		order.Status, order.PayStatus, order.RefundedAmount, order.RefundPendingAmount)

	var ap model.ActivityProduct
	if err := query.NotDeleted(db).First(&ap, 2).Error; err == nil {
		fmt.Printf("activity_product#2 platform_daily_sold=%d/%d bucket=%s\n",
			ap.PlatformDailySold, ap.PlatformDailyMax, ap.PlatformDailyBucket)
	}

	var team model.GroupBuyTeam
	if err := query.NotDeleted(db).First(&team, 7).Error; err == nil {
		fmt.Printf("team#7 status=%d current_count=%d\n", team.Status, team.CurrentCount)
	}
}
