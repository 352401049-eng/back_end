package service

import (
	"testing"

	"yujixinjiang/backend/internal/model"
)

func TestOrderNetBalancesFromLogs_UseCancelClampedByCredit(t *testing.T) {
	// use FIFO 扣了订单 A，cancel 却加回订单 B → B 不得超过其入账
	logs := []model.UserInventoryLog{
		{ID: 1, OrderID: ptrU64(1), EventType: model.InventoryEventOrderCredit, DeltaQty: 1},
		{ID: 2, OrderID: ptrU64(2), EventType: model.InventoryEventOrderCredit, DeltaQty: 1},
		{ID: 3, OrderID: nil, EventType: model.InventoryEventUse, DeltaQty: -1}, // FIFO 扣订单1
		{ID: 4, OrderID: ptrU64(2), EventType: model.InventoryEventUseCancel, DeltaQty: 1}, // 错加回订单2
	}
	bal, seq := orderNetBalancesFromLogs(logs)
	if bal[2] > 1 {
		t.Fatalf("order 2 net=%d want <=1 (cannot exceed credit)", bal[2])
	}
	reconcileNetBalancesToInventory(bal, seq, 1)
	if bal[1]+bal[2] != 1 {
		t.Fatalf("total net=%d want 1 after reconcile", bal[1]+bal[2])
	}
	if bal[2] != 1 || bal[1] != 0 {
		t.Fatalf("want bal[1]=0 bal[2]=1, got bal[1]=%d bal[2]=%d", bal[1], bal[2])
	}
}

func TestReconcileNetBalances_PrefersRecentOrder(t *testing.T) {
	// 旧单虚高 + 新单真实入账，背包只有新单件数时，削去旧单
	bal := map[uint64]int32{84: 1, 88: 2}
	seq := []uint64{84, 88}
	reconcileNetBalancesToInventory(bal, seq, 2)
	if bal[84] != 0 {
		t.Fatalf("order 84 net=%d want 0", bal[84])
	}
	if bal[88] != 2 {
		t.Fatalf("order 88 net=%d want 2", bal[88])
	}
}

func TestOrderNetBalancesFromLogs_RefundLowersCeiling(t *testing.T) {
	logs := []model.UserInventoryLog{
		{ID: 1, OrderID: ptrU64(84), EventType: model.InventoryEventOrderCredit, DeltaQty: 2},
		{ID: 2, OrderID: ptrU64(84), EventType: model.InventoryEventRefund, DeltaQty: -1},
		{ID: 3, OrderID: ptrU64(84), EventType: model.InventoryEventUseCancel, DeltaQty: 1}, // 不可把已退名额加回来
	}
	bal, _ := orderNetBalancesFromLogs(logs)
	if bal[84] != 1 {
		t.Fatalf("order 84 net=%d want 1", bal[84])
	}
}
