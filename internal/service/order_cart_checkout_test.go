package service

import (
	"errors"
	"testing"
)

func TestCheckoutCartRejectsEmptyInput(t *testing.T) {
	s := &OrderService{}
	_, err := s.CheckoutCart(1, CheckoutCartInput{})
	if !errors.Is(err, ErrInvalidProductArg) {
		t.Fatalf("empty ids: got %v", err)
	}
	_, err = s.CheckoutCart(1, CheckoutCartInput{CartItemIDs: []uint64{1}})
	if !errors.Is(err, ErrInvalidProductArg) {
		t.Fatalf("missing merchant: got %v", err)
	}
	_, err = s.CheckoutCart(1, CheckoutCartInput{
		CartItemIDs: []uint64{1, 2}, MerchantID: 9, DeliveryType: 99,
	})
	if err == nil {
		t.Fatal("expected invalid delivery type error")
	}
}
