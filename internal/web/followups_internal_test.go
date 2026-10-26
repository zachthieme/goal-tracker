package web

import (
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// TestFollowUpChipsTakeTheirSignalsNameFromTheDefinitions checks that a chip
// showing its signal's plain name leads with the name followUpDefinitions
// gives it, so renaming a signal there renames its table chip too (#387).
func TestFollowUpChipsTakeTheirSignalsNameFromTheDefinitions(t *testing.T) {
	t.Parallel()
	today := time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)
	g := domain.Goal{CadenceDays: 7}
	for _, kind := range []string{"asks-for-help", "checkin-overdue", "ownerless", "unlinked"} {
		name := followUpName(kind)
		if name == "" {
			t.Fatalf("followUpDefinitions has no %q", kind)
		}
		var b strings.Builder
		if err := followUpChip(today, g, domain.FollowUpSignal{Kind: kind, Days: 3}).Render(t.Context(), &b); err != nil {
			t.Fatalf("render %s chip: %v", kind, err)
		}
		label := b.String()
		open := strings.Index(label, "<button")
		if open < 0 {
			t.Fatalf("%s chip has no button:\n%s", kind, label)
		}
		text := label[strings.Index(label[open:], ">")+open+1:]
		if !strings.HasPrefix(text, name) {
			t.Errorf("%s chip's button reads %q, want it to start with its definition's name %q", kind, text[:min(len(text), 60)], name)
		}
	}
	if followUpName("no-such-signal") != "" {
		t.Error(`followUpName("no-such-signal") isn't empty`)
	}
}

// A row's Fix picks among the actions its row offers, reading each right from
// the row as Service.FollowUps judged it, in the Fix's order: Reassign; for an
// overdue Goal Check in, the disabled "Nudged today by", or Nudge; Link to a
// parent; for a Schedule conflict Check in, its dates open, on the first
// candidate taking an ordinary Check-in, Open Goal on the first whose
// Check-ins the viewer writes, or else the disabled "Nudged today by" or
// Nudge; then Open parent, Suggest a parent, See Action Items and Open Goal.
func TestFollowUpFixPicksAmongWhatTheRowOffers(t *testing.T) {
	t.Parallel()
	goal := domain.Goal{ID: 1, Title: "Ship search"}
	launch := domain.Goal{ID: 2, Title: "Launch"}
	beta := domain.Goal{ID: 3, Title: "beta program"}
	paused := domain.Goal{ID: 4, Title: "Paused outcome"}
	asleep := domain.Goal{ID: 5, Title: "asleep outcome"}
	checksIn := domain.GoalPermissions{CheckinPermissions: domain.CheckinPermissions{CheckIn: true, KeepLifecycle: true}, WritesCheckins: true}
	// onHold is what the Owner or a Delegate of an On Hold Goal may do with
	// its Check-ins: write one, but only to move it on.
	onHold := domain.GoalPermissions{CheckinPermissions: domain.CheckinPermissions{CheckIn: true}, WritesCheckins: true}
	morgan := &domain.Nudge{Sender: domain.Account{Name: "Morgan Lee"}}
	sig := func(kind string) domain.FollowUpSignal { return domain.FollowUpSignal{Kind: kind} }
	conflict := func(parent domain.Goal, perms domain.GoalPermissions) domain.FollowUpSignal {
		return domain.FollowUpSignal{Kind: "schedule-conflicts", Days: 30, Related: &parent, RelatedPermissions: perms}
	}
	halted := func(parent domain.Goal) domain.FollowUpSignal {
		return domain.FollowUpSignal{Kind: "halted-parents", Related: &parent}
	}
	for _, c := range []struct {
		name string
		row  domain.FollowUpRow
		want followUpFix
	}{
		{"an Ownerless Goal an Admin may reassign",
			domain.FollowUpRow{Signals: []domain.FollowUpSignal{sig("checkin-overdue"), sig("ownerless")}, Nudge: true, Permissions: domain.GoalPermissions{Reassign: true}},
			followUpFix{Label: "Reassign", Href: "/goals/1?open=reassign"}},
		{"an Ownerless overdue Goal someone else may only nudge",
			domain.FollowUpRow{Signals: []domain.FollowUpSignal{sig("checkin-overdue"), sig("ownerless")}, Nudge: true},
			followUpFix{Label: "Nudge", Href: "/goals/1/nudge", Post: true}},
		{"an overdue Goal the viewer may check in on",
			domain.FollowUpRow{Signals: []domain.FollowUpSignal{sig("checkin-overdue"), sig("unlinked")}, Permissions: checksIn},
			followUpFix{Label: "Check in", Href: "/goals/1/checkin", Primary: true}},
		{"an overdue Goal nudged today",
			domain.FollowUpRow{Signals: []domain.FollowUpSignal{sig("milestone-overdue")}, Nudge: true, NudgedToday: morgan},
			followUpFix{Label: "Nudged today by Morgan Lee", Disabled: true}},
		{"an overdue Goal the viewer may nudge",
			domain.FollowUpRow{Signals: []domain.FollowUpSignal{sig("path-overdue")}, Nudge: true},
			followUpFix{Label: "Nudge", Href: "/goals/1/nudge", Post: true}},
		{"an overdue Goal nobody is left to check in on",
			domain.FollowUpRow{Signals: []domain.FollowUpSignal{sig("checkin-overdue")}, Unreachable: true},
			followUpFix{Label: "Open Goal", Href: "/goals/1"}},
		{"an Unlinked Goal the viewer may link",
			domain.FollowUpRow{Signals: []domain.FollowUpSignal{sig("unlinked"), conflict(launch, checksIn)}, Permissions: domain.GoalPermissions{LinkParent: true, SuggestParent: true}},
			followUpFix{Label: "Link to a parent", Href: "/goals/1?open=parent-link"}},
		{"a Schedule conflict the viewer may check in on",
			domain.FollowUpRow{Signals: []domain.FollowUpSignal{conflict(launch, checksIn)}, Permissions: checksIn},
			followUpFix{Label: "Check in", Href: "/goals/1/checkin?open=dates", Primary: true}},
		{"a Schedule conflict whose Active parent the viewer may check in on",
			domain.FollowUpRow{Signals: []domain.FollowUpSignal{conflict(launch, checksIn)}, Nudge: true},
			followUpFix{Label: "Check in", Href: "/goals/2/checkin?open=dates", Primary: true}},
		{"a Schedule conflict with two parents the viewer may check in on, the first by title",
			domain.FollowUpRow{Signals: []domain.FollowUpSignal{conflict(launch, checksIn), conflict(beta, checksIn)}},
			followUpFix{Label: "Check in", Href: "/goals/3/checkin?open=dates", Primary: true}},
		{"a Schedule conflict on an On Hold Goal the viewer writes for, with an Active parent they may check in on",
			domain.FollowUpRow{Signals: []domain.FollowUpSignal{conflict(launch, checksIn)}, Permissions: onHold},
			followUpFix{Label: "Check in", Href: "/goals/2/checkin?open=dates", Primary: true}},
		{"a Schedule conflict whose only candidate the viewer writes for is On Hold",
			domain.FollowUpRow{Signals: []domain.FollowUpSignal{conflict(launch, onHold)}, Nudge: true},
			followUpFix{Label: "Open Goal", Href: "/goals/2"}},
		{"a Schedule conflict the viewer may only nudge",
			domain.FollowUpRow{Signals: []domain.FollowUpSignal{conflict(launch, domain.GoalPermissions{SuggestParent: true})}, Nudge: true},
			followUpFix{Label: "Nudge", Href: "/goals/1/nudge", Post: true}},
		{"a Schedule conflict nudged today",
			domain.FollowUpRow{Signals: []domain.FollowUpSignal{conflict(launch, domain.GoalPermissions{})}, Nudge: true, NudgedToday: morgan},
			followUpFix{Label: "Nudged today by Morgan Lee", Disabled: true}},
		{"a Schedule conflict nobody is left to check in on, nudged today, under a halted parent",
			domain.FollowUpRow{Signals: []domain.FollowUpSignal{conflict(launch, domain.GoalPermissions{}), halted(paused)}, Unreachable: true, NudgedToday: morgan},
			followUpFix{Label: "Open parent", Href: "/goals/4"}},
		{"a Goal under two halted parents, the first by title",
			domain.FollowUpRow{Signals: []domain.FollowUpSignal{halted(paused), halted(asleep), sig("unlinked")}, Permissions: domain.GoalPermissions{SuggestParent: true}},
			followUpFix{Label: "Open parent", Href: "/goals/5"}},
		{"an Unlinked Goal the viewer may suggest a parent for",
			domain.FollowUpRow{Signals: []domain.FollowUpSignal{sig("unlinked"), sig("action-item-overdue")}, Permissions: domain.GoalPermissions{SuggestParent: true}},
			followUpFix{Label: "Suggest a parent", Href: "/goals/1/suggest-parent"}},
		{"an Unlinked Goal with an overdue Action Item the viewer may not suggest a parent for",
			domain.FollowUpRow{Signals: []domain.FollowUpSignal{sig("unlinked"), sig("action-item-overdue")}},
			followUpFix{Label: "See Action Items", Href: "/goals/1#action-items"}},
		{"a Goal flagged for its Health alone",
			domain.FollowUpRow{Signals: []domain.FollowUpSignal{sig("red")}, Permissions: checksIn},
			followUpFix{Label: "Open Goal", Href: "/goals/1"}},
	} {
		c.row.Goal = goal
		if got := (followUpGoalRow{FollowUpRow: c.row}).fix(); got != c.want {
			t.Errorf("%s: Fix = %+v, want %+v", c.name, got, c.want)
		}
	}
}
