package service

import (
	"errors"
	"testing"

	"yujixinjiang/backend/internal/model"
)

func TestAssertBagPurchaseAllowed(t *testing.T) {
	actID := uint64(42)

	tests := []struct {
		name              string
		purchaseType      uint8
		activityProductID *uint64
		wantErr           bool
	}{
		{"solo rejected", model.PurchaseTypeSolo, nil, true},
		{"group allowed", model.PurchaseTypeGroup, nil, false},
		{"activity allowed", model.PurchaseTypeSolo, &actID, false},
		{"group+activity allowed", model.PurchaseTypeGroup, &actID, false},
		{"zero activity id treated as absent", model.PurchaseTypeSolo, func() *uint64 { v := uint64(0); return &v }(), true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := assertBagPurchaseAllowed(tc.purchaseType, tc.activityProductID)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if !errors.Is(err, ErrSoloPurchaseDisabled) {
					t.Fatalf("expected ErrSoloPurchaseDisabled, got %v", err)
				}
				return
			}
			if err != nil {
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
				if !errors.Is(err, ErrInvalidDeliveryType) {
					t.Fatalf("expected ErrInvalidDeliveryType, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestCheckoutCartRejectsSoloBagPurchase(t *testing.T) {
	s := &OrderService{}
	_, err := s.CheckoutCart(1, CheckoutCartInput{
		CartItemIDs: []uint64{1}, MerchantID: 9,
	})
	if !errors.Is(err, ErrSoloPurchaseDisabled) {
		t.Fatalf("expected ErrSoloPurchaseDisabled, got %v", err)
	}
}
