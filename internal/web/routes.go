package web

// routes registers every HTTP route. It is the only place routes are wired, so
// a new feature area adds its handlers here and nowhere else.
func (s *Server) routes() {
	s.mux.HandleFunc("GET /", s.handleIndex)
	s.mux.HandleFunc("GET /signin", s.handleSignInForm)
	s.mux.HandleFunc("POST /signin", s.handleSignIn)
	s.mux.HandleFunc("POST /signout", s.handleSignOut)
	s.mux.HandleFunc("GET /goals", s.requireAuth(s.handleGoals))
	s.mux.HandleFunc("POST /goals", s.requireAuth(s.handleCreateGoal))
	s.mux.HandleFunc("GET /goals/{id}", s.requireAuth(s.handleViewGoal))
	s.mux.HandleFunc("POST /goals/{id}/links", s.requireAuth(s.handleRequestLink))
	s.mux.HandleFunc("GET /links", s.requireAuth(s.handlePendingLinks))
	s.mux.HandleFunc("POST /links/{id}/accept", s.requireAuth(s.handleAcceptLink))
	s.mux.HandleFunc("POST /links/{id}/reject", s.requireAuth(s.handleRejectLink))
	s.mux.HandleFunc("POST /links/{id}/remove", s.requireAuth(s.handleRemoveLink))
}
