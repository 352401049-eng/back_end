package service

import (
	"errors"
	"strings"
	"testing"

	"yujixinjiang/backend/internal/model"
)

func TestValidateGroupBuyOrderInput(t *testing.T) {
	teamID := uint64(99)

	tests := []struct {
		name         string
		quantity     uint32
		groupBuyTeam *uint64
		startNewTeam bool
		wantErr      string
	}{
		{"qty ok join team", 1, &teamID, false, ""},
		{"qty ok start new", 1, nil, true, ""},
		{"qty too many", 2, nil, true, "拼团每次只能购买 1 件"},
		{"neither team nor new", 1, nil, false, "请选择拼团或开新团"},
		{"both team and new", 1, &teamID, true, "不能同时指定参团与开新团"},
		{"zero team id treated as absent", 1, func() *uint64 { v := uint64(0); return &v }(), false, "请选择拼团或开新团"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateGroupBuyOrderInput(tc.quantity, tc.groupBuyTeam, tc.startNewTeam)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, ErrGroupBuyInvalid) {
				t.Fatalf("expected ErrGroupBuyInvalid, got %v", err)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected message containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestAssertActivityGroupBuyOnly(t *testing.T) {
	enabled := &ActivityOrderContext{
		ActivityProduct: &model.ActivityProduct{EnableGroupBuy: 1},
	}
	disabled := &ActivityOrderContext{
		ActivityProduct: &model.ActivityProduct{EnableGroupBuy: 0},
	}

	tests := []struct {
		name         string
		purchaseType uint8
		actCtx       *ActivityOrderContext
		wantErr      bool
	}{
		{"group allowed", model.PurchaseTypeGroup, enabled, false},
		{"solo rejected when group only", model.PurchaseTypeSolo, enabled, true},
		{"solo ok when not group only", model.PurchaseTypeSolo, disabled, false},
		{"nil context ok", model.PurchaseTypeSolo, nil, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := assertActivityGroupBuyOnly(tc.purchaseType, tc.actCtx)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				if !errors.Is(err, ErrGroupBuyInvalid) {
					t.Fatalf("expected ErrGroupBuyInvalid, got %v", err)
				}
				if !strings.Contains(err.Error(), "该活动商品仅支持拼团购买") {
					t.Fatalf("unexpected message: %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestValidateTeamJoinLimitForcesDistinct(t *testing.T) {
	if err := validateTeamJoinLimit(1, 0, 1); err == nil {
		t.Fatal("expected error when same user rejoins with allow_repeat forced off")
	}
	if err := validateTeamJoinLimit(0, 0, 1); err != nil {
		t.Fatal(err)
	}
}
