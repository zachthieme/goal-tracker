package testsupport

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"testing/fstest"
)

// templateTestMigrations is a stand-in for the embedded migrations, so a test
// can change one's contents.
func templateTestMigrations(body string) fstest.MapFS {
	return fstest.MapFS{
		"migrations/0001_accounts.sql": {Data: []byte("CREATE TABLE accounts (id INTEGER PRIMARY KEY);")},
		"migrations/0002_goals.sql":    {Data: []byte(body)},
	}
}

// A changed migration gives the template another cache name, so no test
// binary copies a template built from the old one; the same migrations give
// the same name, so every binary finds the one already built.
func TestTemplateCacheNameFollowsTheMigrationsContents(t *testing.T) {
	t.Parallel()
	before, err := templateCacheName(templateTestMigrations("CREATE TABLE goals (id INTEGER PRIMARY KEY);"))
	if err != nil {
		t.Fatalf("templateCacheName: %v", err)
	}
	again, err := templateCacheName(templateTestMigrations("CREATE TABLE goals (id INTEGER PRIMARY KEY);"))
	if err != nil {
		t.Fatalf("templateCacheName: %v", err)
	}
	changed, err := templateCacheName(templateTestMigrations("CREATE TABLE goals (id INTEGER PRIMARY KEY, title TEXT);"))
	if err != nil {
		t.Fatalf("templateCacheName: %v", err)
	}

	if again != before {
		t.Errorf("the same migrations are cached as %q and %q", before, again)
	}
	if changed == before {
		t.Errorf("a changed migration is still cached as %q", before)
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
	migrations := templateTestMigrations("CREATE TABLE goals (id INTEGER PRIMARY KEY);")
	first := &templateBuilds{contents: []byte("the first build")}
	second := &templateBuilds{contents: []byte("a second build")}

	built, err := cachedTemplate(dir, migrations, first.build)
	if err != nil {
		t.Fatalf("first cachedTemplate: %v", err)
	}
	reused, err := cachedTemplate(dir, migrations, second.build)
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

// Building a template deletes those built from other migrations, so a run
// leaves one template behind however often the migrations changed.
func TestBuildingATemplateDeletesThoseOfOtherMigrations(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	old := &templateBuilds{contents: []byte("built before the change")}
	current := &templateBuilds{contents: []byte("built after the change")}
	if _, err := cachedTemplate(dir, templateTestMigrations("CREATE TABLE goals (id INTEGER PRIMARY KEY);"), old.build); err != nil {
		t.Fatalf("cachedTemplate before the change: %v", err)
	}
	unrelated := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(unrelated, []byte("not a template"), 0o600); err != nil {
		t.Fatalf("write %s: %v", unrelated, err)
	}

	changed := templateTestMigrations("CREATE TABLE goals (id INTEGER PRIMARY KEY, title TEXT);")
	if _, err := cachedTemplate(dir, changed, current.build); err != nil {
		t.Fatalf("cachedTemplate after the change: %v", err)
	}

	name, err := templateCacheName(changed)
	if err != nil {
		t.Fatalf("templateCacheName: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var left []string
	for _, entry := range entries {
		left = append(left, entry.Name())
	}
	if want := []string{"notes.txt", name}; !slices.Equal(left, want) {
		t.Errorf("left in the cache: %v, want %v", left, want)
	}
}
