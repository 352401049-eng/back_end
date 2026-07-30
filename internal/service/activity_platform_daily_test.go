package service

import (
	"testing"
	"time"

	"yujixinjiang/backend/internal/model"
)

func TestPlatformDailyBucketKey_BeforeRefresh(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 7, 30, 9, 30, 0, 0, loc)
	key := PlatformDailyBucketKey("10:00:00", now)
	if key != "2026-07-29" {
		t.Fatalf("got %s want 2026-07-29", key)
	}
}

func TestPlatformDailyBucketKey_AfterRefresh(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 7, 30, 10, 0, 0, 0, loc)
	key := PlatformDailyBucketKey("10:00", now)
	if key != "2026-07-30" {
		t.Fatalf("got %s want 2026-07-30", key)
	}
}

func TestNormalizeDailyRefreshTime(t *testing.T) {
	s, err := NormalizeDailyRefreshTime("10:00")
	if err != nil || s != "10:00:00" {
		t.Fatalf("got %q %v", s, err)
	}
	s, err = NormalizeDailyRefreshTime("")
	if err != nil || s != "00:00:00" {
		t.Fatalf("got %q %v", s, err)
	}
	if _, err := NormalizeDailyRefreshTime("25:00"); err == nil {
		t.Fatal("expected error")
	}
}

func TestPlatformDailyRemaining(t *testing.T) {
	ap := &model.ActivityProduct{PlatformDailyMax: 10, PlatformDailySold: 3}
	if platformDailyRemaining(ap) != 7 {
		t.Fatalf("remain=%d", platformDailyRemaining(ap))
	}
	ap.PlatformDailySold = 10
	if platformDailyRemaining(ap) != 0 {
		t.Fatal("want 0")
	}
	ap.PlatformDailyMax = 0
	if platformDailyRemaining(ap) != ^uint32(0) {
		t.Fatal("disabled should be max uint")
	}
}

func TestComputeActivityRemaining_PlatformDailyVirtualReset(t *testing.T) {
	loc := time.FixedZone("CST", 8*3600)
	now := time.Date(2026, 7, 30, 11, 0, 0, 0, loc)
	ap := &model.ActivityProduct{
		PlatformDailyMax:    10,
		PlatformDailySold:   10,
		PlatformDailyBucket: "2026-07-29", // stale bucket → treat sold as 0
		DailyRefreshTime:    "10:00:00",
	}
	out, err := computeActivityRemaining(nil, ap, 100, nil, time.Time{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if out.RemainingQty != 10 {
		t.Fatalf("remain=%d want 10 after virtual reset", out.RemainingQty)
	}

	ap.PlatformDailyBucket = "2026-07-30"
	out, err = computeActivityRemaining(nil, ap, 100, nil, time.Time{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if out.RemainingQty != 0 || !out.LimitReached || out.LimitReason != "platform_daily" {
		t.Fatalf("got remain=%d reached=%v reason=%s", out.RemainingQty, out.LimitReached, out.LimitReason)
	}
}
