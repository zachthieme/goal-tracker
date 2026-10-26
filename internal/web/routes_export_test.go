package web

import "net/http"

// RoutePattern is the pattern of the route among s's that serves r, as the
// mux matches it, so the web_test package's tests can check a path reaches a
// page rather than the catch-all (#609).
func (s *Server) RoutePattern(r *http.Request) string {
	_, pattern := s.mux.Handler(r)
	return pattern
}
