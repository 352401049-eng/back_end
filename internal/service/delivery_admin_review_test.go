package service

import (
	"testing"

	"yujixinjiang/backend/internal/model"
)

func TestDeliveryStatusText_PendingAdminReview(t *testing.T) {
	if got := model.DeliveryStatusText(model.DeliveryPendingAdminReview); got != "待平台审核" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatBagAdminRejectReason(t *testing.T) {
	tests := []struct {
		key, remark, want string
		wantErr           bool
	}{
		{"unclear_address", "", "平台拒绝：地址不清", false},
		{"out_of_zone", "太远", "平台拒绝：超区｜太远", false},
		{"unreachable", "", "平台拒绝：联系不上", false},
		{"other", "特殊情况", "平台拒绝：其它｜特殊情况", false},
		{"other", "  ", "", true},
		{"other", "", "", true},
		{"nope", "", "", true},
	}
	for _, tt := range tests {
		got, err := FormatBagAdminRejectReason(tt.key, tt.remark)
		if tt.wantErr {
			if err == nil {
				t.Fatalf("key=%s expected err", tt.key)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Fatalf("key=%s got=%q err=%v want=%q", tt.key, got, err, tt.want)
		}
	}
}
