package service

import (
	"testing"

	"yujixinjiang/backend/internal/model"
)

func TestProductChannelStock(t *testing.T) {
	p := model.Product{DealStock: 5, GroupStock: 3, TakeoutStock: 7}
	if productChannelStock(p, productChannelDeal) != 5 {
		t.Fatal("deal stock")
	}
	if productChannelStock(p, productChannelGroup) != 3 {
		t.Fatal("group stock")
	}
	if productChannelStock(p, productChannelTakeout) != 7 {
		t.Fatal("takeout stock")
	}
}

func TestAssertProductChannelPurchasable(t *testing.T) {
	p := model.Product{EnableDeal: 1, EnableGroup: 0, EnableTakeout: 0}
	if err := assertProductChannelPurchasable(p, productChannelDeal); err != nil {
		t.Fatal(err)
	}
	if err := assertProductChannelPurchasable(p, productChannelGroup); err == nil {
		t.Fatal("expected group disabled error")
	}
}

func TestStockChannelForOrderActivityFallsBackToDeal(t *testing.T) {
	// 活动拼团挂在仅开启团购通道的商品上（常见：电影票 enable_group=0）
	p := model.Product{EnableDeal: 1, EnableGroup: 0, DealStock: 97, GroupStock: 1}
	got := stockChannelForOrder(p, model.PurchaseTypeGroup, true)
	if got != productChannelDeal {
		t.Fatalf("activity group order should deduct deal stock, got %s", got)
	}
	// 非活动仍按购买方式
	got = stockChannelForOrder(p, model.PurchaseTypeGroup, false)
	if got != productChannelGroup {
		t.Fatalf("non-activity group order should use group channel, got %s", got)
	}
}

func TestStockChannelForOrderActivityUsesNativeGroupWhenEnabled(t *testing.T) {
	p := model.Product{EnableDeal: 1, EnableGroup: 1, DealStock: 10, GroupStock: 5}
	got := stockChannelForOrder(p, model.PurchaseTypeGroup, true)
	if got != productChannelGroup {
		t.Fatalf("expected group when product group channel enabled, got %s", got)
	}
}
