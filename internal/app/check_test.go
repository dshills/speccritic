package app

import (
	"context"
	"strings"
	"testing"

	"github.com/dshills/speccritic/internal/llm"
	"github.com/dshills/speccritic/internal/schema"
)

type fakeProvider struct {
	content string
	reqs    []*llm.Request
}

func (p *fakeProvider) Complete(_ context.Context, req *llm.Request) (*llm.Response, error) {
	p.reqs = append(p.reqs, req)
	return &llm.Response{Content: p.content, Model: "fake:model"}, nil
}

func TestCheckerTextBackedCheck(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "fake")
	t.Setenv("SPECCRITIC_LLM_MODEL", "model")

	provider := &fakeProvider{content: `{"issues":[],"questions":[],"patches":[]}`}
	checker := &Checker{NewProvider: func(string) (llm.Provider, error) { return provider, nil }}

	result, err := checker.Check(context.Background(), CheckRequest{
		Version:           "test",
		SpecName:          "SPEC.md",
		SpecText:          "The system must do one thing.\n",
		Profile:           "general",
		SeverityThreshold: "info",
		Temperature:       0.2,
		MaxTokens:         1000,
		Source:            SourceWeb,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if result.Report.Summary.Verdict != schema.VerdictValid {
		t.Fatalf("verdict = %s, want VALID", result.Report.Summary.Verdict)
	}
	if result.Report.Input.SpecFile != "SPEC.md" {
		t.Fatalf("spec file = %q, want SPEC.md", result.Report.Input.SpecFile)
	}
	if len(provider.reqs) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(provider.reqs))
	}
}

func TestCheckerReturnsAllIssuesRegardlessOfSeverityThreshold(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "fake")
	t.Setenv("SPECCRITIC_LLM_MODEL", "model")

	provider := &fakeProvider{content: `{
		"issues":[
			{"id":"ISSUE-0001","severity":"INFO","category":"AMBIGUOUS_BEHAVIOR","title":"Info","description":"d","evidence":[{"path":"SPEC.md","line_start":1,"line_end":1,"quote":"q"}],"impact":"i","recommendation":"r","blocking":false,"tags":[]},
			{"id":"ISSUE-0002","severity":"CRITICAL","category":"NON_TESTABLE_REQUIREMENT","title":"Critical","description":"d","evidence":[{"path":"SPEC.md","line_start":1,"line_end":1,"quote":"q"}],"impact":"i","recommendation":"r","blocking":true,"tags":[]}
		],
		"questions":[],
		"patches":[]
	}`}
	checker := &Checker{NewProvider: func(string) (llm.Provider, error) { return provider, nil }}

	result, err := checker.Check(context.Background(), CheckRequest{
		Version:           "test",
		SpecName:          "SPEC.md",
		SpecText:          "Requirement.\n",
		Profile:           "general",
		SeverityThreshold: "critical",
		Temperature:       0.2,
		MaxTokens:         1000,
		Source:            SourceWeb,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if got := len(result.Report.Issues); got != 2 {
		t.Fatalf("issues = %d, want unfiltered 2", got)
	}
	if result.Report.Summary.InfoCount != 1 || result.Report.Summary.CriticalCount != 1 {
		t.Fatalf("summary counts = critical %d info %d, want 1/1", result.Report.Summary.CriticalCount, result.Report.Summary.InfoCount)
	}
}

func TestCheckerRejectsWebFilePaths(t *testing.T) {
	checker := NewChecker()
	_, err := checker.Check(context.Background(), CheckRequest{
		SpecPath: "SPEC.md",
		Profile:  "general",
		Source:   SourceWeb,
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "web checks must not use SpecPath") {
		t.Fatalf("error = %v", err)
	}
}
