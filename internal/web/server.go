// Package web is the HTTP layer: stdlib net/http routing, templ pages, and
// htmx. Handlers call only the domain service (domain.Service); they never
// touch SQL, the clock, or the email sender directly.
package web

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"runtime/debug"
	"time"

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
	// app is the mux behind the checks every request passes first (see
	// ServeHTTP).
	app http.Handler
	// sessions signs and checks the session cookie.
	sessions sessionCodec
	// secureCookies marks the session cookie Secure, and has every response
	// carry Strict-Transport-Security, for an app served over https.
	secureCookies bool
	// oidc, when set, is the org's provider people sign in through; the
	// development email form is off.
	oidc *OIDC
	// passwordFailures, when set, means people sign in with a password set
	// through an Admin's set-password link, and counts each client IP's failed
	// tries; the development email form is off (see WithPasswordSignIn).
	passwordFailures *passwordFailuresByIP
	// trustedProxies are the reverse proxies whose X-Forwarded-For names the
	// client a password sign-in is throttled by (see WithTrustedProxies).
	trustedProxies []netip.Prefix
	// directorySync, when set, syncs people and their Managers from the org's
	// directory; the Admin page shows it (see WithDirectorySync).
	directorySync *directory.Sync
	// pdf, when set, renders a publication's print page to a PDF file to
	// download (see WithPDFRenderer); without it, a PDF download is the print
	// page, printed from the browser.
	pdf *pdfDownloads
	// metrics, when set, counts and times every request answered (see
	// WithRequestMetrics).
	metrics *RequestMetrics
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
// over https, and has every response carry Strict-Transport-Security, so a
// browser that has reached the app over https comes back only over https. Set
// it when the app is served over https.
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

// WithTrustedProxies has the password sign-in throttle count a request that
// arrives from one of proxies against the client its X-Forwarded-For names:
// the right-most address there that isn't one of proxies. Without it, or with
// none, X-Forwarded-For is ignored, and every request counts against the
// address it arrived from.
func WithTrustedProxies(proxies []netip.Prefix) Option {
	return func(s *Server) {
		s.trustedProxies = proxies
	}
}

// PDFRenderer renders a page to a PDF: its HTML, whose assets, such as
// /static/fonts/, resolve against baseURL, in; the PDF's bytes out.
type PDFRenderer interface {
	Render(ctx context.Context, html []byte, baseURL string) ([]byte, error)
}

// WithPDFRenderer has a publication download as a PDF file, its print page
// rendered by r, at most pdfRendersAtOnce at a time, each within
// PDFRenderTimeout. Without it, or with a nil r, a PDF download is the print
// page, for the reader to print from the browser.
func WithPDFRenderer(r PDFRenderer) Option {
	return func(s *Server) {
		if r == nil {
			s.pdf = nil
			return
		}
		s.pdf = &pdfDownloads{renderer: r, slots: make(chan struct{}, pdfRendersAtOnce), timeout: PDFRenderTimeout}
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
	s.app = http.NewCrossOriginProtection().Handler(http.HandlerFunc(s.serveRoutes))
	return s
}

// ServeHTTP makes Server an http.Handler. Each request is logged under an ID,
// echoed in the response's X-Request-ID, that every record logged while
// handling it carries, and gets one access line once it is answered. It
// refuses, with a 403, a request other than GET, HEAD or OPTIONS that a
// browser says came from another origin, so a page elsewhere can't act as
// whoever is signed in, and, with a 413, one whose body is over its cap (see
// requestBodyCap). Every response carries securityHeaders, and, when the app
// is served over https (WithSecureCookies), Strict-Transport-Security. A
// handler that panics is answered 500, unless it already wrote a status, and
// its panic is logged (see recoverHandlerPanic). With WithRequestMetrics, each
// request is recorded there, once, under its access line's route and status.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	id := requestIDFor(r)
	w.Header().Set(requestIDHeader, id)
	l := s.log.With("request_id", id)
	aw := &accessWriter{ResponseWriter: w}
	defer func() {
		p := recover()
		if p != nil && p != http.ErrAbortHandler {
			recoverHandlerPanic(aw, r, l, p)
		}
		if aw.route == "" {
			// Refused before routing, or panicked in its handler: logged under
			// the route it was aimed at.
			_, aw.route = s.mux.Handler(r)
		}
		elapsed := time.Since(start)
		aw.logAccess(r.Context(), l, r.Method, elapsed)
		if s.metrics != nil {
			s.metrics.record(r.Method, aw.routeOrUnmatched(), aw.statusOr200(), elapsed)
		}
		if p == http.ErrAbortHandler {
			// net/http aborts the response without logging it.
			panic(p)
		}
	}()
	s.serveChecked(aw, r.WithContext(withRequestLogger(r.Context(), l)))
}

// recoverHandlerPanic logs p, the value r's handler panicked with, at ERROR to
// l, the request's logger, with the stack it panicked from, and answers r 500,
// as serverError does, unless the handler already wrote a status.
func recoverHandlerPanic(w *accessWriter, r *http.Request, l *slog.Logger, p any) {
	l.ErrorContext(r.Context(), "handler panicked", "method", r.Method, "path", r.URL.Path,
		"panic", fmt.Sprint(p), "stack", string(debug.Stack()))
	if w.status == 0 {
		http.Error(w, "something went wrong", http.StatusInternalServerError)
	}
}

// serveChecked serves r once it passes the checks ServeHTTP describes.
func (s *Server) serveChecked(w *accessWriter, r *http.Request) {
	for name, value := range securityHeaders {
		w.Header().Set(name, value)
	}
	if s.secureCookies {
		// Served over https: have the browser come back only over https.
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
	}
	limit := s.requestBodyCap(r)
	if r.ContentLength > limit {
		bodyTooLarge(w)
		return
	}
	// The connection's own writer, so a body read past the cap closes the
	// connection after the response.
	r.Body = http.MaxBytesReader(w.ResponseWriter, r.Body, limit)
	if r.ContentLength < 0 {
		// Sent in chunks, with no length up front: read it now, so a body over
		// the cap is refused here rather than failing whatever reads it.
		body, err := io.ReadAll(r.Body)
		if err != nil {
			if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
				bodyTooLarge(w)
			} else {
				http.Error(w, "Could not read the request.", http.StatusBadRequest)
			}
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		r.ContentLength = int64(len(body))
	}
	s.app.ServeHTTP(w, r)
}

// bodyTooLarge refuses a request whose body is over its cap.
func bodyTooLarge(w http.ResponseWriter) {
	http.Error(w, "That is more than the app accepts at once.", http.StatusRequestEntityTooLarge)
}

// maxRequestBytes caps a request's body, so a huge one can't tie up a
// connection or fill memory. Only an import's upload may be larger.
const maxRequestBytes = 1 << 20 // 1 MiB

// requestBodyCap is the most r's body may carry: maxImportRequestBytes for an
// import's upload whose session signs someone in, maxRequestBytes for anything
// else. The session is checked first, so a signed-out POST /imports can't have
// the server read, or buffer if it's sent in chunks, more than an ordinary
// request.
func (s *Server) requestBodyCap(r *http.Request) int64 {
	if r.Method == http.MethodPost && r.URL.Path == "/imports" {
		if _, _, ok := s.openSession(r); ok {
			return maxImportRequestBytes
		}
	}
	return maxRequestBytes
}

// securityHeaders are set on every response before its handler runs, so a
// handler that sets one of its own, as the set-password page does with
// Referrer-Policy, has the last word. They keep the app out of other sites'
// frames, so a click there can't land on one of its buttons; refuse plugins
// and any <base> tag, so injected markup can't load either or point the page's
// relative links elsewhere; stop the browser guessing a response's content
// type; and send the app's addresses as Referer only to the app itself.
var securityHeaders = map[string]string{
	"Content-Security-Policy": "frame-ancestors 'none'; object-src 'none'; base-uri 'none'",
	"X-Content-Type-Options":  "nosniff",
	"Referrer-Policy":         "same-origin",
}

// serveRoutes hands r to the route it matches. It records the browser's chosen
// theme in the request context, so every page the request renders is pinned to
// it, and the route's pattern for the request's access line.
func (s *Server) serveRoutes(w http.ResponseWriter, r *http.Request) {
	r = withTheme(r)
	s.mux.ServeHTTP(w, r)
	if aw, ok := w.(*accessWriter); ok {
		aw.route = r.Pattern
	}
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
// once per request through its page loader (ADR 0010): what needs acc, the
// Goals Follow-ups flags in acc's default scope, and the people directly
// under acc for My org. A part that can't be read is left out rather than
// failing the page, and the failure is logged at Warn.
func (s *Server) withSidebar(r *http.Request, acc domain.Account) *http.Request {
	sb, err := s.svc.Sidebar(r.Context(), acc.ID)
	if err != nil {
		s.requestLog(r).WarnContext(r.Context(), "sidebar read failed", "method", r.Method, "path", r.URL.Path, "err", err)
	}
	ctx := r.Context()
	if sb.NeedsYouRead {
		ctx = withNeedsYou(ctx, sb.NeedsYou)
	}
	if sb.FollowUpsRead {
		ctx = withFollowUps(ctx, sb.FollowUps)
	}
	if sb.DirectlyUnderRead {
		ctx = withOrgPeople(ctx, sb.DirectlyUnder)
	}
	return r.WithContext(ctx)
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

// withRequestLogger records l, the request's logger, in ctx, so whatever is
// logged while handling the request, such as a page that fails to draw, goes
// to the Server's logger carrying the request's ID.
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

// requestLog is r's logger, carrying its request ID, or the Server's own for a
// request that didn't come through ServeHTTP.
func (s *Server) requestLog(r *http.Request) *slog.Logger {
	if l, ok := r.Context().Value(loggerKey{}).(*slog.Logger); ok {
		return l
	}
	return s.log
}
