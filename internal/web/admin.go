package web

import (
	"context"
	"errors"
	"fmt"
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
		s.refuseNonAdmin(w, r, "only an Admin may open the Admin page")
		return
	}
	s.renderAdmin(w, r, http.StatusOK, current, s.passwordLinksCard(), adminsSectionView{})
}

// refuseNonAdmin answers someone who isn't an Admin, at an Admin-only route,
// with the refusal page at 403: headed "Admins only", saying reason, and
// linking Back to Home.
func (s *Server) refuseNonAdmin(w http.ResponseWriter, r *http.Request, reason string) {
	s.answerRefused(w, r, refused{Err: domain.Refuse(domain.ErrNotAuthorized, "%s", reason), Heading: "Admins only", Back: "/home"})
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
	page, err := s.adminPageFor(r, current, links, admins)
	if err != nil {
		s.serverError(w, r, "could not load the Admin page", err)
		return
	}
	render(w, r, status, page)
}

// adminPageFor is the Admin page renderAdmin draws.
func (s *Server) adminPageFor(r *http.Request, current domain.Account, links *passwordLinksView, admins adminsSectionView) (templ.Component, error) {
	goals, err := s.svc.ListGoals(r.Context())
	if err != nil {
		return nil, fmt.Errorf("read goals: %w", err)
	}
	departed, err := s.svc.DepartedAccounts(r.Context())
	if err != nil {
		return nil, fmt.Errorf("read departed people: %w", err)
	}
	admins.Admins, err = s.svc.Admins(r.Context())
	if err != nil {
		return nil, fmt.Errorf("read the Admins: %w", err)
	}
	return adminPage(&current, s.directorySyncStatus(), links, admins, awaitingReassignment(goals), departed), nil
}

// refusedAdminForm is a refused form on the Admin page, err, as
// answerRefused answers it: one refused for what was typed comes back on the
// Admin page, at 422, as form draws it saying why (domain.Reason), and
// someone who isn't an Admin is refused as refuseNonAdmin refuses them.
func (s *Server) refusedAdminForm(r *http.Request, current domain.Account, err error, form func(reason string) (*passwordLinksView, adminsSectionView)) refused {
	return refused{Err: err, Heading: "Admins only", Back: "/home",
		Form: asPage(func(_ context.Context, err error) (templ.Component, error) {
			links, admins := form(domain.Reason(err))
			return s.adminPageFor(r, current, links, admins)
		})}
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
// page, logging who issued it for whom. A Departed person is refused, as is
// anything that isn't an email address. Only an Admin may.
func (s *Server) handleIssuePasswordLink(w http.ResponseWriter, r *http.Request, current domain.Account) {
	if !current.IsAdmin {
		s.refuseNonAdmin(w, r, "only an Admin may issue set-password links")
		return
	}
	emailAddr := strings.TrimSpace(r.FormValue("email"))
	refuse := func(err error) {
		s.answerRefused(w, r, s.refusedAdminForm(r, current, err, func(reason string) (*passwordLinksView, adminsSectionView) {
			return &passwordLinksView{Email: emailAddr, Refusal: reason}, adminsSectionView{}
		}))
	}
	if emailAddr == "" {
		refuse(domain.Refuse(domain.ErrValidation, "Enter the person's email."))
		return
	}
	link, err := s.svc.IssuePasswordLink(r.Context(), current.ID, emailAddr)
	// The domain's refusals, said as the card says them.
	switch {
	case errors.Is(err, domain.ErrNotAuthorized):
		err = domain.Refuse(domain.ErrNotAuthorized, "only an Admin may issue set-password links")
	case errors.Is(err, domain.ErrNotAnEmail):
		err = domain.Refuse(domain.ErrValidation, "That isn't an email address.")
	case errors.Is(err, domain.ErrDeparted):
		err = domain.Refuse(domain.ErrValidation, "%s has left the company, so can't be given a set-password link. Mark them returned first.", emailAddr)
	}
	if err != nil {
		refuse(err)
		return
	}
	// The link signs in as its person, so who issued one is recorded; the
	// link and its token never are.
	requestLogger(r.Context()).Info("set-password link issued",
		"issuer_id", current.ID, "issuer_email", current.Email,
		"account_id", link.Account.ID, "email", link.Account.Email, "expires", link.Expires)
	w.Header().Set("Cache-Control", "no-store")
	s.renderAdmin(w, r, http.StatusOK, current, &passwordLinksView{
		Email:   link.Account.Email,
		Link:    s.svc.PasswordLinkURL(link.Token),
		Expires: s.displayDates().timestamp(link.Expires),
	}, adminsSectionView{})
}

// handleMakeAdmin makes the person with the email the Admin typed an Admin,
// then brings the Admin back to the Admins section. It never creates an
// Account: an email no one has is refused, as is a Departed person. Only an
// Admin may.
func (s *Server) handleMakeAdmin(w http.ResponseWriter, r *http.Request, current domain.Account) {
	const notAdmin = "only an Admin may make someone an Admin"
	if !current.IsAdmin {
		s.refuseNonAdmin(w, r, notAdmin)
		return
	}
	emailAddr := strings.TrimSpace(r.FormValue("email"))
	refuse := func(err error) {
		s.answerRefused(w, r, s.refusedAdminForm(r, current, err, func(reason string) (*passwordLinksView, adminsSectionView) {
			return s.passwordLinksCard(), adminsSectionView{Email: emailAddr, Refusal: reason}
		}))
	}
	if emailAddr == "" {
		refuse(domain.Refuse(domain.ErrValidation, "Enter the person's email."))
		return
	}
	_, err := s.svc.MakeAdmin(r.Context(), current.ID, emailAddr)
	// The domain's refusals, said as the Admins form says them.
	switch {
	case errors.Is(err, domain.ErrValidation):
		err = domain.Refuse(domain.ErrValidation, "No one with that email has an account yet.")
	case errors.Is(err, domain.ErrDeparted):
		err = domain.Refuse(domain.ErrValidation, "%s has left the company. Mark them returned first.", emailAddr)
	case errors.Is(err, domain.ErrNotAuthorized):
		// They stopped being an Admin since the check above.
		err = domain.Refuse(domain.ErrNotAuthorized, notAdmin)
	}
	if err != nil {
		refuse(err)
		return
	}
	http.Redirect(w, r, "/admin#admin-admins", http.StatusSeeOther)
}

// handleRemoveAdmin takes the Admin role from the Account in the path, then
// brings the Admin back to the Admins section. No Admin can take it from
// themselves (CONTEXT.md: Admin); an id that isn't one, or names no Account,
// is the Not found page. Only an Admin may.
func (s *Server) handleRemoveAdmin(w http.ResponseWriter, r *http.Request, current domain.Account) {
	const notAdmin = "only an Admin may remove an Admin"
	if !current.IsAdmin {
		s.refuseNonAdmin(w, r, notAdmin)
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		s.notFound(w, r)
		return
	}
	refuse := func(err error) {
		s.answerRefused(w, r, s.refusedAdminForm(r, current, err, func(reason string) (*passwordLinksView, adminsSectionView) {
			return s.passwordLinksCard(), adminsSectionView{Refusal: reason}
		}))
	}
	if id == current.ID {
		refuse(domain.Refuse(domain.ErrValidation, "You can't remove your own Admin role."))
		return
	}
	err = s.svc.RemoveAdmin(r.Context(), current.ID, id)
	if errors.Is(err, domain.ErrNotAuthorized) {
		// They stopped being an Admin since the check above.
		err = domain.Refuse(domain.ErrNotAuthorized, notAdmin)
	}
	if err != nil {
		refuse(err)
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

// handleDirectorySyncNow runs the directory sync at once, logging which Admin
// asked, then brings the Admin back to the Admin page showing how it went.
// Only an Admin may.
func (s *Server) handleDirectorySyncNow(w http.ResponseWriter, r *http.Request, current domain.Account) {
	if !current.IsAdmin {
		s.refuseNonAdmin(w, r, "only an Admin may sync the directory")
		return
	}
	logger := requestLogger(r.Context())
	logger.Info("an Admin asked for a directory sync", "admin_id", current.ID, "admin_email", current.Email)
	// The sync is all-or-nothing, so it runs to the end even if the Admin
	// leaves the page. It logs with the request's ID.
	out := s.directorySync.RunForAdmin(context.WithoutCancel(r.Context()), logger, current.ID)
	if errors.Is(out.Err, domain.ErrNotAuthorized) {
		s.refuseNonAdmin(w, r, "only an Admin may sync the directory")
		return
	}
	http.Redirect(w, r, "/admin", http.StatusSeeOther)
}

// awaitingReassignment are the Ownerless Goals still open: a Done or Cancelled
// Goal waits on nobody.
func awaitingReassignment(goals []domain.Goal) []domain.Goal {
	var out []domain.Goal
	for _, g := range goals {
		if g.Ownerless && !domain.LifecycleEnded(g.Lifecycle) {
			out = append(out, g)
		}
	}
	return out
}
