package web

import "net/http"

// HandleForPanicTest serves h at pattern among s's routes, so the web_test
// package's tests can reach a handler that panics.
func (s *Server) HandleForPanicTest(pattern string, h http.HandlerFunc) {
	s.mux.HandleFunc(pattern, h)
}
