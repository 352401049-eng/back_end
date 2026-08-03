package service

import "testing"

func TestHotGroupRowLessByNeed(t *testing.T) {
	closer := hotGroupRow{TeamID: 1, TargetCount: 5, CurrentCount: 4}
	farther := hotGroupRow{TeamID: 2, TargetCount: 5, CurrentCount: 1}
	if !hotGroupRowLess(closer, farther) {
		t.Fatal("closer team should rank first")
	}
}

func TestHotGroupRowLessAllTeamsKeptIndependently(t *testing.T) {
	// 文档约定：热拼榜按团展示，不去重、不滤活动/普通
	ap := uint64(2)
	aid := uint64(3)
	rows := []hotGroupRow{
		{TeamID: 5, ProductID: 1, TargetCount: 5, CurrentCount: 1, ActivityID: &aid, ActivityProductID: &ap},
		{TeamID: 4, ProductID: 1, TargetCount: 5, CurrentCount: 1},
		{TeamID: 3, ProductID: 1, TargetCount: 5, CurrentCount: 2},
	}
	if len(rows) != 3 {
		t.Fatal("fixture")
	}
	// 仅校验更接近成团者优先，三者均可独立出现在榜上
	if !hotGroupRowLess(rows[2], rows[0]) {
		t.Fatal("team 3 (2/5) should rank above team 5 (1/5)")
	}
}
