package app

import (
	"context"
	"errors"
	"fmt"
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

func TestCheckerPreflightOnlySkipsProvider(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "")
	t.Setenv("SPECCRITIC_LLM_MODEL", "")

	called := false
	checker := &Checker{NewProvider: func(string) (llm.Provider, error) {
		called = true
		return nil, errors.New("provider should not be called")
	}}

	result, err := checker.Check(context.Background(), CheckRequest{
		Version:           "test",
		SpecName:          "SPEC.md",
		SpecText:          "TODO define authentication behavior.\n",
		Profile:           "general",
		SeverityThreshold: "info",
		Temperature:       0.2,
		MaxTokens:         1000,
		Preflight:         true,
		PreflightMode:     "only",
		Source:            SourceCLI,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if called {
		t.Fatal("provider was called")
	}
	if result.Model != "preflight" || result.Report.Meta.Model != "preflight" {
		t.Fatalf("model = result %q report %q, want preflight", result.Model, result.Report.Meta.Model)
	}
	if !hasIssue(result.Report.Issues, "PREFLIGHT-TODO-001") {
		t.Fatalf("issues = %#v, want PREFLIGHT-TODO-001", result.Report.Issues)
	}
	if result.Report.Summary.Verdict != schema.VerdictInvalid {
		t.Fatalf("verdict = %s, want INVALID", result.Report.Summary.Verdict)
	}
}

func TestCheckerPreflightGateSkipsProviderOnBlockingIssue(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "")
	t.Setenv("SPECCRITIC_LLM_MODEL", "")

	called := false
	checker := &Checker{NewProvider: func(string) (llm.Provider, error) {
		called = true
		return nil, errors.New("provider should not be called")
	}}

	result, err := checker.Check(context.Background(), CheckRequest{
		Version:           "test",
		SpecName:          "SPEC.md",
		SpecText:          "TODO define rate limits.\n",
		Profile:           "general",
		SeverityThreshold: "info",
		Temperature:       0.2,
		MaxTokens:         1000,
		Preflight:         true,
		PreflightMode:     "gate",
		Source:            SourceCLI,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if called {
		t.Fatal("provider was called")
	}
	if len(result.Report.Issues) == 0 || !result.Report.Issues[0].Blocking {
		t.Fatalf("issues = %#v, want blocking preflight issue", result.Report.Issues)
	}
}

func TestCheckerPreflightWarnCallsProviderAndMergesIssues(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "fake")
	t.Setenv("SPECCRITIC_LLM_MODEL", "model")

	provider := &fakeProvider{content: `{"issues":[],"questions":[],"patches":[]}`}
	checker := &Checker{NewProvider: func(string) (llm.Provider, error) { return provider, nil }}

	result, err := checker.Check(context.Background(), CheckRequest{
		Version:           "test",
		SpecName:          "SPEC.md",
		SpecText:          "TODO define upload validation.\n",
		Profile:           "general",
		SeverityThreshold: "info",
		Temperature:       0.2,
		MaxTokens:         1000,
		Preflight:         true,
		PreflightMode:     "warn",
		Source:            SourceCLI,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(provider.reqs) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(provider.reqs))
	}
	if !hasIssue(result.Report.Issues, "PREFLIGHT-TODO-001") {
		t.Fatalf("issues = %#v, want merged preflight issue", result.Report.Issues)
	}
}

func TestCheckerPreflightWarnSendsKnownFindingsToProvider(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "fake")
	t.Setenv("SPECCRITIC_LLM_MODEL", "model")

	provider := &fakeProvider{content: `{"issues":[],"questions":[],"patches":[]}`}
	checker := &Checker{NewProvider: func(string) (llm.Provider, error) { return provider, nil }}

	_, err := checker.Check(context.Background(), CheckRequest{
		Version:           "test",
		SpecName:          "SPEC.md",
		SpecText:          completeSpecWithRequirement("TODO define upload validation."),
		Profile:           "general",
		SeverityThreshold: "info",
		Temperature:       0.2,
		MaxTokens:         1000,
		Preflight:         true,
		PreflightMode:     "warn",
		Source:            SourceCLI,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(provider.reqs) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(provider.reqs))
	}
	prompt := provider.reqs[0].UserPrompt
	for _, want := range []string{"<known_preflight_findings>", "PREFLIGHT-TODO-001", "duplicates:<PREFLIGHT-ID>"} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("prompt missing %q:\n%s", want, prompt)
		}
	}
}

func TestCheckerPreflightDuplicateTagKeepsLLMIssueCanonical(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "fake")
	t.Setenv("SPECCRITIC_LLM_MODEL", "model")

	provider := &fakeProvider{content: `{
		"issues":[{
			"id":"ISSUE-0001",
			"severity":"CRITICAL",
			"category":"UNSPECIFIED_CONSTRAINT",
			"title":"Placeholder text remains in spec",
			"description":"The model confirms the placeholder.",
			"evidence":[{"path":"SPEC.md","line_start":11,"line_end":11,"quote":"TODO define upload validation."}],
			"impact":"Cannot implement safely.",
			"recommendation":"Replace the placeholder.",
			"blocking":true,
			"tags":["duplicates:PREFLIGHT-TODO-001"]
		}],
		"questions":[],
		"patches":[]
	}`}
	checker := &Checker{NewProvider: func(string) (llm.Provider, error) { return provider, nil }}

	result, err := checker.Check(context.Background(), CheckRequest{
		Version:           "test",
		SpecName:          "SPEC.md",
		SpecText:          completeSpecWithRequirement("TODO define upload validation."),
		Profile:           "general",
		SeverityThreshold: "info",
		Temperature:       0.2,
		MaxTokens:         1000,
		Preflight:         true,
		PreflightMode:     "warn",
		Source:            SourceCLI,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Report.Issues) != 1 {
		t.Fatalf("issues = %#v, want only confirmed LLM issue", result.Report.Issues)
	}
	issue := result.Report.Issues[0]
	if issue.ID != "ISSUE-0001" {
		t.Fatalf("issue ID = %s, want ISSUE-0001", issue.ID)
	}
	if !hasIssueTag(issue.Tags, "preflight-confirmed") || !hasIssueTag(issue.Tags, "preflight-rule:PREFLIGHT-TODO-001") {
		t.Fatalf("tags = %#v, want preflight confirmation tags", issue.Tags)
	}
}

func TestCheckerForcedChunkingUsesChunkPath(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "fake")
	t.Setenv("SPECCRITIC_LLM_MODEL", "model")

	provider := &chunkAwareProvider{}
	checker := &Checker{NewProvider: func(string) (llm.Provider, error) { return provider, nil }}
	result, err := checker.Check(context.Background(), CheckRequest{
		Version:           "test",
		SpecName:          "SPEC.md",
		SpecText:          completeSpecWithRequirement("The service must upload files."),
		Profile:           "general",
		SeverityThreshold: "info",
		Temperature:       0.2,
		MaxTokens:         1000,
		Preflight:         false,
		Chunking:          "on",
		ChunkLines:        4,
		ChunkConcurrency:  2,
		Source:            SourceCLI,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if provider.chunkCalls == 0 {
		t.Fatal("expected chunk calls")
	}
	if len(result.Report.Issues) == 0 || !hasIssueTag(result.Report.Issues[0].Tags, "chunked-review") {
		t.Fatalf("issues = %#v, want merged chunk finding", result.Report.Issues)
	}
}

func TestCheckerAutoChunkingUsesLineThreshold(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "fake")
	t.Setenv("SPECCRITIC_LLM_MODEL", "model")

	provider := &chunkAwareProvider{emptyChunks: true}
	checker := &Checker{NewProvider: func(string) (llm.Provider, error) { return provider, nil }}
	_, err := checker.Check(context.Background(), CheckRequest{
		Version:                "test",
		SpecName:               "SPEC.md",
		SpecText:               longSpec(130),
		Profile:                "general",
		SeverityThreshold:      "info",
		Temperature:            0.2,
		MaxTokens:              1000,
		Preflight:              false,
		Chunking:               "auto",
		ChunkLines:             40,
		ChunkMinLines:          120,
		ChunkTokenThreshold:    1000000,
		ChunkConcurrency:       3,
		SynthesisLineThreshold: 240,
		Source:                 SourceCLI,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if provider.chunkCalls < 2 || provider.singleCalls != 0 {
		t.Fatalf("chunk calls = %d single calls = %d, want multiple chunks only", provider.chunkCalls, provider.singleCalls)
	}
}

func TestCheckerAutoChunkingUsesTokenThreshold(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "fake")
	t.Setenv("SPECCRITIC_LLM_MODEL", "model")

	provider := &chunkAwareProvider{emptyChunks: true}
	checker := &Checker{NewProvider: func(string) (llm.Provider, error) { return provider, nil }}
	_, err := checker.Check(context.Background(), CheckRequest{
		Version:             "test",
		SpecName:            "SPEC.md",
		SpecText:            "# Title\n" + strings.Repeat("long ", 2000),
		Profile:             "general",
		SeverityThreshold:   "info",
		Temperature:         0.2,
		MaxTokens:           1000,
		Preflight:           false,
		Chunking:            "auto",
		ChunkLines:          20,
		ChunkMinLines:       1000,
		ChunkTokenThreshold: 100,
		ChunkConcurrency:    1,
		Source:              SourceCLI,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if provider.chunkCalls != 1 || provider.singleCalls != 0 {
		t.Fatalf("chunk calls = %d single calls = %d, want token-triggered chunk path", provider.chunkCalls, provider.singleCalls)
	}
}

func TestCheckerChunkingOffUsesSingleCall(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "fake")
	t.Setenv("SPECCRITIC_LLM_MODEL", "model")

	provider := &chunkAwareProvider{emptyChunks: true}
	checker := &Checker{NewProvider: func(string) (llm.Provider, error) { return provider, nil }}
	_, err := checker.Check(context.Background(), CheckRequest{
		Version:           "test",
		SpecName:          "SPEC.md",
		SpecText:          longSpec(160),
		Profile:           "general",
		SeverityThreshold: "info",
		Temperature:       0.2,
		MaxTokens:         1000,
		Preflight:         false,
		Chunking:          "off",
		ChunkLines:        20,
		ChunkMinLines:     10,
		ChunkConcurrency:  1,
		Source:            SourceCLI,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if provider.singleCalls != 1 || provider.chunkCalls != 0 {
		t.Fatalf("single calls = %d chunk calls = %d, want single path", provider.singleCalls, provider.chunkCalls)
	}
}

func TestCheckerRejectsInvalidChunkFlags(t *testing.T) {
	checker := NewChecker()
	_, err := checker.Check(context.Background(), CheckRequest{
		SpecName:          "SPEC.md",
		SpecText:          "Requirement.\n",
		Profile:           "general",
		SeverityThreshold: "info",
		Chunking:          "maybe",
		Source:            SourceCLI,
	})
	if err == nil || !strings.Contains(err.Error(), "invalid chunking mode") {
		t.Fatalf("error = %v, want invalid chunking mode", err)
	}
}

func hasIssue(issues []schema.Issue, id string) bool {
	for _, issue := range issues {
		if issue.ID == id {
			return true
		}
	}
	return false
}

func hasIssueTag(tags []string, want string) bool {
	for _, tag := range tags {
		if tag == want {
			return true
		}
	}
	return false
}

type chunkAwareProvider struct {
	emptyChunks bool
	chunkCalls  int
	synthCalls  int
	singleCalls int
}

func (p *chunkAwareProvider) Complete(_ context.Context, req *llm.Request) (*llm.Response, error) {
	switch {
	case strings.Contains(req.UserPromptCachedPrefix, "Analyze cross-section risks"):
		p.synthCalls++
		return &llm.Response{Content: `{"issues":[],"questions":[],"patches":[]}`, Model: "fake:synthesis"}, nil
	case strings.Contains(req.UserPrompt, "<chunk_issue_tag>"):
		p.chunkCalls++
		if p.emptyChunks {
			return &llm.Response{Content: `{"issues":[],"questions":[],"patches":[],"meta":{"chunk_summary":"summary"}}`, Model: "fake:chunk"}, nil
		}
		id := chunkIDFromPrompt(req.UserPrompt)
		line := chunkStartLine(id)
		return &llm.Response{Content: fmt.Sprintf(`{"issues":[{"id":"ISSUE-0001","severity":"WARN","category":"AMBIGUOUS_BEHAVIOR","title":"Chunk finding","description":"d","evidence":[{"path":"SPEC.md","line_start":%d,"line_end":%d,"quote":"q"}],"impact":"i","recommendation":"r","blocking":false,"tags":["chunk:%s"]}],"questions":[],"patches":[],"meta":{"chunk_summary":"summary"}}`, line, line, id), Model: "fake:chunk"}, nil
	default:
		p.singleCalls++
		return &llm.Response{Content: `{"issues":[],"questions":[],"patches":[]}`, Model: "fake:single"}, nil
	}
}

func chunkIDFromPrompt(prompt string) string {
	const prefix = "<chunk_issue_tag>chunk:"
	start := strings.Index(prompt, prefix)
	if start < 0 {
		return "CHUNK-0001-L1-L1"
	}
	start += len(prefix)
	end := strings.Index(prompt[start:], "</chunk_issue_tag>")
	if end < 0 {
		return "CHUNK-0001-L1-L1"
	}
	return prompt[start : start+end]
}

func chunkStartLine(id string) int {
	idx := strings.Index(id, "-L")
	if idx < 0 {
		return 1
	}
	idx += 2
	end := strings.Index(id[idx:], "-L")
	if end < 0 {
		return 1
	}
	var line int
	if _, err := fmt.Sscanf(id[idx:idx+end], "%d", &line); err != nil || line < 1 {
		return 1
	}
	return line
}

func longSpec(lines int) string {
	var b strings.Builder
	b.WriteString("# Long Spec\n")
	for i := 1; i < lines; i++ {
		if i%20 == 0 {
			fmt.Fprintf(&b, "\n## Section %d\n", i/20)
			continue
		}
		fmt.Fprintf(&b, "Requirement line %d.\n", i)
	}
	return b.String()
}

func completeSpecWithRequirement(requirement string) string {
	return strings.Join([]string{
		"# Service Spec",
		"",
		"## Purpose",
		"Define service behavior.",
		"",
		"## Non-goals",
		"Billing is out of scope.",
		"",
		"## Requirements",
		"",
		requirement,
		"",
		"## Acceptance Criteria",
		"Each requirement has an objective test.",
	}, "\n")
}
