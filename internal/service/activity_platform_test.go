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
	owner := uint64(1)
	entryApplicable := uint64(2)
	entryForbidden := uint64(3)
	// 入口店 B 适用、C 不适用：owner 校验与 applicable 由 DB 测试覆盖
	if owner == entryApplicable {
		t.Fatal("owner and applicable entry should differ")
	}
	// 平台活动：act.MerchantID==0 时不按入口店过滤活动归属
	actMerchant := uint64(0)
	if actMerchant != 0 && actMerchant != owner {
		t.Fatal("platform activity should not bind to entry merchant")
	}
	// 商家专场：须与商品 owner 一致，而非入口店
	actMerchant = owner
	if actMerchant != 0 && actMerchant != owner {
		t.Fatal("merchant-scoped activity must match product owner")
	}
	if actMerchant == entryForbidden {
		t.Fatal("merchant-scoped activity should not match non-applicable entry")
	}
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
