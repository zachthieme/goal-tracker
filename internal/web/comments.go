package web

import (
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
	// Permissions are what the reader may do to each open Action Item, by
	// id, as the domain offers it (domain.ActionItemPermissions).
	Permissions map[int64]domain.ActionItemPermissions
	// Author is set when the reader is the Report's author, who raises Action
	// Items.
	Author bool
	// Return is the page a form comes back to.
	Return string
	// Loc is the org's timezone, the Report's times are shown in.
	Loc *time.Location
	// Print is set on the print page, which a PDF is made from: paper may
	// lose the colour, so a Health shows its word beside its shape.
	Print bool
}

// dates is the clock a Report page shows its dates against: every one with
// its year (DESIGN.md: Dates and times).
func (d discussion) dates() displayDates {
	return reportDates(d.Loc)
}

// canClose reports whether the reader may close the Action Item now, as it is
// live: still open, and theirs now.
func (d discussion) canClose(item domain.ActionItem) bool {
	return d.Current != nil && d.Permissions[item.ID].Close
}

// canReassign reports whether the reader may hand the Action Item, still
// open, to a new owner.
func (d discussion) canReassign(item domain.ActionItem) bool {
	return d.Current != nil && d.Permissions[item.ID].Reassign
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
	open, permissions, err := s.liveActionItems(r, current, pub.DefinitionID)
	if err != nil {
		return discussion{}, err
	}
	author, err := s.svc.IsReportAuthor(ctx, current.ID, pub.ID)
	if err != nil {
		return discussion{}, err
	}
	return discussion{
		Current:     &current,
		Pub:         &pub,
		Threads:     byGoal,
		Raised:      raised,
		Open:        open,
		Permissions: permissions,
		Author:      author,
		Return:      publicationPath(pub),
		Loc:         s.svc.Timezone(),
	}, nil
}

// draftDiscussion is the draft's part in the discussion: its Action Items,
// items, are read live, so their owners can close them from it, and those
// permissions lets reassign them can. Its forms come back to back.
func (s *Server) draftDiscussion(current domain.Account, items []domain.ActionItem, permissions map[int64]domain.ActionItemPermissions, back string) discussion {
	return discussion{
		Current:     &current,
		Open:        openByID(items),
		Permissions: permissions,
		Return:      back,
		Loc:         s.svc.Timezone(),
	}
}

// liveActionItems are the Report Definition's Action Items open now, by id,
// and what the reader current may do to each of them.
func (s *Server) liveActionItems(r *http.Request, current domain.Account, defID int64) (map[int64]domain.ActionItem, map[int64]domain.ActionItemPermissions, error) {
	items, err := s.svc.OpenActionItems(r.Context(), defID)
	if err != nil {
		return nil, nil, err
	}
	permissions, err := s.svc.ActionItemPermissions(r.Context(), current.ID, items)
	if err != nil {
		return nil, nil, err
	}
	return openByID(items), permissions, nil
}

// openByID is items by their id.
func openByID(items []domain.ActionItem) map[int64]domain.ActionItem {
	open := make(map[int64]domain.ActionItem, len(items))
	for _, item := range items {
		open[item.ID] = item
	}
	return open
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
		s.writeCommentError(w, r, publicationPath(pub), err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("%s#goal-%d", publicationPath(pub), goalID), http.StatusSeeOther)
}

// handleAddGoalComment comments on a Goal itself and comes back to its
// Comments; the Goal's Owner is emailed.
func (s *Server) handleAddGoalComment(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read the form", http.StatusBadRequest)
		return
	}
	if _, err := s.svc.AddGoalComment(r.Context(), current.ID, id, r.FormValue("body")); err != nil {
		s.writeCommentError(w, r, goalPath(id), err)
		return
	}
	http.Redirect(w, r, goalCommentsPath(id), http.StatusSeeOther)
}

// goalCommentsPath is the Goal page's Comments block.
func goalCommentsPath(goalID int64) string {
	return fmt.Sprintf("/goals/%d#comments", goalID)
}

// handleReplyToComment replies in a comment's thread and comes back to it: on
// the Goal page for a thread on the Goal itself, or else the publication.
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
		s.writeCommentError(w, r, s.commentPagePath(r, id), err)
		return
	}
	if reply.PublicationID == 0 {
		http.Redirect(w, r, goalCommentsPath(reply.GoalID), http.StatusSeeOther)
		return
	}
	pub, err := s.svc.GetPublication(r.Context(), reply.PublicationID)
	if err != nil {
		s.writeCommentError(w, r, goalPath(reply.GoalID), err)
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
			s.writeCommentError(w, r, publicationPath(pub), domain.Refuse(domain.ErrValidation, "the Goal must be a Goal's id"))
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
		s.writeCommentError(w, r, publicationPath(pub), err)
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
		s.writeCommentError(w, r, s.actionItemPagePath(r, id), err)
		return
	}
	back := returnTo(r, fmt.Sprintf("/reports/%d/publications/%d", item.DefinitionID, item.PublicationID))
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
		s.writeCommentError(w, r, s.actionItemPagePath(r, id), err)
		return
	}
	back := returnTo(r, fmt.Sprintf("/reports/%d/publications/%d", item.DefinitionID, item.PublicationID))
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// returnTo is the page a form sends someone back to: its posted return, when
// localPath accepts it, or else fallback, the route's own page. Every redirect
// or Back built from a posted page goes through it, so none leaves the app.
func returnTo(r *http.Request, fallback string) string {
	if back := r.FormValue("return"); localPath(back) {
		return back
	}
	return fallback
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

// writeCommentError answers a domain refusal from a discussion — a comment on
// a Goal or a Report, or an Action Item — with the refusal page, linking Back
// to back, the page it was on; a missing record is the Not found page.
func (s *Server) writeCommentError(w http.ResponseWriter, r *http.Request, back string, err error) {
	s.refuse(w, r, "Can't comment", back, err)
}

// commentPagePath is the page the Comment id is on, for a refusal to link
// Back to: the Goal page for one on the Goal itself, else its publication's
// page, or Home when it can't be read. A refused reply returns no Comment, so
// it is read again.
func (s *Server) commentPagePath(r *http.Request, id int64) string {
	c, err := s.svc.GetComment(r.Context(), id)
	if err != nil {
		return "/home"
	}
	if c.PublicationID == 0 {
		return goalPath(c.GoalID)
	}
	pub, err := s.svc.GetPublication(r.Context(), c.PublicationID)
	if err != nil {
		return "/home"
	}
	return publicationPath(pub)
}

// actionItemPagePath is the page a refused close or reassign of the Action
// Item id links Back to: the form's return, when it is a page of the app's
// own, or else the publication it was raised on, or Home when it can't be
// read. A refused command returns no Action Item, so it is read again.
func (s *Server) actionItemPagePath(r *http.Request, id int64) string {
	raisedOn := "/home"
	if item, err := s.svc.GetActionItem(r.Context(), id); err == nil {
		raisedOn = fmt.Sprintf("/reports/%d/publications/%d", item.DefinitionID, item.PublicationID)
	}
	return returnTo(r, raisedOn)
}
