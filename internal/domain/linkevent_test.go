package domain_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// linkEvents returns g's link events, oldest first, failing the test on error.
func linkEvents(t *testing.T, h *testsupport.Harness, g domain.Goal) []domain.LinkEvent {
	t.Helper()
	events, err := h.Service.LinkEvents(context.Background(), g.ID)
	if err != nil {
		t.Fatalf("LinkEvents: %v", err)
	}
	return events
}

// A request to someone else's Goal is recorded as requested, by the
// requester, now, and is read from either Goal with the other one named.
func TestRequestLinkRecordsARequestOnBothGoals(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	_, sam, child, parent, _ := pendingAcrossOwners(t, h)

	got := linkEvents(t, h, child)
	if len(got) != 1 {
		t.Fatalf("child's link events = %+v, want one", got)
	}
	e := got[0]
	if e.Kind != domain.LinkEventRequested || e.Actor.ID != sam.ID || !e.CreatedAt.Equal(h.Clock.Now()) {
		t.Errorf("event = %+v, want requested by Sam, now", e)
	}
	if e.ChildID != child.ID || e.ParentID != parent.ID || !e.OnChild() {
		t.Errorf("event = %+v, want child %d to parent %d, read on the child", e, child.ID, parent.ID)
	}
	if e.OtherID != parent.ID || e.OtherTitle != "Reduce outages" {
		t.Errorf("other Goal = %d %q, want the parent", e.OtherID, e.OtherTitle)
	}

	got = linkEvents(t, h, parent)
	if len(got) != 1 || got[0].ID != e.ID || got[0].OnChild() || got[0].OtherID != child.ID || got[0].OtherTitle != "Migrate displays" {
		t.Errorf("parent's link events = %+v, want the same event, read on the parent, naming the child", got)
	}
}

// eventsOf sums up events as "kind by actor-email", oldest first.
func eventsOf(events []domain.LinkEvent) []string {
	out := make([]string, 0, len(events))
	for _, e := range events {
		out = append(out, e.Kind+" by "+e.Actor.Email)
	}
	return out
}

// A request to a Goal the requester owns too is accepted at once, so it is
// recorded as linked, not requested.
func TestRequestLinkToYourOwnGoalRecordsLinked(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	sam := h.SignIn("sam@example.com")
	parent := h.CreateGoal(sam, "Reduce outages", "Outages cost trust.")
	child := h.CreateGoal(sam, "Migrate displays", "Old displays fail often.")
	h.RequestLink(sam, child, parent, "")

	for _, g := range []domain.Goal{child, parent} {
		if got, want := eventsOf(linkEvents(t, h, g)), []string{"linked by sam@example.com"}; !slices.Equal(got, want) {
			t.Errorf("%s's link events = %v, want %v", g.Title, got, want)
		}
	}
}

// Accepting a request is recorded on both Goals as accepted, by the parent's
// Owner, at the time they accepted it.
func TestAcceptLinkRecordsTheAcceptance(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	pat, _, child, parent, link := pendingAcrossOwners(t, h)
	h.Clock.Advance(time.Hour)
	if _, err := h.Service.AcceptLink(context.Background(), link.ID, pat.ID); err != nil {
		t.Fatalf("AcceptLink: %v", err)
	}

	want := []string{"requested by sam@example.com", "accepted by pat@example.com"}
	for _, g := range []domain.Goal{child, parent} {
		events := linkEvents(t, h, g)
		if got := eventsOf(events); !slices.Equal(got, want) {
			t.Fatalf("%s's link events = %v, want %v", g.Title, got, want)
		}
		if !events[1].CreatedAt.Equal(h.Clock.Now()) {
			t.Errorf("accepted at %v, want %v", events[1].CreatedAt, h.Clock.Now())
		}
	}
}

// Rejecting a request and undoing the rejection are each recorded on both
// Goals, by the parent's Owner who did both.
func TestRejectLinkAndItsUndoAreRecorded(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := testsupport.New(t)
	pat, _, child, parent, link := pendingAcrossOwners(t, h)
	h.Clock.Advance(time.Hour)
	rejection, err := h.Service.RejectLink(ctx, link.ID, pat.ID)
	if err != nil {
		t.Fatalf("RejectLink: %v", err)
	}
	want := []string{"requested by sam@example.com", "rejected by pat@example.com"}
	if got := eventsOf(linkEvents(t, h, child)); !slices.Equal(got, want) {
		t.Fatalf("after the rejection, link events = %v, want %v", got, want)
	}

	h.Clock.Advance(time.Minute)
	if _, err := h.Service.RestoreLinkRequest(ctx, rejection.ID, pat.ID, rejection.UndoToken); err != nil {
		t.Fatalf("RestoreLinkRequest: %v", err)
	}
	want = append(want, "rejection-undone by pat@example.com")
	for _, g := range []domain.Goal{child, parent} {
		events := linkEvents(t, h, g)
		if got := eventsOf(events); !slices.Equal(got, want) {
			t.Fatalf("%s's link events = %v, want %v", g.Title, got, want)
		}
		if !events[2].CreatedAt.Equal(h.Clock.Now()) {
			t.Errorf("undone at %v, want %v", events[2].CreatedAt, h.Clock.Now())
		}
	}
}

// Removing a link and undoing the removal are each recorded on both Goals, by
// whoever removed it, who is the only one who may undo it.
func TestRemoveLinkAndItsUndoAreRecorded(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := testsupport.New(t)
	_, sam, child, parent, link := acceptedAcrossOwners(t, h)
	h.Clock.Advance(time.Hour)
	removal, err := h.Service.RemoveLink(ctx, link.ID, sam.ID)
	if err != nil {
		t.Fatalf("RemoveLink: %v", err)
	}
	want := []string{"requested by sam@example.com", "accepted by pat@example.com", "removed by sam@example.com"}
	if got := eventsOf(linkEvents(t, h, parent)); !slices.Equal(got, want) {
		t.Fatalf("after the removal, link events = %v, want %v", got, want)
	}

	h.Clock.Advance(time.Minute)
	if _, err := h.Service.RestoreLink(ctx, removal.ID, sam.ID, removal.UndoToken); err != nil {
		t.Fatalf("RestoreLink: %v", err)
	}
	want = append(want, "removal-undone by sam@example.com")
	for _, g := range []domain.Goal{child, parent} {
		events := linkEvents(t, h, g)
		if got := eventsOf(events); !slices.Equal(got, want) {
			t.Fatalf("%s's link events = %v, want %v", g.Title, got, want)
		}
		if !events[3].CreatedAt.Equal(h.Clock.Now()) {
			t.Errorf("undone at %v, want %v", events[3].CreatedAt, h.Clock.Now())
		}
	}
}

// An imported link is accepted at once, so it is recorded as linked, by the
// child's Owner it is attributed to.
func TestImportLinkRecordsLinkedByTheChildsOwner(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	pat := h.SignIn("pat@example.com")
	sam := h.SignIn("sam@example.com")
	parent := h.CreateGoal(pat, "Reduce outages", "Outages cost trust.")
	child := h.CreateGoal(sam, "Migrate displays", "Old displays fail often.")
	if _, err := h.Service.ImportLink(context.Background(), child.ID, parent.ID); err != nil {
		t.Fatalf("ImportLink: %v", err)
	}

	for _, g := range []domain.Goal{child, parent} {
		if got, want := eventsOf(linkEvents(t, h, g)), []string{"linked by sam@example.com"}; !slices.Equal(got, want) {
			t.Errorf("%s's link events = %v, want %v", g.Title, got, want)
		}
	}
}

// A refused link command records nothing on either Goal: one by someone who
// may not make it, a duplicate request, one that would close a cycle, and an
// Undo refused with a spent token or because the link was asked for again
// since.
func TestRefusedLinkCommandsRecordNothing(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	h := testsupport.New(t)
	pat, sam, child, parent, link := pendingAcrossOwners(t, h)
	recorded := len(linkEvents(t, h, child))
	// did runs a command that must succeed, which records one event.
	did := func(name string, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		recorded++
	}
	// refused runs a command that must be refused, recording nothing.
	refused := func(name string, err error) {
		t.Helper()
		if err == nil {
			t.Fatalf("%s: want it refused", name)
		}
		for _, g := range []domain.Goal{child, parent} {
			if got := linkEvents(t, h, g); len(got) != recorded {
				t.Errorf("%s: %s has %d link events, want %d: %v", name, g.Title, len(got), recorded, eventsOf(got))
			}
		}
	}
	request := func(by domain.Account, c, p domain.Goal) (domain.Link, error) {
		return h.Service.RequestLink(ctx, domain.RequestLinkInput{ChildID: c.ID, ParentID: p.ID, RequesterID: by.ID})
	}

	_, err := h.Service.AcceptLink(ctx, link.ID, sam.ID)
	refused("accept by the child's Owner", err)
	_, err = h.Service.RejectLink(ctx, link.ID, sam.ID)
	refused("reject by the child's Owner", err)
	_, err = h.Service.RemoveLink(ctx, link.ID, pat.ID)
	refused("remove while pending", err)
	_, err = request(sam, child, parent)
	refused("a duplicate request", err)

	_, err = h.Service.AcceptLink(ctx, link.ID, pat.ID)
	did("AcceptLink", err)
	_, err = request(pat, parent, child)
	refused("a request closing a cycle", err)
	_, err = h.Service.ImportLink(ctx, parent.ID, child.ID)
	refused("an import closing a cycle", err)

	first, err := h.Service.RemoveLink(ctx, link.ID, sam.ID)
	did("RemoveLink", err)
	restored, err := h.Service.RestoreLink(ctx, first.ID, sam.ID, first.UndoToken)
	did("RestoreLink", err)
	second, err := h.Service.RemoveLink(ctx, restored.ID, sam.ID)
	did("RemoveLink again", err)
	_, err = h.Service.RestoreLink(ctx, first.ID, sam.ID, first.UndoToken)
	refused("an Undo with a spent token", err)

	again, err := request(sam, child, parent)
	did("RequestLink again", err)
	_, err = h.Service.RestoreLink(ctx, second.ID, sam.ID, second.UndoToken)
	refused("an Undo of a removal once the link is asked for again", err)

	rejection, err := h.Service.RejectLink(ctx, again.ID, pat.ID)
	did("RejectLink", err)
	_, err = request(sam, child, parent)
	did("RequestLink once more", err)
	_, err = h.Service.RestoreLinkRequest(ctx, rejection.ID, pat.ID, rejection.UndoToken)
	refused("an Undo of a rejection once the link is asked for again", err)
}
