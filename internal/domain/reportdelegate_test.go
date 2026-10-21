package domain_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// reportDelegateWorld is a Report Definition Alice created, with Boss an
// Admin, Dana a Report Delegate on it and Sam an unrelated signed-in person.
type reportDelegateWorld struct {
	h                      *testsupport.Harness
	boss, alice, dana, sam domain.Account
	def                    domain.ReportDefinition
}

func newReportDelegateWorld(t *testing.T) reportDelegateWorld {
	t.Helper()
	h := testsupport.New(t, "boss@example.com")
	w := reportDelegateWorld{
		h:     h,
		boss:  h.SignIn("boss@example.com"),
		alice: h.SignIn("alice@example.com"),
		dana:  h.SignIn("dana@example.com"),
		sam:   h.SignIn("sam@example.com"),
	}
	g := h.ActiveGoal(w.alice, "Grow revenue", "Revenue funds the roadmap.")
	w.def = h.SaveReportDefinition(w.alice, domain.SaveReportDefinitionInput{
		Name: "Alice's MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID},
	})
	h.AddReportDelegate(w.alice, w.dana, w.def)
	return w
}

func reportDelegateEmails(t *testing.T, h *testsupport.Harness, def domain.ReportDefinition) []string {
	t.Helper()
	delegates, err := h.Service.ListReportDelegates(context.Background(), def.ID)
	if err != nil {
		t.Fatalf("ListReportDelegates: %v", err)
	}
	emails := make([]string, 0, len(delegates))
	for _, d := range delegates {
		emails = append(emails, d.Email)
	}
	return emails
}

// The creator and an Admin add and remove a Definition's Report Delegates; a
// Report Delegate can't, nor can anyone else (CONTEXT.md: Report Delegate).
func TestOnlyTheCreatorOrAnAdminAddsAndRemovesReportDelegates(t *testing.T) {
	t.Parallel()

	w := newReportDelegateWorld(t)
	ctx := context.Background()
	h := w.h
	lee := h.SignIn("lee@example.com")

	if got := reportDelegateEmails(t, h, w.def); len(got) != 1 || got[0] != "dana@example.com" {
		t.Fatalf("Report Delegates = %v, want [dana@example.com]", got)
	}
	for _, who := range []struct {
		name string
		as   domain.Account
	}{{"a Report Delegate", w.dana}, {"anyone else", w.sam}} {
		if err := h.Service.AddReportDelegate(ctx, who.as.ID, w.def.ID, lee.ID); !errors.Is(err, domain.ErrNotAuthorized) {
			t.Errorf("%s adding a Report Delegate: err = %v, want ErrNotAuthorized", who.name, err)
		}
		if err := h.Service.RemoveReportDelegate(ctx, who.as.ID, w.def.ID, w.dana.ID); !errors.Is(err, domain.ErrNotAuthorized) {
			t.Errorf("%s removing a Report Delegate: err = %v, want ErrNotAuthorized", who.name, err)
		}
	}
	if got := reportDelegateEmails(t, h, w.def); len(got) != 1 || got[0] != "dana@example.com" {
		t.Fatalf("after refused changes the Report Delegates = %v, want [dana@example.com]", got)
	}

	if err := h.Service.AddReportDelegateByEmail(ctx, w.boss.ID, w.def.ID, " lee@example.com "); err != nil {
		t.Fatalf("an Admin adding a Report Delegate: %v", err)
	}
	if got := reportDelegateEmails(t, h, w.def); len(got) != 2 || got[0] != "dana@example.com" || got[1] != "lee@example.com" {
		t.Fatalf("Report Delegates = %v, want Dana and Lee", got)
	}
	if err := h.Service.RemoveReportDelegate(ctx, w.alice.ID, w.def.ID, w.dana.ID); err != nil {
		t.Fatalf("the creator removing a Report Delegate: %v", err)
	}
	if err := h.Service.RemoveReportDelegate(ctx, w.boss.ID, w.def.ID, lee.ID); err != nil {
		t.Fatalf("an Admin removing a Report Delegate: %v", err)
	}
	if got := reportDelegateEmails(t, h, w.def); len(got) != 0 {
		t.Fatalf("Report Delegates = %v, want none", got)
	}
}

// Adding the creator, someone already a Report Delegate, a Departed person or
// an email with no Account is refused, as for a Goal's Delegates.
func TestAddReportDelegateRefusals(t *testing.T) {
	t.Parallel()

	w := newReportDelegateWorld(t)
	ctx := context.Background()
	h := w.h
	gone := h.SignIn("gone@example.com")
	if err := h.Service.MarkDeparted(ctx, w.boss.ID, gone.ID); err != nil {
		t.Fatalf("MarkDeparted: %v", err)
	}

	for _, c := range []struct {
		name string
		id   int64
	}{{"the creator", w.alice.ID}, {"a Report Delegate again", w.dana.ID}, {"a Departed person", gone.ID}, {"no such Account", gone.ID + 100}} {
		if err := h.Service.AddReportDelegate(ctx, w.alice.ID, w.def.ID, c.id); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("adding %s: err = %v, want ErrValidation", c.name, err)
		}
	}
	if err := h.Service.AddReportDelegateByEmail(ctx, w.alice.ID, w.def.ID, "nobody@example.com"); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("adding an unknown email: err = %v, want ErrValidation", err)
	}
	if err := h.Service.AddReportDelegate(ctx, w.alice.ID, w.def.ID+100, w.sam.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("adding to no such Definition: err = %v, want ErrNotFound", err)
	}
	if got := reportDelegateEmails(t, h, w.def); len(got) != 1 || got[0] != "dana@example.com" {
		t.Errorf("Report Delegates = %v, want only Dana", got)
	}
}

// Only a Definition's creator, its Report Delegates and Admins may change it,
// curate its narrative or publish it; anyone else signed in is refused with
// ErrNotAuthorized and nothing changes (CONTEXT.md: Report Definition).
func TestOnlyTheCreatorAReportDelegateOrAnAdminManagesAReport(t *testing.T) {
	t.Parallel()

	w := newReportDelegateWorld(t)
	ctx := context.Background()
	h := w.h
	edit := func(name string) domain.SaveReportDefinitionInput {
		return domain.SaveReportDefinitionInput{Name: name, Mode: domain.ReportModePicked, Picked: w.def.Picked}
	}
	notes := func(note string) domain.CurateNarrativeInput {
		return domain.CurateNarrativeInput{Notes: map[string][]string{domain.HighlightInsight: {note}}}
	}

	if _, err := h.Service.UpdateReportDefinition(ctx, w.sam.ID, w.def.ID, edit("Sam's now")); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("anyone else editing: err = %v, want ErrNotAuthorized", err)
	}
	if err := h.Service.CurateNarrative(ctx, w.sam.ID, w.def.ID, notes("Sam's note")); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("anyone else curating: err = %v, want ErrNotAuthorized", err)
	}
	if _, err := h.Service.PublishReport(ctx, w.sam.ID, w.def.ID, time.Time{}); !errors.Is(err, domain.ErrNotAuthorized) {
		t.Errorf("anyone else publishing: err = %v, want ErrNotAuthorized", err)
	}
	if got, _ := h.Service.GetReportDefinition(ctx, w.def.ID); got.Name != "Alice's MBR" {
		t.Errorf("after refused edit the name = %q, want Alice's MBR", got.Name)
	}
	if pubs, _ := h.Service.ListPublications(ctx, w.def.ID); len(pubs) != 0 {
		t.Errorf("after refused publish there are %d publications, want none", len(pubs))
	}

	for i, who := range []struct {
		name string
		as   domain.Account
	}{{"the creator", w.alice}, {"a Report Delegate", w.dana}, {"an Admin", w.boss}} {
		name := who.name + "'s MBR"
		if _, err := h.Service.UpdateReportDefinition(ctx, who.as.ID, w.def.ID, edit(name)); err != nil {
			t.Errorf("%s editing: %v", who.name, err)
		}
		if err := h.Service.CurateNarrative(ctx, who.as.ID, w.def.ID, notes(who.name+" notes")); err != nil {
			t.Errorf("%s curating: %v", who.name, err)
		}
		pub, err := h.Service.PublishReport(ctx, who.as.ID, w.def.ID, time.Time{})
		if err != nil {
			t.Fatalf("%s publishing: %v", who.name, err)
		}
		if pub.PublishedBy.ID != who.as.ID || pub.Report.Definition.Name != name {
			t.Errorf("publication %d: by %d named %q, want by %s named %q", i, pub.PublishedBy.ID, pub.Report.Definition.Name, who.name, name)
		}
	}
}
