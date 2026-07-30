package service

import (
	"errors"
	"testing"

	"yujixinjiang/backend/internal/model"
)

func TestReplaceOptionGroups_RejectsPackage(t *testing.T) {
	err := validateOptionGroupsForSave(model.ProductItemTypePackage, []OptionGroupInput{{Title: "糖分", Items: []OptionItemInput{{Label: "全糖"}, {Label: "无糖"}}}})
	if !errors.Is(err, ErrOptionNotAllowedOnPackage) {
		t.Fatalf("got %v", err)
	}
}

func TestValidateOptionGroupsForSave_NeedsTwoItems(t *testing.T) {
	err := validateOptionGroupsForSave(model.ProductItemTypePhysical, []OptionGroupInput{{Title: "糖分", Items: []OptionItemInput{{Label: "全糖"}}}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestValidateOptionGroupsForSave_EmptyGroupsOK(t *testing.T) {
	if err := validateOptionGroupsForSave(model.ProductItemTypePhysical, nil); err != nil {
		t.Fatalf("got %v", err)
	}
}

func TestValidateOptionGroupsForSave_DuplicateLabel(t *testing.T) {
	err := validateOptionGroupsForSave(model.ProductItemTypePhysical, []OptionGroupInput{{
		Title: "糖分",
		Items: []OptionItemInput{{Label: "全糖"}, {Label: "全糖"}},
	}})
	if !errors.Is(err, ErrOptionInvalid) {
		t.Fatalf("got %v", err)
	}
}

func TestValidateOptionGroupsForSave_TooManyGroups(t *testing.T) {
	groups := make([]OptionGroupInput, 11)
	for i := range groups {
		groups[i] = OptionGroupInput{
			Title: "组",
			Items: []OptionItemInput{{Label: "A"}, {Label: "B"}},
		}
	}
	err := validateOptionGroupsForSave(model.ProductItemTypePhysical, groups)
	if !errors.Is(err, ErrOptionInvalid) {
		t.Fatalf("got %v", err)
	}
}

func TestOptionSnapshot_RequiresEveryGroup(t *testing.T) {
	groups := []model.ProductOptionGroup{
		{
			ID:    1,
			Title: "糖分",
			Items: []model.ProductOptionItem{{ID: 10, GroupID: 1, Label: "全糖"}, {ID: 11, GroupID: 1, Label: "无糖"}},
		},
		{
			ID:    2,
			Title: "温度",
			Items: []model.ProductOptionItem{{ID: 20, GroupID: 2, Label: "热"}, {ID: 21, GroupID: 2, Label: "冰"}},
		},
	}

	_, err := validateSelectionAgainstGroups(100, "奶茶", groups, 1, []OptionSelectionUnitInput{{
		UnitIndex: 1,
		Groups:    []OptionSelectionGroupInput{{GroupID: 1, OptionID: 10}},
	}})
	if !errors.Is(err, ErrOptionRequired) {
		t.Fatalf("missing group: got %v", err)
	}

	_, err = validateSelectionAgainstGroups(100, "奶茶", groups, 2, []OptionSelectionUnitInput{{
		UnitIndex: 1,
		Groups: []OptionSelectionGroupInput{
			{GroupID: 1, OptionID: 10},
			{GroupID: 2, OptionID: 20},
		},
	}})
	if !errors.Is(err, ErrOptionRequired) {
		t.Fatalf("missing unit: got %v", err)
	}
}

func TestOptionSnapshot_ValidSelection(t *testing.T) {
	groups := []model.ProductOptionGroup{
		{
			ID:    1,
			Title: "糖分",
			Items: []model.ProductOptionItem{{ID: 10, GroupID: 1, Label: "全糖"}, {ID: 11, GroupID: 1, Label: "无糖"}},
		},
	}
	snap, err := validateSelectionAgainstGroups(100, "奶茶", groups, 1, []OptionSelectionUnitInput{{
		UnitIndex: 1,
		Groups:    []OptionSelectionGroupInput{{GroupID: 1, OptionID: 11}},
	}})
	if err != nil {
		t.Fatalf("got %v", err)
	}
	if len(snap) != 1 {
		t.Fatalf("len=%d", len(snap))
	}
	if snap[0].UnitIndex != 1 || snap[0].ProductID != 100 || snap[0].ProductName != "奶茶" {
		t.Fatalf("unit snap: %+v", snap[0])
	}
	if len(snap[0].Groups) != 1 || snap[0].Groups[0].OptionLabel != "无糖" {
		t.Fatalf("groups snap: %+v", snap[0].Groups)
	}
}

func TestOptionSnapshot_InvalidOptionID(t *testing.T) {
	groups := []model.ProductOptionGroup{{
		ID:    1,
		Title: "糖分",
		Items: []model.ProductOptionItem{{ID: 10, GroupID: 1, Label: "全糖"}, {ID: 11, GroupID: 1, Label: "无糖"}},
	}}
	_, err := validateSelectionAgainstGroups(100, "奶茶", groups, 1, []OptionSelectionUnitInput{{
		UnitIndex: 1,
		Groups:    []OptionSelectionGroupInput{{GroupID: 1, OptionID: 99}},
	}})
	if !errors.Is(err, ErrOptionInvalid) {
		t.Fatalf("got %v", err)
	}
}

func TestOptionSnapshot_NoGroupsEmptyUnitsOK(t *testing.T) {
	snap, err := validateSelectionAgainstGroups(100, "奶茶", nil, 1, nil)
	if err != nil {
		t.Fatalf("got %v", err)
	}
	if len(snap) != 0 {
		t.Fatalf("len=%d", err)
	}
}
