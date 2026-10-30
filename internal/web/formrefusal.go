package web

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// refused is a refused command, as answerRefused answers it: the domain's
// refusal, Err; the refusal page's heading and way Back, which the handler
// works out, re-reading what it must; and where the form it came from comes
// back in place, if anywhere.
type refused struct {
	Err     error
	Heading string
	Back    string
	// Form is where a refusal of what was typed comes back in place: onPage
	// for a form on a page about something else, asPage for a form that is
	// the page, or nil for the refusal page only.
	Form refusedForm
	// Partial, when set, is the fragment an htmx post gets for a refusal of
	// what was typed, at 200 (answerSwap): its form again, showing why. A
	// nil fragment is a form the page no longer offers, answered with the
	// refusal page; an error is answered as refuse answers it.
	Partial func(err error) (templ.Component, error)
	// Pushed is the address htmx shows for Partial, a whole page
	// (answerPushedPage); Unswapped has htmx show Partial without swapping
	// its target (answerUnswapped).
	Pushed    string
	Unswapped bool
	// Options are refuse's, for its page.
	Options []refusalOption
}

// refusedForm is where a refused form comes back in place: onPage or asPage.
type refusedForm interface {
	// answerInPlace answers rf, a refusal of what was typed, with its form
	// in place, where status is the refusal's, and reports whether it
	// answered: false leaves the answer to the refusal page.
	answerInPlace(s *Server, w http.ResponseWriter, r *http.Request, rf refused, status int) bool
}

// answerRefused answers a refused command, rf, once for every area. A
// refusal of what was typed (domain.ErrValidation) or a cycle
// (domain.ErrCycle) comes back in place: an htmx post with a Partial gets the
// fragment at 200, anything else rf's Form; and with neither, or where they
// fall back, the refusal page, 422, or 409 for a cycle. Someone else's
// action is the refusal page at 403, a thing that isn't there the Not found
// page, and anything else a logged 500: refuse's answers, rf's Options
// included. Don't switch on the error kind in a handler: say rf.
func (s *Server) answerRefused(w http.ResponseWriter, r *http.Request, rf refused) {
	if status, inPlace := refusedStatus(rf.Err); inPlace {
		switch {
		case rf.Partial != nil && isHTMX(r):
			if s.answerRefusedPartial(w, r, rf, status) {
				return
			}
		case rf.Form != nil:
			if rf.Form.answerInPlace(s, w, r, rf, status) {
				return
			}
		}
	}
	s.refuse(w, r, rf.Heading, rf.Back, rf.Err, rf.Options...)
}

// refusedStatus is the status a refusal of what was typed has, 422, or 409
// for a cycle, and whether err is one, so its form may come back in place.
func refusedStatus(err error) (int, bool) {
	switch {
	case errors.Is(err, domain.ErrCycle):
		return http.StatusConflict, true
	case errors.Is(err, domain.ErrValidation):
		return http.StatusUnprocessableEntity, true
	}
	return 0, false
}

// answerRefusedPartial answers rf's htmx post with its Partial, reporting
// whether it answered.
func (s *Server) answerRefusedPartial(w http.ResponseWriter, r *http.Request, rf refused, status int) bool {
	fragment, err := rf.Partial(rf.Err)
	switch {
	case err != nil:
		s.refuse(w, r, rf.Heading, rf.Back, err, rf.Options...)
	case fragment == nil:
		return false
	case rf.Pushed != "":
		answerPushedPage(w, r, status, fragment, rf.Pushed)
	case rf.Unswapped:
		answerUnswapped(w, r, fragment)
	default:
		answerSwap(w, r, fragment)
	}
	return true
}

// formAsPage is a form that is the page it is on, such as the New goal form.
type formAsPage struct {
	render func(err error) (templ.Component, error)
}

// asPage is a form that is the page, which render draws again for a refusal
// of what was typed, err, as it was left and saying why. answerRefused
// answers with it at the refusal's status. A nil page falls back to the
// refusal page, and an error is answered as refuse answers it.
func asPage(render func(err error) (templ.Component, error)) refusedForm {
	return formAsPage{render: render}
}

func (f formAsPage) answerInPlace(s *Server, w http.ResponseWriter, r *http.Request, rf refused, status int) bool {
	page, err := f.render(rf.Err)
	switch {
	case err != nil:
		s.refuse(w, r, rf.Heading, rf.Back, err, rf.Options...)
	case page == nil:
		return false
	default:
		render(w, r, status, page)
	}
	return true
}

// formOnPage is a form on a page about something else, such as an Action
// Item's Close on a publication, which comes back open there.
type formOnPage struct {
	// path is the page's address, query and fragment kept for the redirect.
	path string
	// form names the form on the page, and id the record it is about (0
	// for none).
	form string
	id   int64
	// fields are the inputs the form posts, in the order it shows them.
	fields []string
	// offered reports whether the page still offers the viewer the form;
	// nil when it always does.
	offered func(context.Context) (bool, error)
	// oneSentence is a form saying why above itself, in one sentence, not
	// beside each field; beside, when set, says that sentence beside the
	// input it names instead.
	oneSentence bool
	beside      string
}

// onPage is the form form, about the record id, on the page at path, posting
// fields. answerRefused answers its refusal with a 303 to path, carrying the
// form, what was typed in fields, and each problem beside the field it names,
// once in the form-refusal cookie, so the page shows the form open, as it
// was left (formRefusalFor). It falls back to the refusal page when path
// isn't a page of the app's own, a problem names no input of fields, the
// page no longer offers the form (offeredWhen), or the cookie would be too
// long.
func onPage(path, form string, id int64, fields []string) *formOnPage {
	return &formOnPage{path: path, form: form, id: id, fields: fields}
}

// offeredWhen has the form come back only when offered, asked of the page's
// loader, says the page still offers it. An error is answered as refuse
// answers it.
func (f *formOnPage) offeredWhen(offered func(context.Context) (bool, error)) *formOnPage {
	f.offered = offered
	return f
}

// inOneSentence has the form say why in one sentence above itself, every
// problem joined into it, as the Goal page's forms do.
func (f *formOnPage) inOneSentence() *formOnPage {
	f.oneSentence = true
	return f
}

// besideInput has a form saying why in one sentence say it beside input,
// such as a picker left empty, instead of above itself.
func (f *formOnPage) besideInput(input string) *formOnPage {
	f.beside = input
	return f
}

func (f *formOnPage) answerInPlace(s *Server, w http.ResponseWriter, r *http.Request, rf refused, _ int) bool {
	refusal, ok := f.refusalOf(r, rf.Err)
	if !ok {
		return false
	}
	if f.offered != nil {
		offered, err := f.offered(r.Context())
		if err != nil {
			s.refuse(w, r, rf.Heading, rf.Back, err, rf.Options...)
			return true
		}
		if !offered {
			return false
		}
	}
	if !setFormRefusal(w, pagePath(f.path), refusal) {
		return false
	}
	http.Redirect(w, r, f.path, http.StatusSeeOther)
	return true
}

// refusalOf is err, refusing the form's post r, as the page shows it, and
// whether the page can: its path is the app's own, and every problem err
// names is beside one of its fields, or the form says them in one sentence.
func (f *formOnPage) refusalOf(r *http.Request, err error) (formRefusal, bool) {
	if !localPath(f.path) {
		return formRefusal{}, false
	}
	refusal := formRefusal{Form: f.form, ID: f.id, Fields: f.fields, Input: url.Values{}, Problems: map[string]string{}}
	switch {
	case f.beside != "":
		refusal.Problems[f.beside] = refusalReason(err)
	case f.oneSentence:
		refusal.Reason = refusalReason(err)
	default:
		for _, p := range problemsOf(err) {
			if !slices.Contains(f.fields, p.Input) {
				return formRefusal{}, false
			}
			refusal.Problems[p.Input] = p.Message
		}
	}
	for _, field := range f.fields {
		if typed, ok := r.PostForm[field]; ok {
			refusal.Input[field] = typed
		}
	}
	return refusal, true
}

// refusalReason is why err refuses, every problem joined, as one sentence.
func refusalReason(err error) string {
	return sentence(domain.Reason(err))
}

// pagePath is p without its query or fragment.
func pagePath(p string) string {
	p, _, _ = strings.Cut(p, "#")
	p, _, _ = strings.Cut(p, "?")
	return p
}

// formRefusalCookie carries a refused form from the post to the one page
// shown straight after it, as undoCookie carries an Undo: scoped to that
// page's path and cleared the moment the page reads it, so the form comes
// back open, as it was left, once, and a reload shows the plain page (#660).
// It is echoed only to the browser that set it, and the page escapes what it
// shows of it, so it needs no signing.
const formRefusalCookie = "gt_refused"

// formRefusalMaxBytes is the most a refusal's cookie value may hold; a
// longer one, too long for a browser to keep with the rest, is answered with
// the refusal page instead.
const formRefusalMaxBytes = 3000

// The cookie's value holds the refusal's page, form, id, fields and reason
// under their own keys, each problem under formRefusalProblem and what was
// typed in each input under formRefusalInput, followed by the input's name.
const (
	formRefusalProblem = "problem."
	formRefusalInput   = "input."
)

// formRefusal is a refused form as the page it came back to shows it: the
// form, by test ID or, on the Goal page, its goalForm name, and the record it
// is about (ID, 0 for none); the inputs it posts, Fields, in the order it
// shows them; what it posted, Input; each problem's sentence, by the input
// it is about; and Reason, the sentence refusing the form as a whole, which
// only a form saying why in one sentence carries (#687).
type formRefusal struct {
	Form     string
	ID       int64
	Fields   []string
	Input    url.Values
	Problems map[string]string
	Reason   string
}

// setFormRefusal sets the cookie carrying refusal to the page at path,
// reporting whether it fit: a refusal whose cookie would be too long is not
// set.
func setFormRefusal(w http.ResponseWriter, path string, refusal formRefusal) bool {
	v := url.Values{"page": {path}, "form": {refusal.Form}, "id": {strconv.FormatInt(refusal.ID, 10)}}
	if len(refusal.Fields) > 0 {
		v.Set("fields", strings.Join(refusal.Fields, ","))
	}
	if refusal.Reason != "" {
		v.Set("reason", refusal.Reason)
	}
	for input, problem := range refusal.Problems {
		v.Set(formRefusalProblem+input, problem)
	}
	for input, typed := range refusal.Input {
		v[formRefusalInput+input] = typed
	}
	value := v.Encode()
	if len(value) > formRefusalMaxBytes {
		return false
	}
	http.SetCookie(w, &http.Cookie{
		Name:     formRefusalCookie,
		Value:    value,
		Path:     path,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	return true
}

// takeFormRefusal returns r carrying the refused form sent back to the page r
// asks for, if any (withFormRefusal), and clears it so the form comes back
// only this once. A refusal for another page is left alone: one scoped to
// /reports/1 is also sent to /reports/1/publications/2, and is not that
// page's to take.
func takeFormRefusal(w http.ResponseWriter, r *http.Request) *http.Request {
	for _, c := range r.CookiesNamed(formRefusalCookie) {
		v, err := url.ParseQuery(c.Value)
		if err != nil || v.Get("page") != r.URL.Path {
			continue
		}
		http.SetCookie(w, &http.Cookie{
			Name:     formRefusalCookie,
			Path:     r.URL.Path,
			MaxAge:   -1,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
		id, _ := strconv.ParseInt(v.Get("id"), 10, 64)
		refusal := formRefusal{Form: v.Get("form"), ID: id, Input: url.Values{}, Problems: map[string]string{}, Reason: v.Get("reason")}
		if fields := v.Get("fields"); fields != "" {
			refusal.Fields = strings.Split(fields, ",")
		}
		for key, values := range v {
			if input, ok := strings.CutPrefix(key, formRefusalProblem); ok {
				refusal.Problems[input] = values[0]
			} else if input, ok := strings.CutPrefix(key, formRefusalInput); ok {
				refusal.Input[input] = values
			}
		}
		return r.WithContext(withFormRefusal(r.Context(), refusal))
	}
	return r
}

type formRefusalKey struct{}

// formRefusalSlot holds the page's refused form until a form takes it.
type formRefusalSlot struct {
	refusal formRefusal
	taken   bool
}

// withFormRefusal is ctx carrying refusal for the page's form it is about to
// take (formRefusalFor).
func withFormRefusal(ctx context.Context, refusal formRefusal) context.Context {
	return context.WithValue(ctx, formRefusalKey{}, &formRefusalSlot{refusal: refusal})
}

// formRefusalFor is the refusal the page carries for form, about the record
// id, and whether it carries one. The first such form on the page takes it,
// and no later one: a publication can list an item's Close twice, and only
// one comes back open.
func formRefusalFor(ctx context.Context, form string, id int64) (formRefusal, bool) {
	slot, _ := ctx.Value(formRefusalKey{}).(*formRefusalSlot)
	if slot == nil || slot.taken || slot.refusal.Form != form || slot.refusal.ID != id {
		return formRefusal{}, false
	}
	slot.taken = true
	return slot.refusal, true
}

// formRefusalIn is the refusal the page carries, and whether it carries one,
// without taking it: the Goal page reads its own forms' refusal this way.
func formRefusalIn(ctx context.Context) (formRefusal, bool) {
	slot, _ := ctx.Value(formRefusalKey{}).(*formRefusalSlot)
	if slot == nil {
		return formRefusal{}, false
	}
	return slot.refusal, true
}

// formRefusalUntaken is the refusal the page carries, not yet taken by a
// form, and whether it carries one, without taking it.
func formRefusalUntaken(ctx context.Context) (formRefusal, bool) {
	slot, _ := ctx.Value(formRefusalKey{}).(*formRefusalSlot)
	if slot == nil || slot.taken {
		return formRefusal{}, false
	}
	return slot.refusal, true
}

// typed is what the refused form posted as input, "" when it posted none.
func (f formRefusal) typed(input string) string {
	return f.Input.Get(input)
}

// errorID is the id of input's reason on the page: unique to the form, its
// record, and the input.
func (f formRefusal) errorID(input string) string {
	return fmt.Sprintf("%s-%d-%s-error", f.Form, f.ID, input)
}

// controlAttrs are the attributes of input's control: invalid and described
// by its reason when it was refused, and focused first when it is the form's
// first refused field. None at all when it wasn't refused.
func (f formRefusal) controlAttrs(input string) templ.Attributes {
	if f.Problems[input] == "" {
		return nil
	}
	attrs := templ.Attributes{"aria-invalid": "true", "aria-describedby": f.errorID(input)}
	for _, field := range f.Fields {
		if f.Problems[field] != "" {
			if field == input {
				attrs["autofocus"] = true
			}
			break
		}
	}
	return attrs
}
