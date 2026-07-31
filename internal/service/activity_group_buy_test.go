package service

import (
	"testing"

	"yujixinjiang/backend/internal/model"
)

func floatPtr(v float64) *float64 { return &v }
func uint32Ptr(v uint32) *uint32  { return &v }

func TestValidateActivityProductInput_GroupBuyEqualPriceAllowed(t *testing.T) {
	target := uint32(2)
	input := ActivityProductInput{
		ProductID:           1,
		ActivityPrice:       9.9,
		EnableGroupBuy:      1,
		GroupBuyPrice:       floatPtr(9.9),
		GroupBuyTargetCount: &target,
	}
	if err := validateActivityProductInput(input); err != nil {
		t.Fatalf("equal group/activity price should be valid, got %v", err)
	}
}

func TestValidateActivityProductInput_GroupBuyRequiresPositivePrice(t *testing.T) {
	target := uint32(2)
	input := ActivityProductInput{
		ProductID:           1,
		ActivityPrice:       9.9,
		EnableGroupBuy:      1,
		GroupBuyPrice:       floatPtr(0),
		GroupBuyTargetCount: &target,
	}
	if err := validateActivityProductInput(input); err == nil {
		t.Fatal("group_buy_price <= 0 should be rejected")
	}
}

func TestValidateActivityProductInput_GroupBuyRequiresTargetAtLeastTwo(t *testing.T) {
	target := uint32(1)
	input := ActivityProductInput{
		ProductID:           1,
		ActivityPrice:       9.9,
		EnableGroupBuy:      1,
		GroupBuyPrice:       floatPtr(9.9),
		GroupBuyTargetCount: &target,
	}
	if err := validateActivityProductInput(input); err == nil {
		t.Fatal("group_buy_target_count < 2 should be rejected")
	}
}

func TestActivityProductCanGroupBuy_EqualPriceAllowed(t *testing.T) {
	target := uint32(2)
	ap := &model.ActivityProduct{
		EnableGroupBuy:      1,
		ActivityPrice:       9.9,
		GroupBuyPrice:       floatPtr(9.9),
		GroupBuyTargetCount: &target,
	}
	if !activityProductCanGroupBuy(ap) {
		t.Fatal("equal group/activity price should allow group buy")
	}
}

func TestActivityProductCanGroupBuy_DisabledWhenGroupBuyOff(t *testing.T) {
	target := uint32(2)
	ap := &model.ActivityProduct{
		EnableGroupBuy:      0,
		ActivityPrice:       9.9,
		GroupBuyPrice:       floatPtr(9.9),
		GroupBuyTargetCount: &target,
	}
	if activityProductCanGroupBuy(ap) {
		t.Fatal("enable_group_buy=0 should not allow group buy")
	}
}

func TestNormalizeActivityProductGroupBuyInput_ClearsFieldsWhenDisabled(t *testing.T) {
	target := uint32(2)
	input := ActivityProductInput{
		EnableGroupBuy:          0,
		GroupBuyPrice:           floatPtr(9.9),
		GroupBuyTargetCount:     &target,
		GroupBuyAllowRepeat:     1,
		GroupBuyMaxJoinsPerUser: 3,
	}
	got := normalizeActivityProductGroupBuyInput(input)
	if got.GroupBuyPrice != nil {
		t.Fatal("GroupBuyPrice should be nil when group buy disabled")
	}
	if got.GroupBuyTargetCount != nil {
		t.Fatal("GroupBuyTargetCount should be nil when group buy disabled")
	}
	if got.GroupBuyAllowRepeat != 0 || got.GroupBuyMaxJoinsPerUser != 0 {
		t.Fatal("group buy repeat/max joins should be cleared when disabled")
	}
}
