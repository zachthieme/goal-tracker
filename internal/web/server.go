// Package web is the HTTP layer: stdlib net/http routing, templ pages, and
// htmx. Handlers call only the domain service (domain.Service); they never
// touch SQL, the clock, or the email sender directly.
package web

import (
	"net/http"
	"strconv"

	"github.com/a-h/templ"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

const sessionCookie = "gt_session"

// Server wires the routes over the domain service.
type Server struct {
	svc *domain.Service
	mux *http.ServeMux
}

// NewServer builds a Server whose routes call svc.
func NewServer(svc *domain.Service) *Server {
	s := &Server{svc: svc, mux: http.NewServeMux()}
	s.routes()
	return s
}

// ServeHTTP makes Server an http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// currentAccount resolves the signed-in Account from the session cookie, or nil
// if there is none.
func (s *Server) currentAccount(r *http.Request) *domain.Account {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil
	}
	id, err := strconv.ParseInt(c.Value, 10, 64)
	if err != nil {
		return nil
	}
	acc, err := s.svc.Account(r.Context(), id)
	if err != nil {
		return nil
	}
	return &acc
}

// requireAuth wraps a handler so it runs only for a signed-in Account, which it
// passes along. It counts what needs the Account into the request context, so
// the top bar's Home item can show it on every page.
func (s *Server) requireAuth(h func(http.ResponseWriter, *http.Request, domain.Account)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		acc := s.currentAccount(r)
		if acc == nil {
			http.Redirect(w, r, "/signin", http.StatusSeeOther)
			return
		}
		// A count that can't be read leaves Home uncounted rather than
		// failing the page.
		if v, err := s.loadHome(r.Context(), acc.ID); err == nil {
			r = r.WithContext(withNeedsYou(r.Context(), v.NeedsYou()))
		}
		h(w, r, *acc)
	}
}

// render writes a templ component with the given status. It puts the request
// path in the context so the layout can mark the current page.
func render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = c.Render(withPath(r.Context(), r.URL.Path), w)
}
