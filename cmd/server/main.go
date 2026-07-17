package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"yujixinjiang/backend/internal/backup"
	"yujixinjiang/backend/internal/config"
	"yujixinjiang/backend/internal/database"
	"yujixinjiang/backend/internal/router"

	_ "yujixinjiang/backend/docs"
)

// @title           豫记信疆 API
// @version         1.0
// @description     豫记信疆微信小程序后端接口文档，支持在线调试。
// @termsOfService  http://swagger.io/terms/

// @contact.name   API Support
// @contact.email  support@yujixinjiang.com

// @license.name  MIT
// @license.url   https://opensource.org/licenses/MIT

// @BasePath  /api

// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description 登录后获取的 JWT，格式: Bearer {token}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}

	db, err := database.Connect(cfg.DB)
	if err != nil {
		log.Fatalf("数据库连接失败: %v", err)
	}

	r := router.Setup(cfg, db)
	addr := ":" + cfg.Port
	log.Printf("服务已启动: http://localhost%s", addr)
	log.Printf("健康检查: http://localhost%s/api/health", addr)
	log.Printf("Swagger 文档: http://localhost%s/swagger/index.html", addr)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sched := backup.NewScheduler(cfg.DB, cfg.Backup)
	sched.Start(ctx)
	defer sched.Stop()

	go func() {
		if err := r.Run(addr); err != nil {
			log.Fatalf("服务启动失败: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("服务正在关闭...")
}
