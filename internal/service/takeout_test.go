package service

import (
	"strings"
	"testing"

	"yujixinjiang/backend/internal/model"
)

func TestTakeoutPayAmount(t *testing.T) {
	got := computeTakeoutPayAmount(12.5, 2, 3.0)
	if got != 28.0 {
		t.Fatalf("got %v", got)
	}
}

func TestComputeTakeoutPayAmountRounding(t *testing.T) {
	got := computeTakeoutPayAmount(9.99, 3, 2.5)
	want := roundMoney(9.99*3 + 2.5)
	if got != want {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestGenTakeoutOrderNoPrefix(t *testing.T) {
	no := genTakeoutOrderNo()
	if !strings.HasPrefix(no, "T") {
		t.Fatalf("order no %q should start with T", no)
	}
	if len(no) < 20 {
		t.Fatalf("order no too short: %q", no)
	}
}

func TestTakeoutStatusMeta(t *testing.T) {
	text, code := takeoutStatusMeta(model.TakeoutStatusPreparing)
	if text != "配餐中" || code != "preparing" {
		t.Fatalf("got %q %q", text, code)
	}
}
