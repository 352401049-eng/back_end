package service

import (
	"testing"

	"yujixinjiang/backend/internal/model"
)

func TestValidateCouponTemplateFixedRequiresDiscountLessThanMin(t *testing.T) {
	amt := 10.0
	if err := validateCouponTemplate(model.CouponTypeFixed, 10, &amt, nil); err == nil {
		t.Fatal("expected error when discount >= min")
	}
	if err := validateCouponTemplate(model.CouponTypeFixed, 100, &amt, nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := validateCouponTemplate(model.CouponTypeFixed, 0, &amt, nil); err == nil {
		t.Fatal("expected error when min <= 0")
	}
}
