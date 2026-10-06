// Package cache stores finished reviews on disk, keyed by everything that
// shapes them, so an unchanged spec reviewed again with unchanged settings
// costs nothing and gets the same verdict.
//
// Agents and CI re-run the gate on the same spec over and over. A model asked
// the same question twice may answer differently, so caching also makes the
// gate stable: the verdict changes only when the spec or the settings do.
package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/dshills/speccritic/internal/schema"
)

// DefaultMaxAge is how long a stored review is used. Older entries are
// ignored and removed.
const DefaultMaxAge = 30 * 24 * time.Hour

// formatVersion changes whenever what is stored, or what goes into a key,
// changes meaning.
const formatVersion = "1"

// Store keeps reviews in one directory, one file per key.
type Store struct {
	Dir    string
	MaxAge time.Duration
	// now is replaced in tests.
	now func() time.Time
}

// New returns a store in dir.
func New(dir string) *Store {
	return &Store{Dir: dir, MaxAge: DefaultMaxAge, now: time.Now}
}

// Default returns the store in SPECCRITIC_CACHE_DIR if set, and otherwise in
// the user's cache directory.
func Default() (*Store, error) {
	// Identify the build now, at startup, rather than at the first key: an
	// upgrade that replaces the executable mid-run would otherwise be hashed
	// in place of the code that is running.
	buildID()
	if dir := os.Getenv("SPECCRITIC_CACHE_DIR"); dir != "" {
		return New(dir), nil
	}
	base, err := os.UserCacheDir()
	if err != nil {
		return nil, err
	}
	return New(filepath.Join(base, "speccritic", "reviews")), nil
}

// entry is what is written to disk.
type entry struct {
	Format   string         `json:"format"`
	StoredAt time.Time      `json:"stored_at"`
	Report   *schema.Report `json:"report"`
}

// ErrUnknownBuild is returned by Key when the running program cannot be
// identified. Caching is then off: a review from another build could be
// served after an upgrade.
var ErrUnknownBuild = errors.New("cannot identify this build of speccritic")

// Key returns the key for material, which must hold everything that can
// change the review: the prompts sent, the model and its settings, and the
// build of this program.
func Key(material any) (string, error) {
	build := buildID()
	if build == "" {
		return "", ErrUnknownBuild
	}
	data, err := json.Marshal(struct {
		Format   string `json:"format"`
		Build    string `json:"build"`
		Material any    `json:"material"`
	}{formatVersion, build, material})
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// Get returns the review stored under key and when it was stored. A missing,
// unreadable, stale or corrupt entry is a miss.
func (s *Store) Get(key string) (*schema.Report, time.Time, bool) {
	if !validKey(key) {
		return nil, time.Time{}, false
	}
	data, err := os.ReadFile(s.path(key))
	if err != nil {
		return nil, time.Time{}, false
	}
	var e entry
	if err := json.Unmarshal(data, &e); err != nil || e.Format != formatVersion || e.Report == nil {
		return nil, time.Time{}, false
	}
	if s.MaxAge > 0 && s.clock().Sub(e.StoredAt) > s.MaxAge {
		return nil, time.Time{}, false
	}
	return e.Report, e.StoredAt, true
}

// Put stores report under key. The file is written whole or not at all, so a
// concurrent reader never sees half of it. Entries past their age are removed
// on the way.
func (s *Store) Put(key string, report *schema.Report) error {
	if !validKey(key) {
		return fmt.Errorf("invalid cache key %q", key)
	}
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(entry{Format: formatVersion, StoredAt: s.clock().UTC(), Report: report})
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.Dir, tempPrefix+"*")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), s.path(key)); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	s.prune()
	return nil
}

// tempPrefix starts the name of a file being written.
const tempPrefix = ".speccritic-tmp-"

// prune removes entries older than MaxAge, and temporary files a crashed
// writer left behind. Only files named the way the store names them are
// touched: the directory can be set by the user and may hold other files.
func (s *Store) prune() {
	if s.MaxAge <= 0 {
		return
	}
	entries, err := os.ReadDir(s.Dir)
	if err != nil {
		return
	}
	cutoff := s.clock().Add(-s.MaxAge)
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		name := e.Name()
		stale := isEntryName(name) && info.ModTime().Before(cutoff)
		abandoned := strings.HasPrefix(name, tempPrefix) && info.ModTime().Before(s.clock().Add(-time.Hour))
		if stale || abandoned {
			_ = os.Remove(filepath.Join(s.Dir, name))
		}
	}
}

func (s *Store) path(key string) string { return filepath.Join(s.Dir, key+".json") }

// isEntryName reports whether name is that of a stored review.
func isEntryName(name string) bool {
	key, ok := strings.CutSuffix(name, ".json")
	return ok && validKey(key)
}

// validKey reports whether key is one Key could have returned, so no key can
// name a file outside the store.
func validKey(key string) bool {
	if len(key) != 2*sha256.Size {
		return false
	}
	_, err := hex.DecodeString(key)
	return err == nil
}

func (s *Store) clock() time.Time {
	if s.now == nil {
		return time.Now()
	}
	return s.now()
}

var (
	buildOnce sync.Once
	buildHash string
)

// buildID identifies the running program. It is a hash of the executable, so
// any change to the code, a prompt or the output schema invalidates every
// stored review without anyone having to remember to bump a version. It is
// empty when the executable cannot be read.
func buildID() string {
	buildOnce.Do(func() {
		path, err := os.Executable()
		if err != nil {
			return
		}
		f, err := os.Open(path)
		if err != nil {
			return
		}
		defer func() { _ = f.Close() }()
		h := sha256.New()
		if _, err := io.Copy(h, f); err != nil {
			return
		}
		buildHash = hex.EncodeToString(h.Sum(nil))
	})
	return buildHash
}
