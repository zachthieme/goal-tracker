package web

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// formProblems are the problems a form's post has, each under the name of the
// input it is about (empty for the form as a whole) and each a sentence, as a
// page shows it. A form reads what it can't parse through one, so every
// value that doesn't parse is told the same way, and hands err() to the
// domain as Unparsed or to s.refuse.
type formProblems []*domain.InputError

// date reads typed, input's value, as a date, blank being no date. One that
// doesn't parse is recorded, saying what it was a date for when about isn't
// empty, and read as the zero time.
func (p *formProblems) date(input, typed, about string) time.Time {
	typed = strings.TrimSpace(typed)
	d, err := parseDate(typed)
	if err != nil {
		p.notA(input, typed, "date", about)
		return time.Time{}
	}
	return d
}

// number reads typed, input's value, as a number. A blank one records nothing
// and isn't read: what a blank means is the caller's to say. One that doesn't
// parse, or is NaN or infinite, is recorded, saying what it was a number for
// when about isn't empty, and isn't read.
func (p *formProblems) number(input, typed, about string) (float64, bool) {
	typed = strings.TrimSpace(typed)
	if typed == "" {
		return 0, false
	}
	n, err := strconv.ParseFloat(typed, 64)
	if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
		p.notA(input, typed, "number", about)
		return 0, false
	}
	return n, true
}

// notA records that typed isn't a kind ("date", "number"), for about.
func (p *formProblems) notA(input, typed, kind, about string) {
	if about == "" {
		p.add(input, "%q isn't a %s", typed, kind)
		return
	}
	p.add(input, "%q isn't a %s for %s", typed, kind, about)
}

// add records a problem with input, formatted as fmt.Sprintf does and finished
// as a sentence.
func (p *formProblems) add(input, format string, args ...any) {
	*p = append(*p, &domain.InputError{Input: input, Message: sentence(fmt.Sprintf(format, args...))})
}

// err is the form's refusal, every problem joined, an ErrValidation naming
// each input; nil when there are none.
func (p formProblems) err() error {
	errs := make([]error, 0, len(p))
	for _, problem := range p {
		errs = append(errs, problem)
	}
	return errors.Join(errs...)
}

// problemsOf are the problems a refusal names, each a sentence under its
// input (domain.InputErrors), or one about the whole form, its Input empty,
// saying why (domain.Reason) when it names none.
func problemsOf(err error) []*domain.InputError {
	named := domain.InputErrors(err)
	if len(named) == 0 {
		return []*domain.InputError{{Message: sentence(domain.Reason(err))}}
	}
	problems := make([]*domain.InputError, 0, len(named))
	for _, n := range named {
		problems = append(problems, &domain.InputError{Input: n.Input, Message: sentence(n.Message), Err: n.Err})
	}
	return problems
}

// refusalSentence is the first problem a refusal names, as a sentence: what a
// form with one place for a refusal shows.
func refusalSentence(err error) string {
	return problemsOf(err)[0].Message
}

// parseDate reads a date form field in the HTML date-input format. An empty
// field is the zero time, which the domain treats as "no date".
func parseDate(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, nil
	}
	return time.Parse(dateLayout, value)
}
