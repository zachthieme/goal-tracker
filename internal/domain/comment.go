package domain

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/db"
	"github.com/zachthieme/goal-tracker/internal/email"
)

// Comment is a question or answer about one Goal, made either on the Goal
// itself (ticket #350) or on its block in a published Report (ticket #19). A
// thread's first Comment is routed to the Goal's Owner; the Owner and others
// reply in the thread.
type Comment struct {
	ID int64
	// PublicationID is the published Report it was made on; 0 when it was
	// made on the Goal itself. A reply is where its thread is.
	PublicationID int64
	GoalID        int64
	// ThreadID is the thread's first Comment; 0 when this Comment starts one.
	ThreadID  int64
	Author    Account
	Body      string
	CreatedAt time.Time
}

// Thread is a Comment on a Goal, or on its block in a Report, and its replies,
// oldest first.
type Thread struct {
	Comment Comment
	Replies []Comment
}

// AddComment comments on the Goal goalID's block in the published Report
// pubID, starting a thread. Anyone signed in may comment; actorID records who
// did. The Goal must be one the publication covers, and the comment cannot be
// blank. The Goal's current Owner — the one accountable for it now — is
// emailed the comment unless they wrote it or have left the org, once it is
// kept (see queueEmail); a mail server that is down only delays the alert.
func (s *Service) AddComment(ctx context.Context, actorID, pubID, goalID int64, body string) (Comment, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return Comment{}, fmt.Errorf("%w: a comment cannot be blank", ErrValidation)
	}
	pub, err := s.GetPublication(ctx, pubID)
	if err != nil {
		return Comment{}, err
	}
	if !pub.Report.covers(goalID) {
		return Comment{}, fmt.Errorf("%w: goal %d is not in this publication", ErrValidation, goalID)
	}
	var c Comment
	err = s.WithinTx(ctx, func(tx *Service) error {
		var err error
		if c, err = tx.createComment(ctx, actorID, pubID, goalID, 0, body); err != nil {
			return err
		}
		return tx.alert(ctx, c, &pub, nil)
	})
	return c, err
}

// AddGoalComment comments on the Goal goalID itself, with no Report, starting
// a thread on the Goal's page. Anyone signed in may comment, in any Lifecycle;
// actorID records who did. The comment cannot be blank. The Goal's current
// Owner is emailed it unless they wrote it or have left the org, once it is
// kept (see queueEmail).
func (s *Service) AddGoalComment(ctx context.Context, actorID, goalID int64, body string) (Comment, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return Comment{}, fmt.Errorf("%w: a comment cannot be blank", ErrValidation)
	}
	if _, err := s.queries.GetGoal(ctx, goalID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Comment{}, fmt.Errorf("%w: goal %d", ErrNotFound, goalID)
		}
		return Comment{}, fmt.Errorf("get goal: %w", err)
	}
	var c Comment
	err := s.WithinTx(ctx, func(tx *Service) error {
		var err error
		if c, err = tx.createComment(ctx, actorID, 0, goalID, 0, body); err != nil {
			return err
		}
		return tx.alert(ctx, c, nil, nil)
	})
	return c, err
}

// ReplyToComment replies in the thread commentID belongs to; replying to a
// reply answers in the same thread, on the Goal or on the Report the thread
// is on. Anyone signed in may reply. The Goal's
// current Owner and everyone else who has written in the thread are emailed
// the reply, except its author and anyone who has left the org, once it is
// kept (see queueEmail).
func (s *Service) ReplyToComment(ctx context.Context, actorID, commentID int64, body string) (Comment, error) {
	body = strings.TrimSpace(body)
	if body == "" {
		return Comment{}, fmt.Errorf("%w: a reply cannot be blank", ErrValidation)
	}
	parent, err := s.GetComment(ctx, commentID)
	if err != nil {
		return Comment{}, err
	}
	threadID := parent.ID
	if parent.ThreadID != 0 {
		threadID = parent.ThreadID
	}
	var pub *Publication
	if parent.PublicationID != 0 {
		p, err := s.GetPublication(ctx, parent.PublicationID)
		if err != nil {
			return Comment{}, err
		}
		pub = &p
	}
	var c Comment
	err = s.WithinTx(ctx, func(tx *Service) error {
		var err error
		if c, err = tx.createComment(ctx, actorID, parent.PublicationID, parent.GoalID, threadID, body); err != nil {
			return err
		}
		authors, err := tx.queries.ListThreadAuthors(ctx, threadID)
		if err != nil {
			return fmt.Errorf("list thread authors: %w", err)
		}
		thread := make([]Account, 0, len(authors))
		for _, a := range authors {
			thread = append(thread, accountFromRow(a))
		}
		return tx.alert(ctx, c, pub, thread)
	})
	return c, err
}

// GetComment returns the Comment with the given id, or ErrNotFound if none
// exists.
func (s *Service) GetComment(ctx context.Context, id int64) (Comment, error) {
	row, err := s.queries.GetComment(ctx, id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Comment{}, fmt.Errorf("%w: comment %d", ErrNotFound, id)
		}
		return Comment{}, fmt.Errorf("get comment: %w", err)
	}
	return commentFromRow(row.Comment, row.Account), nil
}

// ListThreads returns the published Report pubID's threads in the order they
// were started, each with its replies oldest first.
func (s *Service) ListThreads(ctx context.Context, pubID int64) ([]Thread, error) {
	rows, err := s.queries.ListPublicationComments(ctx, pubID)
	if err != nil {
		return nil, fmt.Errorf("list comments: %w", err)
	}
	comments := make([]Comment, 0, len(rows))
	for _, r := range rows {
		comments = append(comments, commentFromRow(r.Comment, r.Account))
	}
	return threadsOf(comments), nil
}

// GoalThreads returns the threads started on the Goal goalID itself, not on a
// Report, in the order they were started, each with its replies oldest first.
func (s *Service) GoalThreads(ctx context.Context, goalID int64) ([]Thread, error) {
	rows, err := s.queries.ListGoalThreadComments(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list goal comments: %w", err)
	}
	comments := make([]Comment, 0, len(rows))
	for _, r := range rows {
		comments = append(comments, commentFromRow(r.Comment, r.Account))
	}
	return threadsOf(comments), nil
}

// CommentListing is a Comment listed outside its thread — in the Goal's
// History — with what a reader needs to place it.
type CommentListing struct {
	Comment Comment
	// ReportName is the name of the Report Definition whose publication it was
	// made on; "" when it was made on the Goal itself.
	ReportName string
	// DefinitionID is that Report Definition; 0 when it was made on the Goal.
	DefinitionID int64
}

// GoalComments returns every Comment and reply about the Goal goalID, made on
// the Goal itself or on any published Report, oldest first.
func (s *Service) GoalComments(ctx context.Context, goalID int64) ([]CommentListing, error) {
	rows, err := s.queries.ListGoalComments(ctx, goalID)
	if err != nil {
		return nil, fmt.Errorf("list goal comments: %w", err)
	}
	out := make([]CommentListing, 0, len(rows))
	for _, r := range rows {
		out = append(out, CommentListing{
			Comment:      commentFromRow(r.Comment, r.Account),
			ReportName:   r.ReportName,
			DefinitionID: r.DefinitionID,
		})
	}
	return out, nil
}

// threadsOf groups comments, oldest first, into their threads in the order
// they were started; a reply whose thread isn't among them is dropped.
func threadsOf(comments []Comment) []Thread {
	var out []Thread
	index := map[int64]int{}
	for _, c := range comments {
		if c.ThreadID == 0 {
			index[c.ID] = len(out)
			out = append(out, Thread{Comment: c})
			continue
		}
		if i, ok := index[c.ThreadID]; ok {
			out[i].Replies = append(out[i].Replies, c)
		}
	}
	return out
}

func (s *Service) createComment(ctx context.Context, actorID, pubID, goalID, threadID int64, body string) (Comment, error) {
	author, err := s.queries.GetAccount(ctx, actorID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Comment{}, fmt.Errorf("%w: account %d", ErrNotFound, actorID)
		}
		return Comment{}, fmt.Errorf("look up author: %w", err)
	}
	row, err := s.queries.CreateComment(ctx, db.CreateCommentParams{
		PublicationID: pubID,
		GoalID:        goalID,
		ParentID:      threadID,
		AuthorID:      actorID,
		Body:          body,
		CreatedAt:     s.clock.Now().Format(timeFormat),
	})
	if err != nil {
		return Comment{}, fmt.Errorf("create comment: %w", err)
	}
	return commentFromRow(row, author), nil
}

// alert queues an email of the new Comment c to the Goal's current Owner and
// to others (everyone in its thread, for a reply), once each, skipping its
// author and anyone who has left the org. pub is the published Report c was made on, or
// nil for a Comment on the Goal itself.
func (s *Service) alert(ctx context.Context, c Comment, pub *Publication, others []Account) error {
	goal, err := s.queries.GetGoal(ctx, c.GoalID)
	if err != nil {
		return fmt.Errorf("look up goal: %w", err)
	}
	kind, verb := "Comment", "commented on"
	if c.ThreadID != 0 {
		kind, verb = "Reply", "replied about"
	}
	var subject, body string
	if pub == nil {
		subject = fmt.Sprintf("%s on %s", kind, goal.Goal.Title)
		body = fmt.Sprintf("%s %s the Goal %q:\n\n%s\n\nRead and reply at %s\n",
			c.Author.LongLabel(), verb, goal.Goal.Title, c.Body, s.commentURL(nil, c.GoalID))
	} else {
		report := pub.Report.Definition.Name
		subject = fmt.Sprintf("%s on %s in %s", kind, goal.Goal.Title, report)
		body = fmt.Sprintf("%s %s the Goal %q in the Report %s, published %s:\n\n%s\n\nRead and reply at %s\n",
			c.Author.LongLabel(), verb, goal.Goal.Title, report,
			orgDate(pub.PublishedAt, s.loc).Format(dateFormat), c.Body, s.commentURL(pub, c.GoalID))
	}

	sent := map[int64]bool{c.Author.ID: true}
	for _, a := range append([]Account{accountFromRow(goal.Account)}, others...) {
		if sent[a.ID] || a.Departed {
			continue
		}
		sent[a.ID] = true
		if err := s.queueEmail(ctx, email.Message{To: a.Email, AccountID: a.ID, Subject: subject, Body: body}); err != nil {
			return fmt.Errorf("queue comment alert: %w", err)
		}
	}
	return nil
}

// commentURL is where a Comment is read and replied to, for the link in an
// alert: the Goal page's Comments for one on the Goal itself (pub nil), or the
// Goal's block in the published Report pub.
func (s *Service) commentURL(pub *Publication, goalID int64) string {
	if pub == nil {
		return fmt.Sprintf("%s/goals/%d#comments", s.baseURL, goalID)
	}
	return fmt.Sprintf("%s/reports/%d/publications/%d#goal-%d", s.baseURL, pub.DefinitionID, pub.ID, goalID)
}

func commentFromRow(c db.Comment, author db.Account) Comment {
	createdAt, _ := time.Parse(timeFormat, c.CreatedAt)
	return Comment{
		ID:            c.ID,
		PublicationID: c.PublicationID,
		GoalID:        c.GoalID,
		ThreadID:      c.ParentID,
		Author:        accountFromRow(author),
		Body:          c.Body,
		CreatedAt:     createdAt,
	}
}
