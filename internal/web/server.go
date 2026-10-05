// Package web is the HTTP layer: stdlib net/http routing, templ pages, and
// htmx. Handlers call only the domain service (domain.Service); they never
// touch SQL, the clock, or the email sender directly.
package web

import (
	"context"
	"crypto/rand"
	"log/slog"
	"net/http"

	"github.com/a-h/templ"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

const sessionCookie = "gt_session"

// Server wires the routes over the domain service.
type Server struct {
	svc *domain.Service
	mux *http.ServeMux
	log *slog.Logger
	// sessions signs and checks the session cookie.
	sessions sessionCodec
	// secureCookies marks the session cookie Secure, for an app served over
	// https.
	secureCookies bool
	// oidc, when set, is the org's provider people sign in through; the
	// development email form is off.
	oidc *OIDC
}

// Option configures a Server as NewServer builds it.
type Option func(*Server)

// WithLogger sends the Server's log records, such as the cause of each 500, to
// logger. Without it, or with a nil logger, they go to slog.Default().
func WithLogger(logger *slog.Logger) Option {
	return func(s *Server) {
		if logger != nil {
			s.log = logger
		}
	}
}

// WithSessionKey signs session cookies with key, so sessions survive a restart
// that keeps it. Without it, NewServer signs them with a random key.
func WithSessionKey(key []byte) Option {
	return func(s *Server) {
		s.sessions.key = key
	}
}

// WithSecureCookies marks the session cookie Secure, so browsers send it only
// over https. Set it when the app is served over https.
func WithSecureCookies(secure bool) Option {
	return func(s *Server) {
		s.secureCookies = secure
	}
}

// NewServer builds a Server whose routes call svc.
func NewServer(svc *domain.Service, opts ...Option) *Server {
	s := &Server{svc: svc, mux: http.NewServeMux(), log: slog.Default()}
	s.sessions.key = make([]byte, 32)
	_, _ = rand.Read(s.sessions.key)
	for _, opt := range opts {
		opt(s)
	}
	s.routes()
	return s
}

// ServeHTTP makes Server an http.Handler. It records the browser's chosen
// theme in the request context, so every page the request renders is pinned to
// it, and the Server's logger, so a page that fails to draw is logged there.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	r = withTheme(r)
	s.mux.ServeHTTP(w, r.WithContext(withRequestLogger(r.Context(), s.log)))
}

// currentAccount resolves the signed-in Account from the session cookie, or nil
// if there is none or its signature doesn't verify. A Departed person's session ends with their departure, so
// their Account resolves to nil too (CONTEXT.md: Departed).
func (s *Server) currentAccount(r *http.Request) *domain.Account {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return nil
	}
	id, ok := s.sessions.decode(c.Value)
	if !ok {
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
// path in the context so the layout can mark the current page. The status is
// already written when a component fails partway, so the failure is only
// logged: at Warn when the client went away, at Error otherwise.
func render(w http.ResponseWriter, r *http.Request, status int, c templ.Component) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	err := c.Render(withPath(r.Context(), r.URL.Path), w)
	if err == nil {
		return
	}
	level := slog.LevelError
	if r.Context().Err() != nil {
		level = slog.LevelWarn
	}
	requestLogger(r.Context()).Log(r.Context(), level, "render failed", "method", r.Method, "path", r.URL.Path, "err", err)
}

type loggerKey struct{}

// withRequestLogger records l in ctx, so render can log a page that fails to
// draw to the Server's logger.
func withRequestLogger(ctx context.Context, l *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey{}, l)
}

// requestLogger is the logger recorded in ctx, slog.Default() when none was.
func requestLogger(ctx context.Context) *slog.Logger {
	if l, ok := ctx.Value(loggerKey{}).(*slog.Logger); ok {
		return l
	}
	return slog.Default()
}
