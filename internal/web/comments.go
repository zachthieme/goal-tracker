package web

import (
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// discussion is what a Report page needs to take part in its discussion
// (ticket #19): the comment threads and Action Items raised on a publication,
// and which Action Items the reader may close or reassign. Its zero value is
// read-only —
// the print page shows the Report without it.
type discussion struct {
	Current *domain.Account
	// Pub is the publication being discussed; nil on the draft, which takes
	// no comments.
	Pub *domain.Publication
	// Threads are the publication's comment threads, by the Goal they are on.
	Threads map[int64][]domain.Thread
	// Raised are the Action Items raised on this publication, open or closed.
	Raised []domain.ActionItem
	// Open holds the Action Items open now, by id, so a frozen list's forms
	// follow the live item: its owner now closes it, though the row shows the
	// owner it froze with.
	Open map[int64]domain.ActionItem
	// Reassignable marks the open Action Items the reader may reassign.
	Reassignable map[int64]bool
	// Author is set when the reader is the Report's author, who raises Action
	// Items.
	Author bool
	// Return is the page a form comes back to.
	Return string
	// Loc is the org's timezone, the Report's times are shown in.
	Loc *time.Location
}

// dates is the clock a Report page shows its dates against: every one with
// its year (DESIGN.md: Dates and times).
func (d discussion) dates() displayDates {
	return reportDates(d.Loc)
}

// canClose reports whether the reader may close the Action Item now: it is
// still open and they own it now.
func (d discussion) canClose(item domain.ActionItem) bool {
	live, open := d.Open[item.ID]
	return d.Current != nil && open && d.Current.ID == live.Owner.ID
}

// canReassign reports whether the reader may hand the Action Item, still
// open, to a new owner.
func (d discussion) canReassign(item domain.ActionItem) bool {
	return d.Current != nil && d.Reassignable[item.ID]
}

// closedSince reports whether an Action Item a frozen Report lists as open has
// been closed since. The read-only view (no reader) doesn't know.
func (d discussion) closedSince(item domain.ActionItem) bool {
	_, open := d.Open[item.ID]
	return d.Current != nil && !open
}

// actionItemGoalChoices are the Goals an Action Item raised directly on the
// publication may be about: each one it covers, by title.
func (d discussion) actionItemGoalChoices() []domain.Goal {
	if d.Pub == nil {
		return nil
	}
	var goals []domain.Goal
	for _, b := range d.Pub.Report.Exceptions {
		goals = append(goals, b.Goal)
	}
	for _, sg := range d.Pub.Report.Lines {
		goals = append(goals, sg.Goal)
	}
	slices.SortStableFunc(goals, func(a, b domain.Goal) int { return strings.Compare(a.Title, b.Title) })
	return goals
}

// publicationDiscussion gathers the discussion of the published Report pub
// for the reader current.
func (s *Server) publicationDiscussion(r *http.Request, current domain.Account, pub domain.Publication) (discussion, error) {
	ctx := r.Context()
	threads, err := s.svc.ListThreads(ctx, pub.ID)
	if err != nil {
		return discussion{}, err
	}
	byGoal := map[int64][]domain.Thread{}
	for _, t := range threads {
		byGoal[t.Comment.GoalID] = append(byGoal[t.Comment.GoalID], t)
	}
	raised, err := s.svc.PublicationActionItems(ctx, pub.ID)
	if err != nil {
		return discussion{}, err
	}
	open, reassignable, err := s.liveActionItems(r, current, pub.DefinitionID)
	if err != nil {
		return discussion{}, err
	}
	author, err := s.svc.IsReportAuthor(ctx, current.ID, pub.ID)
	if err != nil {
		return discussion{}, err
	}
	return discussion{
		Current:      &current,
		Pub:          &pub,
		Threads:      byGoal,
		Raised:       raised,
		Open:         open,
		Reassignable: reassignable,
		Author:       author,
		Return:       publicationPath(pub),
		Loc:          s.svc.Timezone(),
	}, nil
}

// draftDiscussion is the draft's part in the discussion: its Action Items are
// read live, so their owners can close them from it, and those who may can
// reassign them.
func (s *Server) draftDiscussion(r *http.Request, current domain.Account, defID int64) (discussion, error) {
	open, reassignable, err := s.liveActionItems(r, current, defID)
	if err != nil {
		return discussion{}, err
	}
	return discussion{
		Current:      &current,
		Open:         open,
		Reassignable: reassignable,
		Return:       fmt.Sprintf("/reports/%d", defID),
		Loc:          s.svc.Timezone(),
	}, nil
}

// liveActionItems are the Report Definition's Action Items open now, by id,
// and which of them the reader current may reassign.
func (s *Server) liveActionItems(r *http.Request, current domain.Account, defID int64) (map[int64]domain.ActionItem, map[int64]bool, error) {
	items, err := s.svc.OpenActionItems(r.Context(), defID)
	if err != nil {
		return nil, nil, err
	}
	open := make(map[int64]domain.ActionItem, len(items))
	reassignable := map[int64]bool{}
	for _, item := range items {
		open[item.ID] = item
		ok, err := s.svc.CanReassignActionItem(r.Context(), current.ID, item)
		if err != nil {
			return nil, nil, err
		}
		if ok {
			reassignable[item.ID] = true
		}
	}
	return open, reassignable, nil
}

// handleAddComment comments on a Goal's block in a published Report and comes
// back to it; the Goal's Owner is emailed.
func (s *Server) handleAddComment(w http.ResponseWriter, r *http.Request, current domain.Account) {
	pub, ok := s.publication(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read the form", http.StatusBadRequest)
		return
	}
	goalID, _ := strconv.ParseInt(r.FormValue("goal"), 10, 64)
	if _, err := s.svc.AddComment(r.Context(), current.ID, pub.ID, goalID, r.FormValue("body")); err != nil {
		s.writeCommentError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("%s#goal-%d", publicationPath(pub), goalID), http.StatusSeeOther)
}

// handleReplyToComment replies in a comment's thread and comes back to it.
func (s *Server) handleReplyToComment(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read the form", http.StatusBadRequest)
		return
	}
	reply, err := s.svc.ReplyToComment(r.Context(), current.ID, id, r.FormValue("body"))
	if err != nil {
		s.writeCommentError(w, r, err)
		return
	}
	pub, err := s.svc.GetPublication(r.Context(), reply.PublicationID)
	if err != nil {
		s.writeCommentError(w, r, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("%s#goal-%d", publicationPath(pub), reply.GoalID), http.StatusSeeOther)
}

// handleRaiseActionItem raises an Action Item on a published Report, from a
// comment or directly, owned by the Account the owner field names by email.
// One raised directly is about the Goal the goal field names, or the whole
// Report when it is 0 or absent, as from the comment form.
func (s *Server) handleRaiseActionItem(w http.ResponseWriter, r *http.Request, current domain.Account) {
	pub, ok := s.publication(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read the form", http.StatusBadRequest)
		return
	}
	due, err := parseDate(r.FormValue("due"))
	if err != nil {
		http.Error(w, "the due date must be a date", http.StatusBadRequest)
		return
	}
	commentID, _ := strconv.ParseInt(r.FormValue("comment"), 10, 64)
	var goalID int64
	if v := r.FormValue("goal"); v != "" {
		if goalID, err = strconv.ParseInt(v, 10, 64); err != nil {
			s.writeCommentError(w, r, fmt.Errorf("%w: the Goal must be a Goal's id", domain.ErrValidation))
			return
		}
	}
	if _, err := s.svc.RaiseActionItemByEmail(r.Context(), current.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID,
		CommentID:     commentID,
		GoalID:        goalID,
		Text:          r.FormValue("text"),
		DueDate:       due,
	}, r.FormValue("owner")); err != nil {
		s.writeCommentError(w, r, err)
		return
	}
	http.Redirect(w, r, publicationPath(pub)+"#action-items", http.StatusSeeOther)
}

// handleCloseActionItem closes an Action Item with a note and comes back to the
// page it was closed from, or else the publication it was raised on.
func (s *Server) handleCloseActionItem(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read the form", http.StatusBadRequest)
		return
	}
	item, err := s.svc.CloseActionItem(r.Context(), current.ID, id, r.FormValue("note"))
	if err != nil {
		s.writeCommentError(w, r, err)
		return
	}
	back := r.FormValue("return")
	if !localPath(back) {
		back = fmt.Sprintf("/reports/%d/publications/%d", item.DefinitionID, item.PublicationID)
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// handleReassignActionItem hands an open Action Item to the Account the owner
// field names by email, and comes back to the page it was reassigned from, or
// else the publication it was raised on.
func (s *Server) handleReassignActionItem(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read the form", http.StatusBadRequest)
		return
	}
	item, err := s.svc.ReassignActionItemByEmail(r.Context(), current.ID, id, r.FormValue("owner"))
	if err != nil {
		s.writeCommentError(w, r, err)
		return
	}
	back := r.FormValue("return")
	if !localPath(back) {
		back = fmt.Sprintf("/reports/%d/publications/%d", item.DefinitionID, item.PublicationID)
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// localPath reports whether p is a path on this site, safe to redirect to.
// Browsers drop tabs and newlines and read a backslash as a slash, so any of
// them could turn "/…" into "//other-site"; such a path is refused outright.
func localPath(p string) bool {
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") {
		return false
	}
	return !strings.ContainsFunc(p, func(r rune) bool { return r <= ' ' || r == 0x7f || r == '\\' })
}

// writeCommentError maps a domain error from a Report's discussion — a
// comment or an Action Item — to an HTTP status.
func (s *Server) writeCommentError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, domain.ErrValidation):
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
	case errors.Is(err, domain.ErrNotAuthorized):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, domain.ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	default:
		s.serverError(w, r, "discussion action failed", err)
	}
}
