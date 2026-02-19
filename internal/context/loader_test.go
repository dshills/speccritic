package context

import (
	"os"
	"strings"
	"testing"
)

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	f, err := os.CreateTemp("", "ctx*.md")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	f.Close()
	t.Cleanup(func() { os.Remove(f.Name()) })
	return f.Name()
}

func TestLoad_ReadsAndRedacts(t *testing.T) {
	path := writeTempFile(t, "glossary content\npassword: secret\n")
	files, err := Load([]string{path})
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(files))
	}
	if strings.Contains(files[0].Content, "secret") {
		t.Errorf("content not redacted: %q", files[0].Content)
	}
	if !strings.Contains(files[0].Content, "[REDACTED]") {
		t.Errorf("expected [REDACTED] in content: %q", files[0].Content)
	}
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load([]string{"/nonexistent/file.md"})
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

func TestFormatForPrompt_XMLTags(t *testing.T) {
	files := []ContextFile{
		{Path: "/some/path/glossary.md", Content: "term: definition\n"},
		{Path: "/other/constraints.md", Content: "constraint one\n"},
	}
	out := FormatForPrompt(files)

	if !strings.Contains(out, `<context file="glossary.md">`) {
		t.Errorf("missing opening tag for glossary.md: %q", out)
	}
	if !strings.Contains(out, "</context>") {
		t.Errorf("missing closing tag: %q", out)
	}
	if !strings.Contains(out, `<context file="constraints.md">`) {
		t.Errorf("missing opening tag for constraints.md: %q", out)
	}
	if !strings.Contains(out, "term: definition") {
		t.Errorf("missing content: %q", out)
	}
}

func TestFormatForPrompt_Empty(t *testing.T) {
	out := FormatForPrompt(nil)
	if out != "" {
		t.Errorf("expected empty string for nil input, got %q", out)
	}
}

func TestFormatForPrompt_MultipleFilesOrdered(t *testing.T) {
	files := []ContextFile{
		{Path: "a.md", Content: "AAAA"},
		{Path: "b.md", Content: "BBBB"},
	}
	out := FormatForPrompt(files)
	posA := strings.Index(out, "AAAA")
	posB := strings.Index(out, "BBBB")
	if posA < 0 || posB < 0 {
		t.Fatalf("content missing from output: %q", out)
	}
	if posA > posB {
		t.Errorf("files not in order: A at %d, B at %d", posA, posB)
	}
}
