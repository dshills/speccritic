package cache

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dshills/speccritic/internal/schema"
)

func testStore(t *testing.T, now *time.Time) *Store {
	t.Helper()
	s := New(t.TempDir())
	s.now = func() time.Time { return *now }
	return s
}

func testKey(t *testing.T, material any) string {
	t.Helper()
	key, err := Key(material)
	if err != nil {
		t.Fatalf("Key: %v", err)
	}
	return key
}

func TestPutThenGetReturnsTheReview(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	s := testStore(t, &now)
	key := testKey(t, "spec A")
	want := &schema.Report{Summary: schema.Summary{Verdict: schema.VerdictValid, Score: 93}}

	if _, _, ok := s.Get(key); ok {
		t.Fatal("Get on an empty store hit")
	}
	if err := s.Put(key, want); err != nil {
		t.Fatalf("Put: %v", err)
	}
	got, storedAt, ok := s.Get(key)
	if !ok {
		t.Fatal("Get missed a stored review")
	}
	if got.Summary != want.Summary || !storedAt.Equal(now) {
		t.Fatalf("Get = %+v at %v, want %+v at %v", got.Summary, storedAt, want.Summary, now)
	}
	info, err := os.Stat(s.path(key))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("entry mode = %v, want it private to the user", perm)
	}
}

func TestKeyDependsOnEveryInput(t *testing.T) {
	type material struct {
		Model string
		Spec  string
	}
	base := testKey(t, material{"m1", "spec"})
	if again := testKey(t, material{"m1", "spec"}); again != base {
		t.Fatal("Key is not deterministic")
	}
	for _, m := range []material{{"m2", "spec"}, {"m1", "spec "}} {
		if testKey(t, m) == base {
			t.Fatalf("Key(%+v) collides with the base key", m)
		}
	}
	if !validKey(base) {
		t.Fatalf("Key returned %q, which the store would reject", base)
	}
}

func TestGetIgnoresStaleEntriesAndPutRemovesThem(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	s := testStore(t, &now)
	old := testKey(t, "old")
	if err := s.Put(old, &schema.Report{}); err != nil {
		t.Fatal(err)
	}
	stamp := now.Add(-DefaultMaxAge - time.Hour)
	if err := os.Chtimes(s.path(old), stamp, stamp); err != nil {
		t.Fatal(err)
	}
	now = now.Add(DefaultMaxAge + time.Minute)

	if _, _, ok := s.Get(old); ok {
		t.Fatal("Get served an entry past its age")
	}
	if err := s.Put(testKey(t, "new"), &schema.Report{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.path(old)); !os.IsNotExist(err) {
		t.Fatalf("stale entry still on disk after Put (stat err = %v)", err)
	}
}

func TestPutRemovesAbandonedTempFiles(t *testing.T) {
	now := time.Now()
	s := testStore(t, &now)
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	abandoned := filepath.Join(s.Dir, tempPrefix+"123")
	fresh := filepath.Join(s.Dir, tempPrefix+"456")
	for _, p := range []string{abandoned, fresh} {
		if err := os.WriteFile(p, []byte("{"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	stamp := now.Add(-2 * time.Hour)
	if err := os.Chtimes(abandoned, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	if err := s.Put(testKey(t, "x"), &schema.Report{}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(abandoned); !os.IsNotExist(err) {
		t.Fatal("abandoned temp file was kept")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Fatal("a temp file another writer may still be using was removed")
	}
}

func TestGetTreatsCorruptOrForeignEntriesAsMisses(t *testing.T) {
	now := time.Now()
	s := testStore(t, &now)
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"corrupt":    `{"format":"1","report":`,
		"old format": `{"format":"0","stored_at":"2026-10-06T00:00:00Z","report":{}}`,
		"no report":  `{"format":"1","stored_at":"2026-10-06T00:00:00Z"}`,
	} {
		key := testKey(t, name)
		if err := os.WriteFile(s.path(key), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, ok := s.Get(key); ok {
			t.Errorf("%s entry: Get hit, want a miss", name)
		}
	}
}

func TestInvalidKeysNeverTouchTheFilesystem(t *testing.T) {
	now := time.Now()
	s := testStore(t, &now)
	for _, key := range []string{"", "../escape", strings.Repeat("g", 64), strings.Repeat("a", 63)} {
		if err := s.Put(key, &schema.Report{}); err == nil {
			t.Errorf("Put(%q) succeeded, want it refused", key)
		}
		if _, _, ok := s.Get(key); ok {
			t.Errorf("Get(%q) hit", key)
		}
	}
	if _, err := os.Stat(s.Dir); err != nil {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(s.Dir)
	if len(entries) != 0 {
		t.Fatalf("store dir holds %d files after refused writes", len(entries))
	}
}

func TestDefaultHonorsTheEnvironment(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SPECCRITIC_CACHE_DIR", dir)
	s, err := Default()
	if err != nil {
		t.Fatal(err)
	}
	if s.Dir != dir || s.MaxAge != DefaultMaxAge {
		t.Fatalf("Default() = %+v, want dir %q and the default age", s, dir)
	}
}

// The directory can be set by the user, so pruning must leave alone every
// file the store did not name, however old.
func TestPruneLeavesForeignFilesAlone(t *testing.T) {
	now := time.Now()
	s := testStore(t, &now)
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		t.Fatal(err)
	}
	stamp := now.Add(-2 * DefaultMaxAge)
	foreign := []string{"notes.txt", "report.json", ".tmp-other", strings.Repeat("z", 64) + ".json"}
	for _, name := range foreign {
		path := filepath.Join(s.Dir, name)
		if err := os.WriteFile(path, []byte("keep"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, stamp, stamp); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Put(testKey(t, "x"), &schema.Report{}); err != nil {
		t.Fatal(err)
	}
	for _, name := range foreign {
		if _, err := os.Stat(filepath.Join(s.Dir, name)); err != nil {
			t.Errorf("%s was removed: %v", name, err)
		}
	}
}
