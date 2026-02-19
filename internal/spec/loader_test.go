package spec

import (
	"os"
	"strings"
	"testing"
)

func writeTempSpec(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "spec*.md")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(f.Name()) })
	defer f.Close()
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	return f.Name()
}

func TestLoad_LineNumbering(t *testing.T) {
	path := writeTempSpec(t, "line one\nline two\nline three\n")

	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if s.LineCount != 3 {
		t.Errorf("LineCount = %d, want 3", s.LineCount)
	}
	if !strings.HasPrefix(s.Numbered, "L1: line one") {
		t.Errorf("Numbered does not start with 'L1: line one': %q", s.Numbered)
	}
	if !strings.Contains(s.Numbered, "L2: line two") {
		t.Errorf("Numbered missing 'L2: line two': %q", s.Numbered)
	}
	if !strings.Contains(s.Numbered, "L3: line three") {
		t.Errorf("Numbered missing 'L3: line three': %q", s.Numbered)
	}
}

func TestLoad_HashStable(t *testing.T) {
	path := writeTempSpec(t, "hello world\n")

	s1, err := Load(path)
	if err != nil {
		t.Fatalf("Load (first): %v", err)
	}
	s2, err := Load(path)
	if err != nil {
		t.Fatalf("Load (second): %v", err)
	}

	if s1.Hash != s2.Hash {
		t.Errorf("hash not stable: %q vs %q", s1.Hash, s2.Hash)
	}
	if !strings.HasPrefix(s1.Hash, "sha256:") {
		t.Errorf("hash missing sha256 prefix: %q", s1.Hash)
	}
}

func TestLoad_LineCountAccurate(t *testing.T) {
	// 5 lines, no trailing newline
	path := writeTempSpec(t, "a\nb\nc\nd\ne")

	s, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if s.LineCount != 5 {
		t.Errorf("LineCount = %d, want 5", s.LineCount)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/spec.md")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}
