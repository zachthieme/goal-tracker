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
}
