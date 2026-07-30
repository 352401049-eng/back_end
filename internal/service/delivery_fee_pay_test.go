package service

import (
	"encoding/json"
	"strings"
	"testing"

	"yujixinjiang/backend/internal/model"
)

func TestGenDeliveryFeeOrderNoPrefix(t *testing.T) {
	no := genDeliveryFeeOrderNo()
	if !strings.HasPrefix(no, "F") {
		t.Fatalf("order no %q should start with F", no)
	}
	if len(no) < 20 {
		t.Fatalf("order no too short: %q", no)
	}
}

func TestDeliveryFeeStatusMeta(t *testing.T) {
	text, code := deliveryFeeStatusMeta(model.DeliveryFeeStatusPendingPay)
	if text != "待支付" || code != "pending_pay" {
		t.Fatalf("got %q %q", text, code)
	}
}

func TestDecodeDeliveryFeePayload(t *testing.T) {
	raw, err := json.Marshal(deliveryFeePayload{
		Items: []UseBatchItemInput{{InventoryID: 9, Quantity: 2}},
		AddressID: 3,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeDeliveryFeePayload(raw)
	if err != nil {
		t.Fatal(err)
	}
	if got.AddressID != 3 || len(got.Items) != 1 || got.Items[0].InventoryID != 9 {
		t.Fatalf("unexpected payload: %+v", got)
	}
}
