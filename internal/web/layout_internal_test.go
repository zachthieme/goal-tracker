package web

import (
	"context"
	"io"
	"strings"
	"testing"

	"github.com/a-h/templ"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// A page that fills the frame's header slot gets its 48px strip at the top of
// the content column, above the page: the breadcrumb to its parent, its title
// and count, and its actions at the right. A page that leaves the slot empty
// gets no strip (DESIGN.md § Page header).
func TestFramedLayoutPutsThePageHeaderAboveThePage(t *testing.T) {
	t.Parallel()

	sam := &domain.Account{ID: 1, Email: "sam@example.com", Name: "Sam"}
	action := templ.Raw(`<a class="btn primary sm" href="/goals/new">New goal</a>`)
	header := templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		return pageHeader(pageHead{Parent: "Goals", ParentHref: "/goals", Title: "Cut churn", Count: "24 in My org"}).Render(templ.WithChildren(ctx, action), w)
	})
	var b strings.Builder
	if err := framedLayout("Cut churn", sam, header).Render(templ.WithChildren(context.Background(), templ.Raw("<p>body</p>")), &b); err != nil {
		t.Fatal(err)
	}
	page := b.String()
	strip := page[strings.Index(page, `<div class="page-header">`):strings.Index(page, `<main class="page">`)]
	for _, want := range []string{`<a class="ph-parent" href="/goals">Goals</a>`, "<h1>Cut churn</h1>", `<span class="ph-count">24 in My org</span>`, `<div class="ph-actions"><a class="btn primary sm" href="/goals/new">New goal</a></div>`} {
		if !strings.Contains(strip, want) {
			t.Errorf("the page header lacks %s:\n%s", want, strip)
		}
	}
	if !strings.Contains(page, `<main class="page"><p>body</p></main>`) {
		t.Errorf("the page's content isn't in main after the header:\n%s", page)
	}

	b.Reset()
	if err := layout("Goals", sam).Render(context.Background(), &b); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "page-header") {
		t.Errorf("a page with no header gets a strip:\n%s", b.String())
	}
}
