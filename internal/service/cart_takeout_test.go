package service

import (
	"errors"
	"testing"

	"yujixinjiang/backend/internal/model"
)

func TestValidateCartAddForTakeout(t *testing.T) {
	tests := []struct {
		name         string
		purchaseType uint8
		product      model.Product
		wantMsg      string
	}{
		{
			name:         "group rejected",
			purchaseType: model.PurchaseTypeGroup,
			product:      model.Product{AllowDelivery: 1, ItemType: model.ProductItemTypePhysical},
			wantMsg:      "拼团商品请在详情页单独下单",
		},
		{
			name:         "allow_delivery off",
			purchaseType: model.PurchaseTypeSolo,
			product:      model.Product{AllowDelivery: 0, ItemType: model.ProductItemTypePhysical},
			wantMsg:      "该商品不支持外卖配送",
		},
		{
			name:         "virtual not deliverable",
			purchaseType: model.PurchaseTypeSolo,
			product:      model.Product{AllowDelivery: 1, ItemType: model.ProductItemTypeVirtual},
			wantMsg:      "该商品不支持外卖配送",
		},
		{
			name:         "solo physical ok",
			purchaseType: model.PurchaseTypeSolo,
			product:      model.Product{AllowDelivery: 1, ItemType: model.ProductItemTypePhysical},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCartAddForTakeout(tc.purchaseType, tc.product)
			if tc.wantMsg == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, ErrInvalidProductArg) {
				t.Fatalf("expected ErrInvalidProductArg, got %v", err)
			}
			if err.Error() != ErrInvalidProductArg.Error()+": "+tc.wantMsg {
				t.Fatalf("message = %q, want %q", err.Error(), ErrInvalidProductArg.Error()+": "+tc.wantMsg)
			}
		})
	}
}

func TestValidateCartCheckoutTakeoutInput(t *testing.T) {
	tests := []struct {
		name    string
		in      CartCheckoutTakeoutInput
		wantErr error
	}{
		{
			name:    "empty cart ids",
			in:      CartCheckoutTakeoutInput{},
			wantErr: ErrInvalidProductArg,
		},
		{
			name: "missing merchant",
			in: CartCheckoutTakeoutInput{
				CartItemIDs: []uint64{1},
			},
			wantErr: ErrInvalidProductArg,
		},
		{
			name: "missing address",
			in: CartCheckoutTakeoutInput{
				CartItemIDs: []uint64{1},
				MerchantID:  9,
			},
			wantErr: ErrAddressRequired,
		},
		{
			name: "ok",
			in: CartCheckoutTakeoutInput{
				CartItemIDs: []uint64{1, 2},
				MerchantID:  9,
				AddressID:   3,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateCartCheckoutTakeoutInput(tc.in)
			if tc.wantErr == nil {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestCreateFromCartRejectsInvalidInput(t *testing.T) {
	s := &TakeoutService{}
	_, err := s.CreateFromCart(1, CartCheckoutTakeoutInput{})
	if !errors.Is(err, ErrInvalidProductArg) {
		t.Fatalf("empty input: got %v", err)
	}
	_, err = s.CreateFromCart(1, CartCheckoutTakeoutInput{
		CartItemIDs: []uint64{1},
		MerchantID:  9,
	})
	if !errors.Is(err, ErrAddressRequired) {
		t.Fatalf("missing address: got %v", err)
	}
}
