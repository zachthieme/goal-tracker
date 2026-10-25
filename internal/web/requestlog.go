package web

import (
	"context"
	"crypto/rand"
	"log/slog"
	"net/http"
	"time"
)

// requestIDHeader carries a request's ID: a proxy in front may send one, and
// every response echoes the one its request was logged under.
const requestIDHeader = "X-Request-ID"

// requestIDFor is the ID r is logged under: the one r's X-Request-ID carries
// when it is 1 to 64 letters, digits, underscores and hyphens, so a proxy's ID
// follows the request into the log; otherwise a new one, so a client can't
// flood or forge the log through it.
func requestIDFor(r *http.Request) string {
	if id := r.Header.Get(requestIDHeader); validRequestID(id) {
		return id
	}
	return rand.Text()
}

// validRequestID says whether id is 1 to 64 of [A-Za-z0-9_-].
func validRequestID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, c := range []byte(id) {
		switch {
		case 'a' <= c && c <= 'z', 'A' <= c && c <= 'Z', '0' <= c && c <= '9', c == '_', c == '-':
		default:
			return false
		}
	}
	return true
}

// accessWriter records what a request was answered with, for its access line.
type accessWriter struct {
	http.ResponseWriter
	// status is the status written, 0 until the handler writes one.
	status int
	// bytes counts the body bytes written.
	bytes int64
	// route is the pattern the request matched (see serveRoutes) or, when it
	// was refused before routing, the one it would have matched (see
	// ServeHTTP); empty when it matches none.
	route string
}

func (a *accessWriter) WriteHeader(code int) {
	if a.status == 0 && code >= http.StatusOK {
		a.status = code
	}
	a.ResponseWriter.WriteHeader(code)
}

func (a *accessWriter) Write(p []byte) (int, error) {
	if a.status == 0 {
		a.status = http.StatusOK
	}
	n, err := a.ResponseWriter.Write(p)
	a.bytes += int64(n)
	return n, err
}

// Unwrap lets an http.ResponseController reach the connection's own writer.
func (a *accessWriter) Unwrap() http.ResponseWriter {
	return a.ResponseWriter
}

// logAccess writes the request's access line to l, the request's logger: its
// method, the route it matched (see routeOrUnmatched), the status it was
// answered with, the bytes sent, and how long it took, elapsed.
func (a *accessWriter) logAccess(ctx context.Context, l *slog.Logger, method string, elapsed time.Duration) {
	l.LogAttrs(ctx, slog.LevelInfo, "request",
		slog.String("method", method),
		slog.String("route", a.routeOrUnmatched()),
		slog.Int("status", a.statusOr200()),
		slog.Int64("bytes", a.bytes),
		slog.Float64("duration_ms", float64(elapsed)/float64(time.Millisecond)),
	)
}

// routeOrUnmatched is the route the request matched (ServeHTTP fills in the
// one a refused request was aimed at), or "unmatched" only if none could be
// found.
func (a *accessWriter) routeOrUnmatched() string {
	if a.route == "" {
		return "unmatched"
	}
	return a.route
}

// statusOr200 is the status the request was answered with: 200 when the
// handler wrote none.
func (a *accessWriter) statusOr200() int {
	if a.status == 0 {
		return http.StatusOK
	}
	return a.status
}
