package web

import (
	"net/http"
	"slices"
	"strings"
)

// notFound answers 404 with the Not found page, for an address that names
// nothing: an unknown URL, or a Goal, Report or other thing that doesn't exist.
// It resolves the signed-in Account itself, so it serves signed-out visitors
// too.
func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusNotFound, notFoundPage(s.currentAccount(r)))
}

// handleUnknown catches a request no route serves with its method; GET's own
// catch-all, handleIndex, answers GET. A path some route serves with another
// method answers 405, as the mux would, with an Allow header naming those
// methods. Any other path names nothing, and answers with the Not found page.
func (s *Server) handleUnknown(w http.ResponseWriter, r *http.Request) {
	if allowed := s.allowedMethods(r); len(allowed) > 0 {
		w.Header().Set("Allow", strings.Join(allowed, ", "))
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	if acc := s.currentAccount(r); acc != nil {
		r = s.withTopBarCounts(r, *acc)
	}
	s.notFound(w, r)
}

// allowedMethods lists, sorted, the methods some route other than a catch-all
// serves r's path with, plus HEAD when GET is among them.
func (s *Server) allowedMethods(r *http.Request) []string {
	var allowed []string
	for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete} {
		probe := r.Clone(r.Context())
		probe.Method = m
		if _, pattern := s.mux.Handler(probe); pattern != "" && pattern != "/" && pattern != "GET /" {
			allowed = append(allowed, m)
		}
	}
	if slices.Contains(allowed, http.MethodGet) {
		allowed = append(allowed, http.MethodHead)
	}
	slices.Sort(allowed)
	return allowed
}
