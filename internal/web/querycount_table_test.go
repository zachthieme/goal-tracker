package web_test

import (
	"testing"

	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// Showing the Goal list's table layout, out of edit mode and in it, runs as
// many queries for 50 Goals as for 10: its rows' Field values are read in one
// query, not once per Goal (#448).
func TestGoalTableQueriesDontGrowWithGoals(t *testing.T) {
	t.Parallel()
	type tableCounts struct{ view, edit int64 }
	countFor := func(n int) tableCounts {
		h := testsupport.New(t, queryCountAdmin)
		seedQueryCountFixture(t, h, n)
		ts, queries := newQueryCountingServer(t, h)
		client := signInClient(t, ts.URL, queryCountAdmin)
		var c tableCounts
		queries.Store(0)
		getBody(t, client, ts.URL+"/goals?org=all&layout=table")
		c.view = queries.Swap(0)
		getBody(t, client, ts.URL+"/goals?org=all&layout=table&edit=1")
		c.edit = queries.Swap(0)
		return c
	}
	ten, fifty := countFor(10), countFor(50)
	if ten.view != fifty.view {
		t.Errorf("the Goal table ran %d queries for 10 Goals and %d for 50; want the same", ten.view, fifty.view)
	}
	if ten.edit != fifty.edit {
		t.Errorf("the Goal table in edit mode ran %d queries for 10 Goals and %d for 50; want the same", ten.edit, fifty.edit)
	}
}
