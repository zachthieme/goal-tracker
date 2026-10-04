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

// ServeHTTP makes Server an http.Handler. It records the browser's chosen
// theme in the request context, so every page the request renders is pinned to
// it.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, withTheme(r))
}

// currentAccount resolves the signed-in Account from the session cookie, or nil
// if there is none. A Departed person's session ends with their departure, so
// their Account resolves to nil too (CONTEXT.md: Departed).
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
	if err != nil || acc.Departed {
		return nil
	}
	return &acc
}

// requireAuth wraps a handler so it runs only for a signed-in Account, which it
// passes along, with the top bar's counts in the request context.
func (s *Server) requireAuth(h func(http.ResponseWriter, *http.Request, domain.Account)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		acc := s.currentAccount(r)
		if acc == nil {
			http.Redirect(w, r, "/signin", http.StatusSeeOther)
			return
		}
		h(w, s.withTopBarCounts(r, *acc), *acc)
	}
}

// withTopBarCounts returns r with what needs acc, and the Goals flagged as
// risks, counted into its context, so the top bar's Home and Risks items can
// show them. A count that can't be read leaves its item uncounted rather than
// failing the page.
func (s *Server) withTopBarCounts(r *http.Request, acc domain.Account) *http.Request {
	if v, err := s.loadHome(r.Context(), acc.ID); err == nil {
		r = r.WithContext(withNeedsYou(r.Context(), v.NeedsYou()))
	}
	if v, err := s.loadRisks(r.Context()); err == nil {
		r = r.WithContext(withRisks(r.Context(), v.Flagged()))
	}
	return r
}

// render writes a templ component with the given status. It puts the request
// path in the context so the layout can mark the current page.
func render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	_ = c.Render(withPath(r.Context(), r.URL.Path), w)
}
