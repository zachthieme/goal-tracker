package web

import (
	"context"
	"fmt"
	"html"
	"io"
	"net/http"

	"github.com/a-h/templ"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// FormRefusalPageForTest is the page HandleFormRefusalForTest's on-page form
// sits on, and FormRefusalBackForTest the refusal page's way back.
const (
	FormRefusalPageForTest = "/form-refusal-test/page"
	FormRefusalBackForTest = "/goals/12"
)

// FormRefusalCaseForTest is how a test-only command, refused with Err,
// states its refusal to answerRefused: Form is "on-page" (its form, posting
// Fields and in one sentence when OneSentence, sits on
// FormRefusalPageForTest, which offers it when Offered), "as-page", or ""
// for none; Partial gives it an htmx fragment.
type FormRefusalCaseForTest struct {
	Err         error
	Form        string
	Fields      []string
	OneSentence bool
	Offered     bool
	Partial     bool
}

// HandleFormRefusalForTest serves at pattern, among s's routes, a command
// that answers its refusal as c states it, headed "Can't do it" and linking
// Back to FormRefusalBackForTest, so the web_test package's tests reach every
// answer answerRefused gives. The as-page form and the fragment each say why
// in an element with the test ID form-reason.
func (s *Server) HandleFormRefusalForTest(pattern string, c FormRefusalCaseForTest) {
	s.mux.HandleFunc(pattern, s.requireAuth(func(w http.ResponseWriter, r *http.Request, _ domain.Account) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "invalid form", http.StatusBadRequest)
			return
		}
		rf := refused{Err: c.Err, Heading: "Can't do it", Back: FormRefusalBackForTest}
		switch c.Form {
		case "on-page":
			form := onPage(FormRefusalPageForTest+"?shown=1#test-form", "test-form", 7, c.Fields).
				offeredWhen(func(context.Context) (bool, error) { return c.Offered, nil })
			if c.OneSentence {
				form = form.inOneSentence()
			}
			rf.Form = form
		case "as-page":
			rf.Form = asPage(func(_ context.Context, err error) (templ.Component, error) {
				return testRefusalMarkup("as-page", refusalReason(err)), nil
			})
		}
		if c.Partial {
			rf.Partial = func(_ context.Context, err error) (templ.Component, error) {
				return testRefusalMarkup("partial", refusalReason(err)), nil
			}
		}
		s.answerRefused(w, r, rf)
	}))
}

// HandleFormRefusalPageForTest serves FormRefusalPageForTest, a page showing
// the test form a refusal carries back to it (test ID refused-form): its
// reason (form-reason), and for each input it carries, what was typed
// (typed-<input>) and its problem (problem-<input>). Without one it is the
// plain page (plain-page).
func (s *Server) HandleFormRefusalPageForTest() {
	s.mux.HandleFunc("GET "+FormRefusalPageForTest, func(w http.ResponseWriter, r *http.Request) {
		render(w, takeFormRefusal(w, r), http.StatusOK, templ.ComponentFunc(func(ctx context.Context, out io.Writer) error {
			refusal, ok := formRefusalFor(ctx, "test-form", 7)
			if !ok {
				_, err := io.WriteString(out, `<main data-testid="plain-page"></main>`)
				return err
			}
			markup := `<main><form data-testid="refused-form">`
			if refusal.Reason != "" {
				markup += `<p data-testid="form-reason">` + html.EscapeString(refusal.Reason) + `</p>`
			}
			for _, input := range refusal.Fields {
				markup += fmt.Sprintf(`<span data-testid="typed-%s">%s</span>`, input, html.EscapeString(refusal.typed(input)))
				if problem := refusal.Problems[input]; problem != "" {
					markup += fmt.Sprintf(`<span data-testid="problem-%s">%s</span>`, input, html.EscapeString(problem))
				}
			}
			_, err := io.WriteString(out, markup+`</form></main>`)
			return err
		}))
	})
}

// testRefusalMarkup is a test form, by test ID, saying reason.
func testRefusalMarkup(testID, reason string) templ.Component {
	return templ.ComponentFunc(func(_ context.Context, out io.Writer) error {
		_, err := fmt.Fprintf(out, `<main data-testid="%s"><p data-testid="form-reason">%s</p></main>`, testID, html.EscapeString(reason))
		return err
	})
}
