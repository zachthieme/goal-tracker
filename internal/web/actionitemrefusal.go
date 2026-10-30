package web

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/a-h/templ"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// actionItemRefusalCookie carries a refused form from the post to the one
// page shown straight after it, as undoCookie carries an Undo: scoped to that
// page's path and cleared the moment the page reads it, so the form comes
// back open, as it was left, once, and a reload shows the plain page (#660).
// It is echoed only to the browser that set it, and the page escapes what it
// shows of it, so it needs no signing.
const actionItemRefusalCookie = "gt_refused"

// actionItemRefusalMaxBytes is the most a refusal's cookie value may hold; a
// longer one, too long for a browser to keep with the rest, is answered with
// the refusal page instead.
const actionItemRefusalMaxBytes = 3000

// actionItemRefusalProblem prefixes a refused input's name in the cookie's
// value, which holds its sentence.
const actionItemRefusalProblem = "problem."

// actionItemRefusalForms are the fields each Action Item form posts, in the
// order it shows them, by the form's test ID. A refusal names only these.
var actionItemRefusalForms = map[string][]string{
	"action-item-form":         {"text", "owner", "due", "goal"},
	"comment-action-item-form": {"text", "owner", "due"},
	"close-action-item":        {"note"},
	"reassign-action-item":     {"owner"},
}

// actionItemRefusalPages are the pages an Action Item form is on, which take
// its refusal back: Home, a Goal page, a Report's draft and a publication.
var actionItemRefusalPages = regexp.MustCompile(`^/(home|goals/\d+|reports/\d+(/publications/\d+)?)$`)

// actionItemRefusal is a refused form as the page it came back to shows it:
// the form, by test ID, and the record it is about (ID, 0 for none); what it
// posted, Input; and each problem's sentence, by the input it is about.
type actionItemRefusal struct {
	Form     string
	ID       int64
	Input    url.Values
	Problems map[string]string
}

// refuseActionItem answers an Action Item's raise, close or reassign refused
// with err. Refused on its fields alone, it sends the person back to back,
// query and fragment kept, where form (a test ID), about the record id, comes
// back open, holding what was posted and each reason beside its field.
// Anything else — a refusal of the whole form, of who they are, or of a
// record that isn't there; a back that isn't one of the forms' pages; or a
// refusal too long to carry — is s.refuse's page, headed heading.
func (s *Server) refuseActionItem(w http.ResponseWriter, r *http.Request, form string, id int64, heading, back string, err error) {
	if c, ok := actionItemRefusalCookieFor(r, form, id, back, err); ok {
		http.SetCookie(w, c)
		http.Redirect(w, r, back, http.StatusSeeOther)
		return
	}
	s.refuse(w, r, heading, back, err)
}

// actionItemRefusalCookieFor is the cookie taking form's refusal, err, back
// to back, and whether it can: every problem err names is one of form's
// fields, back is one of the forms' pages, and the cookie isn't too long.
func actionItemRefusalCookieFor(r *http.Request, form string, id int64, back string, err error) (*http.Cookie, bool) {
	fields, ok := actionItemRefusalForms[form]
	if !ok || !localPath(back) {
		return nil, false
	}
	path, _, _ := strings.Cut(back, "#")
	path, _, _ = strings.Cut(path, "?")
	if !actionItemRefusalPages.MatchString(path) {
		return nil, false
	}
	v := url.Values{"page": {path}, "form": {form}, "id": {strconv.FormatInt(id, 10)}}
	for _, p := range problemsOf(err) {
		if !slices.Contains(fields, p.Input) {
			return nil, false
		}
		v.Set(actionItemRefusalProblem+p.Input, p.Message)
	}
	for _, f := range fields {
		if typed, ok := r.PostForm[f]; ok {
			v[f] = typed
		}
	}
	value := v.Encode()
	if len(value) > actionItemRefusalMaxBytes {
		return nil, false
	}
	return &http.Cookie{
		Name:     actionItemRefusalCookie,
		Value:    value,
		Path:     path,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}, true
}

// takeActionItemRefusal returns r carrying the refused form sent back to the
// page r asks for, if any (withActionItemRefusal), and clears it so the form
// comes back only this once. A refusal for another page is left alone: one
// scoped to /reports/1 is also sent to /reports/1/publications/2, and is not
// that page's to take. It reads any form's refusal, not only an Action
// Item's.
func takeActionItemRefusal(w http.ResponseWriter, r *http.Request) *http.Request {
	for _, c := range r.CookiesNamed(actionItemRefusalCookie) {
		v, err := url.ParseQuery(c.Value)
		if err != nil || v.Get("page") != r.URL.Path {
			continue
		}
		http.SetCookie(w, &http.Cookie{
			Name:     actionItemRefusalCookie,
			Path:     r.URL.Path,
			MaxAge:   -1,
			HttpOnly: true,
			SameSite: http.SameSiteLaxMode,
		})
		id, _ := strconv.ParseInt(v.Get("id"), 10, 64)
		refusal := actionItemRefusal{Form: v.Get("form"), ID: id, Input: url.Values{}, Problems: map[string]string{}}
		for key, values := range v {
			switch {
			case key == "page" || key == "form" || key == "id":
			case strings.HasPrefix(key, actionItemRefusalProblem):
				refusal.Problems[strings.TrimPrefix(key, actionItemRefusalProblem)] = values[0]
			default:
				refusal.Input[key] = values
			}
		}
		return r.WithContext(withActionItemRefusal(r.Context(), refusal))
	}
	return r
}

type actionItemRefusalKey struct{}

// actionItemRefusalSlot holds the page's refused form until a form takes it.
type actionItemRefusalSlot struct {
	refusal actionItemRefusal
	taken   bool
}

// withActionItemRefusal is ctx carrying refusal for the page's form it is
// about to take (actionItemRefusalFor).
func withActionItemRefusal(ctx context.Context, refusal actionItemRefusal) context.Context {
	return context.WithValue(ctx, actionItemRefusalKey{}, &actionItemRefusalSlot{refusal: refusal})
}

// actionItemRefusalFor is the refusal the page carries for form, about the
// record id, and whether it carries one. The first such form on the page
// takes it, and no later one: a publication can list an item's Close twice,
// and only one comes back open.
func actionItemRefusalFor(ctx context.Context, form string, id int64) (actionItemRefusal, bool) {
	slot, _ := ctx.Value(actionItemRefusalKey{}).(*actionItemRefusalSlot)
	if slot == nil || slot.taken || slot.refusal.Form != form || slot.refusal.ID != id {
		return actionItemRefusal{}, false
	}
	slot.taken = true
	return slot.refusal, true
}

// refusalNamesComment reports whether the page carries a refusal, not yet
// taken, of the form making the comment commentID an Action Item, without
// taking it: the thread holding that comment comes back open around it.
func refusalNamesComment(ctx context.Context, commentID int64) bool {
	slot, _ := ctx.Value(actionItemRefusalKey{}).(*actionItemRefusalSlot)
	return slot != nil && !slot.taken && slot.refusal.Form == "comment-action-item-form" && slot.refusal.ID == commentID
}

// typed is what the refused form posted as input, "" when it posted none.
func (f actionItemRefusal) typed(input string) string {
	return f.Input.Get(input)
}

// errorID is the id of input's reason on the page: unique to the form, its
// record, and the input.
func (f actionItemRefusal) errorID(input string) string {
	return fmt.Sprintf("%s-%d-%s-error", f.Form, f.ID, input)
}

// controlAttrs are the attributes of input's control: invalid and described
// by its reason when it was refused, and focused first when it is the form's
// first refused field. None at all when it wasn't refused.
func (f actionItemRefusal) controlAttrs(input string) templ.Attributes {
	if f.Problems[input] == "" {
		return nil
	}
	attrs := templ.Attributes{"aria-invalid": "true", "aria-describedby": f.errorID(input)}
	for _, field := range actionItemRefusalForms[f.Form] {
		if f.Problems[field] != "" {
			if field == input {
				attrs["autofocus"] = true
			}
			break
		}
	}
	return attrs
}

// refusalNamesThread reports whether the page's refusal, not yet taken, is of
// the form making a comment in threads an Action Item (refusalNamesComment).
func refusalNamesThread(ctx context.Context, threads []domain.Thread) bool {
	for _, t := range threads {
		if refusalNamesComment(ctx, t.Comment.ID) {
			return true
		}
		for _, reply := range t.Replies {
			if refusalNamesComment(ctx, reply.ID) {
				return true
			}
		}
	}
	return false
}

// refusedGoalChoice is the Goal the refused Add Action Item form's About had
// chosen, of goals, or 0 for the whole Report: what it shows before any
// refusal, and when what was posted isn't one of goals.
func refusedGoalChoice(refused actionItemRefusal, goals []domain.Goal) int64 {
	id, err := strconv.ParseInt(refused.typed(domain.InputActionItemGoal), 10, 64)
	if err != nil {
		return 0
	}
	for _, g := range goals {
		if g.ID == id {
			return id
		}
	}
	return 0
}
