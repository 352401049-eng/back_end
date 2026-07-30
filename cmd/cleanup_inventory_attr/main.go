package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"yujixinjiang/backend/internal/config"
	"yujixinjiang/backend/internal/database"
	"yujixinjiang/backend/internal/service"
)

// 清理背包流水错账：补全 use.order_id，纠正错挂到虚高旧单的 inventory_refund，并同步订单退款金额字段。
//
//	go run ./cmd/cleanup_inventory_attr -dry-run
//	go run ./cmd/cleanup_inventory_attr -apply
func main() {
	dryRun := flag.Bool("dry-run", true, "只扫描统计，不写库（默认）")
	apply := flag.Bool("apply", false, "实际写库修复")
	flag.Parse()
	if *apply {
		*dryRun = false
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("加载配置失败: %v", err)
	}
	db, err := database.Connect(cfg.DB)
	if err != nil {
		log.Fatalf("数据库连接失败: %v", err)
	}

	mode := "DRY-RUN"
	if !*dryRun {
		mode = "APPLY"
	}
	fmt.Printf("=== 背包来源归属清理 [%s] ===\n", mode)

	report, err := service.CleanupInventoryAttribution(db, *dryRun)
	if err != nil {
		log.Fatalf("清理失败: %v", err)
	}
	fmt.Printf("扫描商品组: %d\n", report.GroupsScanned)
	fmt.Printf("补全 use.order_id: %d\n", report.UseLogsStamped)
	fmt.Printf("纠正退款流水归属: %d\n", report.RefundsMoved)
	fmt.Printf("同步订单退款金额: %d\n", report.OrdersMoneyFix)
	if *dryRun {
		fmt.Println("（dry-run 未写库，加 -apply 执行修复）")
	} else {
		fmt.Println("已写库完成")
	}
	os.Exit(0)
}
