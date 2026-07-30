package service

import (
	"sort"
	"testing"
	"time"
)

type joinableTeamSortRow struct {
	ID           uint64
	CurrentCount uint32
	ExpireAt     time.Time
}

func joinableTeamLess(a, b joinableTeamSortRow) bool {
	if a.CurrentCount != b.CurrentCount {
		return a.CurrentCount > b.CurrentCount
	}
	if !a.ExpireAt.Equal(b.ExpireAt) {
		return a.ExpireAt.Before(b.ExpireAt)
	}
	return a.ID < b.ID
}

func TestJoinableTeamSortOrder(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	rows := []joinableTeamSortRow{
		{ID: 3, CurrentCount: 1, ExpireAt: now.Add(2 * time.Hour)},
		{ID: 1, CurrentCount: 2, ExpireAt: now.Add(3 * time.Hour)},
		{ID: 2, CurrentCount: 2, ExpireAt: now.Add(1 * time.Hour)},
		{ID: 4, CurrentCount: 1, ExpireAt: now.Add(1 * time.Hour)},
	}
	sort.SliceStable(rows, func(i, j int) bool {
		return joinableTeamLess(rows[i], rows[j])
	})
	wantIDs := []uint64{2, 1, 4, 3}
	for i, id := range wantIDs {
		if rows[i].ID != id {
			t.Fatalf("index %d: got id %d want %d", i, rows[i].ID, id)
		}
	}
}
