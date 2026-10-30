package web

import (
	"github.com/zachthieme/goal-tracker/internal/domain"
)

// goalPickerView is what a Goal picker shows: the define page's Contributes
// to control, and the one-pick Parent Goal of Suggest a parent and of the
// Goal page's Link to a parent Goal. Script searches the Goals by title as it
// is typed, each result showing its Health, title and Owner, and picking one
// adds its chip; without script it is a select of the candidates.
type goalPickerView struct {
	// Single picks at most one Goal: a pick replaces the chip there, the
	// server renders none, and the no-script select is a single one starting
	// on "Choose a Goal…". Otherwise it picks any number, starting with
	// Picked, and the select is a multiple one.
	Single bool
	// SearchURL is where the search asks for its matches, and SearchLabel
	// its accessible name.
	SearchURL   string
	SearchLabel string
	// Candidates are the Goals the no-script select offers.
	Candidates []domain.Goal
	// Picked are the chips it starts with, each with why it was refused, in
	// multi-pick mode.
	Picked []goalPickerChip
	// Error is why the pick was refused, beside the picker, or "".
	Error string
	// Autofocus gives the search the page's focus.
	Autofocus bool
}

// goalPickerChip is a picked Goal's chip and why it was refused, or "".
type goalPickerChip struct {
	Choice parentChoice
	Error  string
}
