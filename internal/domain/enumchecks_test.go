package domain_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// enumCheckColumns are the enum columns the schema CHECKs, each with the
// prefix naming the domain's constants for its values (less any longer prefix
// that names another group), and whether the domain also writes an empty value
// to it.
var enumCheckColumns = []struct {
	table, column, prefix string
	except                []string
	empty                 bool
}{
	{"goals", "lifecycle", "Lifecycle", nil, false},
	{"goals", "kind", "Goal", nil, true},
	{"checkins", "health", "Health", nil, true},
	{"checkins", "lifecycle_from", "Lifecycle", nil, true},
	{"checkins", "lifecycle_to", "Lifecycle", nil, true},
	{"milestones", "status", "Milestone", []string{"MilestoneChange", "MilestoneMark"}, false},
	{"milestone_changes", "kind", "MilestoneChange", nil, false},
	{"links", "status", "Link", []string{"LinkEvent"}, false},
	{"link_events", "kind", "LinkEvent", nil, false},
	{"parent_suggestions", "status", "Suggestion", nil, false},
	{"handoffs", "status", "Handoff", nil, false},
	{"report_definitions", "mode", "ReportMode", nil, false},
	{"report_definition_goals", "list", "reportList", nil, false},
	{"report_rules", "attribute", "Rule", []string{"RuleIs"}, false},
	{"report_rules", "op", "RuleIs", nil, false},
	{"narrative_picks", "section", "Highlight", nil, false},
	{"narrative_texts", "section", "Highlight", nil, false},
	{"metrics", "direction", "Metric", nil, false},
	{"highlights", "kind", "Highlight", nil, false},
	{"draft_highlights", "kind", "Highlight", nil, true},
	{"undo_tokens", "kind", "undo", nil, false},
}

// flagCheckColumns are the 0/1 flag columns the schema CHECKs.
var flagCheckColumns = []struct{ table, column string }{
	{"accounts", "is_admin"},
	{"accounts", "departed"},
	{"goals", "top_level"},
	{"milestones", "added_while_active"},
	{"dimensions", "retired"},
	{"dimensions", "required"},
	{"dimension_values", "retired"},
	{"fields", "retired"},
	{"fields", "required"},
	{"checkins", "asks_for_help"},
	{"date_slips", "back_to_green"},
	{"goal_value_changes", "several"},
}

// Every value the domain names for an enum column is one the column's CHECK
// accepts, the empty value too where the domain writes it, and a value outside
// them is refused by a CHECK named for the table and column. The values are
// read from the domain's own string constants by name, so a new Lifecycle,
// status or kind fails here until the schema's CHECK takes it.
func TestEnumColumnsAcceptEveryDomainValueAndNoOther(t *testing.T) {
	t.Parallel()
	consts := domainStringConsts(t)
	for _, col := range enumCheckColumns {
		t.Run(col.table+"."+col.column, func(t *testing.T) {
			t.Parallel()
			values := constValues(consts, col.prefix, col.except)
			if len(values) == 0 {
				t.Fatalf("no domain constant is named %s...", col.prefix)
			}
			if col.empty {
				values = append(values, "")
			}
			h := testsupport.New(t)
			testsupport.ArrangeEnumCheckRows(t, h.DB)
			update := `UPDATE ` + col.table + ` SET ` + col.column + ` = ? WHERE id = 1`
			for _, v := range values {
				if _, err := h.DB.Exec(update, v); err != nil {
					t.Errorf("%s.%s refused the domain's %q: %v", col.table, col.column, v, err)
				}
			}
			outside := []string{"Unknown"}
			if !col.empty {
				outside = append(outside, "")
			}
			for _, v := range outside {
				assertCheckRefuses(t, h, update, v, col.table, col.column)
			}
		})
	}
}

// Each 0/1 flag column takes 0 and 1 and refuses 2 with a CHECK named for the
// table and column.
func TestFlagColumnsAcceptOnlyZeroAndOne(t *testing.T) {
	t.Parallel()
	for _, col := range flagCheckColumns {
		t.Run(col.table+"."+col.column, func(t *testing.T) {
			t.Parallel()
			h := testsupport.New(t)
			testsupport.ArrangeEnumCheckRows(t, h.DB)
			update := `UPDATE ` + col.table + ` SET ` + col.column + ` = ? WHERE id = 1`
			for _, v := range []int{0, 1} {
				if _, err := h.DB.Exec(update, v); err != nil {
					t.Errorf("%s.%s refused %d: %v", col.table, col.column, v, err)
				}
			}
			assertCheckRefuses(t, h, update, 2, col.table, col.column)
		})
	}
}

// assertCheckRefuses fails unless update, given v, is refused with an error
// naming table and column.
func assertCheckRefuses(t *testing.T, h *testsupport.Harness, update string, v any, table, column string) {
	t.Helper()
	_, err := h.DB.Exec(update, v)
	if err == nil {
		t.Errorf("%s.%s took %v", table, column, v)
		return
	}
	if name := table + "_" + column + "_valid"; !strings.Contains(err.Error(), name) {
		t.Errorf("%s.%s refused %v with %q, which doesn't name %s", table, column, v, err, name)
	}
}

// domainStringConsts reads the domain package's source and returns the value
// of each string constant by name, resolving one constant defined as another.
func domainStringConsts(t *testing.T) map[string]string {
	t.Helper()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the domain package: %v", err)
	}
	exprs := map[string]ast.Expr{}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		for _, decl := range file.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, ident := range vs.Names {
					if i < len(vs.Values) {
						exprs[ident.Name] = vs.Values[i]
					}
				}
			}
		}
	}
	var value func(name string) (string, bool)
	value = func(name string) (string, bool) {
		switch e := exprs[name].(type) {
		case *ast.BasicLit:
			if e.Kind != token.STRING {
				return "", false
			}
			s, err := strconv.Unquote(e.Value)
			return s, err == nil
		case *ast.Ident:
			return value(e.Name)
		}
		return "", false
	}
	out := map[string]string{}
	for name := range exprs {
		if v, ok := value(name); ok {
			out[name] = v
		}
	}
	return out
}

// constValues returns the distinct values of the constants in consts named
// prefix, alone or followed by an upper-case letter, leaving out those named by
// any of except, in order.
func constValues(consts map[string]string, prefix string, except []string) []string {
	var values []string
	for name, v := range consts {
		rest, ok := strings.CutPrefix(name, prefix)
		if !ok || rest != "" && !unicode.IsUpper(rune(rest[0])) {
			continue
		}
		if slices.ContainsFunc(except, func(e string) bool { return strings.HasPrefix(name, e) }) {
			continue
		}
		if !slices.Contains(values, v) {
			values = append(values, v)
		}
	}
	slices.Sort(values)
	return values
}
