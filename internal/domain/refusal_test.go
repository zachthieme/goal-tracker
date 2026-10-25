package domain_test

import (
	"errors"
	"fmt"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// A refusal's reason, the text a page shows, comes back from Reason however
// many times the refusal has been wrapped on its way up.
func TestReasonFindsARefusalsReasonThroughWrapping(t *testing.T) {
	t.Parallel()

	refused := &domain.Refusal{Kind: domain.ErrNotAuthorized, Reason: "only the Goal's Owner may hand it off"}
	err := fmt.Errorf("hand off goal 7: %w", fmt.Errorf("authorize: %w", refused))

	if got, want := domain.Reason(err), "only the Goal's Owner may hand it off"; got != want {
		t.Errorf("Reason(%v) = %q, want %q", err, got, want)
	}
}

// A refusal reads in logs as its kind then its reason, and is its kind.
func TestRefusalIsItsKindAndReadsAsKindThenReason(t *testing.T) {
	t.Parallel()

	refused := &domain.Refusal{Kind: domain.ErrValidation, Reason: "a Goal needs a title"}

	if got, want := refused.Error(), "validation failed: a Goal needs a title"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
	if !errors.Is(fmt.Errorf("save: %w", refused), domain.ErrValidation) {
		t.Error("a wrapped validation Refusal is not ErrValidation")
	}
	if errors.Is(refused, domain.ErrNotAuthorized) {
		t.Error("a validation Refusal is ErrNotAuthorized")
	}
}

// An InputError's reason is its Message.
func TestReasonOfAnInputErrorIsItsMessage(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("save goal: %w", &domain.InputError{Input: domain.InputTitle, Message: "a Goal needs a title"})

	if got, want := domain.Reason(err), "a Goal needs a title"; got != want {
		t.Errorf("Reason(%v) = %q, want %q", err, got, want)
	}
}

// Refusals joined, as Activate joins each rule a Goal doesn't meet, give
// every reason, one to a line.
func TestReasonOfJoinedRefusalsGivesEachReason(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("activate goal 3: %w", errors.Join(
		&domain.Refusal{Kind: domain.ErrValidation, Reason: "an Ongoing Goal needs at least one Metric"},
		&domain.Refusal{Kind: domain.ErrValidation, Reason: "a Goal needs a value in Budget to become Active"},
	))

	if got, want := domain.Reason(err), "an Ongoing Goal needs at least one Metric\na Goal needs a value in Budget to become Active"; got != want {
		t.Errorf("Reason(%v) = %q, want %q", err, got, want)
	}
}

// An error that refuses nothing reads as itself.
func TestReasonOfAnErrorThatRefusesNothingIsItsText(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("load goal 3: %w", domain.ErrNotFound)

	if got, want := domain.Reason(err), "load goal 3: not found"; got != want {
		t.Errorf("Reason(%v) = %q, want %q", err, got, want)
	}
}
