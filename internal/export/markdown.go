// Package export renders a published Report for reading outside the tool.
package export

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// Markdown renders a published Report as Markdown with the same content as its
// publication page, in the order and words its facts list them
// (domain.PublicationFacts), as frozen when it was published: who published
// it and when, what it read its changes against, how it selected its Goals as
// it read when published, its Goals counted by Health and Check-in overdue
// when it selects any, the line counting what moved since then, its open
// Action Items, the introduction, the narrative curated from Highlights, each
// exception's full MBR block (writeBlock), most urgent first, every other
// selected Goal On track one line, each Goal with the Fields the Report chose,
// and the Goals that entered or left it (CONTEXT.md: Report). It writes only the Markdown: headings, emphasis,
// escaping, dates and numbers. With no hover to show an email, a person's
// first mention reads Name (email) and later ones their Name (CONTEXT.md:
// Name).
func Markdown(p domain.Publication) string {
	var b strings.Builder
	var people domain.Mentions
	fmt.Fprintf(&b, "# %s\n\n", text(p.Name))
	// "No Goals selected." stands where the Goals would, before the Goals
	// that entered or left, when no fact holds any: no exceptions and none On
	// track.
	noGoals := !slices.ContainsFunc(p.Facts, func(f domain.ReportFact) bool {
		return f.Kind == domain.ReportFactExceptions || f.Kind == domain.ReportFactOnTrack
	})
	for _, f := range p.Facts {
		if noGoals && f.Kind == domain.ReportFactMembershipChanges {
			b.WriteString("\nNo Goals selected.\n")
			noGoals = false
		}
		writeFact(&b, &people, f)
	}
	if noGoals {
		b.WriteString("\nNo Goals selected.\n")
	}
	return b.String()
}

// writeFact writes one of a publication's facts.
func writeFact(b *strings.Builder, people *domain.Mentions, f domain.ReportFact) {
	switch f.Kind {
	case domain.ReportFactByline:
		fmt.Fprintf(b, "%s %s by %s. ", f.Label, fmtPublished(f.Byline.PublishedAt), text(people.Of(f.Byline.PublishedBy)))
		if words := f.Byline.Since.Words; words != "" {
			fmt.Fprintf(b, "Changes since %s, %s.\n", words, fmtPublished(f.Byline.Since.At))
		} else {
			fmt.Fprintf(b, "Changes since %s.\n", fmtDate(f.Byline.Since.At))
		}
	case domain.ReportFactSelection:
		fmt.Fprintf(b, "\n**%s:** %s\n", f.Label, text(f.Text))
	case domain.ReportFactHealthCounts:
		h := f.HealthCounts
		fmt.Fprintf(b, "\n**%s:** %d %s · %d %s · %d %s · %d %s\n", f.Label,
			h.Red, domain.HealthRed, h.Yellow, domain.HealthYellow, h.Green, domain.HealthGreen, h.CheckinOverdue, domain.BadgeCheckinOverdue)
	case domain.ReportFactMovement:
		since := f.Movement.Since.Words
		if since == "" {
			since = fmtDate(f.Movement.Since.At)
		}
		if len(f.Movement.Parts) > 0 {
			fmt.Fprintf(b, "\n**%s %s:** %s\n", f.Label, since, strings.Join(f.Movement.Parts, " · "))
		} else {
			fmt.Fprintf(b, "\nNo movement since %s.\n", since)
		}
	case domain.ReportFactActionItems:
		fmt.Fprintf(b, "\n## %s\n\n", f.Label)
		for _, item := range f.ActionItems {
			fmt.Fprintf(b, "- %s — %s — due %s\n", text(item.Text), text(people.Of(item.Owner)), fmtDate(item.DueDate))
		}
	case domain.ReportFactIntroduction:
		fmt.Fprintf(b, "\n%s\n", text(f.Text))
	case domain.ReportFactNarrative:
		for _, sec := range f.Narrative {
			fmt.Fprintf(b, "\n## %s\n", sec.Heading)
			for _, note := range sec.Notes {
				fmt.Fprintf(b, "\n%s\n", text(note))
			}
			if len(sec.Highlights) > 0 {
				b.WriteString("\n")
				for _, nh := range sec.Highlights {
					fmt.Fprintf(b, "- %s — %s, %s\n", text(nh.Highlight.Note), text(people.Of(nh.Highlight.Owner)), text(nh.GoalTitle))
				}
			}
		}
	case domain.ReportFactExceptions:
		fmt.Fprintf(b, "\n## %s\n", f.Label)
		for _, blk := range f.Blocks {
			writeBlock(b, people, blk)
		}
	case domain.ReportFactOnTrack:
		fmt.Fprintf(b, "\n## %s\n\n", f.Label)
		for _, sg := range f.Lines {
			fmt.Fprintf(b, "- %s — %s — %s", health(sg.Health), text(sg.Goal.Title), text(people.Of(sg.Goal.Owner)))
			if !sg.Goal.DeliveryDate.IsZero() {
				fmt.Fprintf(b, " — due %s", fmtDate(sg.Goal.DeliveryDate))
			}
			if len(sg.Fields) > 0 {
				fmt.Fprintf(b, " — %s", fields(sg.Fields, "%s: %s"))
			}
			b.WriteString("\n")
		}
	case domain.ReportFactMembershipChanges:
		fmt.Fprintf(b, "\n## %s\n\n", f.Label)
		for _, c := range f.MembershipChanges {
			fmt.Fprintf(b, "- %s — %s — %s\n", text(c.Goal.Title), c.Direction, c.Reason)
		}
	}
}

// writeBlock writes an exception Goal's full MBR block from its facts
// (domain.ReportBlock.Facts), as read the instant the Report was published:
// its Health, which leads a Goal's title everywhere, then the title
// and badges ("### Red · Launch in EU"); the Owner, Lifecycle, Run off Green
// as published ("5 wks"), what the Health was when it changed since the
// baseline and the due date with its history struck through; then each of its
// other facts under its label. A Path to Green's back-to-Green date is struck
// through as a Milestone's is when it slipped in the Run off Green
// ("2026-10-03 ~~2026-09-12~~ · 2 slips") and marked Overdue once that date
// has passed ("— **Overdue**"). A Milestone's name is set off by " · " from its
// date's slip count when that shows.
func writeBlock(b *strings.Builder, people *domain.Mentions, blk domain.ReportBlockFacts) {
	for _, f := range blk.Facts {
		writeBlockFact(b, people, f)
	}
}

// writeBlockFact writes one of an exception block's facts.
func writeBlockFact(b *strings.Builder, people *domain.Mentions, f domain.ReportFact) {
	switch f.Kind {
	case domain.ReportFactBlockHead:
		h := f.Head
		fmt.Fprintf(b, "\n### %s · %s", health(h.Health), text(h.Title))
		for _, badge := range h.Badges {
			fmt.Fprintf(b, " **[%s]**", badge)
		}
		fmt.Fprintf(b, "\n\n%s · %s", text(people.Of(h.Owner)), h.Lifecycle)
		for _, part := range []string{h.RunOffGreen, h.WasHealth} {
			if part != "" {
				fmt.Fprintf(b, " · %s", part)
			}
		}
		if !h.Due.IsZero() {
			fmt.Fprintf(b, " · due %s%s", struck(h.Due, h.PriorDue), fmtDate(h.Due))
		}
	case domain.ReportFactChanged:
		fmt.Fprintf(b, "\n\n**%s:** %s", f.Label, strings.Join(f.Changes, " · "))
	case domain.ReportFactScheduleConflict:
		fmt.Fprintf(b, "\n\n**%s:** %s", f.Label, scheduleConflicts(people, f.ScheduleConflictGroups))
	case domain.ReportFactContributesTo:
		fmt.Fprintf(b, "\n\n**%s:** %s", f.Label, linkedGoals(f.Goals))
	case domain.ReportFactFields:
		fmt.Fprintf(b, "\n\n%s", fields(f.Fields, "**%s:** %s"))
	case domain.ReportFactWhyItMatters:
		fmt.Fprintf(b, "\n\n**%s:** %s\n", f.Label, text(f.Text))
	case domain.ReportFactStatus, domain.ReportFactExplanation:
		fmt.Fprintf(b, "\n**%s:** %s\n", f.Label, text(f.Text))
	case domain.ReportFactPathToGreen:
		path := f.PathToGreen
		fmt.Fprintf(b, "\n**%s:** %s (%s %s)", f.Label, text(path.Plan), path.ByWords, milestoneDate(path.By, path.PriorDates))
		if path.Overdue {
			fmt.Fprintf(b, " — **%s**", path.OverdueWords)
		}
		b.WriteString("\n")
	case domain.ReportFactAskForHelp:
		if f.Ask.Addressee != nil {
			fmt.Fprintf(b, "\n**%s** of %s\n", f.Label, text(people.Of(*f.Ask.Addressee)))
		} else {
			fmt.Fprintf(b, "\n**%s** (%s)\n", f.Label, f.Ask.Words)
		}
	case domain.ReportFactMilestones:
		fmt.Fprintf(b, "\n**%s:**\n\n", f.Label)
		for _, m := range f.Milestones {
			b.WriteString("- ")
			if m.Mark != domain.MilestoneMarkNone {
				fmt.Fprintf(b, "**[%s]** ", m.Mark)
			}
			name := text(m.Milestone.Name)
			if m.Milestone.Status == domain.MilestoneRemoved {
				name = "~~" + name + "~~"
			}
			sep := " "
			if len(m.PriorDates) > 1 {
				sep = " · "
			}
			fmt.Fprintf(b, "%s%s%s", milestoneDate(m.Milestone.TargetDate, m.PriorDates), sep, name)
			if m.Milestone.RemovedReason != "" {
				fmt.Fprintf(b, " — %s", text(m.Milestone.RemovedReason))
			}
			b.WriteString("\n")
		}
	case domain.ReportFactMetrics:
		fmt.Fprintf(b, "\n**%s:**\n\n", f.Label)
		for _, m := range f.Metrics {
			fmt.Fprintf(b, "- %s: %s %s %s %s by %s (baseline %s, %s)\n",
				text(m.Metric.Name), m.Reading(fmtNum), m.AgainstWords, fmtNum(m.Metric.Target), text(m.Metric.Unit),
				fmtDate(m.Metric.TargetDate), fmtNum(m.Metric.Baseline), m.Metric.Direction)
		}
	case domain.ReportFactRolledUp:
		var parts []string
		if f.RolledUp.Present {
			parts = append(parts, "**"+f.Label+":** "+f.RolledUp.Health)
		}
		if f.RolledUp.Count != "" {
			parts = append(parts, f.RolledUp.Count)
		}
		fmt.Fprintf(b, "\n%s\n", strings.Join(parts, " · "))
	case domain.ReportFactSupportedBy:
		fmt.Fprintf(b, "\n**%s:** %s\n", f.Label, linkedGoals(f.Goals))
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

// scheduleConflicts names the Schedule conflicts an exception block's Goal is
// in by side, as its fact groups them (domain.ReportScheduleConflictGroups),
// the sides joined by "; ", each its words, then each Goal at the other end
// with its Owner, its words before a date and its delivery date, joined by
// " · ": "Parent Goal due earlier · Open in Iberia (Pat
// Parker), due 2026-10-15; Contributing Goal due later · Lisbon office (Sam
// Lee), due 2026-12-01". With no base URL to link from, the other Goal is a
// name, not a link.
func scheduleConflicts(people *domain.Mentions, groups []domain.ReportScheduleConflictGroup) string {
	sides := make([]string, 0, len(groups))
	for _, g := range groups {
		entries := []string{g.Words}
		for _, c := range g.Conflicts {
			entries = append(entries, fmt.Sprintf("%s (%s), %s %s",
				text(c.Title), text(people.Of(c.Owner)), g.DueWords, fmtDate(c.Due)))
		}
		sides = append(sides, strings.Join(entries, " · "))
	}
	return strings.Join(sides, "; ")
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
// that is more than one: 11/20 ~~11/03~~ · 3 slips (CONTEXT.md: Date Slip). Prior
// dates come earliest first. A date equal to the current one is never struck
// through: the most recent that differs is, or none.
func milestoneDate(current time.Time, prior []time.Time) string {
	out := fmtDate(current)
	if struck := domain.StruckDates(current, prior); len(struck) > 0 {
		out += fmt.Sprintf(" ~~%s~~", fmtDate(struck[len(struck)-1]))
	}
	if n := len(prior); n > 1 {
		out += " · " + domain.SlipsWords(n)
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
