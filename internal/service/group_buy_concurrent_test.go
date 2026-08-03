package service

import "testing"

func TestCanStartNewTeam(t *testing.T) {
	tests := []struct {
		name     string
		max      uint32
		current  uint32
		want     bool
	}{
		{"unlimited zero max", 0, 99, true},
		{"under limit", 2, 1, true},
		{"at limit", 2, 2, false},
		{"over limit", 1, 3, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := canStartNewTeam(tc.max, tc.current); got != tc.want {
				t.Fatalf("canStartNewTeam(%d,%d)=%v want %v", tc.max, tc.current, got, tc.want)
			}
		})
	}
}
