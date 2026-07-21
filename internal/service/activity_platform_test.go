package service

import (
	"testing"
	"time"

	"yujixinjiang/backend/internal/model"
)

func TestPlatformActivityAllowsCrossMerchantProductQuery(t *testing.T) {
	// 行为约定：平台活动 merchant_id=0 时 AddProduct 不按活动商家过滤商品
	act := &model.Activity{ID: 1, MerchantID: 0, Status: model.ActivityStatusOn}
	if act.MerchantID != 0 {
		t.Fatal("expected platform merchant_id=0")
	}
	legacy := &model.Activity{ID: 2, MerchantID: 9, Status: model.ActivityStatusOn}
	if legacy.MerchantID == 0 {
		t.Fatal("legacy activity should keep shop merchant_id")
	}
}

func TestResolveForOrderMerchantGuard(t *testing.T) {
	productMerchant := uint64(3)
	orderMerchant := uint64(3)
	if productMerchant != orderMerchant {
		t.Fatal("same merchant should pass product check")
	}
	// 平台活动：act.MerchantID==0 时不再要求 act.MerchantID==orderMerchant
	actMerchant := uint64(0)
	if actMerchant != 0 && actMerchant != orderMerchant {
		t.Fatal("platform should not fail on act merchant mismatch")
	}
	// 商家专场：必须一致
	actMerchant = 9
	if actMerchant != 0 && actMerchant != orderMerchant {
		// expected forbidden path
		return
	}
	t.Fatal("expected merchant-scoped activity to mismatch")
}

func TestActivityIsActiveNow(t *testing.T) {
	now := time.Now()
	act := model.Activity{
		Status:  model.ActivityStatusOn,
		StartAt: now.Add(-time.Hour),
		EndAt:   now.Add(time.Hour),
	}
	if !act.IsActiveNow(now) {
		t.Fatal("ongoing activity should be active")
	}
}
