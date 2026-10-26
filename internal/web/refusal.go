package web

import (
	"errors"
	"net/http"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// cycleRefusal is a cycle's refusal as a person reads it: ErrCycle's own text
// names the two Goals by ID.
const cycleRefusal = "Linking these Goals would make a cycle: the parent already contributes to the child."

// refuseOptions are what refuse says of what isn't a refusal of its own
// kind: a thing that isn't there, or a failure. An empty sentence leaves it to
// the Not found page, or the plain 500.
type refuseOptions struct {
	notFound, failure string
}

// refusalOption changes how refuse answers a thing that isn't there, or a
// failure.
type refusalOption func(*refuseOptions)

// refuseNotFoundSaying has refuse say sentence on its page, at 404, when the
// refused thing isn't there, instead of answering with the Not found page.
func refuseNotFoundSaying(sentence string) refusalOption {
	return func(o *refuseOptions) { o.notFound = sentence }
}

// refuseFailureSaying has refuse say sentence on its page, at 500, when the
// action failed, instead of answering with a plain 500. The failure is logged
// either way.
func refuseFailureSaying(sentence string) refusalOption {
	return func(o *refuseOptions) { o.failure = sentence }
}

// refuse answers an action refused with err with a page inside the app,
// headed by what was refused (heading, e.g. "Can't accept link"), saying why
// and linking Back to back, or Home when back isn't a page of the app's own
// (localPath): a cycle 409, a validation 422, someone else's action 403, each
// the domain's reason said plainly. A thing that isn't there is the Not found
// page, and anything else a logged 500, unless opts say otherwise. It
// resolves the signed-in Account itself, as notFound does.
func (s *Server) refuse(w http.ResponseWriter, r *http.Request, heading, back string, err error, opts ...refusalOption) {
	var o refuseOptions
	for _, opt := range opts {
		opt(&o)
	}
	var status int
	var reason string
	switch {
	case errors.Is(err, domain.ErrCycle):
		status, reason = http.StatusConflict, cycleRefusal
	case errors.Is(err, domain.ErrValidation):
		status, reason = http.StatusUnprocessableEntity, sentence(domain.Reason(err))
	case errors.Is(err, domain.ErrNotAuthorized):
		status, reason = http.StatusForbidden, sentence(domain.Reason(err))
	case errors.Is(err, domain.ErrNotFound) && o.notFound != "":
		status, reason = http.StatusNotFound, o.notFound
	case errors.Is(err, domain.ErrNotFound):
		s.notFound(w, r)
		return
	case o.failure != "":
		s.logServerError(r, err)
		status, reason = http.StatusInternalServerError, o.failure
	default:
		s.serverError(w, r, "action failed", err)
		return
	}
	if !localPath(back) {
		back = "/home"
	}
	render(w, r, status, refusalPage(s.currentAccount(r), heading, reason, back))
}
