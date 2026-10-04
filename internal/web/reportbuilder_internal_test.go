package web

import (
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// A refusal that names no input the builder shows, or none at all, is listed
// at the top of the form; one naming an input is not, as it shows beside it.
func TestBuilderListsARefusalNamingNoInputAtTheTop(t *testing.T) {
	t.Parallel()

	v := reportBuilderView{Mode: domain.ReportModeRules, Problems: []*domain.InputError{
		{Message: "the report could not be saved"},
		{Input: "elsewhere", Message: "an input the page lacks"},
		{Input: domain.ReportInputName, Message: "a Report Definition needs a name"},
	}}
	var page strings.Builder
	if err := reportBuilderPage(&domain.Account{}, v).Render(t.Context(), &page); err != nil {
		t.Fatalf("render: %v", err)
	}
	_, form, ok := strings.Cut(page.String(), `data-testid="report-builder"`)
	if !ok {
		t.Fatalf("no builder form:\n%s", page.String())
	}
	top, rest, ok := strings.Cut(form, "<span>Name</span>")
	if !ok {
		t.Fatalf("the builder has no Name input:\n%s", form)
	}
	for _, want := range []string{"the report could not be saved", "an input the page lacks"} {
		if !strings.Contains(top, want) {
			t.Errorf("%q isn't at the top of the builder:\n%s", want, top)
		}
	}
	if strings.Contains(top, "needs a name") || !strings.Contains(rest, "needs a name") {
		t.Errorf("the name's refusal isn't beside the name alone:\n%s", form)
	}
}
