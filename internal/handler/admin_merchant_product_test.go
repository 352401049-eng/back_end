package handler

import (
	"testing"

	"yujixinjiang/backend/internal/model"
)

func TestBuildProductInputApplicableMerchantIDs(t *testing.T) {
	ids := []uint64{1, 2}
	req := ProductRequest{
		MerchantID:            1,
		Name:                  "跨店商品",
		CoverURL:              "https://example.com/cover.jpg",
		Price:                 10,
		ApplicableMerchantIDs: &ids,
	}
	input := buildProductInput(req, nil)
	if len(input.ApplicableMerchantIDs) != 2 || input.ApplicableMerchantIDs[0] != 1 || input.ApplicableMerchantIDs[1] != 2 {
		t.Fatalf("want applicable [1 2], got %v", input.ApplicableMerchantIDs)
	}
}

func TestBuildProductInputNilApplicableDefaultsEmpty(t *testing.T) {
	req := ProductRequest{
		MerchantID: 1,
		Name:       "默认适用店主",
		CoverURL:   "https://example.com/cover.jpg",
		Price:      10,
	}
	input := buildProductInput(req, nil)
	if input.ApplicableMerchantIDs != nil {
		t.Fatalf("want nil applicable slice for default owner-only seed, got %v", input.ApplicableMerchantIDs)
	}
}

func TestBuildPatchProductInputApplicableMerchantIDs(t *testing.T) {
	ids := []uint64{1, 3}
	req := UpdateProductRequest{ApplicableMerchantIDs: &ids}
	existing := &model.Product{MerchantID: 1, CategoryID: 2, Name: "x", CoverURL: "c", Price: 1}
	input := buildPatchProductInput(req, existing)
	if !input.HasApplicableMerchantIDs {
		t.Fatal("expected HasApplicableMerchantIDs")
	}
	if len(input.ApplicableMerchantIDs) != 2 || input.ApplicableMerchantIDs[1] != 3 {
		t.Fatalf("want applicable [1 3], got %v", input.ApplicableMerchantIDs)
	}
}

func TestBuildPatchProductInputNilApplicableKeepsExisting(t *testing.T) {
	req := UpdateProductRequest{Name: strPtr("新名")}
	existing := &model.Product{MerchantID: 1, CategoryID: 2, Name: "x", CoverURL: "c", Price: 1}
	input := buildPatchProductInput(req, existing)
	if input.HasApplicableMerchantIDs {
		t.Fatal("nil applicable_merchant_ids must not replace existing rows")
	}
}

func TestUpdateProductRequestHasFieldApplicable(t *testing.T) {
	ids := []uint64{2}
	req := UpdateProductRequest{ApplicableMerchantIDs: &ids}
	if !req.hasField() {
		t.Fatal("applicable_merchant_ids alone should count as a patch field")
	}
}

func strPtr(s string) *string { return &s }
