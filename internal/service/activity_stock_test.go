package service

import (
	"testing"

	"yujixinjiang/backend/internal/model"
)

func TestAvailableActivityStockUsesChannelStock(t *testing.T) {
	ap := &model.ActivityProduct{ActivityStock: 0, SoldCount: 0}
	p := &model.Product{Stock: 0, DealStock: 97, GroupStock: 1}
	if got := availableActivityStock(ap, p); got != 97 {
		t.Fatalf("unlimited activity stock should use max channel stock, got %d", got)
	}
}

func TestAvailableActivityStockClampsByActivityRemain(t *testing.T) {
	ap := &model.ActivityProduct{ActivityStock: 10, SoldCount: 3}
	p := &model.Product{Stock: 0, DealStock: 97, GroupStock: 1}
	if got := availableActivityStock(ap, p); got != 7 {
		t.Fatalf("expected activity remain 7, got %d", got)
	}
}

func TestAvailableActivityStockWhenProductChannelsEmpty(t *testing.T) {
	ap := &model.ActivityProduct{ActivityStock: 20, SoldCount: 5}
	p := &model.Product{Stock: 0, DealStock: 0, GroupStock: 0}
	if got := availableActivityStock(ap, p); got != 15 {
		t.Fatalf("expected activity-only remain 15, got %d", got)
	}
}
