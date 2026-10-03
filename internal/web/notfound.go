package web

import "net/http"

// notFound answers 404 with the Not found page, for an address that names
// nothing: an unknown URL, or a Goal, Report or other thing that doesn't exist.
// It resolves the signed-in Account itself, so it serves signed-out visitors
// too.
func (s *Server) notFound(w http.ResponseWriter, r *http.Request) {
	render(w, r, http.StatusNotFound, notFoundPage(s.currentAccount(r)))
}
