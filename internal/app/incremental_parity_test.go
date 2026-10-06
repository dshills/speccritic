package app

import (
	"context"
	"strings"
	"testing"

	"github.com/dshills/speccritic/internal/llm"
)

// An incremental range review is judged against the same material as a full
// review: the context files, the whole spec and the known preflight findings,
// in the same shared prefix the verification call uses. A range finding that
// names a preflight finding as its duplicate confirms it, as in a full review.
func TestCheckerIncrementalRangesShareTheFullReviewPrefix(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "fake")
	t.Setenv("SPECCRITIC_LLM_MODEL", "model")

	previous := completeSpecWithRequirement("Uploads are validated against the schema.")
	current := completeSpecWithRequirement("TODO define upload validation.")
	prevPath := writePreviousReport(t, specForTest("SPEC.md", previous).Hash, "general", false, "info", "## Purpose")
	provider := &recordingProvider{fn: func(req *llm.Request) string {
		if strings.Contains(req.UserPrompt, "<critical_findings>") {
			return `{"verdicts":[{"id":"F1","reason":"r","decision":"confirm","severity":"CRITICAL","line_start":0,"line_end":0,"quote":""}]}`
		}
		return `{"issues":[{"id":"ISSUE-0001","severity":"CRITICAL","category":"UNSPECIFIED_CONSTRAINT","title":"Upload validation is a placeholder","description":"d","evidence":[{"line_start":11,"line_end":11,"quote":"define upload validation"}],"impact":"i","recommendation":"r","blocking":true,"tags":["duplicates:PREFLIGHT-TODO-001"]}],"questions":[],"patches":[]}`
	}}
	checker := &Checker{NewProvider: func(string) (llm.Provider, error) { return provider, nil }}
	result, err := checker.Check(context.Background(), CheckRequest{
		Version:                         "test",
		SpecName:                        "SPEC.md",
		SpecText:                        current,
		ContextDocuments:                []ContextDocument{{Name: "glossary.md", Text: "An upload is a file sent by a client.\n"}},
		Profile:                         "general",
		SeverityThreshold:               "info",
		MaxTokens:                       1000,
		Preflight:                       true,
		PreflightMode:                   "warn",
		Chunking:                        "off",
		IncrementalFrom:                 prevPath,
		IncrementalBaseText:             previous,
		IncrementalMode:                 "on",
		IncrementalMaxChangeRatio:       1,
		IncrementalMaxRemapFailureRatio: 1,
		IncrementalContextLines:         2,
		Source:                          SourceWeb,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if result.Report.Version != "test" {
		t.Errorf("report version = %q, want the request's version", result.Report.Version)
	}
	if len(provider.reqs) < 2 {
		t.Fatalf("calls = %d, want a range review and a verification", len(provider.reqs))
	}
	rangeReq, verifyReq := provider.reqs[0], provider.reqs[len(provider.reqs)-1]
	if !strings.Contains(rangeReq.UserPrompt, "<incremental_range") {
		t.Fatalf("first call is not a range review:\n%s", rangeReq.UserPrompt)
	}
	prefix := rangeReq.UserPromptCachedPrefix
	for _, want := range []string{"An upload is a file sent by a client.", "L11: TODO define upload validation.", "L14: Each requirement has an objective test.", "<known_preflight_findings>", "PREFLIGHT-TODO-001"} {
		if !strings.Contains(prefix, want) {
			t.Errorf("range prefix lacks %q:\n%s", want, prefix)
		}
	}
	if prefix != verifyReq.UserPromptCachedPrefix || rangeReq.SystemPrompt != verifyReq.SystemPrompt {
		t.Error("the range review and the verification call do not share one cached prefix")
	}

	var confirmed, preflightLeft int
	for _, issue := range result.Report.Issues {
		if hasIssueTag(issue.Tags, "preflight-confirmed") && hasIssueTag(issue.Tags, "preflight-rule:PREFLIGHT-TODO-001") {
			confirmed++
		}
		if strings.HasPrefix(issue.ID, "PREFLIGHT-TODO") {
			preflightLeft++
		}
	}
	if confirmed != 1 || preflightLeft != 0 {
		t.Fatalf("issues = %#v, want the range finding to confirm and replace the preflight finding", result.Report.Issues)
	}
}
