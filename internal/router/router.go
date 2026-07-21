package router

import (
	"log"
	"os"
	"time"

	"yujixinjiang/backend/internal/config"
	"yujixinjiang/backend/internal/handler"
	"yujixinjiang/backend/internal/middleware"
	"yujixinjiang/backend/internal/model"
	"yujixinjiang/backend/internal/payment"
	"yujixinjiang/backend/internal/service"
	"yujixinjiang/backend/internal/storage"
	"yujixinjiang/backend/internal/wechat"

	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

func Setup(cfg *config.Config, db *gorm.DB) *gin.Engine {
	if os.Getenv("GIN_MODE") == "release" {
		gin.SetMode(gin.ReleaseMode)
	}

	r := gin.Default()
	r.Use(middleware.CORS())

	uploadStore := storage.NewLocal(cfg.Upload)
	if err := uploadStore.EnsureDir(); err != nil {
		panic("创建上传目录失败: " + err.Error())
	}
	r.Static(cfg.Upload.URLPrefix, cfg.Upload.Dir)

	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	health := &handler.HealthHandler{DB: db}

	var wechatClient *wechat.Client
	if cfg.WeChat.AppID != "" && cfg.WeChat.Secret != "" {
		wechatClient = wechat.NewClient(cfg.WeChat.AppID, cfg.WeChat.Secret)
	}
	authHandler := &handler.AuthHandler{Svc: &service.AuthService{
		DB:               db,
		JWTSecret:        cfg.JWT.Secret,
		WeChat:           wechatClient,
		AvatarPublicBase: cfg.Upload.AvatarPublicBase,
	}}

	cartSvc := &service.CartService{DB: db}
	deliveryZoneSvc := &service.DeliveryZoneService{DB: db}
	inventorySvc := &service.InventoryService{DB: db, ZoneSvc: deliveryZoneSvc}
	couponSvc := &service.CouponService{DB: db}
	activitySvc := &service.ActivityService{DB: db}
	announcementSvc := &service.AnnouncementService{DB: db}
	payProvider := payment.NewProvider(cfg, db)
	log.Printf("支付渠道: %s (immediate_settle=%v)", payProvider.Name(), payProvider.ImmediateSettle())
	orderSvc := &service.OrderService{
		DB: db, InventorySvc: inventorySvc, CouponSvc: couponSvc,
		ActivitySvc: activitySvc, ZoneSvc: deliveryZoneSvc, Payment: payProvider,
	}
	startGroupExpireWorker(orderSvc)
	verifySvc := &service.VerificationService{DB: db, InventorySvc: inventorySvc}
	paymentHandler := &handler.PaymentHandler{OrderSvc: orderSvc}
	deliverySvc := &service.DeliveryService{DB: db}
	dashboardSvc := &service.DashboardService{DB: db}
	categorySvc := &service.CategoryService{DB: db}
	userSvc := &service.UserService{DB: db}

	userHandler := &handler.UserHandler{
		Svc:          userSvc,
		RiderSvc:     &service.RiderApplicationService{DB: db},
		AddressSvc:   &service.AddressService{DB: db},
		CartSvc:      cartSvc,
		OrderSvc:     orderSvc,
		InventorySvc: inventorySvc,
		DeliverySvc:  deliverySvc,
	}
	merchantSvc := &service.MerchantService{DB: db}
	productSvc := &service.ProductService{DB: db, CategorySvc: categorySvc}
	riderSvc := &service.RiderApplicationService{DB: db}
	adminHandler := &handler.AdminHandler{MerchantSvc: merchantSvc, ProductSvc: productSvc, RiderSvc: riderSvc}
	merchantHandler := &handler.MerchantHandler{MerchantSvc: merchantSvc, ProductSvc: productSvc, CategorySvc: categorySvc}
	storeHandler := &handler.StoreHandler{
		MerchantSvc: merchantSvc, ProductSvc: productSvc, OrderSvc: orderSvc,
		CategorySvc: categorySvc, CouponSvc: couponSvc, ActivitySvc: activitySvc,
		ZoneSvc: deliveryZoneSvc,
	}
	categoryHandler := &handler.CategoryHandler{CategorySvc: categorySvc}
	merchantOrderHandler := &handler.MerchantOrderHandler{
		MerchantSvc: merchantSvc, OrderSvc: orderSvc, VerifySvc: verifySvc,
		InventorySvc: inventorySvc, DashboardSvc: dashboardSvc,
	}
	riderHandler := &handler.RiderHandler{DeliverySvc: deliverySvc}
	adminDashboardHandler := &handler.AdminDashboardHandler{
		DashboardSvc: dashboardSvc, OrderSvc: orderSvc, VerifySvc: verifySvc,
	}
	uploadHandler := &handler.UploadHandler{Store: uploadStore}
	couponHandler := &handler.CouponHandler{
		CouponSvc: couponSvc, MerchantSvc: merchantSvc, ProductSvc: productSvc,
	}
	adminExtraHandler := &handler.AdminExtraHandler{
		UserSvc: userSvc, CategorySvc: categorySvc,
		DeliverySvc: deliverySvc, InventorySvc: inventorySvc,
	}
	announcementHandler := &handler.AnnouncementHandler{
		AnnouncementSvc: announcementSvc, MerchantSvc: merchantSvc,
	}
	activityHandler := &handler.ActivityHandler{
		ActivitySvc: activitySvc, MerchantSvc: merchantSvc,
	}
	rankHandler := &handler.RankHandler{
		RankSvc: &service.RankService{DB: db},
	}
	deliveryZoneHandler := &handler.DeliveryZoneHandler{
		ZoneSvc: deliveryZoneSvc, MerchantSvc: merchantSvc,
	}

	r.GET("/api/health", health.Check)

	public := r.Group("/api")
	{
		public.POST("/payments/wechat/notify", paymentHandler.WeChatNotify)
		public.POST("/auth/login", authHandler.Login)
		public.GET("/categories", categoryHandler.ListCategories)
		public.GET("/products", storeHandler.ListProductsByMerchant)
		public.GET("/products/:id/group", middleware.OptionalAuth(cfg.JWT.Secret, db), storeHandler.GetGroupProgress)
		public.GET("/products/:id", storeHandler.GetProduct)

		public.GET("/announcements", announcementHandler.ListPublic)
		public.GET("/seckill/products", middleware.OptionalAuth(cfg.JWT.Secret, db), activityHandler.ListSeckillProducts)
		public.GET("/rank/hot-groups", rankHandler.ListHotGroups)
		public.GET("/rank/hot-sales", rankHandler.ListHotSales)
		public.GET("/rank/save", rankHandler.ListSaveRank)
		public.GET("/activities/:id", activityHandler.GetPublic)
		public.GET("/activities/:id/products", activityHandler.ListPublicProducts)
		public.GET("/activities/:id/products/:activity_product_id", middleware.OptionalAuth(cfg.JWT.Secret, db), activityHandler.GetPublicProduct)
		public.GET("/activities/:id/products/:activity_product_id/group", middleware.OptionalAuth(cfg.JWT.Secret, db), storeHandler.GetActivityGroupProgress)

		public.GET("/merchants", storeHandler.ListMerchants)
		public.GET("/merchants/:id", storeHandler.GetMerchant)
		public.GET("/merchants/:id/store", middleware.OptionalAuth(cfg.JWT.Secret, db), storeHandler.GetMerchantStore)
		public.GET("/merchants/:id/announcements", announcementHandler.ListByMerchantPublic)
		public.GET("/merchants/:id/categories", storeHandler.ListMerchantCategories)
		public.GET("/merchants/:id/activities", activityHandler.ListPublicByMerchant)
		public.GET("/merchants/:id/coupons", middleware.OptionalAuth(cfg.JWT.Secret, db), storeHandler.ListMerchantCoupons)
		public.GET("/merchants/:id/products", storeHandler.ListMerchantProducts)
		public.GET("/merchants/:id/products/:product_id", storeHandler.GetMerchantProduct)
		public.GET("/merchants/:id/delivery-zone", deliveryZoneHandler.GetPublic)
		public.POST("/merchants/:id/delivery-zone/check", deliveryZoneHandler.CheckPublic)
		public.GET("/coupons", middleware.OptionalAuth(cfg.JWT.Secret, db), couponHandler.ListPublic)
	}

	authorized := r.Group("/api")
	authorized.Use(middleware.RequireAuth(cfg.JWT.Secret, db))
	{
		authorized.GET("/auth/me", authHandler.Me)
		authorized.PATCH("/auth/profile", authHandler.UpdateProfile)
		authorized.POST("/auth/wechat/phone", authHandler.WeChatPhone)
		authorized.POST("/auth/avatar", authHandler.Avatar)
		authorized.POST("/upload", uploadHandler.UploadImage)

		// 骑手申请：任意已登录账号可访问，具体能否提交由业务层按账号类型判断
		authorized.POST("/user/rider/application", userHandler.ApplyRider)
		authorized.GET("/user/rider/application", userHandler.GetRiderApplication)

		user := authorized.Group("")
		user.Use(middleware.RequireAccountTypes(model.AccountTypeUser))
		registerUserRoutes(user, userHandler, couponHandler, paymentHandler)

		merchant := authorized.Group("/merchant")
		merchant.Use(middleware.RequireAccountTypes(model.AccountTypeMerchant, model.AccountTypeAdmin))
		registerMerchantRoutes(merchant, merchantHandler, merchantOrderHandler, couponHandler, announcementHandler, activityHandler, deliveryZoneHandler)

		admin := authorized.Group("/admin")
		admin.Use(middleware.RequireAccountTypes(model.AccountTypeAdmin))
		registerAdminRoutes(admin, adminHandler, adminDashboardHandler, couponHandler, adminExtraHandler, announcementHandler, deliveryZoneHandler, activityHandler)

		rider := authorized.Group("/rider")
		rider.Use(middleware.RequireRider())
		registerRiderRoutes(rider, riderHandler)
	}

	return r
}

func registerUserRoutes(r *gin.RouterGroup, h *handler.UserHandler, ch *handler.CouponHandler, ph *handler.PaymentHandler) {
	r.GET("/user/overview", h.Overview)
	r.GET("/user/profile", h.Profile)
	r.GET("/user/orders", h.Orders)
	r.POST("/user/orders", h.CreateOrder)
	r.GET("/user/orders/:id", h.OrderDetail)
	r.POST("/user/orders/:id/pay", ph.CreatePrepay)
	r.POST("/user/orders/:id/cancel", h.CancelOrder)
	r.POST("/user/orders/:id/request-use", h.RequestUse)
	r.POST("/user/orders/:id/confirm-pickup", h.ConfirmPickup)
	r.POST("/user/orders/:id/confirm-receipt", h.ConfirmOrderReceipt)
	r.GET("/user/payment/provider", ph.Provider)
	r.GET("/user/deliveries", h.ListUserDeliveries)
	r.GET("/user/deliveries/:id", h.GetUserDelivery)
	r.POST("/user/deliveries/:id/confirm", h.ConfirmDeliveryReceipt)
	r.GET("/user/cart", h.Cart)
	r.POST("/user/cart", h.AddCart)
	r.PATCH("/user/cart/:id", h.UpdateCart)
	r.DELETE("/user/cart/:id", h.DeleteCart)
	r.GET("/user/coupons", h.Coupons)
	r.GET("/user/coupons/applicable", ch.ListApplicable)
	r.POST("/user/coupons/claim", ch.Claim)
	r.GET("/user/inventory", h.Inventory)
	r.POST("/user/inventory/:id/use", h.UseInventory)
	r.GET("/user/inventory/usages", h.ListInventoryUsages)
	r.GET("/user/inventory/usages/:id", h.GetInventoryUsage)
	r.POST("/user/inventory/usages/:id/cancel", h.CancelInventoryUsage)

	r.GET("/user/addresses", h.ListAddresses)
	r.POST("/user/addresses", h.CreateAddress)
	r.GET("/user/addresses/:id", h.GetAddress)
	r.PUT("/user/addresses/:id", h.UpdateAddress)
	r.PATCH("/user/addresses/:id", h.PatchAddress)
	r.DELETE("/user/addresses/:id", h.DeleteAddress)
	r.PATCH("/user/addresses/:id/default", h.SetDefaultAddress)
}

// startGroupExpireWorker 定时关闭超时未成团拼团并模拟退款。
func startGroupExpireWorker(orderSvc *service.OrderService) {
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for range ticker.C {
			n, err := orderSvc.ExpireStaleGroupTeams(time.Now())
			if err != nil {
				log.Printf("拼团超时部分失败: %v (本批成功 %d)", err, n)
			} else if n > 0 {
				log.Printf("拼团超时已处理 %d 个团", n)
			}
		}
	}()
}

func registerMerchantRoutes(r *gin.RouterGroup, h *handler.MerchantHandler, mo *handler.MerchantOrderHandler, ch *handler.CouponHandler, ah *handler.AnnouncementHandler, act *handler.ActivityHandler, dz *handler.DeliveryZoneHandler) {
	r.GET("/profile", h.GetProfile)
	r.PATCH("/profile", h.UpdateProfile)
	r.PATCH("/profile/images", h.UpdateShopImages)
	r.GET("/delivery-zone", dz.GetMerchant)
	r.PUT("/delivery-zone", dz.PutMerchant)
	r.PATCH("/delivery-zone", dz.PatchMerchant)
	r.DELETE("/delivery-zone", dz.DeleteMerchant)
	r.GET("/dashboard", mo.Dashboard)
	r.GET("/sales", mo.SalesReport)

	// 商品/分类/活动：商家只读；写操作仅管理员（管理端走 /admin）
	catalogWrite := r.Group("")
	catalogWrite.Use(middleware.RejectMerchantCatalogWrites())
	{
		catalogWrite.POST("/products", h.CreateProduct)
		catalogWrite.PUT("/products/:id", h.UpdateProduct)
		catalogWrite.PATCH("/products/:id", h.PatchProduct)
		catalogWrite.DELETE("/products/:id", h.DeleteProduct)
		catalogWrite.PATCH("/products/:id/status", h.UpdateProductStatus)
		catalogWrite.PATCH("/products/:id/price", h.UpdateProductPrice)
		catalogWrite.PATCH("/products/:id/stock", h.UpdateProductStock)
		catalogWrite.PATCH("/products/:id/group-buy", h.UpdateProductGroupBuy)
		catalogWrite.PATCH("/products/:id/coupon", h.UpdateProductCoupon)
		catalogWrite.PATCH("/products/:id/sale", h.UpdateProductSale)
		catalogWrite.PATCH("/products/:id/images", h.UpdateProductImages)

		catalogWrite.POST("/categories", h.CreateCategory)
		catalogWrite.PATCH("/categories/:id", h.UpdateCategory)
		catalogWrite.DELETE("/categories/:id", h.DeleteCategory)

		catalogWrite.POST("/activities", act.CreateMerchant)
		catalogWrite.PATCH("/activities/:id", act.UpdateMerchant)
		catalogWrite.DELETE("/activities/:id", act.DeleteMerchant)
		catalogWrite.POST("/activities/:id/products", act.AddProduct)
		catalogWrite.PATCH("/activities/:id/products/:activity_product_id", act.UpdateProduct)
		catalogWrite.PUT("/activities/:id/products/:activity_product_id", act.UpdateProductPut)
		catalogWrite.DELETE("/activities/:id/products/:activity_product_id", act.RemoveProduct)
	}

	r.GET("/products", h.ListProducts)
	r.GET("/products/:id", h.GetProduct)
	r.GET("/categories", h.ListCategories)

	r.GET("/orders", mo.ListOrders)
	r.GET("/orders/:id", mo.GetOrder)
	r.PATCH("/orders/:id/review", mo.ReviewOrder)
	r.PATCH("/orders/:id/use-review", mo.UseReviewOrder)
	r.GET("/verify", mo.PreviewVerify)
	r.POST("/verify", mo.Verify)
	r.GET("/verification-records", mo.ListVerificationRecords)
	r.GET("/inventory-usages", mo.ListInventoryUsages)
	r.GET("/inventory-usages/:id", mo.GetInventoryUsage)
	r.PATCH("/inventory-usages/:id/cancel-review", mo.ReviewCancelInventoryUsage)

	r.GET("/coupons", ch.ListMerchant)
	r.POST("/coupons", ch.CreateMerchant)
	r.GET("/coupons/:id", ch.GetMerchant)
	r.PATCH("/coupons/:id", ch.UpdateMerchant)
	r.PATCH("/coupons/:id/status", ch.UpdateMerchantStatus)

	r.GET("/announcements", ah.ListMerchant)
	r.POST("/announcements", ah.CreateMerchant)
	r.PATCH("/announcements/:id", ah.UpdateMerchant)
	r.DELETE("/announcements/:id", ah.DeleteMerchant)

	r.GET("/activities", act.ListMerchant)
	r.GET("/activities/:id", act.GetMerchant)
	r.GET("/activities/:id/products", act.ListMerchantProducts)
	r.GET("/activities/:id/products/:activity_product_id", act.GetMerchantProduct)
}

func registerAdminRoutes(r *gin.RouterGroup, h *handler.AdminHandler, ad *handler.AdminDashboardHandler, ch *handler.CouponHandler, ae *handler.AdminExtraHandler, ah *handler.AnnouncementHandler, dz *handler.DeliveryZoneHandler, act *handler.ActivityHandler) {
	r.POST("/merchants", h.CreateMerchant)
	r.GET("/merchants", h.ListMerchants)
	r.GET("/merchants/:id", h.GetMerchant)
	r.PATCH("/merchants/:id", h.UpdateMerchant)
	r.PATCH("/merchants/:id/status", h.UpdateMerchantStatus)
	r.PATCH("/merchants/:id/images", h.UpdateMerchantImages)
	r.GET("/merchants/:id/operator", h.GetMerchantOperator)
	r.PUT("/merchants/:id/operator", h.PutMerchantOperator)
	r.DELETE("/merchants/:id/operator", h.DeleteMerchantOperator)
	r.GET("/merchants/:id/delivery-zone", dz.GetAdmin)
	r.PUT("/merchants/:id/delivery-zone", dz.PutAdmin)
	r.PATCH("/merchants/:id/delivery-zone", dz.PatchAdmin)
	r.DELETE("/merchants/:id/delivery-zone", dz.DeleteAdmin)

	r.POST("/products", h.CreateProduct)
	r.GET("/products", h.ListProducts)
	r.GET("/products/:id", h.GetProduct)
	r.PUT("/products/:id", h.UpdateProduct)
	r.PATCH("/products/:id", h.PatchProduct)
	r.DELETE("/products/:id", h.DeleteProduct)
	r.PATCH("/products/:id/status", h.UpdateProductStatus)
	r.PATCH("/products/:id/price", h.UpdateProductPrice)
	r.PATCH("/products/:id/stock", h.UpdateProductStock)
	r.PATCH("/products/:id/group-buy", h.UpdateProductGroupBuy)
	r.PATCH("/products/:id/coupon", h.UpdateProductCoupon)
	r.PATCH("/products/:id/sale", h.UpdateProductSale)
	r.PATCH("/products/:id/images", h.UpdateProductImages)

	r.GET("/activities", act.ListAdmin)
	r.POST("/activities", act.CreateAdmin)
	r.GET("/activities/:id", act.GetAdmin)
	r.PATCH("/activities/:id", act.UpdateAdmin)
	r.DELETE("/activities/:id", act.DeleteAdmin)
	r.GET("/activities/:id/products", act.ListAdminProducts)
	r.POST("/activities/:id/products", act.AddAdminProduct)
	r.GET("/activities/:id/products/:activity_product_id", act.GetAdminProduct)
	r.PATCH("/activities/:id/products/:activity_product_id", act.UpdateAdminProduct)
	r.PUT("/activities/:id/products/:activity_product_id", act.UpdateAdminProductPut)
	r.DELETE("/activities/:id/products/:activity_product_id", act.RemoveAdminProduct)

	r.GET("/dashboard", ad.Dashboard)
	r.GET("/sales", ad.SalesReport)
	r.GET("/orders", ad.ListOrders)
	r.GET("/orders/:id", ad.GetOrder)
	r.GET("/verification-records", ad.ListVerificationRecords)

	r.GET("/users", ae.ListUsers)
	r.GET("/categories", ae.ListCategories)
	r.POST("/categories", ae.CreateCategory)
	r.GET("/categories/:id", ae.GetCategory)
	r.PATCH("/categories/:id", ae.UpdateCategory)
	r.DELETE("/categories/:id", ae.DeleteCategory)
	r.GET("/deliveries", ae.ListDeliveries)
	r.GET("/inventory-usages", ae.ListInventoryUsages)

	r.GET("/rider/applications", h.ListRiderApplications)
	r.GET("/rider/applications/:id", h.GetRiderApplication)
	r.PATCH("/rider/applications/:id/review", h.ReviewRiderApplication)

	r.GET("/coupons", ch.ListAdmin)
	r.POST("/coupons", ch.CreateAdmin)
	r.GET("/coupons/:id", ch.GetAdmin)
	r.PATCH("/coupons/:id", ch.UpdateAdmin)
	r.PATCH("/coupons/:id/status", ch.UpdateAdminStatus)

	r.GET("/announcements", ah.ListAdmin)
	r.POST("/announcements", ah.CreateAdmin)
	r.PATCH("/announcements/:id", ah.UpdateAdmin)
	r.DELETE("/announcements/:id", ah.DeleteAdmin)
}

func registerRiderRoutes(r *gin.RouterGroup, h *handler.RiderHandler) {
	r.GET("/orders", h.ListOrders)
	r.POST("/orders/:id/accept", h.AcceptDelivery)
	r.POST("/orders/:id/start", h.StartDelivery)
	r.POST("/orders/:id/complete", h.CompleteDelivery)
}
