package service

import (
	"testing"

	"yujixinjiang/backend/internal/model"
)

func TestAssertBagPurchaseAllowed(t *testing.T) {
	actID := uint64(42)

	tests := []struct {
		name              string
		purchaseType      uint8
		activityProductID *uint64
	}{
		{"solo allowed", model.PurchaseTypeSolo, nil},
		{"group allowed", model.PurchaseTypeGroup, nil},
		{"activity allowed", model.PurchaseTypeSolo, &actID},
		{"group+activity allowed", model.PurchaseTypeGroup, &actID},
		{"zero activity id treated as absent", model.PurchaseTypeSolo, func() *uint64 { v := uint64(0); return &v }()},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if err := assertBagPurchaseAllowed(tc.purchaseType, tc.activityProductID); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestAssertBagPickupOnly(t *testing.T) {
	tests := []struct {
		name    string
		dt      uint8
		wantErr bool
	}{
		{"pickup ok", model.DeliveryTypePickup, false},
		{"zero defaults pickup", 0, false},
		{"delivery rejected", model.DeliveryTypeDelivery, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := assertBagPickupOnly(tc.dt)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPurchaseTypeToChannel(t *testing.T) {
	if purchaseTypeToChannel(model.PurchaseTypeSolo) != productChannelDeal {
		t.Fatal("solo should map to deal")
	}
	if purchaseTypeToChannel(model.PurchaseTypeGroup) != productChannelGroup {
		t.Fatal("group should map to group")
	}
}

func TestTakeoutGoodsUnitPrice(t *testing.T) {
	price := 19.9
	p := model.Product{EnableTakeout: 1, OriginalPrice: &price}
	got, err := takeoutGoodsUnitPrice(p)
	if err != nil || got != price {
		t.Fatalf("got %v err %v", got, err)
	}
	_, err = takeoutGoodsUnitPrice(model.Product{EnableTakeout: 0, OriginalPrice: &price})
	if err == nil {
		t.Fatal("expected error when takeout disabled")
	}
}
