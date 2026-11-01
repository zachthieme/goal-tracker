package buildinfo_test

import (
	"runtime/debug"
	"testing"
	"time"

	"github.com/zachthieme/goal-tracker/internal/buildinfo"
)

// day shows a commit time as the tests expect it: the date it fell on, in UTC.
func day(t time.Time) string { return t.UTC().Format("Jan 2 2006") }

// built is the build info Go records for a binary built from the checkout at
// version, with the vcs settings given.
func built(version string, settings ...debug.BuildSetting) *debug.BuildInfo {
	return &debug.BuildInfo{Main: debug.Module{Path: "github.com/zachthieme/goal-tracker", Version: version}, Settings: settings}
}

// A build from a tagged commit names its tag, its commit short, and when the
// commit was made.
func TestATaggedBuildNamesItsTagCommitAndCommitTime(t *testing.T) {
	t.Parallel()

	info := buildinfo.From(built("v0.3.0",
		debug.BuildSetting{Key: "vcs", Value: "git"},
		debug.BuildSetting{Key: "vcs.revision", Value: "478f0655bc83a1e2d3c4b5a6978877665544332211"},
		debug.BuildSetting{Key: "vcs.time", Value: "2026-10-31T23:06:28Z"},
		debug.BuildSetting{Key: "vcs.modified", Value: "false"},
	), "")

	want := buildinfo.Info{Version: "v0.3.0", Commit: "478f0655", CommitTime: time.Date(2026, 10, 31, 23, 6, 28, 0, time.UTC)}
	if info != want {
		t.Errorf("From = %+v, want %+v", info, want)
	}
	if got, want := info.Line(day), "Version v0.3.0 · commit 478f0655 · committed Oct 31 2026"; got != want {
		t.Errorf("Line = %q, want %q", got, want)
	}
}

// A build past a tag, from a tree with uncommitted changes, names its
// pseudo-version and says the tree was changed.
func TestABuildWithUncommittedChangesSaysSo(t *testing.T) {
	t.Parallel()

	info := buildinfo.From(built("v0.3.1-0.20261031230628-478f0655bc83+dirty",
		debug.BuildSetting{Key: "vcs.revision", Value: "478f0655bc83a1e2d3c4b5a6978877665544332211"},
		debug.BuildSetting{Key: "vcs.time", Value: "2026-10-31T23:06:28Z"},
		debug.BuildSetting{Key: "vcs.modified", Value: "true"},
	), "")

	if !info.Modified {
		t.Errorf("From = %+v, want Modified", info)
	}
	want := "Version v0.3.1-0.20261031230628-478f0655bc83+dirty · commit 478f0655 · committed Oct 31 2026 · uncommitted changes"
	if got := info.Line(day); got != want {
		t.Errorf("Line = %q, want %q", got, want)
	}
}

// A build Go couldn't name, as a Docker build without a VERSION is, reads as
// "unknown", with no commit or commit time; so does a binary with no build
// info at all.
func TestADevelBuildIsUnknown(t *testing.T) {
	t.Parallel()

	for name, bi := range map[string]*debug.BuildInfo{"(devel)": built("(devel)"), "no build info": nil} {
		info := buildinfo.From(bi, "")
		if want := (buildinfo.Info{Version: "unknown"}); info != want {
			t.Errorf("%s: From = %+v, want %+v", name, info, want)
		}
		if got, want := info.Line(day), "Version unknown"; got != want {
			t.Errorf("%s: Line = %q, want %q", name, got, want)
		}
	}
}

// A version set at link time (-ldflags "-X …Version=") is the one shown, in
// place of the one Go recorded.
func TestALinkTimeVersionOverridesTheRecordedOne(t *testing.T) {
	t.Parallel()

	for name, bi := range map[string]*debug.BuildInfo{"(devel)": built("(devel)"), "tagged": built("v0.3.0"), "no build info": nil} {
		if got := buildinfo.From(bi, "v9.9.9").Version; got != "v9.9.9" {
			t.Errorf("%s: Version = %q, want the override v9.9.9", name, got)
		}
	}
}
