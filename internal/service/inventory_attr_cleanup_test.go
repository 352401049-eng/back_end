package service

import "testing"

func TestValidateRefundAllocs(t *testing.T) {
	nets := map[uint64]int32{88: 2, 84: 0}
	err := validateRefundAllocs(nets, 2, []refundAlloc{
		{OrderID: 84, Quantity: 1, Amount: 0.01},
	})
	if err == nil {
		t.Fatal("expected reject alloc on zero-net order 84")
	}
	err = validateRefundAllocs(nets, 2, []refundAlloc{
		{OrderID: 88, Quantity: 1, Amount: 0.01},
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	err = validateRefundAllocs(nets, 1, []refundAlloc{
		{OrderID: 88, Quantity: 2, Amount: 0.02},
	})
	if err == nil {
		t.Fatal("expected reject when qty > bag")
	}
}
