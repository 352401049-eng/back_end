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
