package domain_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/email"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// A reader comments on a Goal's block in a published Report; the comment is
// routed to that Goal's Owner, who gets an email (ticket #19).
func TestCommentOnGoalBlockEmailsTheOwner(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	owner := h.SignIn("owner@example.com")
	reader := h.SignIn("reader@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(reader, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
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
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	owner := h.SignIn("owner@example.com")
	reader := h.SignIn("reader@example.com")
	peer := h.SignIn("peer@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(reader, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
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
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	owner := h.SignIn("owner@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	other := h.ActiveGoal(owner, "Cut churn", "Keep customers.")
	def := h.SaveReportDefinition(owner, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
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

// A comment whose alert can't be sent is kept, as is a reply, and their
// alerts go out once mail is back (ticket #412).
func TestCommentIsKeptWhenItsAlertFails(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	owner := h.SignIn("owner@example.com")
	reader := h.SignIn("reader@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(reader, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(reader, def)
	down := domain.NewService(h.DB, h.Clock, failingSender{}, nil, domain.WithEmailDeliveredOnCommit())

	question, err := down.AddComment(ctx, reader.ID, pub.ID, g.ID, "Why?")
	if err != nil {
		t.Fatalf("AddComment: %v, want it kept though its alert could not be sent", err)
	}
	if _, err := down.ReplyToComment(ctx, owner.ID, question.ID, "Because."); err != nil {
		t.Fatalf("ReplyToComment: %v, want it kept though its alert could not be sent", err)
	}
	threads, err := h.Service.ListThreads(ctx, pub.ID)
	if err != nil {
		t.Fatalf("ListThreads: %v", err)
	}
	if len(threads) != 1 || len(threads[0].Replies) != 1 {
		t.Errorf("threads %+v, want the comment and its reply", threads)
	}

	h.Clock.Advance(time.Minute)
	h.DeliverEmail()
	assertSentTo(t, h.Email.Sent(), "owner@example.com", "reader@example.com")
}

// assertSentTo fails the test unless sent went to want, in order.
func assertSentTo(t *testing.T, sent []email.Message, want ...string) {
	t.Helper()
	var to []string
	for _, m := range sent {
		to = append(to, m.To)
	}
	if !slices.Equal(to, want) {
		t.Errorf("sent to %v, want %v", to, want)
	}
}

// Anyone can comment on a Goal itself, with no Report; the comment is routed
// to the Goal's Owner, whose email links to the Goal page (ticket #350).
func TestCommentOnTheGoalItselfEmailsTheOwner(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	owner := h.SignIn("owner@example.com")
	reader := h.SignIn("reader@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")

	c, err := h.Service.AddGoalComment(ctx, reader.ID, g.ID, "  Why did the vendor slip?  ")
	if err != nil {
		t.Fatalf("AddGoalComment: %v", err)
	}
	if c.Body != "Why did the vendor slip?" || c.Author.ID != reader.ID || c.GoalID != g.ID || c.PublicationID != 0 || c.ThreadID != 0 {
		t.Errorf("comment %+v, want the reader's trimmed question on the Goal, on no publication", c)
	}

	threads, err := h.Service.GoalThreads(ctx, g.ID)
	if err != nil {
		t.Fatalf("GoalThreads: %v", err)
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
	if m.Subject != "Comment on Launch in EU" {
		t.Errorf("subject %q, want %q", m.Subject, "Comment on Launch in EU")
	}
	wantBody := fmt.Sprintf("reader (reader@example.com) commented on the Goal \"Launch in EU\":\n\nWhy did the vendor slip?\n\nRead and reply at /goals/%d#comments\n", g.ID)
	if m.Body != wantBody {
		t.Errorf("body %q, want %q", m.Body, wantBody)
	}
}

// The Owner's own comment on their Goal alerts no one.
func TestOwnersCommentOnTheirOwnGoalSendsNoEmail(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	owner := h.SignIn("owner@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")

	if _, err := h.Service.AddGoalComment(context.Background(), owner.ID, g.ID, "Vendor slipped a week."); err != nil {
		t.Fatalf("AddGoalComment: %v", err)
	}
	if sent := h.Email.Sent(); len(sent) != 0 {
		t.Errorf("sent %+v, want nothing", sent)
	}
}

// A comment on a Goal cannot be blank, and the Goal must exist; a refused
// comment is kept on neither the Goal nor the unknown one it was posted to.
func TestGoalCommentRejectsBlankOrUnknownGoal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	owner := h.SignIn("owner@example.com")
	reader := h.SignIn("reader@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")

	if _, err := h.Service.AddGoalComment(ctx, reader.ID, g.ID, "   "); !errors.Is(err, domain.ErrValidation) {
		t.Errorf("blank comment: err %v, want ErrValidation", err)
	}
	unknown := g.ID + 100
	if _, err := h.Service.AddGoalComment(ctx, reader.ID, unknown, "Why?"); !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("comment on an unknown Goal: err %v, want ErrNotFound", err)
	}
	comments, err := h.Service.GoalComments(ctx, g.ID)
	if err != nil {
		t.Fatalf("GoalComments: %v", err)
	}
	if len(comments) != 0 || len(h.Email.Sent()) != 0 {
		t.Errorf("comments %+v, emails %+v, want nothing kept or sent", comments, h.Email.Sent())
	}
	onUnknown, err := h.Service.GoalComments(ctx, unknown)
	if err != nil {
		t.Fatalf("GoalComments on the unknown Goal: %v", err)
	}
	if len(onUnknown) != 0 {
		t.Errorf("comments on the unknown Goal %+v, want none kept", onUnknown)
	}
}

// Replies to a comment on the Goal stay on the Goal, in the one thread, and
// alert the Owner and the thread's other authors, never the replier.
func TestRepliesToAGoalCommentStayOnTheGoal(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	owner := h.SignIn("owner@example.com")
	reader := h.SignIn("reader@example.com")
	peer := h.SignIn("peer@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	question, err := h.Service.AddGoalComment(ctx, reader.ID, g.ID, "Why did the vendor slip?")
	if err != nil {
		t.Fatalf("AddGoalComment: %v", err)
	}

	answer, err := h.Service.ReplyToComment(ctx, owner.ID, question.ID, "Their factory flooded.")
	if err != nil {
		t.Fatalf("ReplyToComment (owner): %v", err)
	}
	more, err := h.Service.ReplyToComment(ctx, peer.ID, answer.ID, "We hit the same vendor last year.")
	if err != nil {
		t.Fatalf("ReplyToComment (peer, replying to a reply): %v", err)
	}
	for _, r := range []domain.Comment{answer, more} {
		if r.PublicationID != 0 || r.ThreadID != question.ID {
			t.Errorf("reply %+v, want it in the question's thread, on no publication", r)
		}
	}

	threads, err := h.Service.GoalThreads(ctx, g.ID)
	if err != nil {
		t.Fatalf("GoalThreads: %v", err)
	}
	if len(threads) != 1 || len(threads[0].Replies) != 2 ||
		threads[0].Replies[0].ID != answer.ID || threads[0].Replies[1].ID != more.ID {
		t.Fatalf("threads %+v, want the one thread with both replies in order", threads)
	}

	sent := h.Email.Sent()
	var to []string
	for _, m := range sent {
		to = append(to, m.To)
	}
	wantTo := []string{"owner@example.com", "reader@example.com", "owner@example.com", "reader@example.com"}
	if strings.Join(to, ",") != strings.Join(wantTo, ",") {
		t.Errorf("emails to %v, want %v", to, wantTo)
	}
	last := sent[len(sent)-1]
	wantBody := fmt.Sprintf("peer (peer@example.com) replied about the Goal \"Launch in EU\":\n\nWe hit the same vendor last year.\n\nRead and reply at /goals/%d#comments\n", g.ID)
	if last.Subject != "Reply on Launch in EU" || last.Body != wantBody {
		t.Errorf("reply alert %+v, want subject %q and body %q", last, "Reply on Launch in EU", wantBody)
	}
}

// A Goal's own threads and a Report's threads are kept apart, but the Goal's
// History lists every Comment about it, oldest first, naming the Report a
// Comment was made on.
func TestGoalThreadsAndReportThreadsAreApartButGoalCommentsListsBoth(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	owner := h.SignIn("owner@example.com")
	reader := h.SignIn("reader@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	def := h.SaveReportDefinition(reader, domain.SaveReportDefinitionInput{Name: "MBR", Mode: domain.ReportModePicked, Picked: []int64{g.ID}})
	pub := h.PublishReport(reader, def)

	onReport, err := h.Service.AddComment(ctx, reader.ID, pub.ID, g.ID, "Why did the vendor slip?")
	if err != nil {
		t.Fatalf("AddComment: %v", err)
	}
	onGoal, err := h.Service.AddGoalComment(ctx, reader.ID, g.ID, "Is the EU date still real?")
	if err != nil {
		t.Fatalf("AddGoalComment: %v", err)
	}
	reply, err := h.Service.ReplyToComment(ctx, owner.ID, onGoal.ID, "Yes.")
	if err != nil {
		t.Fatalf("ReplyToComment: %v", err)
	}

	threads, err := h.Service.GoalThreads(ctx, g.ID)
	if err != nil {
		t.Fatalf("GoalThreads: %v", err)
	}
	if len(threads) != 1 || threads[0].Comment.ID != onGoal.ID {
		t.Errorf("GoalThreads %+v, want only the comment on the Goal", threads)
	}
	pubThreads, err := h.Service.ListThreads(ctx, pub.ID)
	if err != nil {
		t.Fatalf("ListThreads: %v", err)
	}
	if len(pubThreads) != 1 || pubThreads[0].Comment.ID != onReport.ID {
		t.Errorf("ListThreads %+v, want only the comment on the Report", pubThreads)
	}

	listed, err := h.Service.GoalComments(ctx, g.ID)
	if err != nil {
		t.Fatalf("GoalComments: %v", err)
	}
	var got []string
	for _, l := range listed {
		got = append(got, fmt.Sprintf("%d %q def %d", l.Comment.ID, l.ReportName, l.DefinitionID))
	}
	want := []string{
		fmt.Sprintf("%d %q def %d", onReport.ID, "MBR", def.ID),
		fmt.Sprintf("%d %q def %d", onGoal.ID, "", 0),
		fmt.Sprintf("%d %q def %d", reply.ID, "", 0),
	}
	if !slices.Equal(got, want) {
		t.Errorf("GoalComments %v, want %v", got, want)
	}
}

// A comment on the Goal whose alert can't be sent is kept, as is a reply to
// one, and their alerts go out once mail is back (ticket #412).
func TestGoalCommentIsKeptWhenItsAlertFails(t *testing.T) {
	t.Parallel()

	h := testsupport.New(t)
	ctx := context.Background()
	owner := h.SignIn("owner@example.com")
	reader := h.SignIn("reader@example.com")
	g := h.ActiveGoal(owner, "Launch in EU", "Expand the market.")
	down := domain.NewService(h.DB, h.Clock, failingSender{}, nil, domain.WithEmailDeliveredOnCommit())

	question, err := down.AddGoalComment(ctx, reader.ID, g.ID, "Why?")
	if err != nil {
		t.Fatalf("AddGoalComment: %v, want it kept though its alert could not be sent", err)
	}
	if _, err := down.ReplyToComment(ctx, owner.ID, question.ID, "Because."); err != nil {
		t.Fatalf("ReplyToComment: %v, want it kept though its alert could not be sent", err)
	}
	threads, err := h.Service.GoalThreads(ctx, g.ID)
	if err != nil {
		t.Fatalf("GoalThreads: %v", err)
	}
	if len(threads) != 1 || len(threads[0].Replies) != 1 {
		t.Errorf("threads %+v, want the comment and its reply", threads)
	}

	h.Clock.Advance(time.Minute)
	h.DeliverEmail()
	assertSentTo(t, h.Email.Sent(), "owner@example.com", "reader@example.com")
}
