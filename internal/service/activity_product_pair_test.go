package service

import (
	"testing"
)

// TestFindActivityProductPairQueryIsActivityScoped documents the uniqueness rule:
// duplicate is only within the same activity_id + product_id pair, not globally.
func TestFindActivityProductPairQueryIsActivityScoped(t *testing.T) {
	const want = "activity_id = ? AND product_id = ?"
	got := activityProductPairWhereClause()
	if got != want {
		t.Fatalf("pair lookup must be activity-scoped: got %q want %q", got, want)
	}
}
