package domain

import (
	"errors"
	"fmt"
	"strings"
)

// Refusal refuses an action as one of the domain's kinds of refusal
// (ErrValidation, ErrNotAuthorized, ErrPasswordRules), saying why as a page
// shows it. It is its Kind, and reads as "<Kind>: <Reason>".
type Refusal struct {
	Kind error
	// Reason is the person-facing text, without Kind's.
	Reason string
}

func (e *Refusal) Error() string { return e.Kind.Error() + ": " + e.Reason }

func (e *Refusal) Unwrap() error { return e.Kind }

func (e *Refusal) shownReason() string { return e.Reason }

// Refuse is a Refusal of kind, its Reason formatted as fmt.Sprintf does.
func Refuse(kind error, format string, args ...any) error {
	return &Refusal{Kind: kind, Reason: fmt.Sprintf(format, args...)}
}

// reasoned is a refusal that can say why as a page shows it.
type reasoned interface {
	error
	shownReason() string
}

// Reason is why err refuses, as a page shows it: the reason of the outermost
// refusal err wraps (a *Refusal's Reason, an *InputError's Message), each
// joined one's on its own line, or err's own text when it wraps none.
func Reason(err error) string {
	var r reasoned
	if !errors.As(err, &r) {
		return err.Error()
	}
	switch e := err.(type) { //nolint:errorlint // walks the joined tree itself, to join every refusal's reason; errors.As would stop at the first
	case reasoned:
		return e.shownReason()
	case interface{ Unwrap() []error }:
		reasons := make([]string, 0, len(e.Unwrap()))
		for _, inner := range e.Unwrap() {
			reasons = append(reasons, Reason(inner))
		}
		return strings.Join(reasons, "\n")
	case interface{ Unwrap() error }:
		return Reason(e.Unwrap())
	}
	return r.shownReason()
}
