package app

import (
	"context"
	"testing"

	"github.com/dshills/speccritic/internal/buildinfo"
	"github.com/dshills/speccritic/internal/llm"
)

// A caller that passes no version, such as the web UI or a library user,
// gets the version Go recorded in the binary; one that passes a version
// keeps it.
func TestCheckerFillsAMissingVersion(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "fake")
	t.Setenv("SPECCRITIC_LLM_MODEL", "model")
	provider := &fakeProvider{content: `{"issues":[],"questions":[],"patches":[]}`}
	checker := &Checker{NewProvider: func(string) (llm.Provider, error) { return provider, nil }}

	req := patchTestRequest("")
	req.Version = ""
	result, err := checker.Check(context.Background(), req)
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if got := result.Report.Version; got == "" || got != buildinfo.Version() {
		t.Errorf("report version = %q, want buildinfo.Version() %q", got, buildinfo.Version())
	}

	req.Version = "v9.9.9"
	result, err = checker.Check(context.Background(), req)
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if result.Report.Version != "v9.9.9" {
		t.Errorf("report version = %q, want the caller's v9.9.9", result.Report.Version)
	}
}
