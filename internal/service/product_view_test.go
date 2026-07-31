package service

import (
	"testing"

	"yujixinjiang/backend/internal/model"
)

func TestBuildProductStoreView_DealAndGroupSaleOptions(t *testing.T) {
	target := uint32(3)
	groupPrice := 8.0
	p := model.Product{
		ID:                  1,
		EnableDeal:          1,
		EnableGroup:         1,
		Price:               10.0,
		DealStock:           5,
		GroupStock:          2,
		EnableCoupon:        1,
		GroupBuyTargetCount: &target,
		GroupBuyPrice:       &groupPrice,
	}
	gb := &model.GroupBuy{ID: 99, GroupPrice: 7.5}

	view := buildProductStoreView(p, gb)

	if !view.SaleOptions.Deal.Available {
		t.Fatal("deal should be available when enable_deal=1")
	}
	if view.SaleOptions.Deal.Price != 10.0 {
		t.Fatalf("deal price = %v, want 10", view.SaleOptions.Deal.Price)
	}
	if !view.SaleOptions.Group.Available {
		t.Fatal("group should be available with active group_buy")
	}
	if view.SaleOptions.Group.Price != 7.5 {
		t.Fatalf("group price = %v, want 7.5 from group_buy", view.SaleOptions.Group.Price)
	}
	if view.EnableDeal != 1 || view.DealStock != 5 || view.GroupStock != 2 {
		t.Fatal("channel flags and stocks should be exposed on view")
	}
}

func TestBuildProductStoreView_DealDisabled(t *testing.T) {
	p := model.Product{EnableDeal: 0, Price: 9.9}
	view := buildProductStoreView(p, nil)
	if view.SaleOptions.Deal.Available {
		t.Fatal("deal should not be available when enable_deal=0")
	}
}
