package domain_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/email"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// A reader comments on a Goal's block in a published Report; the comment is
// routed to that Goal's Owner, who gets an email (ticket #19).
func TestCommentOnGoalBlockEmailsTheOwner(t *testing.T) {
	h := testsupport.New(t)
	ctx := context.Background()
	owner := h.SignIn("owner@example.com")
	reader := h.SignIn("reader@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(reader, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{g.ID}})
	pub := h.PublishReport(reader, def)

	c, err := h.Service.AddComment(ctx, reader.ID, pub.ID, g.ID, "  Why did the vendor slip?  ")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if c.Body != "Why did the vendor slip?" || c.Author.ID != reader.ID || c.GoalID != g.ID || c.PublicationID != pub.ID {
		t.Errorf("comment %+v, want the reader's trimmed question on the Goal in the publication", c)
	}

	threads, err := h.Service.ListThreads(ctx, pub.ID)
	if err != nil {
		t.Fatalf("ListThreads: %v", err)
	}
	if len(threads) != 1 || threads[0].Comment.ID != c.ID {
		t.Fatalf("threads %+v, want the one comment", threads)
	}

	sent := h.Email.Sent()
	if len(sent) != 1 {
		t.Fatalf("sent %d emails, want 1 comment alert to the Owner: %+v", len(sent), sent)
	}
	m := sent[0]
	if m.To != "owner@example.com" {
		t.Errorf("alert to %q, want the Goal's Owner", m.To)
	}
	for _, want := range []string{"Launch in EU", "MBR", "reader (reader@example.com) commented on", "Why did the vendor slip?"} {
		if !strings.Contains(m.Subject+"\n"+m.Body, want) {
			t.Errorf("alert %+v does not mention %q", m, want)
		}
	}
}

// The Owner and others reply in the thread. A reply alerts everyone else in
// the thread — the asker hears the Owner's answer — but never its own author
// (ticket #19).
func TestOwnersAndOthersReplyInTheThread(t *testing.T) {
	h := testsupport.New(t)
	ctx := context.Background()
	owner := h.SignIn("owner@example.com")
	reader := h.SignIn("reader@example.com")
	peer := h.SignIn("peer@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(reader, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{g.ID}})
	pub := h.PublishReport(reader, def)
	question, err := h.Service.AddComment(ctx, reader.ID, pub.ID, g.ID, "Why did the vendor slip?")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}

	answer, err := h.Service.ReplyToComment(ctx, owner.ID, question.ID, "Their factory flooded.")
	if err != nil {
		t.Fatalf("ReplyToComment (owner): %v", err)
	}
	if _, err := h.Service.ReplyToComment(ctx, peer.ID, answer.ID, "We hit the same vendor last year."); err != nil {
		t.Fatalf("ReplyToComment (peer, replying to a reply): %v", err)
	}

	threads, err := h.Service.ListThreads(ctx, pub.ID)
	if err != nil {
		t.Fatalf("ListThreads: %v", err)
	}
	if len(threads) != 1 {
		t.Fatalf("threads %+v, want the one thread", threads)
	}
	var got []string
	for _, r := range threads[0].Replies {
		got = append(got, r.Author.Email+": "+r.Body)
	}
	want := []string{"owner@example.com: Their factory flooded.", "peer@example.com: We hit the same vendor last year."}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("replies %q, want %q", got, want)
	}

	// The question alerted the Owner; the Owner's answer alerted the reader;
	// the peer's reply alerted the Owner and the reader.
	var to []string
	for _, m := range h.Email.Sent() {
		to = append(to, m.To)
	}
	wantTo := []string{"owner@example.com", "reader@example.com", "owner@example.com", "reader@example.com"}
	if strings.Join(to, ",") != strings.Join(wantTo, ",") {
		t.Errorf("emails to %v, want %v", to, wantTo)
	}
}

// A comment must be on a Goal the publication covers, and cannot be blank.
func TestCommentRejectsBlankOrUncoveredGoal(t *testing.T) {
	h := testsupport.New(t)
	ctx := context.Background()
	owner := h.SignIn("owner@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	other := h.ActiveGoal(owner, "Cut churn", "Keep customers.")
	def := h.SaveReportDefinition(owner, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{g.ID}})
	pub := h.PublishReport(owner, def)

	if _, err := h.Service.AddComment(ctx, owner.ID, pub.ID, other.ID, "Why?"); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("comment on a Goal outside the publication: err %v, want ErrValidation", err)
	}
	if _, err := h.Service.AddComment(ctx, owner.ID, pub.ID, g.ID, "   "); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("blank comment: err %v, want ErrValidation", err)
	}
	if _, err := h.Service.AddComment(ctx, owner.ID, pub.ID+1, g.ID, "Why?"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("comment on a missing publication: err %v, want ErrNotFound", err)
	}
	if len(h.Email.Sent()) != 0 {
		t.Errorf("sent %v, want nothing (the Owner's own comments never alert them)", h.Email.Sent())
	}
}

// failingSender is an email sender whose every send fails.
type failingSender struct{}

func (failingSender) Send(context.Context, email.Message) error { return errors.New("mail is down") }

// A comment whose alert can't be sent is not kept, so posting it again doesn't
// leave it twice in the thread.
func TestCommentIsNotKeptWhenItsAlertFails(t *testing.T) {
	h := testsupport.New(t)
	ctx := context.Background()
	owner := h.SignIn("owner@example.com")
	reader := h.SignIn("reader@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(reader, domain.SaveReportDefinitionInput{Name: "MBR", RootIDs: []int64{g.ID}})
	pub := h.PublishReport(reader, def)
	down := domain.NewService(h.DB, h.Clock, failingSender{}, nil)

	if _, err := down.AddComment(ctx, reader.ID, pub.ID, g.ID, "Why?"); err == nil {
		t.Fatal("AddComment succeeded though its alert could not be sent")
	}
	question, err := h.Service.AddComment(ctx, reader.ID, pub.ID, g.ID, "Why?")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	if _, err := down.ReplyToComment(ctx, owner.ID, question.ID, "Because."); err == nil {
		t.Fatal("ReplyToComment succeeded though its alert could not be sent")
	}
	threads, err := h.Service.ListThreads(ctx, pub.ID)
	if err != nil {
		t.Fatalf("ListThreads: %v", err)
	}
	if len(threads) != 1 || len(threads[0].Replies) != 0 {
		t.Errorf("threads %+v, want only the comment whose alert was sent", threads)
	}
}
