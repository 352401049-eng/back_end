package service

import (
	"slices"
	"testing"
	"time"

	"yujixinjiang/backend/internal/model"
)

func TestCalendarWindowAt_Day_WithRefresh(t *testing.T) {
	loc := time.Local
	// Wed 2026-07-30 13:00, refresh 12:00 → [today 12:00, tomorrow 12:00)
	now := time.Date(2026, 7, 30, 13, 0, 0, 0, loc)
	start, end := calendarWindowAt(now, "day", "12:00:00")

	wantStart := time.Date(2026, 7, 30, 12, 0, 0, 0, loc)
	wantEnd := time.Date(2026, 7, 31, 12, 0, 0, 0, loc)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("day window after refresh: got [%v, %v), want [%v, %v)", start, end, wantStart, wantEnd)
	}

	// Same day but before refresh → previous day window
	now = time.Date(2026, 7, 30, 11, 0, 0, 0, loc)
	start, end = calendarWindowAt(now, "day", "12:00:00")
	wantStart = time.Date(2026, 7, 29, 12, 0, 0, 0, loc)
	wantEnd = time.Date(2026, 7, 30, 12, 0, 0, 0, loc)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("day window before refresh: got [%v, %v), want [%v, %v)", start, end, wantStart, wantEnd)
	}
}

func TestCalendarWindowAt_Week_MondayRefresh(t *testing.T) {
	loc := time.Local
	// Tue 2026-07-21 13:00, refresh 12:00 → week [Mon 07-20 12:00, Mon 07-27 12:00)
	now := time.Date(2026, 7, 21, 13, 0, 0, 0, loc)
	start, end := calendarWindowAt(now, "week", "12:00:00")

	wantStart := time.Date(2026, 7, 20, 12, 0, 0, 0, loc)
	wantEnd := time.Date(2026, 7, 27, 12, 0, 0, 0, loc)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("week window (Tue): got [%v, %v), want [%v, %v)", start, end, wantStart, wantEnd)
	}

	// Monday before refresh → previous week
	mon := time.Date(2026, 7, 20, 11, 0, 0, 0, loc)
	start, end = calendarWindowAt(mon, "week", "12:00:00")
	wantStart = time.Date(2026, 7, 13, 12, 0, 0, 0, loc)
	wantEnd = time.Date(2026, 7, 20, 12, 0, 0, 0, loc)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("week window (Mon before refresh): got [%v, %v), want [%v, %v)", start, end, wantStart, wantEnd)
	}
}

func TestCalendarWindowAt_Month_FirstDayRefresh(t *testing.T) {
	loc := time.Local
	now := time.Date(2026, 7, 21, 15, 30, 0, 0, loc)
	start, end := calendarWindowAt(now, "month", "10:00:00")

	wantStart := time.Date(2026, 7, 1, 10, 0, 0, 0, loc)
	wantEnd := time.Date(2026, 8, 1, 10, 0, 0, 0, loc)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("month window: got [%v, %v), want [%v, %v)", start, end, wantStart, wantEnd)
	}

	// Dec 1 before refresh → November window
	dec := time.Date(2026, 12, 1, 9, 0, 0, 0, loc)
	start, end = calendarWindowAt(dec, "month", "10:00:00")
	wantStart = time.Date(2026, 11, 1, 10, 0, 0, 0, loc)
	wantEnd = time.Date(2026, 12, 1, 10, 0, 0, 0, loc)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("month window (before refresh on 1st): got [%v, %v), want [%v, %v)", start, end, wantStart, wantEnd)
	}
}

func TestCalendarWindowAt_InvalidRefreshFallsBackMidnight(t *testing.T) {
	now := time.Date(2026, 7, 21, 15, 30, 0, 0, time.Local)
	start, end := calendarWindowAt(now, "day", "invalid")
	wantStart := time.Date(2026, 7, 21, 0, 0, 0, 0, time.Local)
	wantEnd := time.Date(2026, 7, 22, 0, 0, 0, 0, time.Local)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("invalid refresh fallback: got [%v, %v), want [%v, %v)", start, end, wantStart, wantEnd)
	}
}

func TestSumBoughtQtyExcludesGroupFailed(t *testing.T) {
	if !slices.Contains(orderStatusesExcludedFromBoughtQty, int(model.OrderStatusCancelled)) {
		t.Fatal("excluded statuses must include Cancelled")
	}
	if !slices.Contains(orderStatusesExcludedFromBoughtQty, int(model.OrderStatusGroupFailed)) {
		t.Fatal("excluded statuses must include GroupFailed")
	}
	if len(orderStatusesExcludedFromBoughtQty) != 2 {
		t.Fatalf("expected exactly 2 excluded statuses, got %v", orderStatusesExcludedFromBoughtQty)
	}
}

func TestOrderStatusesExcludedAreIntSlice(t *testing.T) {
	// Regression: []uint8 is bound by GORM as a single []byte → MySQL 1064 near '?'.
	var v any = orderStatusesExcludedFromBoughtQty
	if _, ok := v.([]int); !ok {
		t.Fatalf("orderStatusesExcludedFromBoughtQty must be []int for GORM IN clause, got %T", v)
	}
}
