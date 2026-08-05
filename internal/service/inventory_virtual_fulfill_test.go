package service

import (
	"errors"
	"testing"

	"yujixinjiang/backend/internal/model"
)

func TestValidateFulfillmentFlagsVirtualPickupAllowed(t *testing.T) {
	p := model.Product{
		ItemType:      model.ProductItemTypeVirtual,
		AllowPickup:   0,
		AllowDelivery: 0,
	}
	if err := validateFulfillmentFlags(p, model.DeliveryTypePickup); err != nil {
		t.Fatalf("virtual pickup/verify should be allowed, got %v", err)
	}
	if err := validateFulfillmentFlags(p, model.DeliveryTypeDelivery); !errors.Is(err, ErrVirtualNotDeliverable) {
		t.Fatalf("virtual delivery should be blocked, got %v", err)
	}
}

func TestValidateFulfillmentFlagsPhysicalRespectsFlags(t *testing.T) {
	p := model.Product{ItemType: model.ProductItemTypePhysical, AllowPickup: 0, AllowDelivery: 1}
	if err := validateFulfillmentFlags(p, model.DeliveryTypePickup); err != nil {
		t.Fatalf("physical pickup should always be allowed, got %v", err)
	}
	if err := validateFulfillmentFlags(p, model.DeliveryTypeDelivery); err != nil {
		t.Fatalf("physical with delivery should pass, got %v", err)
	}
	p.AllowDelivery = 0
	if err := validateFulfillmentFlags(p, model.DeliveryTypeDelivery); !errors.Is(err, ErrDeliveryNotAllowed) {
		t.Fatalf("physical without delivery should fail, got %v", err)
	}
}
