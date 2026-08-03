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

func TestApplyTakeoutDisplayMetaMerchantReject(t *testing.T) {
	reason := "商家拒单：今日已打烊"
	view := &TakeoutView{
		TakeoutOrder: model.TakeoutOrder{
			Status:    model.TakeoutStatusCancelled,
			PayStatus: model.PayStatusRefunding,
			Remark:    &reason,
		},
		StatusText: "已取消",
		StatusCode: "cancelled",
	}
	applyTakeoutDisplayMeta(view)
	if view.StatusCode != "merchant_rejected" || view.StatusText != "商家拒单" {
		t.Fatalf("status=%q/%q", view.StatusText, view.StatusCode)
	}
	if view.RejectReason != reason {
		t.Fatalf("reject reason=%q", view.RejectReason)
	}
	if view.RefundStatusText != "退款中" {
		t.Fatalf("refund=%q", view.RefundStatusText)
	}
}

func TestApplyTakeoutDisplayMetaUserCancel(t *testing.T) {
	view := &TakeoutView{
		TakeoutOrder: model.TakeoutOrder{Status: model.TakeoutStatusCancelled},
		StatusText:   "已取消",
		StatusCode:   "cancelled",
	}
	applyTakeoutDisplayMeta(view)
	if view.StatusCode != "cancelled" || view.RejectReason != "" {
		t.Fatalf("got code=%q reason=%q", view.StatusCode, view.RejectReason)
	}
}

func TestParseMerchantTakeoutStatusFilter(t *testing.T) {
	st, err := parseMerchantTakeoutStatusFilter("preparing")
	if err != nil || st == nil || *st != model.TakeoutStatusPreparing {
		t.Fatalf("preparing: st=%v err=%v", st, err)
	}
	st, err = parseMerchantTakeoutStatusFilter("")
	if err != nil || st != nil {
		t.Fatalf("empty: st=%v err=%v", st, err)
	}
	_, err = parseMerchantTakeoutStatusFilter("bogus")
	if err == nil {
		t.Fatal("expected error for bogus status")
	}
}

func TestDecodeTakeoutPackageUnits(t *testing.T) {
	raw := []byte(`[{"package_selections":[{"group_id":1,"items":[{"product_id":2,"qty":1}]}]}]`)
	units, err := decodeTakeoutPackageUnits(raw, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 1 || len(units[0]) != 1 || units[0][0].GroupID != 1 {
		t.Fatalf("unexpected units: %+v", units)
	}

	legacy := []byte(`[{"group_id":3,"items":[{"product_id":4,"qty":2}]}]`)
	units, err = decodeTakeoutPackageUnits(legacy, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(units) != 2 {
		t.Fatalf("want 2 units, got %d", len(units))
	}
}
