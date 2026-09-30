package web

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// discussion is what a Report page needs to take part in its discussion
// (ticket #19): the comment threads and Action Items raised on a publication,
// and which Action Items the reader may close. Its zero value is read-only —
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
	// Open marks the Action Items open now, so a frozen list can still offer
	// their owners the close form.
	Open map[int64]bool
	// Author is set when the reader is the Report's author, who raises Action
	// Items.
	Author bool
	// Return is the page a form comes back to.
	Return string
}

// canClose reports whether the reader may close the Action Item now: they own
// it and it is still open.
func (d discussion) canClose(item domain.ActionItem) bool {
	return d.Current != nil && d.Current.ID == item.Owner.ID && d.Open[item.ID]
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
	open, err := s.openActionItems(r, pub.DefinitionID)
	if err != nil {
		return discussion{}, err
	}
	author, err := s.svc.IsReportAuthor(ctx, current.ID, pub.ID)
	if err != nil {
		return discussion{}, err
	}
	return discussion{
		Current: &current,
		Pub:     &pub,
		Threads: byGoal,
		Raised:  raised,
		Open:    open,
		Author:  author,
		Return:  publicationPath(pub),
	}, nil
}

// draftDiscussion is the draft's part in the discussion: its Action Items are
// read live, so their owners can close them from it.
func (s *Server) draftDiscussion(r *http.Request, current domain.Account, defID int64) (discussion, error) {
	open, err := s.openActionItems(r, defID)
	if err != nil {
		return discussion{}, err
	}
	return discussion{Current: &current, Open: open, Return: fmt.Sprintf("/reports/%d", defID)}, nil
}

// openActionItems is the set of the Report Definition's Action Items open now.
func (s *Server) openActionItems(r *http.Request, defID int64) (map[int64]bool, error) {
	items, err := s.svc.OpenActionItems(r.Context(), defID)
	if err != nil {
		return nil, err
	}
	open := make(map[int64]bool, len(items))
	for _, item := range items {
		open[item.ID] = true
	}
	return open, nil
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
		writeCommentError(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("%s#goal-%d", publicationPath(pub), goalID), http.StatusSeeOther)
}

// handleReplyToComment replies in a comment's thread and comes back to it.
func (s *Server) handleReplyToComment(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read the form", http.StatusBadRequest)
		return
	}
	reply, err := s.svc.ReplyToComment(r.Context(), current.ID, id, r.FormValue("body"))
	if err != nil {
		writeCommentError(w, err)
		return
	}
	pub, err := s.svc.GetPublication(r.Context(), reply.PublicationID)
	if err != nil {
		writeCommentError(w, err)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("%s#goal-%d", publicationPath(pub), reply.GoalID), http.StatusSeeOther)
}

// handleRaiseActionItem raises an Action Item on a published Report, from a
// comment or directly, owned by the Account the owner field names by email.
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
	if _, err := s.svc.RaiseActionItemByEmail(r.Context(), current.ID, domain.RaiseActionItemInput{
		PublicationID: pub.ID,
		CommentID:     commentID,
		Text:          r.FormValue("text"),
		DueDate:       due,
	}, r.FormValue("owner")); err != nil {
		writeCommentError(w, err)
		return
	}
	http.Redirect(w, r, publicationPath(pub)+"#action-items", http.StatusSeeOther)
}

// handleCloseActionItem closes an Action Item with a note and comes back to the
// page it was closed from, or else the publication it was raised on.
func (s *Server) handleCloseActionItem(w http.ResponseWriter, r *http.Request, current domain.Account) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "could not read the form", http.StatusBadRequest)
		return
	}
	item, err := s.svc.CloseActionItem(r.Context(), current.ID, id, r.FormValue("note"))
	if err != nil {
		writeCommentError(w, err)
		return
	}
	back := r.FormValue("return")
	if !localPath(back) {
		back = fmt.Sprintf("/reports/%d/publications/%d", item.DefinitionID, item.PublicationID)
	}
	http.Redirect(w, r, back, http.StatusSeeOther)
}

// localPath reports whether p is a path on this site, safe to redirect to.
func localPath(p string) bool {
	return strings.HasPrefix(p, "/") && !strings.HasPrefix(p, "//") && !strings.Contains(p, `\`)
}

// writeCommentError maps a domain comment or Action Item error to an HTTP
// status.
func writeCommentError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrValidation):
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
	case errors.Is(err, domain.ErrNotAuthorized):
		http.Error(w, err.Error(), http.StatusForbidden)
	case errors.Is(err, domain.ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	default:
		http.Error(w, "comment action failed", http.StatusInternalServerError)
	}
}
