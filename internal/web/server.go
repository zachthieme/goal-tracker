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

	"github.com/zachthieme/goal-tracker/internal/directory"
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
	// passwordFailures, when set, means people sign in with a password set
	// through an Admin's set-password link, and counts each client IP's failed
	// tries; the development email form is off (see WithPasswordSignIn).
	passwordFailures *passwordFailuresByIP
	// directorySync, when set, syncs people and their Managers from the org's
	// directory; the Admin page shows it (see WithDirectorySync).
	directorySync *directory.Sync
	// pdf, when set, renders a publication's print page to a PDF file to
	// download (see WithPDFRenderer); without it, a PDF download is the print
	// page, printed from the browser.
	pdf *pdfDownloads
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

// WithPasswordSignIn has people sign in with an email and password, the
// password set only through a set-password link an Admin issues on the Admin
// page. The development email form is off.
func WithPasswordSignIn() Option {
	return func(s *Server) {
		s.passwordFailures = newPasswordFailuresByIP()
	}
}

// PDFRenderer renders a page to a PDF: its HTML, whose assets, such as
// /static/fonts/, resolve against baseURL, in; the PDF's bytes out.
type PDFRenderer interface {
	Render(ctx context.Context, html []byte, baseURL string) ([]byte, error)
}

// WithPDFRenderer has a publication download as a PDF file, its print page
// rendered by r, at most pdfRendersAtOnce at a time, each within
// pdfRenderTimeout. Without it, or with a nil r, a PDF download is the print
// page, for the reader to print from the browser.
func WithPDFRenderer(r PDFRenderer) Option {
	return func(s *Server) {
		if r == nil {
			s.pdf = nil
			return
		}
		s.pdf = &pdfDownloads{renderer: r, slots: make(chan struct{}, pdfRendersAtOnce), timeout: pdfRenderTimeout}
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
// if there is none or it no longer signs anyone in (see openSession): its
// signature doesn't verify, it carries a session generation EndSessions has
// since ended, or it has expired. A Departed person's session ends with their
// departure, so their Account resolves to nil too (CONTEXT.md: Departed).
func (s *Server) currentAccount(r *http.Request) *domain.Account {
	_, acc, ok := s.openSession(r)
	if !ok {
		return nil
	}
	return &acc
}

// requireAuth wraps a handler so it runs only for a signed-in Account, which it
// passes along, with what the sidebar shows in the request context. It slides
// the session's idle limit on (see slideSession).
func (s *Server) requireAuth(h func(http.ResponseWriter, *http.Request, domain.Account)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		sess, acc, ok := s.openSession(r)
		if !ok {
			http.Redirect(w, r, "/signin", http.StatusSeeOther)
			return
		}
		s.slideSession(w, sess)
		h(w, s.withSidebar(r, acc), acc)
	}
}

// withSidebar returns r with what the sidebar shows read into its context,
// once per request: what needs acc, the Goals Follow-ups flags in acc's
// default scope, and the people directly under acc for My org. Anything that
// can't be read is left out rather than failing the page.
func (s *Server) withSidebar(r *http.Request, acc domain.Account) *http.Request {
	if v, err := s.loadHome(r.Context(), acc.ID); err == nil {
		r = r.WithContext(withNeedsYou(r.Context(), v.NeedsYou()))
	}
	if n, err := s.countDefaultFollowUps(r.Context(), acc); err == nil {
		r = r.WithContext(withFollowUps(r.Context(), n))
	}
	if people, err := s.svc.DirectlyUnder(r.Context(), acc.ID); err == nil {
		r = r.WithContext(withOrgPeople(r.Context(), people))
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
