package domain_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

func TestAddMilestoneListsUnderTheGoal(t *testing.T) {
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "Customers wait too long for v2.")

	date := time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC)
	m, err := h.Service.AddMilestone(context.Background(), domain.AddMilestoneInput{
		GoalID:     g.ID,
		Name:       "Beta cut",
		TargetDate: date,
	})
	if err != nil {
		t.Fatalf("AddMilestone: %v", err)
	}
	if m.ID == 0 {
		t.Error("want a persisted Milestone with a non-zero ID")
	}
	if m.Name != "Beta cut" || !m.TargetDate.Equal(date) {
		t.Errorf("Milestone = %+v", m)
	}

	list, err := h.Service.ListMilestones(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("ListMilestones: %v", err)
	}
	if len(list) != 1 || list[0].Name != "Beta cut" {
		t.Errorf("ListMilestones = %+v", list)
	}
}

func TestAddMilestoneRequiresNameAndDate(t *testing.T) {
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "why")

	cases := map[string]domain.AddMilestoneInput{
		"no name": {GoalID: g.ID, Name: "  ", TargetDate: time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC)},
		"no date": {GoalID: g.ID, Name: "Beta cut", TargetDate: time.Time{}},
	}
	for name, in := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := h.Service.AddMilestone(context.Background(), in); !errors.Is(err, domain.ErrValidation) {
				t.Errorf("err = %v, want ErrValidation", err)
			}
		})
	}
}

func TestEditMilestoneChangesNameAndDate(t *testing.T) {
	h := testsupport.New(t)
	owner := h.SignIn("sam@example.com")
	g := h.CreateGoal(owner, "Ship v2", "why")
	m, err := h.Service.AddMilestone(context.Background(), domain.AddMilestoneInput{
		GoalID:     g.ID,
		Name:       "Beta cut",
		TargetDate: time.Date(2026, 3, 16, 0, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("AddMilestone: %v", err)
	}

	newDate := time.Date(2026, 4, 20, 0, 0, 0, 0, time.UTC)
	got, err := h.Service.EditMilestone(context.Background(), domain.EditMilestoneInput{
		MilestoneID: m.ID,
		Name:        "GA cut",
		TargetDate:  newDate,
	})
	if err != nil {
		t.Fatalf("EditMilestone: %v", err)
	}
	if got.Name != "GA cut" || !got.TargetDate.Equal(newDate) {
		t.Errorf("EditMilestone = %+v", got)
	}
}
