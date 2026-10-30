package domain

import (
	"context"
	"log/slog"
	"path/filepath"
	"reflect"
	"slices"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/clock"
	"github.com/zachthieme/goal-tracker/internal/db"
	"github.com/zachthieme/goal-tracker/internal/email"
)

// withinTxOwnFields are the Service fields a transaction WithinTx opens has
// for itself; every other field is shared with the Service that opened it.
var withinTxOwnFields = []string{"queries", "inTx", "queuedEmail"}

// withinTxCopyParent is a Service with every field set but the transaction's
// own, built only through NewService and its options, and an import.
func withinTxCopyParent(t *testing.T) *Service {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "goals.db"))
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Migrate(context.Background(), sqlDB); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	s := NewService(sqlDB, clock.NewFixed(time.Date(2026, 3, 2, 9, 0, 0, 0, time.UTC)), email.NewRecorder(),
		[]string{"admin@example.com"},
		WithTimezone(time.FixedZone("UTC-5", -5*60*60)),
		WithBaseURL("https://goals.example.com"),
		WithEmailDeliveredOnCommit(),
		WithEmailOutboxLogger(slog.New(slog.DiscardHandler)),
	)
	s.goalImport = &goalImport{adminID: 1, created: map[int64]bool{}}
	return s
}

// zeroServiceFields names every field of s that is zero, but those in except.
func zeroServiceFields(s Service, except ...string) []string {
	v := reflect.ValueOf(s)
	var zero []string
	for i := range v.NumField() {
		name := v.Type().Field(i).Name
		if !slices.Contains(except, name) && v.Field(i).IsZero() {
			zero = append(zero, name)
		}
	}
	return zero
}

// serviceFieldDiffs names every field but the transaction's own on which a
// and b aren't deeply equal.
func serviceFieldDiffs(a, b Service) []string {
	av, bv := reflect.ValueOf(&a).Elem(), reflect.ValueOf(&b).Elem()
	var diffs []string
	for i := range av.NumField() {
		name := av.Type().Field(i).Name
		if slices.Contains(withinTxOwnFields, name) {
			continue
		}
		if !reflect.DeepEqual(unexportedField(av.Field(i)), unexportedField(bv.Field(i))) {
			diffs = append(diffs, name)
		}
	}
	return diffs
}

// unexportedField is the value of f, an addressable field, though it is
// unexported and Interface would panic on it.
func unexportedField(f reflect.Value) any {
	return reflect.NewAt(f.Type(), f.Addr().UnsafePointer()).Elem().Interface()
}

// serviceFieldsNotShared names every field but the transaction's own that
// refers to something different in a and b, nil included: a pointer, map or
// chan, or the pointer or map behind an interface. Struct fields are left to
// serviceFieldDiffs.
func serviceFieldsNotShared(a, b Service) []string {
	av, bv := reflect.ValueOf(a), reflect.ValueOf(b)
	var notShared []string
	for i := range av.NumField() {
		name := av.Type().Field(i).Name
		if slices.Contains(withinTxOwnFields, name) {
			continue
		}
		af, bf := av.Field(i), bv.Field(i)
		switch af.Kind() {
		case reflect.Pointer, reflect.Map, reflect.Chan:
			if af.Pointer() != bf.Pointer() {
				notShared = append(notShared, name)
			}
		case reflect.Interface:
			if af.IsNil() != bf.IsNil() {
				notShared = append(notShared, name)
				continue
			}
			if af.IsNil() {
				continue
			}
			ae, be := af.Elem(), bf.Elem()
			if isReferenceKind(ae.Kind()) && isReferenceKind(be.Kind()) && ae.Pointer() != be.Pointer() {
				notShared = append(notShared, name)
			}
		}
	}
	return notShared
}

// isReferenceKind says whether a value of kind k refers to what it holds by a
// pointer that Value.Pointer reads.
func isReferenceKind(k reflect.Kind) bool {
	return k == reflect.Pointer || k == reflect.Map
}

// A Service bound to a transaction is its parent but for the transaction's
// own fields: every other field holds the same value, and a pointer, map or
// interface holds the very same thing, so a field added to Service can't
// reset to zero inside a transaction.
func TestWithinTxBindsACopyOfTheWholeService(t *testing.T) {
	t.Parallel()
	parent := withinTxCopyParent(t)
	if zero := zeroServiceFields(*parent, withinTxOwnFields...); len(zero) > 0 {
		t.Fatalf("the parent leaves %v zero; set them through NewService's options so the copy is checked on them", zero)
	}

	var tx Service
	if err := parent.WithinTx(context.Background(), func(txSvc *Service) error {
		tx = *txSvc
		return nil
	}); err != nil {
		t.Fatalf("WithinTx: %v", err)
	}

	if diffs := serviceFieldDiffs(*parent, tx); len(diffs) > 0 {
		t.Errorf("the transaction's Service differs from its parent on %v", diffs)
	}
	if notShared := serviceFieldsNotShared(*parent, tx); len(notShared) > 0 {
		t.Errorf("the transaction's Service doesn't share %v with its parent", notShared)
	}
	if !tx.inTx {
		t.Error("the transaction's Service isn't marked as in a transaction")
	}
	if tx.queuedEmail == nil || tx.queuedEmail == parent.queuedEmail {
		t.Errorf("the transaction's queuedEmail is %p, want one of its own (the parent's is %p)", tx.queuedEmail, parent.queuedEmail)
	}
	if tx.queries == parent.queries {
		t.Error("the transaction's Service queries through its parent's Queries, not the transaction")
	}
}

// zeroServiceFields names every field of an empty Service but those it is
// told to skip.
func TestZeroServiceFieldsNamesEveryZeroFieldButTheExceptions(t *testing.T) {
	t.Parallel()
	got := zeroServiceFields(Service{}, withinTxOwnFields...)
	var want []string
	for _, f := range reflect.VisibleFields(reflect.TypeOf(Service{})) {
		if !slices.Contains(withinTxOwnFields, f.Name) {
			want = append(want, f.Name)
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("zeroServiceFields(Service{}) = %v, want %v", got, want)
	}
	for _, own := range withinTxOwnFields {
		if slices.Contains(got, own) {
			t.Errorf("zeroServiceFields named %s, which it was told to skip", own)
		}
	}
}

// A copy of the parent that has lost a field is caught by both comparisons,
// on that field alone; one that differs only on the transaction's own fields
// is caught by neither.
func TestServiceComparisonsNameOnlyTheFieldThatDiffers(t *testing.T) {
	t.Parallel()
	parent := withinTxCopyParent(t)

	lost := *parent
	lost.dbFile = nil
	if got := serviceFieldDiffs(*parent, lost); !slices.Equal(got, []string{"dbFile"}) {
		t.Errorf("serviceFieldDiffs with dbFile lost = %v, want [dbFile]", got)
	}
	if got := serviceFieldsNotShared(*parent, lost); !slices.Equal(got, []string{"dbFile"}) {
		t.Errorf("serviceFieldsNotShared with dbFile lost = %v, want [dbFile]", got)
	}

	ownDiffer := *parent
	ownDiffer.queries = parent.queries.WithTx(nil)
	ownDiffer.inTx = true
	ownDiffer.queuedEmail = new(bool)
	if got := serviceFieldDiffs(*parent, ownDiffer); len(got) > 0 {
		t.Errorf("serviceFieldDiffs on the transaction's own fields = %v, want none", got)
	}
	if got := serviceFieldsNotShared(*parent, ownDiffer); len(got) > 0 {
		t.Errorf("serviceFieldsNotShared on the transaction's own fields = %v, want none", got)
	}
}
