package web

import (
	"errors"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// onlyProblem is the one problem p records, failing t when it records any
// other number of them.
func onlyProblem(t *testing.T, p formProblems) domain.InputError {
	t.Helper()
	if len(p) != 1 {
		t.Fatalf("problems = %v, want one", p)
	}
	return *p[0]
}

// A date that doesn't parse is recorded under its input, quoting what was
// typed, with what it is about when there is one, and read as no date.
func TestFormProblemsDateQuotesWhatWasTyped(t *testing.T) {
	t.Parallel()

	for _, c := range []struct{ about, want string }{
		{"", `"soon" isn't a date.`},
		{`Milestone "Beta"`, `"soon" isn't a date for Milestone "Beta".`},
	} {
		var p formProblems
		if got := p.date("target_date", "soon", c.about); !got.IsZero() {
			t.Errorf("date(about %q) = %v, want the zero time", c.about, got)
		}
		if got := onlyProblem(t, p); got.Input != "target_date" || got.Message != c.want {
			t.Errorf("date(about %q) recorded %+v, want %q under target_date", c.about, got, c.want)
		}
	}
}

// A date that parses is read, and a blank one is no date; neither is a problem.
func TestFormProblemsDateReadsADateAndABlank(t *testing.T) {
	t.Parallel()

	var p formProblems
	if got, want := p.date("d", "2026-03-04", ""), time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC); !got.Equal(want) {
		t.Errorf("date = %v, want %v", got, want)
	}
	if got := p.date("d", "", ""); !got.IsZero() {
		t.Errorf("blank date = %v, want the zero time", got)
	}
	if len(p) != 0 {
		t.Errorf("problems = %v, want none", p)
	}
}

// A number that doesn't parse, or is NaN or infinite, is recorded quoting what
// was typed, with what it is about when there is one, and isn't read.
func TestFormProblemsNumberQuotesWhatWasTyped(t *testing.T) {
	t.Parallel()

	for _, c := range []struct{ typed, about, want string }{
		{"lots", "", `"lots" isn't a number.`},
		{"lots", "the baseline", `"lots" isn't a number for the baseline.`},
		{"x", `"Signups"`, `"x" isn't a number for "Signups".`},
		{"NaN", `"Signups"`, `"NaN" isn't a number for "Signups".`},
		{"Inf", "", `"Inf" isn't a number.`},
		{"-Inf", "", `"-Inf" isn't a number.`},
	} {
		var p formProblems
		if n, ok := p.number("reading", c.typed, c.about); ok || n != 0 {
			t.Errorf("number(%q) = %v, %v, want 0, false", c.typed, n, ok)
		}
		if got := onlyProblem(t, p); got.Input != "reading" || got.Message != c.want {
			t.Errorf("number(%q, about %q) recorded %+v, want %q under reading", c.typed, c.about, got, c.want)
		}
	}
}

// A number is read; a blank one isn't, and isn't a problem either: what a
// blank means is the caller's to say.
func TestFormProblemsNumberReadsANumberAndLeavesABlank(t *testing.T) {
	t.Parallel()

	var p formProblems
	if n, ok := p.number("n", " 12.5 ", ""); !ok || n != 12.5 {
		t.Errorf("number = %v, %v, want 12.5, true", n, ok)
	}
	if n, ok := p.number("n", "  ", "the target"); ok || n != 0 {
		t.Errorf("blank number = %v, %v, want 0, false", n, ok)
	}
	if len(p) != 0 {
		t.Errorf("problems = %v, want none", p)
	}
}

// Any other problem is recorded as a sentence.
func TestFormProblemsAddFinishesASentence(t *testing.T) {
	t.Parallel()

	var p formProblems
	p.add("baseline", "a Metric needs a %s", "baseline")
	if got := onlyProblem(t, p); got.Input != "baseline" || got.Message != "A Metric needs a baseline." {
		t.Errorf("add recorded %+v, want %q under baseline", got, "A Metric needs a baseline.")
	}
}

// A form with no problems is no refusal; one with some is a validation
// refusal that names each of them.
func TestFormProblemsErr(t *testing.T) {
	t.Parallel()

	var p formProblems
	if err := p.err(); err != nil {
		t.Fatalf("err() with no problems = %v, want nil", err)
	}
	p.add("a", "first")
	p.add("b", "second")
	err := p.err()
	if !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err() = %v, want an ErrValidation", err)
	}
	if got := domain.InputErrors(err); len(got) != 2 || got[0].Input != "a" || got[1].Input != "b" {
		t.Errorf("InputErrors(err()) = %v, want a then b", got)
	}
}

// A refusal's problems are each a sentence: those it names by input, or one
// about the whole form when it names none.
func TestProblemsOfARefusal(t *testing.T) {
	t.Parallel()

	named := errors.Join(
		&domain.InputError{Input: "title", Message: "a Goal needs a title"},
		&domain.InputError{Input: "kind", Message: "Kind must be chosen."},
	)
	got := problemsOf(named)
	if len(got) != 2 ||
		*got[0] != (domain.InputError{Input: "title", Message: "A Goal needs a title."}) ||
		*got[1] != (domain.InputError{Input: "kind", Message: "Kind must be chosen."}) {
		t.Errorf("problemsOf(named) = %+v, want each a sentence under its input", got)
	}

	whole := problemsOf(domain.Refuse(domain.ErrValidation, "a rule needs a value"))
	if len(whole) != 1 || *whole[0] != (domain.InputError{Message: "A rule needs a value."}) {
		t.Errorf("problemsOf(plain refusal) = %+v, want one whole-form sentence", whole)
	}
}
