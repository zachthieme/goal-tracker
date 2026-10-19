package web

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"github.com/zachthieme/goal-tracker/internal/directory"
	"github.com/zachthieme/goal-tracker/internal/domain"
)

// WithDirectorySync puts sync, the directory sync the app runs hourly, on the
// Admin page: when it last ran, how it went, and Sync now to run it at once.
// Without it the Admin page shows neither (ADR 0008).
func WithDirectorySync(sync *directory.Sync) Option {
	return func(s *Server) {
		s.directorySync = sync
	}
}

// handleAdmin is the home of the Admin tools: Dimensions, Import goals, the
// directory sync when there is one, set-password links with password sign-in,
// the Admins, the Ownerless Goals waiting for an Admin to reassign them, and
// the Departed people an Admin can mark returned. Only an Admin may open it.
func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request, current domain.Account) {
	if !current.IsAdmin {
		http.Error(w, "only an Admin may open the Admin page", http.StatusForbidden)
		return
	}
	s.renderAdmin(w, r, http.StatusOK, current, s.passwordLinksCard(), adminsSectionView{})
}

// passwordLinksCard is the set-password links card as the Admin page first
// shows it: empty with password sign-in, or no card (nil) without.
func (s *Server) passwordLinksCard() *passwordLinksView {
	if s.passwordFailures == nil {
		return nil
	}
	return &passwordLinksView{}
}

// renderAdmin draws the Admin page for current with status, its set-password
// links card showing links, or no card when links is nil, and its Admins
// section's form showing admins' Email and Refusal; renderAdmin lists the
// Admins themselves.
func (s *Server) renderAdmin(w http.ResponseWriter, r *http.Request, status int, current domain.Account, links *passwordLinksView, admins adminsSectionView) {
	goals, err := s.svc.ListGoals(r.Context())
	if err != nil {
		s.serverError(w, r, "could not read goals", err)
		return
	}
	departed, err := s.svc.DepartedAccounts(r.Context())
	if err != nil {
		s.serverError(w, r, "could not read departed people", err)
		return
	}
	admins.Admins, err = s.svc.Admins(r.Context())
	if err != nil {
		s.serverError(w, r, "could not read the Admins", err)
		return
	}
	render(w, r, status, adminPage(&current, s.directorySyncStatus(), links, admins, awaitingReassignment(goals), departed))
}

// adminsSectionView is the Admin page's Admins section: every Admin, and the
// Make Admin form, empty, or keeping the email typed with why it was refused.
type adminsSectionView struct {
	Admins  []domain.Account
	Email   string
	Refusal string
}

// passwordLinksView is the Admin page's set-password links card: empty before
// an Admin issues one, then the link just issued, shown this once, or why it
// wasn't.
type passwordLinksView struct {
	Email   string
	Link    string
	Expires string
	Refusal string
}

// handleIssuePasswordLink issues a set-password link for the email the Admin
// typed, creating the Account if there is none, and shows it on the Admin
// page. A Departed person is refused, as is anything that isn't an email
// address. Only an Admin may.
func (s *Server) handleIssuePasswordLink(w http.ResponseWriter, r *http.Request, current domain.Account) {
	if !current.IsAdmin {
		http.Error(w, "only an Admin may issue set-password links", http.StatusForbidden)
		return
	}
	emailAddr := strings.TrimSpace(r.FormValue("email"))
	if emailAddr == "" {
		s.renderAdmin(w, r, http.StatusUnprocessableEntity, current, &passwordLinksView{Refusal: "Enter the person's email."}, adminsSectionView{})
		return
	}
	token, expires, err := s.svc.IssuePasswordLink(r.Context(), emailAddr)
	if errors.Is(err, domain.ErrNotAnEmail) {
		s.renderAdmin(w, r, http.StatusUnprocessableEntity, current, &passwordLinksView{
			Email:   emailAddr,
			Refusal: "That isn't an email address.",
		}, adminsSectionView{})
		return
	}
	if errors.Is(err, domain.ErrDeparted) {
		s.renderAdmin(w, r, http.StatusUnprocessableEntity, current, &passwordLinksView{
			Email:   emailAddr,
			Refusal: emailAddr + " has left the company, so can't be given a set-password link. Mark them returned first.",
		}, adminsSectionView{})
		return
	}
	if err != nil {
		s.serverError(w, r, "could not issue the set-password link", err)
		return
	}
	acc, err := s.svc.AccountByEmail(r.Context(), emailAddr)
	if err != nil {
		s.serverError(w, r, "could not issue the set-password link", err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	s.renderAdmin(w, r, http.StatusOK, current, &passwordLinksView{
		Email:   acc.Email,
		Link:    s.svc.PasswordLinkURL(token),
		Expires: s.displayDates().timestamp(expires),
	}, adminsSectionView{})
}

// handleMakeAdmin makes the person with the email the Admin typed an Admin,
// then brings the Admin back to the Admins section. It never creates an
// Account: an email no one has is refused, as is a Departed person. Only an
// Admin may.
func (s *Server) handleMakeAdmin(w http.ResponseWriter, r *http.Request, current domain.Account) {
	if !current.IsAdmin {
		http.Error(w, "only an Admin may make someone an Admin", http.StatusForbidden)
		return
	}
	emailAddr := strings.TrimSpace(r.FormValue("email"))
	refuse := func(refusal string) {
		s.renderAdmin(w, r, http.StatusUnprocessableEntity, current, s.passwordLinksCard(), adminsSectionView{Email: emailAddr, Refusal: refusal})
	}
	if emailAddr == "" {
		refuse("Enter the person's email.")
		return
	}
	_, err := s.svc.MakeAdmin(r.Context(), current.ID, emailAddr)
	switch {
	case errors.Is(err, domain.ErrValidation):
		refuse("No one with that email has an account yet.")
		return
	case errors.Is(err, domain.ErrDeparted):
		refuse(emailAddr + " has left the company. Mark them returned first.")
		return
	case err != nil:
		s.serverError(w, r, "could not make the Admin", err)
		return
	}
	http.Redirect(w, r, "/admin#admin-admins", http.StatusSeeOther)
}

// handleRemoveAdmin takes the Admin role from the Account in the path, then
// brings the Admin back to the Admins section. No Admin can take it from
// themselves (CONTEXT.md: Admin); an Account that doesn't exist is not found.
// Only an Admin may.
func (s *Server) handleRemoveAdmin(w http.ResponseWriter, r *http.Request, current domain.Account) {
	if !current.IsAdmin {
		http.Error(w, "only an Admin may remove an Admin", http.StatusForbidden)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	if id == current.ID {
		s.renderAdmin(w, r, http.StatusUnprocessableEntity, current, s.passwordLinksCard(), adminsSectionView{Refusal: "You can't remove your own Admin role."})
		return
	}
	err = s.svc.RemoveAdmin(r.Context(), current.ID, id)
	if errors.Is(err, domain.ErrValidation) {
		s.notFound(w, r)
		return
	}
	if err != nil {
		s.serverError(w, r, "could not remove the Admin", err)
		return
	}
	http.Redirect(w, r, "/admin#admin-admins", http.StatusSeeOther)
}

// directorySyncView is the directory sync as the Admin page shows it: whether
// it has run, and when it last did, how many people it read or its error.
type directorySyncView struct {
	Ran    bool
	At     string
	People int
	Err    string
}

// directorySyncStatus is how the directory sync last went, or nil when the
// app doesn't sync a directory.
func (s *Server) directorySyncStatus() *directorySyncView {
	if s.directorySync == nil {
		return nil
	}
	last, ran := s.directorySync.Last()
	if !ran {
		return &directorySyncView{}
	}
	v := &directorySyncView{Ran: true, At: s.displayDates().timestamp(last.At), People: last.People}
	if last.Err != nil {
		v.Err = last.Err.Error()
	}
	return v
}

// directorySyncPeople says how many people a sync read: "1 person", "28 people".
func directorySyncPeople(n int) string {
	if n == 1 {
		return "1 person"
	}
	return strconv.Itoa(n) + " people"
}

// syncingScript shows Sync now's form busy once it's sent, as the sync runs
// inside the request: the form is marked aria-busy, and its button is disabled
// and reads "Syncing…". It lets the form post.
var syncingScript = templ.JSUnsafeFuncCall("this.setAttribute('aria-busy', 'true'); " +
	"const button = this.querySelector('button'); button.disabled = true; button.textContent = 'Syncing…'; " +
	"return true")

// handleDirectorySyncNow runs the directory sync at once, then brings the Admin
// back to the Admin page showing how it went. Only an Admin may.
func (s *Server) handleDirectorySyncNow(w http.ResponseWriter, r *http.Request, current domain.Account) {
	if !current.IsAdmin {
		http.Error(w, "only an Admin may sync the directory", http.StatusForbidden)
		return
	}
	// The sync is all-or-nothing, so it runs to the end even if the Admin
	// leaves the page.
	s.directorySync.Run(context.WithoutCancel(r.Context()))
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// awaitingReassignment are the Ownerless Goals still open: a Done or Cancelled
// Goal waits on nobody.
func awaitingReassignment(goals []domain.Goal) []domain.Goal {
	var out []domain.Goal
	for _, g := range goals {
		if g.Ownerless && g.Lifecycle != domain.LifecycleDone && g.Lifecycle != domain.LifecycleCancelled {
			out = append(out, g)
		}
	}
	return out
}
