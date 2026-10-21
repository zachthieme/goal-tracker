package web

import (
	"context"
	"io"
	"net/http"
	"time"
)

// healthPingTimeout bounds how long a health check waits on the database, so a
// probe hears back promptly even when the database hangs.
const healthPingTimeout = time.Second

// handleHealthz answers a load balancer's or orchestrator's probe, signed in or
// not: 200 ok while the database answers, 503 when it doesn't.
func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), healthPingTimeout)
	defer cancel()
	if err := s.svc.Ping(ctx); err != nil {
		s.requestLog(r).WarnContext(r.Context(), "health check failed", "err", err)
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = io.WriteString(w, "ok")
}
