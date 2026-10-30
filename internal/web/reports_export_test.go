package web

import (
	"context"
	"strings"
	"time"

	"github.com/a-h/templ"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// RenderPublicationContent renders a publication's column as publicationPage
// does — its byline, its Selection and the Report's content — with a
// discussion holding only loc, for the web_test package's tests.
func RenderPublicationContent(p domain.Publication, loc *time.Location) string {
	var b strings.Builder
	d := discussion{Loc: loc}
	for _, c := range []templ.Component{publishedLine(p.Facts, loc), publicationSelection(p.Facts), reportContent(p.Facts, d)} {
		if err := c.Render(context.Background(), &b); err != nil {
			panic(err)
		}
	}
	return b.String()
}

// RenderPublicationPrint renders a publication's Print page as
// handlePrintPublication does, without hover, so each person's first mention
// reads Name (email), its times in loc, for the web_test package's tests.
func RenderPublicationPrint(p domain.Publication, loc *time.Location) string {
	var b strings.Builder
	if err := publicationPrintPage(p, loc).Render(withoutHover(context.Background()), &b); err != nil {
		panic(err)
	}
	return b.String()
}
