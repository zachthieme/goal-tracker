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

	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// enumCheckColumns are the enum columns the schema CHECKs, each with the
// domain constant whose const block names its values, and whether the domain
// also writes an empty value to it.
var enumCheckColumns = []struct {
	table, column, anchor string
	empty                 bool
}{
	{"goals", "lifecycle", "LifecycleProposed", false},
	{"goals", "kind", "GoalDated", true},
	{"checkins", "health", "HealthGreen", true},
	{"checkins", "lifecycle_from", "LifecycleProposed", true},
	{"checkins", "lifecycle_to", "LifecycleProposed", true},
	{"milestones", "status", "MilestonePlanned", false},
	{"milestone_changes", "kind", "MilestoneChangeAdded", false},
	{"links", "status", "LinkPending", false},
	{"link_events", "kind", "LinkEventRequested", false},
	{"parent_suggestions", "status", "SuggestionOpen", false},
	{"handoffs", "status", "HandoffPending", false},
	{"report_definitions", "mode", "ReportModeRules", false},
	{"report_definition_goals", "list", "reportListPicked", false},
	{"report_rules", "attribute", "RuleDimension", false},
	{"report_rules", "op", "RuleIs", false},
	{"narrative_picks", "section", "HighlightInsight", false},
	{"narrative_texts", "section", "HighlightInsight", false},
	{"metrics", "direction", "MetricUp", false},
	{"highlights", "kind", "HighlightInsight", false},
	{"draft_highlights", "kind", "HighlightInsight", true},
	{"undo_tokens", "kind", "undoLinkRemoval", false},
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
// read from the domain's own const blocks, so a new Lifecycle, status or kind
// fails here until the schema's CHECK takes it.
func TestEnumColumnsAcceptEveryDomainValueAndNoOther(t *testing.T) {
	t.Parallel()
	blocks := domainConstBlocks(t)
	for _, col := range enumCheckColumns {
		t.Run(col.table+"."+col.column, func(t *testing.T) {
			t.Parallel()
			values, ok := blocks[col.anchor]
			if !ok {
				t.Fatalf("no domain const %s", col.anchor)
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

// domainConstBlocks reads the domain package's source and returns, for each
// constant, the string values of every constant declared in the same const
// block, resolving one constant defined as another.
func domainConstBlocks(t *testing.T) map[string][]string {
	t.Helper()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read the domain package: %v", err)
	}
	exprs := map[string]ast.Expr{}
	var blocks [][]string
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
			var names []string
			for _, spec := range gen.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, ident := range vs.Names {
					if i < len(vs.Values) {
						exprs[ident.Name] = vs.Values[i]
					}
					names = append(names, ident.Name)
				}
			}
			blocks = append(blocks, names)
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
	out := map[string][]string{}
	for _, names := range blocks {
		var values []string
		for _, name := range names {
			if v, ok := value(name); ok && !slices.Contains(values, v) {
				values = append(values, v)
			}
		}
		for _, name := range names {
			out[name] = values
		}
	}
	return out
}
