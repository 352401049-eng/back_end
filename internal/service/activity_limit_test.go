package service

import (
	"testing"
	"time"
)

func TestCalendarWindow_Day(t *testing.T) {
	now := time.Date(2026, 7, 21, 15, 30, 0, 0, time.Local)
	start, end := calendarWindow(now, "day")

	wantStart := time.Date(2026, 7, 21, 0, 0, 0, 0, time.Local)
	wantEnd := time.Date(2026, 7, 22, 0, 0, 0, 0, time.Local)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("day window: got [%v, %v), want [%v, %v)", start, end, wantStart, wantEnd)
	}
}

func TestCalendarWindow_Week_MondayStart(t *testing.T) {
	// 2026-07-21 is Tuesday → week [Mon 07-20, Mon 07-27)
	now := time.Date(2026, 7, 21, 15, 30, 0, 0, time.Local)
	start, end := calendarWindow(now, "week")

	wantStart := time.Date(2026, 7, 20, 0, 0, 0, 0, time.Local)
	wantEnd := time.Date(2026, 7, 27, 0, 0, 0, 0, time.Local)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("week window (Tue): got [%v, %v), want [%v, %v)", start, end, wantStart, wantEnd)
	}

	// Sunday still belongs to the same Mon–Sun week
	sun := time.Date(2026, 7, 26, 23, 59, 59, 0, time.Local)
	start, end = calendarWindow(sun, "week")
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("week window (Sun): got [%v, %v), want [%v, %v)", start, end, wantStart, wantEnd)
	}

	// Monday is the start of a new week
	mon := time.Date(2026, 7, 27, 0, 0, 0, 0, time.Local)
	start, end = calendarWindow(mon, "week")
	wantStart = time.Date(2026, 7, 27, 0, 0, 0, 0, time.Local)
	wantEnd = time.Date(2026, 8, 3, 0, 0, 0, 0, time.Local)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("week window (Mon): got [%v, %v), want [%v, %v)", start, end, wantStart, wantEnd)
	}
}

func TestCalendarWindow_Month(t *testing.T) {
	now := time.Date(2026, 7, 21, 15, 30, 0, 0, time.Local)
	start, end := calendarWindow(now, "month")

	wantStart := time.Date(2026, 7, 1, 0, 0, 0, 0, time.Local)
	wantEnd := time.Date(2026, 8, 1, 0, 0, 0, 0, time.Local)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("month window: got [%v, %v), want [%v, %v)", start, end, wantStart, wantEnd)
	}

	// Dec → Jan next year
	dec := time.Date(2026, 12, 15, 10, 0, 0, 0, time.Local)
	start, end = calendarWindow(dec, "month")
	wantStart = time.Date(2026, 12, 1, 0, 0, 0, 0, time.Local)
	wantEnd = time.Date(2027, 1, 1, 0, 0, 0, 0, time.Local)
	if !start.Equal(wantStart) || !end.Equal(wantEnd) {
		t.Fatalf("month window (Dec): got [%v, %v), want [%v, %v)", start, end, wantStart, wantEnd)
	}
}

func TestRegisterDeadline(t *testing.T) {
	createdAt := time.Date(2026, 7, 21, 10, 0, 0, 0, time.Local)
	got := registerDeadline(createdAt, 24)
	want := createdAt.Add(24 * time.Hour)
	if !got.Equal(want) {
		t.Fatalf("registerDeadline: got %v, want %v", got, want)
	}
	if !registerDeadline(createdAt, 0).Equal(createdAt) {
		t.Fatal("registerDeadline hours=0 should equal createdAt")
	}
}

func TestInRegisterWindow(t *testing.T) {
	createdAt := time.Date(2026, 7, 21, 10, 0, 0, 0, time.Local)
	hours := uint32(24)
	deadline := createdAt.Add(24 * time.Hour)

	cases := []struct {
		name string
		now  time.Time
		want bool
	}{
		{"at start inclusive", createdAt, true},
		{"mid window", createdAt.Add(12 * time.Hour), true},
		{"at end exclusive", deadline, false},
		{"before created", createdAt.Add(-time.Second), false},
		{"after end", deadline.Add(time.Second), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := inRegisterWindow(createdAt, tc.now, hours); got != tc.want {
				t.Fatalf("inRegisterWindow(%v): got %v, want %v", tc.now, got, tc.want)
			}
		})
	}

	if inRegisterWindow(createdAt, createdAt.Add(time.Hour), 0) {
		t.Fatal("hours=0 should yield empty window (false)")
	}
}
