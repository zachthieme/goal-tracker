package testsupport_test

import (
	"context"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/zachthieme/goal-tracker/internal/domain"
	"github.com/zachthieme/goal-tracker/internal/testsupport"
)

// SetManager with a zero manager syncs the person with no Manager email, so
// they have none.
func TestSetManagerWithAZeroManagerClearsTheManager(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t)
	person := h.SignIn("dev@example.com")
	boss := h.SignIn("boss@example.com")
	h.SetManager(person, boss)

	h.SetManager(person, domain.Account{})

	got, err := h.Service.Account(context.Background(), person.ID)
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if got.ManagerID != nil {
		t.Errorf("ManagerID = %d, want none", *got.ManagerID)
	}
}

// SetTimezone keeps the person's Manager and SetManager keeps their Timezone,
// even given the person as they were before the other was set, and neither
// changes their Name, Departed or Admin.
func TestSetManagerAndSetTimezoneKeepWhatTheyDoNotSet(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t, "admin@example.com", "dev@example.com")
	admin := h.SignIn("admin@example.com")
	stale := h.SignInNamed("dev@example.com", "Dev Patel")
	boss := h.SignIn("boss@example.com")
	other := h.SignIn("other@example.com")
	h.MarkDeparted(admin, stale)

	h.SetManager(stale, boss)
	h.SetTimezone(stale, "Europe/London")
	h.SetManager(stale, other)

	got, err := h.Service.Account(context.Background(), stale.ID)
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if got.ManagerID == nil || *got.ManagerID != other.ID {
		t.Errorf("ManagerID = %v, want %d (other@example.com)", got.ManagerID, other.ID)
	}
	if got.Timezone != "Europe/London" {
		t.Errorf("Timezone = %q, want Europe/London", got.Timezone)
	}
	if got.Name != "Dev Patel" || !got.Departed || !got.IsAdmin {
		t.Errorf("Name, Departed, IsAdmin = %q, %t, %t; want Dev Patel, true, true", got.Name, got.Departed, got.IsAdmin)
	}

	h.SetTimezone(stale, "")

	got, err = h.Service.Account(context.Background(), stale.ID)
	if err != nil {
		t.Fatalf("Account: %v", err)
	}
	if got.Timezone != "" || got.ManagerID == nil || *got.ManagerID != other.ID {
		t.Errorf("after SetTimezone(\"\"): Timezone, ManagerID = %q, %v; want none, %d", got.Timezone, got.ManagerID, other.ID)
	}
}

// SetManager closing a Manager cycle, and SetTimezone given a Timezone that
// isn't an IANA name, each fail the test, naming the cycle's people or the
// value. Harness.T is a *testing.T, so each runs in a child test binary.
func TestSetManagerAndSetTimezoneFailOnWhatTheSyncRefuses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		build func(h *testsupport.Harness)
		want  []string
	}{
		{"SetManager closing a cycle", func(h *testsupport.Harness) {
			a := h.SignIn("a@example.com")
			b := h.SignIn("b@example.com")
			h.SetManager(a, b)
			h.SetManager(b, a)
		}, []string{"SetManager(b@example.com, a@example.com)", "Manager cycle", "a@example.com", "b@example.com"}},
		{"SetTimezone not an IANA name", func(h *testsupport.Harness) {
			h.SetTimezone(h.SignIn("dev@example.com"), "Not/AZone")
		}, []string{"dev@example.com", `"Not/AZone"`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if os.Getenv("SYNC_REFUSAL_CHILD") == "1" {
				tc.build(testsupport.New(t))
				return
			}
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^"+t.Name()+"$", "-test.count=1")
			cmd.Env = append(os.Environ(), "SYNC_REFUSAL_CHILD=1")
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("%s passed, want it to fail the test:\n%s", tc.name, out)
			}
			for _, want := range tc.want {
				if !strings.Contains(string(out), want) {
					t.Errorf("%s failed without %q:\n%s", tc.name, want, out)
				}
			}
		})
	}
}

// SyncDirectory stores the Manager cycle the directory gives, self-references
// included, and returns it rather than failing the test; a later sync returns
// it again.
func TestSyncDirectoryStoresAndReturnsTheCyclesItGives(t *testing.T) {
	t.Parallel()
	h := testsupport.New(t)
	h.SignIn("a@example.com")

	res := h.SyncDirectory(
		domain.DirectoryPerson{Email: "B@example.com", ManagerEmail: "A@example.com"},
		domain.DirectoryPerson{Email: "a@example.com", ManagerEmail: "b@example.com"},
		domain.DirectoryPerson{Email: "self@example.com", ManagerEmail: "self@example.com"},
	)

	want := [][]string{{"a@example.com", "b@example.com"}, {"self@example.com"}}
	if got := testsupport.CycleEmails(res.Cycles); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("Cycles = %q, want %q", got, want)
	}
	res = h.SyncDirectory(domain.DirectoryPerson{Email: "c@example.com"})
	if got := testsupport.CycleEmails(res.Cycles); !slices.EqualFunc(got, want, slices.Equal) {
		t.Errorf("a later sync's Cycles = %q, want %q", got, want)
	}
}
