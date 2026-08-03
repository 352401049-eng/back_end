package service

import (
	"errors"
	"testing"

	"yujixinjiang/backend/internal/model"
)

func TestPendingPayDuplicateErrorUnwrap(t *testing.T) {
	err := &PendingPayDuplicateError{OrderID: 9, OrderNo: "O1", ProductID: 3, Status: model.OrderStatusPendingPay}
	if !errors.Is(err, ErrPendingPayDuplicate) {
		t.Fatalf("expected errors.Is PendingPayDuplicate")
	}
	if err.StatusCode() != "pending_pay" {
		t.Fatalf("status code=%s", err.StatusCode())
	}
	g := &PendingPayDuplicateError{Status: model.OrderStatusPendingGroup}
	if g.StatusCode() != "pending_group" {
		t.Fatalf("group status code=%s", g.StatusCode())
	}
}

func TestUniqueUint64s(t *testing.T) {
	got := uniqueUint64s([]uint64{0, 2, 2, 1, 0, 1})
	if len(got) != 2 || got[0] != 2 || got[1] != 1 {
		t.Fatalf("got %#v", got)
	}
}
