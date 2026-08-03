package service

import (
	"testing"

	"yujixinjiang/backend/internal/model"
)

func TestPlanOrderItemRefundWithMeta_UsesPayWeightedUnit(t *testing.T) {
	// 优惠后单价 8、实付 8、数量 1 → 应按 8 退
	meta := &orderRefundMeta{
		UnitPrice: 8,
		PayAmount: 8,
		PayStatus: model.PayStatusPaid,
	}
	qty, amount, err := planOrderItemRefundWithMeta(meta, 1)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if qty != 1 || amount != 8 {
		t.Fatalf("got qty=%d amount=%v want 1 / 8", qty, amount)
	}
}
