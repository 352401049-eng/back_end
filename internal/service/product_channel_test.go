package service

import (
	"testing"

	"yujixinjiang/backend/internal/model"
)

func TestValidateProductChannels_AtLeastOne(t *testing.T) {
	p := &model.Product{}
	if err := validateProductChannels(p); err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateProductChannels_GroupPriceMustBeBelowDealPrice(t *testing.T) {
	target := uint32(2)
	groupPrice := 10.0
	p := &model.Product{
		EnableDeal:          1,
		EnableGroup:         1,
		Price:               10.0,
		GroupBuyTargetCount: &target,
		GroupBuyPrice:       &groupPrice,
	}
	if err := validateProductChannels(p); err == nil {
		t.Fatal("expected error when group_buy_price >= price")
	}
}

func TestValidateProductChannels_DealOnlyOK(t *testing.T) {
	p := &model.Product{
		EnableDeal: 1,
		Price:      9.9,
	}
	if err := validateProductChannels(p); err != nil {
		t.Fatalf("expected valid deal-only product, got %v", err)
	}
}

func TestValidateProductChannels_TakeoutRequiresOriginalPrice(t *testing.T) {
	p := &model.Product{
		EnableTakeout: 1,
	}
	if err := validateProductChannels(p); err == nil {
		t.Fatal("expected error when takeout enabled without original_price")
	}
}
