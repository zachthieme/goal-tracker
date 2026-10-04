package web

import "net/http"

// serverError answers an unexpected failure with a plain-text 500 carrying
// msg, and logs err, the failure's cause, with the request it failed.
func (s *Server) serverError(w http.ResponseWriter, r *http.Request, msg string, err error) {
	s.logServerError(r, err)
	http.Error(w, msg, http.StatusInternalServerError)
}

// logServerError logs err as the cause of the 500 that r is answered with. A
// handler that draws its own 500 page calls it; serverError calls it for the
// rest.
func (s *Server) logServerError(r *http.Request, err error) {
	s.log.ErrorContext(r.Context(), "server error", "method", r.Method, "path", r.URL.Path, "err", err)
}
