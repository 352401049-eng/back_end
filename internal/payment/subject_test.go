package payment

import (
	"testing"

	"yujixinjiang/backend/internal/model"
)

func TestSubjectTypeFromOrderNo(t *testing.T) {
	tests := []struct {
		orderNo string
		want    string
	}{
		{"YJ20260101120000abc", model.PaySubjectOrder},
		{"T20260101120000abc", model.PaySubjectTakeout},
		{"F20260101120000abc", model.PaySubjectDeliveryFee},
	}
	for _, tc := range tests {
		if got := SubjectTypeFromOrderNo(tc.orderNo); got != tc.want {
			t.Fatalf("order_no %q: got %q want %q", tc.orderNo, got, tc.want)
		}
	}
}

func TestPaySubjectValidate(t *testing.T) {
	valid := PaySubject{
		Type: model.PaySubjectOrder, ID: 1, OrderNo: "YJ1", Amount: 9.9, AccountID: 2,
	}
	if err := valid.Validate(); err != nil {
		t.Fatal(err)
	}

	invalid := PaySubject{Type: model.PaySubjectTakeout, ID: 0, OrderNo: "T1", Amount: 1, AccountID: 2}
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected invalid subject error")
	}
}

func TestPaymentTransactionOrderID(t *testing.T) {
	orderSub := PaySubject{Type: model.PaySubjectOrder, ID: 42}
	if got := paymentTransactionOrderID(orderSub); got != 42 {
		t.Fatalf("order subject order_id=%d", got)
	}
	takeoutSub := PaySubject{Type: model.PaySubjectTakeout, ID: 7}
	if got := paymentTransactionOrderID(takeoutSub); got != 0 {
		t.Fatalf("takeout subject order_id=%d want 0", got)
	}
}
