package completion

import (
	"strings"
	"testing"

	"github.com/dshills/speccritic/internal/schema"
)

func TestBuildResultEmitsSafePatchAndMetadata(t *testing.T) {
	original := "# Spec\n"
	candidates := []Candidate{safeCandidate("ISSUE-0001", original, "# Spec\n\n## Purpose\nOPEN DECISION: State purpose.")}
	result, issues, err := BuildResult(BuildInput{
		OriginalSpec: original,
		Candidates:   candidates,
		Issues:       []schema.Issue{{ID: "ISSUE-0001"}},
		Config:       Config{Mode: ModeAuto, MaxPatches: 8},
		Template:     schema.CompletionTemplateGeneral,
	})
	if err != nil {
		t.Fatalf("BuildResult: %v", err)
	}
	if len(result.Patches) != 1 || result.Patches[0].IssueID != "ISSUE-0001" {
		t.Fatalf("patches = %#v", result.Patches)
	}
	if result.Meta.GeneratedPatches != 1 || result.Meta.SkippedSuggestions != 0 || result.Meta.OpenDecisions != 1 {
		t.Fatalf("meta = %#v", result.Meta)
	}
	if len(issues) != 1 || !hasCompletionTag(issues[0].Tags) {
		t.Fatalf("issues = %#v", issues)
	}
}

func TestBuildResultSkipsAbsentAndDuplicateBefore(t *testing.T) {
	result, _, err := BuildResult(BuildInput{
		OriginalSpec: "same\nsame\n",
		Candidates: []Candidate{
			safeCandidate("ISSUE-0001", "missing", "after"),
			safeCandidate("ISSUE-0002", "same", "same\n\ninsert"),
		},
		Config:   Config{Mode: ModeAuto, MaxPatches: 8},
		Template: schema.CompletionTemplateGeneral,
	})
	if err != nil {
		t.Fatalf("BuildResult: %v", err)
	}
	if len(result.Patches) != 0 || result.Meta.SkippedSuggestions != 2 {
		t.Fatalf("result = %#v", result)
	}
}

func TestBuildResultSkipsOverlap(t *testing.T) {
	original := "alpha\nbeta\n"
	first := safeCandidate("ISSUE-0001", "alpha\nbeta", "alpha\nbeta\n\ninsert one")
	second := safeCandidate("ISSUE-0002", "beta", "beta\n\ninsert two")
	result, _, err := BuildResult(BuildInput{
		OriginalSpec: original,
		Candidates:   []Candidate{first, second},
		Config:       Config{Mode: ModeAuto, MaxPatches: 8},
		Template:     schema.CompletionTemplateGeneral,
	})
	if err != nil {
		t.Fatalf("BuildResult: %v", err)
	}
	if len(result.Patches) != 1 || result.Candidates[1].Status != StatusSkippedOverlap {
		t.Fatalf("result = %#v", result)
	}
}

func TestBuildResultSkipsRedaction(t *testing.T) {
	result, _, err := BuildResult(BuildInput{
		OriginalSpec: "[REDACTED]\nclean\naccess_key = AKIAIOSFODNN7EXAMPLE\n",
		Candidates: []Candidate{
			safeCandidate("ISSUE-0001", "[REDACTED]", "[REDACTED]\n\ninsert"),
			safeCandidate("ISSUE-0002", "clean", "clean\n\n[REDACTED]"),
			safeCandidate("ISSUE-0003", "access_key = AKIAIOSFODNN7EXAMPLE", "access_key = AKIAIOSFODNN7EXAMPLE\n\ninsert"),
		},
		Config:   Config{Mode: ModeAuto, MaxPatches: 8},
		Template: schema.CompletionTemplateGeneral,
	})
	if err != nil {
		t.Fatalf("BuildResult: %v", err)
	}
	if len(result.Patches) != 0 || result.Meta.SkippedSuggestions != 3 {
		t.Fatalf("result = %#v", result)
	}
}

func TestBuildResultAppliesPatchLimit(t *testing.T) {
	result, _, err := BuildResult(BuildInput{
		OriginalSpec: "one\ntwo\n",
		Candidates: []Candidate{
			safeCandidate("ISSUE-0001", "one", "one\n\ninsert one"),
			safeCandidate("ISSUE-0002", "two", "two\n\ninsert two"),
		},
		Config:   Config{Mode: ModeAuto, MaxPatches: 1},
		Template: schema.CompletionTemplateGeneral,
	})
	if err != nil {
		t.Fatalf("BuildResult: %v", err)
	}
	if len(result.Patches) != 1 || result.Meta.GeneratedPatches != 1 || result.Meta.SkippedSuggestions != 1 {
		t.Fatalf("result = %#v", result)
	}
	if result.Candidates[1].Status != StatusSkippedLimit {
		t.Fatalf("second status = %s", result.Candidates[1].Status)
	}
}

func TestBuildResultZeroPatchLimit(t *testing.T) {
	result, _, err := BuildResult(BuildInput{
		OriginalSpec: "one\n",
		Candidates:   []Candidate{safeCandidate("ISSUE-0001", "one", "one\n\ninsert")},
		Config:       Config{Mode: ModeAuto, MaxPatches: 0},
		Template:     schema.CompletionTemplateGeneral,
	})
	if err != nil {
		t.Fatalf("BuildResult: %v", err)
	}
	if len(result.Patches) != 0 || result.Candidates[0].Status != StatusSkippedLimit {
		t.Fatalf("result = %#v", result)
	}
}

func TestBuildResultModeOnRequiredFailure(t *testing.T) {
	_, _, err := BuildResult(BuildInput{
		OriginalSpec: "one\n",
		Candidates: []Candidate{{
			SourceIssueID: "PREFLIGHT-STRUCTURE-001",
			Status:        StatusSkippedNoSafeLocation,
		}},
		Issues: []schema.Issue{{
			ID:       "PREFLIGHT-STRUCTURE-001",
			Blocking: true,
			Tags:     []string{"missing-section"},
		}},
		Config:   Config{Mode: ModeOn, MaxPatches: 8},
		Template: schema.CompletionTemplateGeneral,
	})
	if err == nil || !strings.Contains(err.Error(), "PREFLIGHT-STRUCTURE-001") {
		t.Fatalf("err = %v", err)
	}
}

func safeCandidate(issueID, before, after string) Candidate {
	return Candidate{
		SourceIssueID: issueID,
		Status:        StatusPatchGenerated,
		Text:          strings.TrimPrefix(after, before),
		Before:        before,
		After:         after,
	}
}
