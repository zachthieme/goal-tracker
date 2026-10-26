package web

import "net/http"

// RefusalOption is refusalOption, for the web_test package's tests.
type RefusalOption = refusalOption

// RefuseNotFoundSaying and RefuseFailureSaying are refuse's options, for the
// web_test package's tests.
var (
	RefuseNotFoundSaying = refuseNotFoundSaying
	RefuseFailureSaying  = refuseFailureSaying
)

// HandleRefusalForTest serves at pattern, among s's routes, a handler that
// refuses with heading, back and err, as refuse does with opts, so the
// web_test package's tests can reach every kind of refusal.
func (s *Server) HandleRefusalForTest(pattern, heading, back string, err error, opts ...RefusalOption) {
	s.mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
		s.refuse(w, r, heading, back, err, opts...)
	})
}
