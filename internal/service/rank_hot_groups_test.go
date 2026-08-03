package service

import "testing"

func TestHotGroupRowLessByNeed(t *testing.T) {
	closer := hotGroupRow{TeamID: 1, TargetCount: 5, CurrentCount: 4}
	farther := hotGroupRow{TeamID: 2, TargetCount: 5, CurrentCount: 1}
	if !hotGroupRowLess(closer, farther) {
		t.Fatal("closer team should rank first")
	}
}

func TestHotGroupDedupeKeepsBestTeamPerProduct(t *testing.T) {
	// 产品约定：热拼榜同一商品只展示一团（最接近成团）
	rows := []hotGroupRow{
		{TeamID: 5, ProductID: 1, TargetCount: 5, CurrentCount: 1},
		{TeamID: 4, ProductID: 1, TargetCount: 5, CurrentCount: 1},
		{TeamID: 3, ProductID: 1, TargetCount: 5, CurrentCount: 2},
		{TeamID: 9, ProductID: 2, TargetCount: 3, CurrentCount: 1},
	}
	bestByProduct := map[uint64]hotGroupRow{}
	for _, r := range rows {
		prev, ok := bestByProduct[r.ProductID]
		if !ok || hotGroupRowLess(r, prev) {
			bestByProduct[r.ProductID] = r
		}
	}
	if len(bestByProduct) != 2 {
		t.Fatalf("want 2 products, got %d", len(bestByProduct))
	}
	if bestByProduct[1].TeamID != 3 {
		t.Fatalf("product 1 should keep team 3 (2/5), got team %d", bestByProduct[1].TeamID)
	}
	if bestByProduct[2].TeamID != 9 {
		t.Fatalf("product 2 should keep team 9, got team %d", bestByProduct[2].TeamID)
	}
}
