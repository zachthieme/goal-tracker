// Package export renders a published Report for reading outside the tool.
package export

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// Markdown renders a published Report as Markdown with the same content as its
// snapshot page: who published it and when, what it read its changes against,
// its open Action Items, the introduction, the narrative curated from
// Highlights, each exception's full MBR block, and every other selected Goal
// one line (CONTEXT.md: Report), each Goal with the Fields the Report chose. With no hover to show an email, a person's
// first mention reads Name (email) and later ones their Name (CONTEXT.md: Name).
func Markdown(p domain.Publication) string {
	var b strings.Builder
	var people domain.Mentions
	r := p.Report
	fmt.Fprintf(&b, "# %s\n\n", text(r.Definition.Name))
	fmt.Fprintf(&b, "Published %s by %s. ", fmtPublished(p.PublishedAt), text(people.Of(p.PublishedBy)))
	if r.Previous.ID != 0 {
		fmt.Fprintf(&b, "Changes since the previous publication, %s.\n", fmtPublished(r.Previous.PublishedAt))
	} else {
		fmt.Fprintf(&b, "Changes since %s.\n", fmtDate(r.Baseline))
	}
	if len(r.ActionItems) > 0 {
		b.WriteString("\n## Open Action Items\n\n")
		for _, item := range r.ActionItems {
			fmt.Fprintf(&b, "- %s — %s — due %s\n", text(item.Text), text(people.Of(item.Owner)), fmtDate(item.DueDate))
		}
	}
	if r.Definition.Introduction != "" {
		fmt.Fprintf(&b, "\n%s\n", text(r.Definition.Introduction))
	}
	for _, sec := range r.Narrative {
		fmt.Fprintf(&b, "\n## %s\n", sec.Heading())
		for _, note := range sec.Notes {
			fmt.Fprintf(&b, "\n%s\n", text(note))
		}
		if len(sec.Highlights) > 0 {
			b.WriteString("\n")
			for _, nh := range sec.Highlights {
				fmt.Fprintf(&b, "- %s — %s, %s\n", text(nh.Highlight.Note), text(people.Of(nh.Highlight.Owner)), text(nh.GoalTitle))
			}
		}
	}
	if len(r.Exceptions) == 0 && len(r.Lines) == 0 {
		b.WriteString("\nNo Goals selected.\n")
	}
	if len(r.Exceptions) > 0 {
		b.WriteString("\n## Exceptions\n")
		for _, blk := range r.Exceptions {
			writeBlock(&b, &people, blk, p.PublishedAt)
		}
	}
	if len(r.Lines) > 0 {
		b.WriteString("\n## Other Goals\n\n")
		for _, sg := range r.Lines {
			fmt.Fprintf(&b, "- %s — %s — %s", text(sg.Goal.Title), text(people.Of(sg.Goal.Owner)), health(sg.Health))
			if !sg.Goal.DeliveryDate.IsZero() {
				fmt.Fprintf(&b, " — due %s", fmtDate(sg.Goal.DeliveryDate))
			}
			if len(sg.Fields) > 0 {
				fmt.Fprintf(&b, " — %s", fields(sg.Fields, "%s: %s"))
			}
			b.WriteString("\n")
		}
	}
	return b.String()
}

// writeBlock writes an exception Goal's full MBR block: title and badges, due
// date with its history struck through, Health, the chosen Fields, So What, latest status, Path
// to Green, Milestones, Metrics against target, and the Rolled-up Health with
// the Owner's explanation. Each Milestone is marked as of asOf, the day the
// Report was published.
func writeBlock(b *strings.Builder, people *domain.Mentions, blk domain.ReportBlock, asOf time.Time) {
	g := blk.Goal
	fmt.Fprintf(b, "\n### %s", text(g.Title))
	for _, badge := range blk.Badges {
		fmt.Fprintf(b, " **[%s]**", badge)
	}
	fmt.Fprintf(b, "\n\n%s · %s · Health: **%s**", text(people.Of(g.Owner)), g.Lifecycle, health(blk.Health))
	if !g.DeliveryDate.IsZero() {
		fmt.Fprintf(b, " · due %s%s", struck(blk.PriorDueDates), fmtDate(g.DeliveryDate))
	}
	if len(blk.Fields) > 0 {
		fmt.Fprintf(b, "\n\n%s", fields(blk.Fields, "**%s:** %s"))
	}
	fmt.Fprintf(b, "\n\n**So What:** %s\n", text(g.SoWhat))
	if blk.Status != "" {
		fmt.Fprintf(b, "\n**Status:** %s\n", text(blk.Status))
	}
	if blk.PathToGreen != "" {
		fmt.Fprintf(b, "\n**Path to Green:** %s (back to Green by %s)\n", text(blk.PathToGreen), fmtDate(blk.PathTargetDate))
	}
	if len(blk.Milestones) > 0 {
		b.WriteString("\n**Milestones:**\n\n")
		for _, m := range blk.Milestones {
			b.WriteString("- ")
			if mark := domain.MilestoneMarkOf(m.Milestone, m.PriorDates, m.New, asOf); mark != domain.MilestoneMarkNone {
				fmt.Fprintf(b, "**[%s]** ", mark)
			}
			name := text(m.Milestone.Name)
			if m.Milestone.Status == domain.MilestoneRemoved {
				name = "~~" + name + "~~"
			}
			fmt.Fprintf(b, "%s %s", milestoneDate(m.Milestone.TargetDate, m.PriorDates), name)
			if m.Milestone.RemovedReason != "" {
				fmt.Fprintf(b, " — %s", text(m.Milestone.RemovedReason))
			}
			b.WriteString("\n")
		}
	}
	if len(blk.Metrics) > 0 {
		b.WriteString("\n**Metrics:**\n\n")
		for _, m := range blk.Metrics {
			current := "no reading yet"
			if m.Read {
				current = fmtNum(m.Current)
			}
			fmt.Fprintf(b, "- %s: %s against a target of %s %s by %s (baseline %s, %s)\n",
				text(m.Metric.Name), current, fmtNum(m.Metric.Target), text(m.Metric.Unit),
				fmtDate(m.Metric.TargetDate), fmtNum(m.Metric.Baseline), m.Metric.Direction)
		}
	}
	if blk.RolledUp.Present || blk.RolledUp.StaleChildren > 0 {
		var parts []string
		if blk.RolledUp.Present {
			parts = append(parts, "**Rolled-up Health:** "+blk.RolledUp.Health)
		}
		if blk.RolledUp.StaleChildren > 0 {
			parts = append(parts, fmt.Sprintf("%d of %d Stale", blk.RolledUp.StaleChildren, blk.RolledUp.ActiveChildren))
		}
		fmt.Fprintf(b, "\n%s\n", strings.Join(parts, " · "))
		if blk.Explanation != "" {
			fmt.Fprintf(b, "\n**Why the Health differs:** %s\n", text(blk.Explanation))
		}
	}
}

// fields renders a Goal's values in the Fields its Report chose on one line,
// each through format with the Field's name and its value: a number with its
// unit, and a long text with its line breaks read as spaces. Each stands alone;
// nothing is totalled (ADR 0005).
func fields(values []domain.FieldValue, format string) string {
	parts := make([]string, 0, len(values))
	for _, v := range values {
		value := v.Value
		if v.Field.Type == domain.FieldNumber && v.Field.Unit != "" {
			value += " " + v.Field.Unit
		}
		parts = append(parts, fmt.Sprintf(format, text(v.Field.Name), text(value)))
	}
	return strings.Join(parts, " · ")
}

// inlineMarkup is the Markdown that would change how text people typed reads
// anywhere in a line: emphasis, strikethrough, links, code, and HTML.
var inlineMarkup = strings.NewReplacer(
	`\`, `\\`, "*", `\*`, "_", `\_`, "~", `\~`, "[", `\[`, "]", `\]`, "`", "\\`", "<", `\<`,
)

// blockMarker matches what would start a heading, list, or quote when it opens
// a line; its last character is the one to escape.
var blockMarker = regexp.MustCompile(`^(#|-|\+|>|=|\d+[.)])`)

// text renders what people typed as Markdown that reads the same as on the
// snapshot page: a line break reads as a space, and Markdown syntax is escaped
// so it shows as typed.
func text(s string) string {
	s = inlineMarkup.Replace(strings.Join(strings.Fields(s), " "))
	if m := blockMarker.FindString(s); m != "" {
		s = m[:len(m)-1] + `\` + s[len(m)-1:]
	}
	return s
}

// struck renders the dates a date held before its Date Slips, each struck
// through ahead of the current date: ~~10/15~~ 11/03 (CONTEXT.md: Date Slip).
func struck(dates []time.Time) string {
	var b strings.Builder
	for _, d := range dates {
		fmt.Fprintf(&b, "~~%s~~ ", fmtDate(d))
	}
	return b.String()
}

// milestoneDate renders a Milestone's date current first, then the most recent
// date it slipped from struck through, and the number of times it slipped once
// that is more than one: 11/20 ~~11/03~~ (3) (CONTEXT.md: Date Slip). Prior
// dates come earliest first.
func milestoneDate(current time.Time, prior []time.Time) string {
	out := fmtDate(current)
	if n := len(prior); n > 0 {
		out += fmt.Sprintf(" ~~%s~~", fmtDate(prior[n-1]))
		if n > 1 {
			out += fmt.Sprintf(" (%d)", n)
		}
	}
	return out
}

// health renders a Health, or a dash when the Goal has no Check-in yet.
func health(h string) string {
	if h == "" {
		return "—"
	}
	return h
}

// fmtDate renders a calendar date.
func fmtDate(t time.Time) string {
	return t.Format("2006-01-02")
}

// fmtNum renders a Metric's value without trailing zeros.
func fmtNum(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// fmtPublished renders the instant a Report was published, to the minute.
func fmtPublished(t time.Time) string {
	return t.UTC().Format("2006-01-02 15:04 UTC")
}
