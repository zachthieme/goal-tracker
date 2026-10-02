package domain_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// One table edit changes the values of three Goals — two its author owns and
// one they are a Delegate on — and each change is in its Goal's history
// (#80; CONTEXT.md: Delegate, Field).
func TestEditGoalValuesSavesEveryGoalWithHistory(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Trust")
	growth, trust := pillar.Values[0], pillar.Values[1]
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	alpha := h.CreateGoal(sam, "Alpha", "A matters.")
	beta := h.CreateGoal(sam, "Beta", "B matters.")
	gamma := h.CreateGoal(pat, "Gamma", "C matters.")
	h.AddDelegate(pat, sam, gamma.ID)
	ctx := context.Background()
	if err := h.Service.AssignGoalValue(ctx, boss.ID, beta.ID, growth.ID); err != nil {
		t.Fatalf("AssignGoalValue: %v", err)
	}

	err := h.Service.EditGoalValues(ctx, sam.ID, []domain.ValueEdit{
		{GoalID: alpha.ID, DimensionID: pillar.ID, ValueIDs: []int64{growth.ID}},
		{GoalID: beta.ID, FieldID: budget.ID, Value: "1200"},
		{GoalID: gamma.ID, DimensionID: pillar.ID, ValueIDs: []int64{trust.ID}},
		{GoalID: gamma.ID, FieldID: budget.ID, Value: "50"},
	})
	if err != nil {
		t.Fatalf("EditGoalValues: %v", err)
	}

	if got := goalValueNames(t, h, alpha.ID); len(got) != 1 || got[0] != "Growth" {
		t.Errorf("Alpha's Pillar = %q, want Growth", got)
	}
	if got := goalFieldValues(t, h, beta.ID)["Budget"]; got != "1200" {
		t.Errorf("Beta's Budget = %q, want 1200", got)
	}
	if got := goalValueNames(t, h, beta.ID); len(got) != 1 || got[0] != "Growth" {
		t.Errorf("Beta's Pillar = %q, want Growth untouched", got)
	}
	if got := goalValueNames(t, h, gamma.ID); len(got) != 1 || got[0] != "Trust" {
		t.Errorf("Gamma's Pillar = %q, want Trust", got)
	}
	if got := goalFieldValues(t, h, gamma.ID)["Budget"]; got != "50" {
		t.Errorf("Gamma's Budget = %q, want 50", got)
	}

	for _, tc := range []struct {
		goal domain.Goal
		want []string
	}{
		{alpha, []string{"Pillar:→Growth"}},
		{beta, []string{"Budget:→1200"}},
		{gamma, []string{"Pillar:→Trust", "Budget:→50"}},
	} {
		goal, want := tc.goal, tc.want
		var got []string
		for _, c := range valueHistory(t, h, goal.ID) {
			if c.Actor.ID == sam.ID {
				got = append(got, c.Attribute+":"+c.Before+"→"+c.After)
			}
		}
		if len(got) != len(want) {
			t.Errorf("%s's history by Sam = %q, want %q", goal.Title, got, want)
			continue
		}
		for i := range want {
			if got[i] != want[i] {
				t.Errorf("%s's history by Sam = %q, want %q", goal.Title, got, want)
			}
		}
	}
}

// A table edit is all or nothing: when any cell is invalid nothing is saved,
// and the refusal names every bad cell by its Goal and Field or Dimension, each
// with the reason the Goal page would give (#80).
func TestEditGoalValuesWithABadCellSavesNothing(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pillar := h.CreateDimension(boss, "Pillar", "Growth", "Trust")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	start := h.CreateField(boss, "Start", domain.FieldDate, "")
	alpha := h.CreateGoal(sam, "Alpha", "A matters.")
	beta := h.CreateGoal(sam, "Beta", "B matters.")

	err := h.Service.EditGoalValues(context.Background(), sam.ID, []domain.ValueEdit{
		{GoalID: alpha.ID, DimensionID: pillar.ID, ValueIDs: []int64{pillar.Values[0].ID}},
		{GoalID: alpha.ID, FieldID: budget.ID, Value: "1200"},
		{GoalID: beta.ID, FieldID: budget.ID, Value: "lots"},
		{GoalID: beta.ID, FieldID: start.ID, Value: "next week"},
	})

	var refusal *domain.ValueEditError
	if !errors.As(err, &refusal) || !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v, want a *ValueEditError that is an ErrValidation", err)
	}
	if len(refusal.Cells) != 2 {
		t.Fatalf("bad cells = %+v, want Beta's Budget and Start", refusal.Cells)
	}
	for i, want := range []struct {
		fieldID int64
		says    string
	}{{budget.ID, "Budget takes a number"}, {start.ID, "Start takes a date"}} {
		got := refusal.Cells[i]
		if got.GoalID != beta.ID || got.FieldID != want.fieldID || got.DimensionID != 0 || !strings.Contains(got.Message, want.says) {
			t.Errorf("bad cell %d = %+v, want Beta's Field %d saying %q", i, got, want.fieldID, want.says)
		}
	}
	if got := goalValueNames(t, h, alpha.ID); len(got) != 0 {
		t.Errorf("Alpha's Pillar = %q, want nothing saved", got)
	}
	if got := goalFieldValues(t, h, alpha.ID); len(got) != 0 {
		t.Errorf("Alpha's Fields = %v, want nothing saved", got)
	}
	if got := valueHistory(t, h, alpha.ID); len(got) != 0 {
		t.Errorf("Alpha's history = %+v, want nothing recorded", got)
	}
}

// A table edit touching a Goal its author may not set values on — not its
// Owner, a Delegate or an Admin — is refused naming that Goal, and nothing in
// it is saved, the other Goals' cells included (#80; CONTEXT.md: Delegate).
func TestEditGoalValuesOnAGoalTheAuthorCantEditSavesNothing(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	pat := h.SignIn("pat@example.com")
	budget := h.CreateField(boss, "Budget", domain.FieldNumber, "$")
	mine := h.CreateGoal(sam, "Mine", "M matters.")
	theirs := h.CreateGoal(pat, "Theirs", "T matters.")

	err := h.Service.EditGoalValues(context.Background(), sam.ID, []domain.ValueEdit{
		{GoalID: mine.ID, FieldID: budget.ID, Value: "10"},
		{GoalID: theirs.ID, FieldID: budget.ID, Value: "lots"},
	})

	if !errors.Is(err, domain.ErrNotAuthorized) || !strings.Contains(err.Error(), "Theirs") {
		t.Fatalf("err = %v, want ErrNotAuthorized naming Theirs", err)
	}
	for _, g := range []domain.Goal{mine, theirs} {
		if got := goalFieldValues(t, h, g.ID); len(got) != 0 {
			t.Errorf("%s's Fields = %v, want nothing saved", g.Title, got)
		}
	}
}

// A cell in an Extendable Dimension can take a value typed in, added to the
// list first when it is new, as the Goal page's add does: in a one-value
// Dimension it replaces the value chosen, and in a several-values one it joins
// the values ticked (#80; CONTEXT.md: Extendable).
func TestEditGoalValuesAddsTypedValuesToExtendableLists(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	sponsor := h.CreateExtendableDimension(boss, "Sponsor", "Ana")
	tags := h.CreateExtendableDimension(boss, "Tags", "infra", "ux")
	h.SetDimensionSelection(boss, tags, domain.SelectionSeveral)
	goal := h.CreateGoal(sam, "Alpha", "A matters.")

	err := h.Service.EditGoalValues(context.Background(), sam.ID, []domain.ValueEdit{
		{GoalID: goal.ID, DimensionID: sponsor.ID, ValueIDs: []int64{sponsor.Values[0].ID}, NewValue: "Dana"},
		{GoalID: goal.ID, DimensionID: tags.ID, ValueIDs: []int64{tags.Values[1].ID}, NewValue: "Infra "},
	})
	if err != nil {
		t.Fatalf("EditGoalValues: %v", err)
	}

	if got := strings.Join(goalValueNames(t, h, goal.ID), ", "); got != "Dana, infra, ux" {
		t.Errorf("Alpha's values = %q, want Dana, infra, ux", got)
	}
	dims, err := h.Service.ListDimensions(context.Background())
	if err != nil {
		t.Fatalf("ListDimensions: %v", err)
	}
	for _, d := range dims {
		var names []string
		for _, v := range d.Values {
			names = append(names, v.Value)
		}
		want := map[string]string{"Sponsor": "Ana, Dana", "Tags": "infra, ux"}[d.Name]
		if got := strings.Join(names, ", "); got != want {
			t.Errorf("%s's list = %q, want %q", d.Name, got, want)
		}
	}
}

// A value typed into a Fixed list by someone not an Admin, or matching a
// Retired value, is a bad cell, as on the Goal page, so nothing is saved and
// no list grows (#80; CONTEXT.md: Fixed, Retired).
func TestEditGoalValuesRefusesTypedValuesTheGoalPageRefuses(t *testing.T) {
	h := testsupport.New(t, "boss@example.com")
	boss := h.SignIn("boss@example.com")
	sam := h.SignIn("sam@example.com")
	ctx := context.Background()
	pillar := h.CreateDimension(boss, "Pillar", "Growth")
	sponsor := h.CreateExtendableDimension(boss, "Sponsor", "Ana", "Old Co")
	if err := h.Service.RetireDimensionValue(ctx, boss.ID, sponsor.Values[1].ID); err != nil {
		t.Fatalf("RetireDimensionValue: %v", err)
	}
	team := h.CreateExtendableDimension(boss, "Team", "Core")
	goal := h.CreateGoal(sam, "Alpha", "A matters.")

	err := h.Service.EditGoalValues(ctx, sam.ID, []domain.ValueEdit{
		{GoalID: goal.ID, DimensionID: team.ID, NewValue: "Edge"},
		{GoalID: goal.ID, DimensionID: pillar.ID, NewValue: "Moonshot"},
		{GoalID: goal.ID, DimensionID: sponsor.ID, NewValue: "old co"},
	})

	var refusal *domain.ValueEditError
	if !errors.As(err, &refusal) {
		t.Fatalf("err = %v, want a *ValueEditError", err)
	}
	var got []string
	for _, c := range refusal.Cells {
		got = append(got, fmt.Sprintf("%d: %s", c.DimensionID, c.Message))
	}
	want := []string{
		fmt.Sprintf("%d: Pillar is a Fixed list, so only an Admin may add to it", pillar.ID),
		fmt.Sprintf("%d: Old Co is retired, so it can't be added or newly assigned", sponsor.ID),
	}
	if !slices.Equal(got, want) {
		t.Errorf("bad cells = %q, want %q", got, want)
	}
	if got := goalValueNames(t, h, goal.ID); len(got) != 0 {
		t.Errorf("Alpha's values = %q, want nothing saved", got)
	}
	dims, err := h.Service.ListDimensions(ctx)
	if err != nil {
		t.Fatalf("ListDimensions: %v", err)
	}
	for _, d := range dims {
		if d.ID == team.ID && len(d.Values) != 1 {
			t.Errorf("Team's list = %+v, want Edge not added", d.Values)
		}
	}
}
