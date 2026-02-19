package patch

import (
	"strings"
	"testing"

	"github.com/dshills/speccritic/internal/schema"
)

func TestGenerateDiff_ExactMatch(t *testing.T) {
	spec := "The system must be fast.\nOther line.\n"
	patches := []schema.Patch{
		{IssueID: "ISSUE-0001", Before: "The system must be fast.", After: "The system must respond within 250ms p95."},
	}
	out := GenerateDiff(spec, patches, nil)
	if out == "" {
		t.Error("expected non-empty diff for exact match")
	}
	if !strings.Contains(out, "ISSUE-0001") {
		t.Errorf("diff missing issue ID: %q", out)
	}
}

func TestGenerateDiff_NormalizedMatch(t *testing.T) {
	// Spec has trailing spaces; patch 'before' has them trimmed
	spec := "The system must be fast.   \nOther line.\n"
	patches := []schema.Patch{
		{IssueID: "ISSUE-0002", Before: "The system must be fast.", After: "The system must respond within 250ms p95."},
	}
	var warnBuf strings.Builder
	out := GenerateDiff(spec, patches, &warnBuf)
	if out == "" {
		t.Error("expected non-empty diff for normalized match")
	}
	if warnBuf.Len() > 0 {
		t.Errorf("unexpected warning for normalized match: %q", warnBuf.String())
	}
}

func TestGenerateDiff_UnmatchedBeforeSkipped(t *testing.T) {
	spec := "Some spec content.\n"
	patches := []schema.Patch{
		{IssueID: "ISSUE-0003", Before: "text that does not exist", After: "replacement"},
	}
	var warnBuf strings.Builder
	out := GenerateDiff(spec, patches, &warnBuf)
	if out != "" {
		t.Errorf("expected empty diff for unmatched patch, got: %q", out)
	}
	if !strings.Contains(warnBuf.String(), "ISSUE-0003") {
		t.Errorf("expected warning mentioning ISSUE-0003: %q", warnBuf.String())
	}
}

func TestGenerateDiff_EmptyPatches(t *testing.T) {
	out := GenerateDiff("some spec", nil, nil)
	if out != "" {
		t.Errorf("expected empty string for nil patches, got %q", out)
	}
}
