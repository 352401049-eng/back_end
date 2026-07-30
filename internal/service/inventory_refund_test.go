package service

import (
	"testing"

	"yujixinjiang/backend/internal/model"
)

func ptrU64(v uint64) *uint64 { return &v }

func TestOrderNetBalancesFromLogs_UseConsumesFIFO(t *testing.T) {
	// 旧订单入账后被核销（use 无 order_id），不应再显示可退净余额
	logs := []model.UserInventoryLog{
		{ID: 1, OrderID: ptrU64(44), EventType: model.InventoryEventOrderCredit, DeltaQty: 1},
		{ID: 2, OrderID: nil, EventType: model.InventoryEventUse, DeltaQty: -1},
		{ID: 3, OrderID: ptrU64(45), EventType: model.InventoryEventOrderCredit, DeltaQty: 2},
		{ID: 4, OrderID: nil, EventType: model.InventoryEventUse, DeltaQty: -1},
		{ID: 5, OrderID: nil, EventType: model.InventoryEventUse, DeltaQty: -1},
		{ID: 6, OrderID: ptrU64(86), EventType: model.InventoryEventOrderCredit, DeltaQty: 3},
	}
	bal, seq := orderNetBalancesFromLogs(logs)
	if bal[44] != 0 {
		t.Fatalf("order 44 net=%d want 0 (already used)", bal[44])
	}
	if bal[45] != 0 {
		t.Fatalf("order 45 net=%d want 0 (already used)", bal[45])
	}
	if bal[86] != 3 {
		t.Fatalf("order 86 net=%d want 3", bal[86])
	}
	if len(seq) < 3 || seq[0] != 44 || seq[2] != 86 {
		t.Fatalf("unexpected orderSeq=%v", seq)
	}
}

func TestOrderNetBalancesFromLogs_RefundAfterCredit(t *testing.T) {
	logs := []model.UserInventoryLog{
		{ID: 1, OrderID: ptrU64(86), EventType: model.InventoryEventOrderCredit, DeltaQty: 3},
		{ID: 2, OrderID: ptrU64(86), EventType: model.InventoryEventRefund, DeltaQty: -3},
	}
	bal, _ := orderNetBalancesFromLogs(logs)
	if bal[86] != 0 {
		t.Fatalf("order 86 net=%d want 0", bal[86])
	}
}
