package app

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/dshills/speccritic/internal/llm"
)

const patchTestSpec = "# Spec\nThe service must be fast.\nRetries are unlimited.\n"

const patchTestResponse = `{"issues":[{"id":"ISSUE-0001","severity":"WARN","category":"NON_TESTABLE_REQUIREMENT","title":"No number","description":"d","evidence":[{"line_start":2,"line_end":2,"quote":"must be fast"}],"impact":"i","recommendation":"r","blocking":false,"tags":[]}],"questions":[],"patches":[{"issue_id":"ISSUE-0001","line_start":2,"line_end":2,"after":"The service must answer within 200 ms."}]}`

func patchTestRequest(patches string) CheckRequest {
	return CheckRequest{
		Version: "test", SpecName: "SPEC.md", SpecText: patchTestSpec, Profile: "general",
		SeverityThreshold: "info", MaxTokens: 1000, Verify: VerifyOff, Patches: patches, Source: SourceWeb,
	}
}

// A patch given as a line range reaches the report with the replaced text
// taken from the spec, and the patch diff applies it.
func TestCheckerAppliesLineRangePatches(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "fake")
	t.Setenv("SPECCRITIC_LLM_MODEL", "model")
	provider := &fakeProvider{content: patchTestResponse}
	checker := &Checker{NewProvider: func(string) (llm.Provider, error) { return provider, nil }}

	result, err := checker.Check(context.Background(), patchTestRequest(""))
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if !strings.Contains(string(provider.reqs[0].Schema.JSON), `"patches"`) {
		t.Error("the default request does not ask for patches")
	}
	patches := result.Report.Patches
	if len(patches) != 1 || patches[0].Before != "The service must be fast." || patches[0].After != "The service must answer within 200 ms." {
		t.Fatalf("patches = %#v, want the line-range patch with its text filled in", patches)
	}
	if !strings.Contains(result.PatchDiff, "# patch for ISSUE-0001") {
		t.Errorf("patch diff does not carry the patch:\n%s", result.PatchDiff)
	}
	if got := result.Report.Issues[0].Evidence[0].Quote; got != "The service must be fast." {
		t.Errorf("evidence quote = %q, want the short anchor expanded to its line", got)
	}
}

// With patches off the model is not asked for them, and any it sends anyway
// are left out.
func TestCheckerPatchesOff(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "fake")
	t.Setenv("SPECCRITIC_LLM_MODEL", "model")
	provider := &fakeProvider{content: patchTestResponse}
	checker := &Checker{NewProvider: func(string) (llm.Provider, error) { return provider, nil }}

	result, err := checker.Check(context.Background(), patchTestRequest(PatchesOff))
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	req := provider.reqs[0]
	if strings.Contains(string(req.Schema.JSON), `"patches"`) || strings.Contains(req.Schema.PromptFallback, `"patches"`) {
		t.Error("the request still asks for patches")
	}
	if len(result.Report.Patches) != 0 || result.PatchDiff != "" {
		t.Fatalf("patches = %#v diff = %q, want none", result.Report.Patches, result.PatchDiff)
	}
	if len(result.Report.Issues) != 1 {
		t.Fatalf("issues = %d, want the finding kept", len(result.Report.Issues))
	}
}

func TestCheckerRejectsUnknownPatchesSetting(t *testing.T) {
	_, err := (&Checker{NewProvider: func(string) (llm.Provider, error) { return nil, errors.New("unused") }}).Check(context.Background(), patchTestRequest("sometimes"))
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Kind != ErrorInput || !strings.Contains(err.Error(), `patches "sometimes"`) {
		t.Fatalf("error = %v, want an input error naming the setting", err)
	}
}
