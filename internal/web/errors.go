package web

import (
	"context"
	"errors"
	"net/http"
)

// serverError answers an unexpected failure with a plain-text 500 carrying
// msg, and logs err, the failure's cause, with the request it failed.
func (s *Server) serverError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	s.logServerError(r, err)
	http.Error(w, msg, http.StatusInternalServerError)
}

// logServerError logs err as the cause of the 500 that r is answered with. A
// handler that draws its own 500 page calls it; serverError calls it for the
// rest. A failure caused by the client abandoning r, which cancels r's
// context, isn't a server fault, so it's logged at Debug without its cause.
func (s *Server) logServerError(r *http.Request, err error) {
	if errors.Is(err, context.Canceled) && r.Context().Err() != nil {
		s.log.DebugContext(r.Context(), "request abandoned", "method", r.Method, "path", r.URL.Path)
		return
	}
	s.log.ErrorContext(r.Context(), "server error", "method", r.Method, "path", r.URL.Path, "err", err)
}
