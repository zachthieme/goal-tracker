package domain_test

import (
	"context"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// A Goal's own dates are judged in its Owner's Timezone, and an Ownerless
// Goal, or one whose Owner has none, in the company's (CONTEXT.md: Timezone).
// The zone follows the Goal's current Owner, so after a Handoff the new
// Owner's applies.
func TestGoalTimezoneIsTheOwnersOrElseTheCompanys(t *testing.T) {
	t.Parallel()

	la, err := time.LoadLocation("America/Los_Angeles")
	if err != nil {
		t.Fatalf("LoadLocation: %v", err)
	}
	h := testsupport.New(t, "admin@example.com")
	h.Service = domain.NewService(h.DB, h.Clock, h.Email, []string{"admin@example.com"}, domain.WithTimezone(la))
	ctx := context.Background()
	admin := h.SignIn("admin@example.com")
	kenji := h.SignIn("kenji@example.com")
	sam := h.SignIn("sam@example.com")
	lena := h.SignIn("lena@example.com")
	h.SetTimezone(kenji, "Asia/Tokyo")
	h.SetTimezone(lena, "Europe/Berlin")
	tokyoGoal := h.ActiveGoal(kenji, "Ship search", "People can't find things.")
	plainGoal := h.ActiveGoal(sam, "Reduce outages", "Outages cost trust.")

	zoneOf := func(goalID int64) string {
		t.Helper()
		g, err := h.Service.ViewGoal(ctx, goalID)
		if err != nil {
			t.Fatalf("ViewGoal: %v", err)
		}
		return h.Service.GoalTimezone(g).String()
	}

	if got := zoneOf(tokyoGoal.ID); got != "Asia/Tokyo" {
		t.Errorf("GoalTimezone of a Goal whose Owner is in Tokyo = %s, want Asia/Tokyo", got)
	}
	if got := zoneOf(plainGoal.ID); got != "America/Los_Angeles" {
		t.Errorf("GoalTimezone of a Goal whose Owner has no Timezone = %s, want the company's, America/Los_Angeles", got)
	}

	if err := h.Service.MarkDeparted(ctx, admin.ID, kenji.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}
	if got := zoneOf(tokyoGoal.ID); got != "America/Los_Angeles" {
		t.Errorf("GoalTimezone of the Ownerless Goal = %s, want the company's, America/Los_Angeles", got)
	}

	if _, err := h.Service.ReassignGoal(ctx, admin.ID, tokyoGoal.ID, lena.ID); err != nil {
		t.Fatalf("ReassignGoal: %v", err)
	}
	if got := zoneOf(tokyoGoal.ID); got != "Europe/Berlin" {
		t.Errorf("GoalTimezone after reassigning to an Owner in Berlin = %s, want Europe/Berlin", got)
	}
}
