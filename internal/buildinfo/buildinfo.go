// Package buildinfo names the build of Goal Tracker that is running: its
// version, the commit it was built from and when that commit was made, and
// whether the tree had uncommitted changes. Go records all of it in the binary
// (runtime/debug.ReadBuildInfo) when it builds from a checkout; a Docker build
// has no checkout, so it is given its version at link time instead (Version).
package buildinfo

import (
	"runtime/debug"
	"strings"
	"time"

	"golang.org/x/mod/module"
)

// Version, when set at link time, is the version shown, as given apart from
// its build metadata, in place of the one Go records:
//
//	go build -ldflags "-X github.com/zachthieme/goal-tracker/internal/buildinfo.Version=v0.3.0"
//
// The Dockerfile sets it from its VERSION build argument.
var Version string

// unknown is the version of a build that names none.
const unknown = "unknown"

// shortCommit is how many characters of a commit hash name it.
const shortCommit = 8

// Info is the build that is running.
type Info struct {
	// Version is the release tag (v0.3.0), a pseudo-version past one, or
	// "unknown".
	Version string
	// Commit is the commit built from, short; "" when unknown.
	Commit string
	// CommitTime is when that commit was made, not when the binary was built;
	// zero when unknown.
	CommitTime time.Time
	// Modified is whether the tree built from had uncommitted changes.
	Modified bool
	// LinkTime is whether Version was set at link time (Version, the package
	// variable) rather than recorded by Go.
	LinkTime bool
}

// Read is the running binary's Info, with Version overriding the version Go
// recorded when it is set.
func Read() Info {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		bi = nil
	}
	return From(bi, Version)
}

// From is the Info bi records, nil when there is none, with override as its
// version when it isn't empty. A version Go couldn't name, "(devel)", reads
// as "unknown".
func From(bi *debug.BuildInfo, override string) Info {
	info := Info{Version: override, LinkTime: override != ""}
	if bi == nil {
		if info.Version == "" {
			info.Version = unknown
		}
		return info
	}
	if info.Version == "" {
		info.Version = bi.Main.Version
	}
	if info.Version == "" || info.Version == "(devel)" {
		info.Version = unknown
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			info.Commit = s.Value[:min(len(s.Value), shortCommit)]
		case "vcs.time":
			if t, err := time.Parse(time.RFC3339, s.Value); err == nil {
				info.CommitTime = t
			}
		case "vcs.modified":
			info.Modified = s.Value == "true"
		}
	}
	return info
}

// Line says info as the Admin page shows it, with day saying the commit time:
// "Version v0.3.0 · commit 478f0655 · committed Oct 31", leaving out what
// isn't known and ending "· uncommitted changes" when the tree was modified.
// A version Go recorded is shortened (shownVersion), since the rest of the
// line already says what a pseudo-version and its build metadata would; a
// link-time one only drops its build metadata.
func (info Info) Line(day func(time.Time) string) string {
	parts := []string{"Version " + shownVersion(info.Version, info.LinkTime)}
	if info.Commit != "" {
		parts = append(parts, "commit "+info.Commit)
	}
	if !info.CommitTime.IsZero() {
		parts = append(parts, "committed "+day(info.CommitTime))
	}
	if info.Modified {
		parts = append(parts, "uncommitted changes")
	}
	return strings.Join(parts, " · ")
}

// untagged is how Line shows a pseudo-version with no release before it.
const untagged = "untagged"

// shownVersion is version as Line shows it: without build metadata ("+dirty"),
// and, unless it was set at link time (linkTime), a pseudo-version as the
// release it follows with a "+" ("v0.3.0+"), or "untagged" when no release
// came before it. A link-time version is otherwise shown as given: a Docker
// build names no commit, so its version may be the only one the line names.
func shownVersion(version string, linkTime bool) string {
	version, _, _ = strings.Cut(version, "+")
	if !linkTime && module.IsPseudoVersion(version) {
		if base, err := module.PseudoVersionBase(version); err == nil {
			if base == "" {
				return untagged
			}
			return base + "+"
		}
	}
	if version == "" {
		return unknown
	}
	return version
}
