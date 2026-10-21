package web

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"

	"github.com/zachthieme/goal-tracker/internal/domain"
)

// goalTable is the Goal list's table layout: its columns and its rows, each
// with the Field values its Field columns show. It has no totals row (ADR
// 0005) and isn't grouped. It is read-only but in edit mode (?edit=1), where
// it is one form setting the Dimension values and Fields of the Goals the
// person may (#80).
type goalTable struct {
	// Columns are the columns shown; All adds the ones this browser hides, for
	// the Columns control, and Hidden names those.
	Columns []tableColumn
	All     []tableColumn
	Hidden  map[string]bool
	Rows    []goalTableRow
	// Sort is the Key of the column the rows are sorted by (?sort=), "" for the
	// list's problem-first order; Desc reverses it (?dir=desc).
	Sort string
	Desc bool
	// Edit is edit mode, and Editable names the Goals whose rows get inputs:
	// those the person owns, is a Delegate on, or any as an Admin.
	Edit     bool
	Editable map[int64]bool
	// Typed is what a refused save posted, so the form comes back as typed,
	// and Bad says why each refused cell was, by its input's name; Refusal is
	// a reason the save was refused that isn't any one cell's.
	Typed   url.Values
	Bad     map[string]string
	Refusal string
}

// tableColumn is one of the Goal table's columns, keyed for the URL. A
// Dimension's or Field's column carries it.
type tableColumn struct {
	Key       string
	Label     string
	Dimension *domain.Dimension
	Field     *domain.Field
}

// goalTableRow is one Goal in the table: its list row and its value in each
// Field, by Field ID.
type goalTableRow struct {
	goalRow
	Fields map[int64]string
}

// goalTableView builds the table layout over the list's filtered, sorted rows,
// leaving out the columns this browser hides. It sorts by the ?sort= column,
// if there is one.
func (s *Server) goalTableView(ctx context.Context, rows []goalRow, dims []domain.Dimension, q url.Values, hidden map[string]bool, current domain.Account) (*goalTable, error) {
	all, err := s.tableColumns(ctx, dims)
	if err != nil {
		return nil, err
	}
	table := &goalTable{All: all, Hidden: hidden, Edit: q.Get("edit") == "1"}
	if table.Edit {
		if table.Editable, err = s.editableGoals(ctx, rows, current); err != nil {
			return nil, err
		}
	}
	for _, col := range all {
		if !hidden[col.Key] {
			table.Columns = append(table.Columns, col)
		}
	}
	for _, row := range rows {
		values, err := s.svc.GoalFields(ctx, row.Goal.ID)
		if err != nil {
			return nil, err
		}
		tr := goalTableRow{goalRow: row, Fields: map[int64]string{}}
		for _, fv := range values {
			tr.Fields[fv.Field.ID] = fv.Value
		}
		table.Rows = append(table.Rows, tr)
	}
	for _, col := range all {
		if col.Key == q.Get("sort") {
			table.Sort, table.Desc = col.Key, q.Get("dir") == "desc"
			sortTableRows(table.Rows, col, table.Desc)
		}
	}
	return table, nil
}

// tableColumns are the Goal table's columns: the fixed ones, then each live
// Dimension and each live Field in name order (CONTEXT.md: Retired).
func (s *Server) tableColumns(ctx context.Context, dims []domain.Dimension) ([]tableColumn, error) {
	all, err := s.svc.ListFields(ctx)
	if err != nil {
		return nil, err
	}
	fields := domain.OfferedFields(all)
	columns := []tableColumn{
		{Key: "title", Label: "Title"},
		{Key: "owner", Label: "Owner"},
		{Key: "health", Label: "Health"},
		{Key: "lifecycle", Label: "Lifecycle"},
		{Key: "due", Label: "Delivery date"},
		{Key: "checkin", Label: "Last check-in"},
	}
	for i := range dims {
		columns = append(columns, tableColumn{Key: fmt.Sprintf("d%d", dims[i].ID), Label: dims[i].Name, Dimension: &dims[i]})
	}
	for i := range fields {
		columns = append(columns, tableColumn{Key: fmt.Sprintf("f%d", fields[i].ID), Label: fields[i].Name, Field: &fields[i]})
	}
	return columns, nil
}

// hiddenColumnsCookie remembers, in this browser, the Goal table columns a
// person hid: their keys, comma-separated.
const hiddenColumnsCookie = "goal_table_hidden"

// hiddenColumns reads the columns this browser hides. Title is never hidden.
func hiddenColumns(r *http.Request) map[string]bool {
	hidden := map[string]bool{}
	if c, err := r.Cookie(hiddenColumnsCookie); err == nil {
		for _, key := range strings.Split(c.Value, ",") {
			if key != "" && key != "title" {
				hidden[key] = true
			}
		}
	}
	return hidden
}

// handleGoalTableColumns saves the Columns control's choice: every column it
// offers that isn't ticked (?show=) is hidden, remembered in a cookie so it
// holds on the next visit. It then redirects to the same view, so the choice
// stays out of a URL someone might share.
func (s *Server) handleGoalTableColumns(w http.ResponseWriter, r *http.Request) {
	dims, err := s.svc.ListDimensions(r.Context())
	if err != nil {
		s.serverError(w, r, "could not list goals", err)
		return
	}
	columns, err := s.tableColumns(r.Context(), domain.OfferedDimensions(dims))
	if err != nil {
		s.serverError(w, r, "could not list goals", err)
		return
	}
	q := r.URL.Query()
	var hidden []string
	for _, col := range columns {
		if col.Key != "title" && !slices.Contains(q["show"], col.Key) {
			hidden = append(hidden, col.Key)
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     hiddenColumnsCookie,
		Value:    strings.Join(hidden, ","),
		Path:     "/",
		MaxAge:   400 * 24 * 60 * 60,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	})
	q.Del("columns")
	q.Del("show")
	http.Redirect(w, r, string(goalsListData{Query: q}.queryURL(func(url.Values) {})), http.StatusSeeOther)
}

// carriedQuery is the view's query string as name/value pairs, in a stable
// order, for a form that must land back on the same view.
func (v goalsListData) carriedQuery() [][2]string {
	var pairs [][2]string
	for _, k := range slices.Sorted(maps.Keys(v.Query)) {
		for _, val := range v.Query[k] {
			pairs = append(pairs, [2]string{k, val})
		}
	}
	return pairs
}

// sortURL links to this view sorted by the column key: ascending, or
// descending when it is already sorted ascending by it.
func (v goalsListData) sortURL(key string) templ.SafeURL {
	return v.queryURL(func(q url.Values) {
		desc := v.Table != nil && v.Table.Sort == key && !v.Table.Desc
		q.Set("sort", key)
		if desc {
			q.Set("dir", "desc")
		} else {
			q.Del("dir")
		}
	})
}

// ariaSort says whether the table is sorted by col's column, and which way.
func (t goalTable) ariaSort(col tableColumn) string {
	switch {
	case t.Sort != col.Key:
		return "none"
	case t.Desc:
		return "descending"
	}
	return "ascending"
}

// sortTableRows sorts rows by col, reversed when desc, keeping the
// problem-first order among equal values. The Goals with no value in col sort
// last either way.
func sortTableRows(rows []goalTableRow, col tableColumn, desc bool) {
	slices.SortStableFunc(rows, func(a, b goalTableRow) int {
		ka, aSet := col.sortKey(a)
		kb, bSet := col.sortKey(b)
		switch {
		case !aSet || !bSet:
			return cmp.Compare(boolRank(!aSet), boolRank(!bSet))
		case desc:
			return kb.compare(ka)
		}
		return ka.compare(kb)
	})
}

func boolRank(b bool) int {
	if b {
		return 1
	}
	return 0
}

// cellKey is a cell's value as it sorts: by its numbers, then its text.
type cellKey struct {
	nums []float64
	text string
}

func (k cellKey) compare(o cellKey) int {
	if c := slices.Compare(k.nums, o.nums); c != 0 {
		return c
	}
	return cmp.Compare(k.text, o.text)
}

// healthOrder sorts Health most urgent first, the way the list does.
var healthOrder = []string{domain.HealthRed, domain.HealthYellow, domain.HealthGreen}

// sortKey is row's value in col as it sorts, and false when it has none: dates
// and numbers as numbers, Health most urgent first, Lifecycle and a Dimension's
// values in their own order, and text ignoring case.
func (c tableColumn) sortKey(row goalTableRow) (cellKey, bool) {
	g := row.Goal
	num := func(n float64) (cellKey, bool) { return cellKey{nums: []float64{n}}, true }
	text := func(s string) (cellKey, bool) { return cellKey{text: strings.ToLower(s)}, s != "" }
	switch {
	case c.Key == "title":
		return text(g.Title)
	case c.Key == "owner":
		return text(g.Owner.Label())
	case c.Key == "health":
		if i := slices.Index(healthOrder, row.health()); i >= 0 {
			return num(float64(i))
		}
	case c.Key == "lifecycle":
		return num(float64(slices.Index(lifecycleFilters, g.Lifecycle)))
	case c.Key == "due":
		if !g.DeliveryDate.IsZero() {
			return num(float64(g.DeliveryDate.Unix()))
		}
	case c.Key == "checkin":
		if row.Latest != nil {
			return num(float64(row.Latest.CreatedAt.Unix()))
		}
	case c.Dimension != nil:
		var key cellKey
		for _, v := range row.Values {
			if v.DimensionID == c.Dimension.ID {
				i := slices.IndexFunc(c.Dimension.Values, func(dv domain.DimensionValue) bool { return dv.ID == v.ID })
				key.nums = append(key.nums, float64(i))
			}
		}
		return key, len(key.nums) > 0
	case c.Field != nil:
		value, ok := row.Fields[c.Field.ID]
		if !ok {
			break
		}
		switch c.Field.Type {
		case domain.FieldNumber:
			if n, err := strconv.ParseFloat(value, 64); err == nil {
				return num(n)
			}
		case domain.FieldDate:
			if d, err := time.Parse(dateLayout, value); err == nil {
				return num(float64(d.Unix()))
			}
		default:
			return text(value)
		}
	}
	return cellKey{}, false
}

// text is row's value in a Dimension's or Field's column: a several-values
// Dimension's values joined by commas, a number Field's value with its unit, a
// date Field's as dates shows it.
func (c tableColumn) text(row goalTableRow, dates displayDates) string {
	switch {
	case c.Dimension != nil:
		var values []string
		for _, v := range row.Values {
			if v.DimensionID == c.Dimension.ID {
				values = append(values, v.Value)
			}
		}
		return strings.Join(values, ", ")
	case c.Field != nil:
		value, ok := row.Fields[c.Field.ID]
		if ok && c.Field.Type == domain.FieldDate {
			return dates.dateText(value)
		}
		if ok && c.Field.Unit != "" {
			return value + " " + c.Field.Unit
		}
		return value
	}
	return ""
}

// editableGoals names the Goals among rows whose values current may set: those
// they own or are a Delegate on, or every one for an Admin (CONTEXT.md:
// Delegate).
func (s *Server) editableGoals(ctx context.Context, rows []goalRow, current domain.Account) (map[int64]bool, error) {
	delegated, err := s.svc.DelegatedGoals(ctx, current.ID)
	if err != nil {
		return nil, err
	}
	editable := map[int64]bool{}
	for _, g := range delegated {
		editable[g.ID] = true
	}
	for _, row := range rows {
		if current.IsAdmin || row.Goal.Owner.ID == current.ID {
			editable[row.Goal.ID] = true
		}
	}
	return editable, nil
}

// editURL is this table in edit mode, and viewURL this table out of it.
func (v goalsListData) editURL() templ.SafeURL {
	return v.queryURL(func(q url.Values) { q.Set("edit", "1") })
}

func (v goalsListData) viewURL() templ.SafeURL {
	return v.queryURL(func(q url.Values) { q.Del("edit") })
}

// saveURL is where edit mode's form posts: the table's own view, so a refused
// save comes back to it and a saved one lands on it.
func (v goalsListData) saveURL() templ.SafeURL {
	q := url.Values{}
	for k, vs := range v.Query {
		q[k] = slices.Clone(vs)
	}
	q.Del("edit")
	if len(q) == 0 {
		return "/goals/values"
	}
	return templ.SafeURL("/goals/values?" + q.Encode())
}

// A cell's inputs in edit mode are named for its Goal and its Dimension
// ("d.<goal>.<dimension>", the values picked, and "n.<goal>.<dimension>", a
// value typed into an Extendable list) or Field ("f.<goal>.<field>"). Each
// cell's "was." twin holds what it showed, so a save applies only the cells
// that changed and leaves the rest to whoever else is editing them.
const wasPrefix = "was."

// cellName is the name of col's input on goal's row.
func cellName(col tableColumn, goalID int64) string {
	if col.Field != nil {
		return cellNameOf(goalID, 0, col.Field.ID)
	}
	return cellNameOf(goalID, col.Dimension.ID, 0)
}

// cellNameOf is the name of the input for the Goal's value in a Dimension, or
// in a Field when dimensionID is 0.
func cellNameOf(goalID, dimensionID, fieldID int64) string {
	if dimensionID == 0 {
		return fmt.Sprintf("f.%d.%d", goalID, fieldID)
	}
	return fmt.Sprintf("d.%d.%d", goalID, dimensionID)
}

// newValueName is the name of the input typing a value into an Extendable
// Dimension's cell on goal's row.
func newValueName(goalID, dimensionID int64) string {
	return fmt.Sprintf("n.%d.%d", goalID, dimensionID)
}

// editsCell reports whether col's cell on row gets an input: in edit mode, a
// Dimension's or Field's cell on a row the person may edit.
func (t goalTable) editsCell(col tableColumn, row goalTableRow) bool {
	return t.Edit && t.Editable[row.Goal.ID] && (col.Dimension != nil || col.Field != nil)
}

// typed reports whether a refused save posted col's cell on row, so the cell
// shows what was typed rather than what is saved.
func (t goalTable) typed(col tableColumn, row goalTableRow) bool {
	return t.Typed != nil && t.Typed.Has(wasPrefix+cellName(col, row.Goal.ID))
}

// was is what col's cell on row shows as saved: a Dimension's value IDs
// comma-separated, or a Field's value.
func (t goalTable) was(col tableColumn, row goalTableRow) string {
	if t.typed(col, row) {
		return t.Typed.Get(wasPrefix + cellName(col, row.Goal.ID))
	}
	if col.Field != nil {
		return row.Fields[col.Field.ID]
	}
	return joinIDs(carriedIn(row, *col.Dimension))
}

// fieldInputValue is the value col's Field input on row holds.
func (t goalTable) fieldInputValue(col tableColumn, row goalTableRow) string {
	if t.typed(col, row) {
		return t.Typed.Get(cellName(col, row.Goal.ID))
	}
	return row.Fields[col.Field.ID]
}

// picks reports whether col's Dimension input on row has value picked.
func (t goalTable) picks(col tableColumn, row goalTableRow, value domain.DimensionValue) bool {
	if t.typed(col, row) {
		return slices.Contains(t.Typed[cellName(col, row.Goal.ID)], strconv.FormatInt(value.ID, 10))
	}
	return slices.Contains(carriedIn(row, *col.Dimension), value.ID)
}

// newValueTyped is the value typed into col's Extendable Dimension on row.
func (t goalTable) newValueTyped(col tableColumn, row goalTableRow) string {
	if t.Typed == nil {
		return ""
	}
	return t.Typed.Get(newValueName(row.Goal.ID, col.Dimension.ID))
}

// offers reports whether col's Dimension input on row offers value: every
// value still offered, and a Retired one only where the Goal carries it, so
// saving doesn't drop it (CONTEXT.md: Retired).
func (t goalTable) offers(col tableColumn, row goalTableRow, value domain.DimensionValue) bool {
	return !value.Retired || slices.Contains(carriedIn(row, *col.Dimension), value.ID)
}

// bad is why col's cell on row was refused, "" when it wasn't.
func (t goalTable) bad(col tableColumn, row goalTableRow) string {
	return t.Bad[cellName(col, row.Goal.ID)]
}

// carriedIn is the IDs of the values row's Goal carries in dim.
func carriedIn(row goalTableRow, dim domain.Dimension) []int64 {
	var ids []int64
	for _, v := range row.Values {
		if v.DimensionID == dim.ID {
			ids = append(ids, v.ID)
		}
	}
	return ids
}

// joinIDs is ids sorted and comma-separated, so two sets compare as text.
func joinIDs(ids []int64) string {
	sorted := slices.Sorted(slices.Values(ids))
	parts := make([]string, 0, len(sorted))
	for _, id := range sorted {
		parts = append(parts, strconv.FormatInt(id, 10))
	}
	return strings.Join(parts, ",")
}

// handleSaveGoalTable saves the Goal table's edit form (#80): every cell that
// changed from what it showed, by the Goal page's rules, all or nothing. A
// saved edit lands back on the table out of edit mode. A refused one comes
// back in edit mode as typed, each bad cell marked with why. An edit touching
// a Goal the person may not set values on is refused outright.
func (s *Server) handleSaveGoalTable(w http.ResponseWriter, r *http.Request, current domain.Account) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "invalid form", http.StatusBadRequest)
		return
	}
	edits, err := tableEdits(r.PostForm)
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	q := r.URL.Query()
	q.Set("layout", layoutTable)
	err = s.svc.EditGoalValues(r.Context(), current.ID, edits)
	switch {
	case err == nil:
		q.Del("edit")
		http.Redirect(w, r, string(goalsListData{Query: q}.queryURL(func(url.Values) {})), http.StatusSeeOther)
		return
	case errors.Is(err, domain.ErrNotAuthorized):
		http.Error(w, err.Error(), http.StatusForbidden)
		return
	case !errors.Is(err, domain.ErrValidation):
		s.serverError(w, r, "could not save values", err)
		return
	}

	q.Set("edit", "1")
	r.URL.RawQuery = q.Encode()
	view, verr := s.goalsListView(r, current)
	if verr != nil {
		s.serverError(w, r, "could not list goals", verr)
		return
	}
	view.Table.Typed = r.PostForm
	var refusal *domain.ValueEditError
	if errors.As(err, &refusal) {
		view.Table.Bad = map[string]string{}
		for _, c := range refusal.Cells {
			view.Table.Bad[cellNameOf(c.GoalID, c.DimensionID, c.FieldID)] = c.Message
		}
	} else {
		view.Table.Refusal = strings.TrimPrefix(err.Error(), domain.ErrValidation.Error()+": ")
	}
	render(w, r, http.StatusUnprocessableEntity, goalsPage(&current, view))
}

// tableCellRef is one cell of the Goal table's edit form, read off its inputs'
// names (see wasPrefix).
type tableCellRef struct {
	goalID      int64
	dimensionID int64
	fieldID     int64
}

// tableEdits reads the Goal table's edit form into the cells that changed:
// those whose input differs from its "was." twin, or that have no twin. They
// come ordered by Goal, then Dimensions before Fields.
func tableEdits(form url.Values) ([]domain.ValueEdit, error) {
	cells := map[tableCellRef]bool{}
	for key := range form {
		kind, rest, ok := strings.Cut(strings.TrimPrefix(key, wasPrefix), ".")
		if !ok || (kind != "d" && kind != "n" && kind != "f") {
			continue
		}
		rawGoal, rawAttr, ok := strings.Cut(rest, ".")
		goalID, gerr := strconv.ParseInt(rawGoal, 10, 64)
		attrID, aerr := strconv.ParseInt(rawAttr, 10, 64)
		if !ok || gerr != nil || aerr != nil || goalID <= 0 || attrID <= 0 {
			return nil, fmt.Errorf("invalid cell %q", key)
		}
		if kind == "f" {
			cells[tableCellRef{goalID: goalID, fieldID: attrID}] = true
		} else {
			cells[tableCellRef{goalID: goalID, dimensionID: attrID}] = true
		}
	}
	refs := slices.SortedFunc(maps.Keys(cells), func(a, b tableCellRef) int {
		return cmp.Or(cmp.Compare(a.goalID, b.goalID), cmp.Compare(a.fieldID, b.fieldID), cmp.Compare(a.dimensionID, b.dimensionID))
	})
	var edits []domain.ValueEdit
	for _, c := range refs {
		name := cellNameOf(c.goalID, c.dimensionID, c.fieldID)
		was, shown := form[wasPrefix+name]
		if c.fieldID != 0 {
			value := form.Get(name)
			if shown && strings.TrimSpace(value) == strings.TrimSpace(was[0]) {
				continue
			}
			edits = append(edits, domain.ValueEdit{GoalID: c.goalID, FieldID: c.fieldID, Value: value})
			continue
		}
		var ids []int64
		for _, raw := range form[name] {
			if raw == "" {
				continue
			}
			id, err := strconv.ParseInt(raw, 10, 64)
			if err != nil {
				return nil, fmt.Errorf("invalid value %q", raw)
			}
			ids = append(ids, id)
		}
		newValue := form.Get(newValueName(c.goalID, c.dimensionID))
		if shown && joinIDs(ids) == was[0] && strings.TrimSpace(newValue) == "" {
			continue
		}
		edits = append(edits, domain.ValueEdit{GoalID: c.goalID, DimensionID: c.dimensionID, ValueIDs: ids, NewValue: newValue})
	}
	return edits, nil
}
