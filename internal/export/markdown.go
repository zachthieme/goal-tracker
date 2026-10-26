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
// how it selected its Goals as it read when published (nothing for a
// publication frozen before selections were), the line counting what moved
// since then, its open Action Items, the introduction, the narrative curated
// from Highlights, each exception's full MBR block opening with what changed on
// it and naming the Goals it links to, most urgent first, and every other
// selected Goal one line (CONTEXT.md: Report), each Goal with the Fields the
// Report chose. With no hover to show an email, a person's first mention reads
// Name (email) and later ones their Name (CONTEXT.md: Name).
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
	if p.Scope != "" {
		fmt.Fprintf(&b, "\n**Selection:** %s\n", text(p.Scope))
	}
	if r.Movement != nil {
		since := fmtDate(r.Baseline)
		if r.Previous.ID != 0 {
			since = "the previous publication"
		}
		if parts := r.Movement.Parts(); len(parts) > 0 {
			fmt.Fprintf(&b, "\n**Since %s:** %s\n", since, strings.Join(parts, " · "))
		} else {
			fmt.Fprintf(&b, "\nNo movement since %s.\n", since)
		}
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
			fmt.Fprintf(&b, "- %s — %s — %s", health(sg.Health), text(sg.Goal.Title), text(people.Of(sg.Goal.Owner)))
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

// writeBlock writes an exception Goal's full MBR block: its Health, which
// leads a Goal's title everywhere, then the title and badges ("### Red ·
// Launch in EU"); the Owner, Lifecycle, Run off Green as published ("5 wks"),
// what the Health was when it changed since the baseline and the due date with
// its history struck through; what changed on it since, the Goals in the
// Report it contributes to, the chosen Fields, Why it matters, latest status, Path to Green with its back-to-Green
// date struck through as a Milestone's is when it slipped in the Run off Green
// ("2026-10-03 ~~2026-09-12~~ (2)") and marked Overdue once that date has
// passed ("— **Overdue**"), whom it asks for help, Milestones, Metrics against
// target, and the Rolled-up Health with how many children are Red, Yellow and
// Check-in overdue, the Goals behind it, and the Owner's explanation. Each
// Milestone, and the Path to Green (domain.PathToGreenOverdueAt), is marked as
// of asOf, the instant the Report was published, read in the zone the block's
// Goal was judged in.
func writeBlock(b *strings.Builder, people *domain.Mentions, blk domain.ReportBlock, asOf time.Time) {
	g := blk.Goal
	fmt.Fprintf(b, "\n### %s · %s", health(blk.Health), text(g.Title))
	for _, badge := range blk.Badges {
		fmt.Fprintf(b, " **[%s]**", badge)
	}
	fmt.Fprintf(b, "\n\n%s · %s", text(people.Of(g.Owner)), g.Lifecycle)
	if blk.RunOffGreenWeeks > 0 {
		fmt.Fprintf(b, " · %s", domain.RunOffGreenLabel(blk.RunOffGreenWeeks))
	}
	if blk.PriorHealth != "" {
		fmt.Fprintf(b, " · was %s", blk.PriorHealth)
	}
	if !g.DeliveryDate.IsZero() {
		fmt.Fprintf(b, " · due %s%s", struck(g.DeliveryDate, blk.PriorDueDates), fmtDate(g.DeliveryDate))
	}
	if len(blk.Changes) > 0 {
		fmt.Fprintf(b, "\n\n**Changed:** %s", strings.Join(blk.Changes, " · "))
	}
	if len(blk.ContributesTo) > 0 {
		fmt.Fprintf(b, "\n\n**Contributes to:** %s", linkedGoals(blk.ContributesTo))
	}
	if len(blk.Fields) > 0 {
		fmt.Fprintf(b, "\n\n%s", fields(blk.Fields, "**%s:** %s"))
	}
	fmt.Fprintf(b, "\n\n**Why it matters:** %s\n", text(g.WhyItMatters))
	if blk.Status != "" {
		fmt.Fprintf(b, "\n**Status:** %s\n", text(blk.Status))
	}
	if blk.PathToGreen != "" {
		fmt.Fprintf(b, "\n**Path to Green:** %s (back to Green by %s)", text(blk.PathToGreen), milestoneDate(blk.PathTargetDate, blk.PriorPathTargetDates))
		if domain.PathToGreenOverdueAt(blk, asOf) {
			b.WriteString(" — **Overdue**")
		}
		b.WriteString("\n")
	}
	if blk.AsksForHelp {
		if blk.AskAddressee != nil {
			fmt.Fprintf(b, "\n**Asks for help** of %s\n", text(people.Of(*blk.AskAddressee)))
		} else {
			b.WriteString("\n**Asks for help** (no one above the Owner)\n")
		}
	}
	if len(blk.Milestones) > 0 {
		b.WriteString("\n**Milestones:**\n\n")
		for _, m := range blk.Milestones {
			b.WriteString("- ")
			if mark := domain.MilestoneMarkOf(m.Milestone, m.PriorDates, m.New, asOf.In(blk.Location())); mark != domain.MilestoneMarkNone {
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
	var parts []string
	if blk.RolledUp.Present {
		parts = append(parts, "**Rolled-up Health:** "+blk.RolledUp.Health)
	}
	if count := blk.RolledUp.CountLine(); count != "" {
		parts = append(parts, count)
	}
	if len(parts) > 0 {
		fmt.Fprintf(b, "\n%s\n", strings.Join(parts, " · "))
	}
	if len(blk.Behind) > 0 {
		fmt.Fprintf(b, "\n**Supported by:** %s\n", linkedGoals(blk.Behind))
	}
	if (blk.RolledUp.Present || blk.RolledUp.CheckinOverdueChildren > 0) && blk.Explanation != "" {
		fmt.Fprintf(b, "\n**Why the Health differs:** %s\n", text(blk.Explanation))
	}
}

// linkedGoals names the Goals an exception block links to, joined by " · ",
// each led by its Health when it has one and marked ↓ when the Report holds it
// ("Red Alpha ledger ↓"). With no base URL to link from, they are names, not
// links.
func linkedGoals(goals []domain.LinkedGoal) string {
	names := make([]string, 0, len(goals))
	for _, g := range goals {
		name := text(g.Title)
		if g.Health != "" {
			name = g.Health + " " + name
		}
		if g.InReport {
			name += " ↓"
		}
		names = append(names, name)
	}
	return strings.Join(names, " · ")
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
// A date equal to the current one is never struck through.
func struck(current time.Time, prior []time.Time) string {
	var b strings.Builder
	for _, d := range domain.StruckDates(current, prior) {
		fmt.Fprintf(&b, "~~%s~~ ", fmtDate(d))
	}
	return b.String()
}

// milestoneDate renders a Milestone's date current first, then the most recent
// date it slipped from struck through, and the number of times it slipped once
// that is more than one: 11/20 ~~11/03~~ (3) (CONTEXT.md: Date Slip). Prior
// dates come earliest first. A date equal to the current one is never struck
// through: the most recent that differs is, or none.
func milestoneDate(current time.Time, prior []time.Time) string {
	out := fmtDate(current)
	if struck := domain.StruckDates(current, prior); len(struck) > 0 {
		out += fmt.Sprintf(" ~~%s~~", fmtDate(struck[len(struck)-1]))
	}
	if n := len(prior); n > 1 {
		out += fmt.Sprintf(" (%d)", n)
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
