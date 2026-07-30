package service

import (
	"testing"
	"time"

	"yujixinjiang/backend/internal/model"
)

func TestAttachRiderContact_NilRider(t *testing.T) {
	view := &DeliveryView{}
	attachRiderContact(nil, view)
	attachRiderContact(nil, nil)
	viewNil := (*DeliveryView)(nil)
	attachRiderContact(nil, viewNil)
}

func TestAdminResolveResume_StatusChoice(t *testing.T) {
	now := time.Now()
	tests := []struct {
		name string
		d    *model.DeliveryOrder
		want uint8
	}{
		{"no started_at -> Accepted", &model.DeliveryOrder{}, model.DeliveryAccepted},
		{"started_at set -> Delivering", &model.DeliveryOrder{StartedAt: &now}, model.DeliveryDelivering},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := resumeTargetStatus(tt.d); got != tt.want {
				t.Fatalf("resumeTargetStatus() = %d, want %d", got, tt.want)
			}
		})
	}
}
