package service

import (
	"testing"

	"yujixinjiang/backend/internal/model"
)

func TestApplyProductChannelsUpdateKeepsZeroStock(t *testing.T) {
	existing := &model.Product{
		EnableDeal: 1, EnableGroup: 1, EnableTakeout: 1,
		DealStock: 10, GroupStock: 8, TakeoutStock: 6,
	}
	var p model.Product
	applyProductChannels(&p, ProductInput{
		EnableDeal: 1, EnableGroup: 1, EnableTakeout: 1,
		DealStock: 10, GroupStock: 0, TakeoutStock: 0,
	}, existing, false)
	if p.GroupStock != 0 || p.TakeoutStock != 0 {
		t.Fatalf("expected zero channel stocks, got group=%d takeout=%d", p.GroupStock, p.TakeoutStock)
	}
}

func TestApplyProductChannelsUpdateAppliesNewStock(t *testing.T) {
	existing := &model.Product{
		EnableDeal: 1, EnableGroup: 1, EnableTakeout: 1,
		DealStock: 1, GroupStock: 1, TakeoutStock: 1,
	}
	var p model.Product
	applyProductChannels(&p, ProductInput{
		EnableDeal: 1, EnableGroup: 1, EnableTakeout: 1,
		DealStock: 5, GroupStock: 9, TakeoutStock: 12,
	}, existing, false)
	if p.DealStock != 5 || p.GroupStock != 9 || p.TakeoutStock != 12 {
		t.Fatalf("stocks not applied: deal=%d group=%d takeout=%d", p.DealStock, p.GroupStock, p.TakeoutStock)
	}
}
