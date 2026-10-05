package web

import (
	"context"
	"net/http"
	"strconv"

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
// directory sync when there is one, the Ownerless Goals waiting for an Admin
// to reassign them, and the Departed people an Admin can mark returned. Only
// an Admin may open it.
func (s *Server) handleAdmin(w http.ResponseWriter, r *http.Request, current domain.Account) {
	if !current.IsAdmin {
		http.Error(w, "only an Admin may open the Admin page", http.StatusForbidden)
		return
	}
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
	render(w, r, http.StatusOK, adminPage(&current, s.directorySyncStatus(), awaitingReassignment(goals), departed))
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
	v := &directorySyncView{Ran: true, At: last.At.In(s.svc.Timezone()).Format("2 Jan 2006 15:04"), People: last.People}
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
