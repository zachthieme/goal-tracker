package testsupport

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"slices"
	"testing"
	"testing/fstest"
	"time"
)

// templateTestMigrations is a stand-in for the embedded migrations, so a test
// can change one's contents.
func templateTestMigrations(body string) fstest.MapFS {
	return fstest.MapFS{
		"migrations/0001_accounts.sql": {Data: []byte("CREATE TABLE accounts (id INTEGER PRIMARY KEY);")},
		"migrations/0002_goals.sql":    {Data: []byte(body)},
	}
}

// templateTestBuilder is a stand-in for the Go source that builds the
// template, so a test can change one file's contents.
func templateTestBuilder(migrate string) map[string][]byte {
	return map[string][]byte{
		"internal/db/migrate.go":           []byte(migrate),
		"internal/db/open.go":              []byte("package db // opens it"),
		"internal/testsupport/template.go": []byte("package testsupport // builds it"),
	}
}

// templateTestName is the cache name of the template built from fake inputs,
// failing the test if it can't be worked out.
func templateTestName(t *testing.T, migrations fs.FS, builder map[string][]byte, sqliteVersion string) string {
	t.Helper()
	name, err := templateCacheName(migrations, builder, sqliteVersion)
	if err != nil {
		t.Fatalf("templateCacheName: %v", err)
	}
	return name
}

// A changed migration gives the template another cache name, so no test
// binary copies a template built from the old one; the same migrations give
// the same name, so every binary finds the one already built.
func TestTemplateCacheNameFollowsTheMigrationsContents(t *testing.T) {
	t.Parallel()
	builder := templateTestBuilder("package db // migrates")
	before := templateTestName(t, templateTestMigrations("CREATE TABLE goals (id INTEGER PRIMARY KEY);"), builder, "v1.61.0")
	again := templateTestName(t, templateTestMigrations("CREATE TABLE goals (id INTEGER PRIMARY KEY);"), builder, "v1.61.0")
	changed := templateTestName(t, templateTestMigrations("CREATE TABLE goals (id INTEGER PRIMARY KEY, title TEXT);"), builder, "v1.61.0")

	if again != before {
		t.Errorf("the same migrations are cached as %q and %q", before, again)
	}
	if changed == before {
		t.Errorf("a changed migration is still cached as %q", before)
	}
}

// A change to the Go code that applies the migrations, or to the SQLite it
// runs them on, gives the template another cache name, so no test binary
// copies a template the old code built.
func TestTemplateCacheNameFollowsTheCodeThatBuildsIt(t *testing.T) {
	t.Parallel()
	migrations := templateTestMigrations("CREATE TABLE goals (id INTEGER PRIMARY KEY);")
	before := templateTestName(t, migrations, templateTestBuilder("package db // migrates"), "v1.61.0")

	if again := templateTestName(t, migrations, templateTestBuilder("package db // migrates"), "v1.61.0"); again != before {
		t.Errorf("the same inputs are cached as %q and %q", before, again)
	}
	if changed := templateTestName(t, migrations, templateTestBuilder("package db // migrates differently"), "v1.61.0"); changed == before {
		t.Errorf("changed migration code is still cached as %q", before)
	}
	if upgraded := templateTestName(t, migrations, templateTestBuilder("package db // migrates"), "v1.62.0"); upgraded == before {
		t.Errorf("another modernc.org/sqlite version is still cached as %q", before)
	}
}

// templateBuilds is a template build that writes contents and counts how often
// it ran.
type templateBuilds struct {
	contents []byte
	count    int
}

func (b *templateBuilds) build(path string) error {
	b.count++
	return os.WriteFile(path, b.contents, 0o600)
}

// A test binary that finds the template another has built copies it rather
// than migrating its own.
func TestCachedTemplateIsReusedRatherThanRebuilt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	first := &templateBuilds{contents: []byte("the first build")}
	second := &templateBuilds{contents: []byte("a second build")}

	built, err := cachedTemplate(dir, "template-0000000000000001.db", first.build)
	if err != nil {
		t.Fatalf("first cachedTemplate: %v", err)
	}
	reused, err := cachedTemplate(dir, "template-0000000000000001.db", second.build)
	if err != nil {
		t.Fatalf("second cachedTemplate: %v", err)
	}

	if first.count != 1 || !bytes.Equal(built, first.contents) {
		t.Errorf("first binary: built %d times, got %q; want one build, %q", first.count, built, first.contents)
	}
	if second.count != 0 || !bytes.Equal(reused, first.contents) {
		t.Errorf("second binary: built %d times, got %q; want no build, the first's %q", second.count, reused, first.contents)
	}
}

// templateTestFile writes a file called name in dir, last modified at
// modified.
func templateTestFile(t *testing.T, dir, name string, modified time.Time) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(name), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	if err := os.Chtimes(path, modified, modified); err != nil {
		t.Fatalf("set %s's modification time: %v", path, err)
	}
	return path
}

// Building a template deletes the other templates no binary has read for a
// day, which the migrations or the code that builds them has left behind, so
// a run leaves few templates behind however often they changed. It keeps one
// read or written lately, so another checkout running its tests at the same
// time, from other migrations, keeps sharing its own.
func TestBuildingATemplateDeletesOnlyOthersUnusedForADay(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	now := time.Now()
	templateTestFile(t, dir, "template-00000000000000a1.db", now.Add(-25*time.Hour))
	templateTestFile(t, dir, "template-00000000000000a2.db", now.Add(-time.Second))
	templateTestFile(t, dir, "template-00000000000000a3.db", now.Add(-23*time.Hour))
	templateTestFile(t, dir, "notes.txt", now.Add(-48*time.Hour))

	current := &templateBuilds{contents: []byte("built after the change")}
	if _, err := cachedTemplate(dir, "template-00000000000000b0.db", current.build); err != nil {
		t.Fatalf("cachedTemplate: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var left []string
	for _, entry := range entries {
		left = append(left, entry.Name())
	}
	want := []string{"notes.txt", "template-00000000000000a2.db", "template-00000000000000a3.db", "template-00000000000000b0.db"}
	if !slices.Equal(left, want) {
		t.Errorf("left in the cache: %v, want %v", left, want)
	}
}

// Reading a cached template marks it as used, so a template a run keeps
// reading is never deleted as unused, however long ago it was built.
func TestReadingACachedTemplateMarksItUsed(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	built := time.Now().Add(-48 * time.Hour)
	path := templateTestFile(t, dir, "template-00000000000000c0.db", built)

	before := time.Now()
	unused := &templateBuilds{contents: []byte("a rebuild")}
	if _, err := cachedTemplate(dir, "template-00000000000000c0.db", unused.build); err != nil {
		t.Fatalf("cachedTemplate: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if unused.count != 0 {
		t.Errorf("the cached template was built again %d times", unused.count)
	}
	if modified := info.ModTime(); modified.Before(before.Add(-time.Second)) {
		t.Errorf("after a read the template was last modified %v, want no earlier than %v", modified, before)
	}
}

// The SQLite in the template's cache name is the modernc.org/sqlite module
// the test binary was built with, a replacement's if it is replaced, and
// "unknown" without build info that records it.
func TestSQLiteModuleVersionIsTheOneTheBinaryWasBuiltWith(t *testing.T) {
	t.Parallel()
	recorded := &debug.BuildInfo{Deps: []*debug.Module{
		{Path: "modernc.org/libc", Version: "v1.79.0"},
		{Path: "modernc.org/sqlite", Version: "v1.61.0", Sum: "h1:recorded="},
	}}
	replaced := &debug.BuildInfo{Deps: []*debug.Module{
		{Path: "modernc.org/sqlite", Version: "v1.61.0", Replace: &debug.Module{Path: "../sqlite"}},
	}}
	without := &debug.BuildInfo{Deps: []*debug.Module{{Path: "modernc.org/libc", Version: "v1.79.0"}}}

	for _, tc := range []struct {
		name string
		info *debug.BuildInfo
		ok   bool
		want string
	}{
		{"recorded", recorded, true, "modernc.org/sqlite@v1.61.0 h1:recorded="},
		{"replaced", replaced, true, "../sqlite@ "},
		{"not a dependency", without, true, "unknown"},
		{"no build info", nil, false, "unknown"},
	} {
		if got := sqliteModuleVersion(tc.info, tc.ok); got != tc.want {
			t.Errorf("%s: sqliteModuleVersion = %q, want %q", tc.name, got, tc.want)
		}
	}
}
