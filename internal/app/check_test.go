package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/dshills/speccritic/internal/llm"
	"github.com/dshills/speccritic/internal/schema"
	"github.com/dshills/speccritic/internal/spec"
)

type fakeProvider struct {
	content string
	reqs    []*llm.Request
}

func (p *fakeProvider) Complete(_ context.Context, req *llm.Request) (*llm.Response, error) {
	p.reqs = append(p.reqs, req)
	return &llm.Response{Content: p.content, Model: "fake:model"}, nil
}

type repairRecordingProvider struct {
	calls     int
	responses []string
	maxTokens []int
	prompts   []string
}

func (p *repairRecordingProvider) Complete(_ context.Context, req *llm.Request) (*llm.Response, error) {
	p.maxTokens = append(p.maxTokens, req.MaxTokens)
	p.prompts = append(p.prompts, req.UserPrompt)
	resp := p.responses[p.calls]
	p.calls++
	return &llm.Response{Content: resp, Model: "fake:model"}, nil
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

func TestCheckerKeepsOriginalSpecOutOfLLMRedaction(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "fake")
	t.Setenv("SPECCRITIC_LLM_MODEL", "model")

	provider := &fakeProvider{content: `{"issues":[{"id":"ISSUE-0001","severity":"WARN","category":"AMBIGUOUS_BEHAVIOR","title":"Secret handling unclear","description":"d","evidence":[{"path":"SPEC.md","line_start":1,"line_end":1,"quote":"[REDACTED]"}],"impact":"i","recommendation":"r","blocking":false,"tags":[]}],"questions":[],"patches":[{"issue_id":"ISSUE-0001","before":"password=[REDACTED]","after":"password is configured out of band"}]}`}
	checker := &Checker{NewProvider: func(string) (llm.Provider, error) { return provider, nil }}
	secretSpec := "The service password=supersecret and key sk-12345678901234567890.\n"

	result, err := checker.Check(context.Background(), CheckRequest{
		Version:           "test",
		SpecName:          "SPEC.md",
		SpecText:          secretSpec,
		Profile:           "general",
		SeverityThreshold: "info",
		MaxTokens:         1000,
		Source:            SourceWeb,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if result.OriginalSpec != secretSpec {
		t.Fatalf("original spec = %q, want unredacted input", result.OriginalSpec)
	}
	if len(provider.reqs) != 1 || strings.Contains(provider.reqs[0].UserPrompt, "supersecret") || strings.Contains(provider.reqs[0].UserPrompt, "sk-12345678901234567890") {
		t.Fatalf("LLM prompt was not redacted: %#v", provider.reqs)
	}
	if !strings.Contains(provider.reqs[0].UserPrompt, "[REDACTED]") {
		t.Fatalf("LLM prompt missing redaction marker: %s", provider.reqs[0].UserPrompt)
	}
	if result.PatchDiff != "" || len(result.Report.Patches) != 0 {
		t.Fatalf("patch diff should be suppressed and unsafe redacted patch filtered, diff %q patches %#v", result.PatchDiff, result.Report.Patches)
	}
}

func TestCheckerFiltersUnsafeSingleCallPatches(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "fake")
	t.Setenv("SPECCRITIC_LLM_MODEL", "model")

	provider := &fakeProvider{content: `{"issues":[{"id":"ISSUE-0001","severity":"WARN","category":"AMBIGUOUS_BEHAVIOR","title":"Ambiguous","description":"d","evidence":[{"path":"SPEC.md","line_start":1,"line_end":1,"quote":"same"}],"impact":"i","recommendation":"r","blocking":false,"tags":[]}],"questions":[],"patches":[{"issue_id":"ISSUE-0001","before":"same","after":"clear"}]}`}
	checker := &Checker{NewProvider: func(string) (llm.Provider, error) { return provider, nil }}

	result, err := checker.Check(context.Background(), CheckRequest{
		Version:           "test",
		SpecName:          "SPEC.md",
		SpecText:          "same\nsame\n",
		Profile:           "general",
		SeverityThreshold: "info",
		MaxTokens:         1000,
		Source:            SourceWeb,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(result.Report.Patches) != 0 || result.PatchDiff != "" {
		t.Fatalf("patches = %#v diff=%q, want ambiguous single-call patch filtered", result.Report.Patches, result.PatchDiff)
	}
}

func TestCheckerUsesRequestModelOverride(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "")
	t.Setenv("SPECCRITIC_LLM_MODEL", "")

	provider := &fakeProvider{content: `{"issues":[],"questions":[],"patches":[]}`}
	var providerModel string
	checker := &Checker{NewProvider: func(model string) (llm.Provider, error) {
		providerModel = model
		return provider, nil
	}}

	_, err := checker.Check(context.Background(), CheckRequest{
		Version:           "test",
		SpecName:          "SPEC.md",
		SpecText:          "The system must do one thing.\n",
		Profile:           "general",
		SeverityThreshold: "info",
		LLMProvider:       "openai",
		LLMModel:          "gpt-5",
		MaxTokens:         1000,
		Source:            SourceWeb,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if providerModel != "openai:gpt-5" {
		t.Fatalf("provider model = %q, want openai:gpt-5", providerModel)
	}
}

func TestCheckerDefaultsModelForRequestProvider(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "")
	t.Setenv("SPECCRITIC_LLM_MODEL", "")

	provider := &fakeProvider{content: `{"issues":[],"questions":[],"patches":[]}`}
	var providerModel string
	checker := &Checker{NewProvider: func(model string) (llm.Provider, error) {
		providerModel = model
		return provider, nil
	}}

	_, err := checker.Check(context.Background(), CheckRequest{
		Version:           "test",
		SpecName:          "SPEC.md",
		SpecText:          "The system must do one thing.\n",
		Profile:           "general",
		SeverityThreshold: "info",
		LLMProvider:       "openai",
		MaxTokens:         1000,
		Source:            SourceWeb,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if providerModel != "openai:gpt-4o" {
		t.Fatalf("provider model = %q, want openai:gpt-4o", providerModel)
	}
}

func TestCheckerMergesRequestModelWithEnvProvider(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "openai")
	t.Setenv("SPECCRITIC_LLM_MODEL", "")

	provider := &fakeProvider{content: `{"issues":[],"questions":[],"patches":[]}`}
	var providerModel string
	checker := &Checker{NewProvider: func(model string) (llm.Provider, error) {
		providerModel = model
		return provider, nil
	}}

	_, err := checker.Check(context.Background(), CheckRequest{
		Version:           "test",
		SpecName:          "SPEC.md",
		SpecText:          "The system must do one thing.\n",
		Profile:           "general",
		SeverityThreshold: "info",
		LLMModel:          "gpt-5",
		MaxTokens:         1000,
		Source:            SourceWeb,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if providerModel != "openai:gpt-5" {
		t.Fatalf("provider model = %q, want openai:gpt-5", providerModel)
	}
}

func TestCheckerInfersProviderFromRequestModel(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "")
	t.Setenv("SPECCRITIC_LLM_MODEL", "")

	provider := &fakeProvider{content: `{"issues":[],"questions":[],"patches":[]}`}
	var providerModel string
	checker := &Checker{NewProvider: func(model string) (llm.Provider, error) {
		providerModel = model
		return provider, nil
	}}

	_, err := checker.Check(context.Background(), CheckRequest{
		Version:           "test",
		SpecName:          "SPEC.md",
		SpecText:          "The system must do one thing.\n",
		Profile:           "general",
		SeverityThreshold: "info",
		LLMModel:          "gpt-5",
		MaxTokens:         1000,
		Source:            SourceWeb,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if providerModel != "openai:gpt-5" {
		t.Fatalf("provider model = %q, want openai:gpt-5", providerModel)
	}
}

func TestCheckerIncreasesRepairTokensForIncompleteJSON(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "fake")
	t.Setenv("SPECCRITIC_LLM_MODEL", "model")

	provider := &repairRecordingProvider{
		responses: []string{
			`{"issues":[`,
			`{"issues":[],"questions":[],"patches":[]}`,
		},
	}
	checker := &Checker{NewProvider: func(string) (llm.Provider, error) { return provider, nil }}

	_, err := checker.Check(context.Background(), CheckRequest{
		Version:           "test",
		SpecName:          "SPEC.md",
		SpecText:          "The system must do one thing.\n",
		Profile:           "general",
		SeverityThreshold: "info",
		MaxTokens:         1000,
		Source:            SourceWeb,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(provider.maxTokens) != 2 {
		t.Fatalf("calls = %d, want 2", len(provider.maxTokens))
	}
	if provider.maxTokens[1] <= provider.maxTokens[0] {
		t.Fatalf("repair max tokens = %d, want greater than initial %d", provider.maxTokens[1], provider.maxTokens[0])
	}
}

func TestCheckerContinuesResponseCutOffAtOutputCap(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "fake")
	t.Setenv("SPECCRITIC_LLM_MODEL", "model")

	first := `{"id":"ISSUE-0001","severity":"CRITICAL","category":"NON_TESTABLE_REQUIREMENT","title":"First","description":"d","evidence":[{"path":"SPEC.md","line_start":1,"line_end":1,"quote":"q"}],"impact":"i","recommendation":"r","blocking":true,"tags":[]}`
	second := `{"id":"ISSUE-0002","severity":"WARN","category":"AMBIGUOUS_BEHAVIOR","title":"Second","description":"d","evidence":[{"path":"SPEC.md","line_start":2,"line_end":2,"quote":"q"}],"impact":"i","recommendation":"r","blocking":false,"tags":[]}`
	provider := &repairRecordingProvider{
		responses: []string{
			`{"issues":[` + first + `,{"id":"ISSUE-0002","severity":"WARN","categ`,
			`{"issues":[` + second + `],"questions":[],"patches":[]}`,
		},
	}
	checker := &Checker{NewProvider: func(string) (llm.Provider, error) { return provider, nil }}

	result, err := checker.Check(context.Background(), CheckRequest{
		Version:           "test",
		SpecName:          "SPEC.md",
		SpecText:          "Requirement one.\nRequirement two.\n",
		Profile:           "general",
		SeverityThreshold: "info",
		MaxTokens:         1000,
		Source:            SourceWeb,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if provider.calls != 2 {
		t.Fatalf("calls = %d, want initial + continuation", provider.calls)
	}
	if provider.maxTokens[1] != provider.maxTokens[0] {
		t.Fatalf("continuation max tokens = %d, want unchanged %d", provider.maxTokens[1], provider.maxTokens[0])
	}
	if !strings.Contains(provider.prompts[1], "<received_findings>") || !strings.Contains(provider.prompts[1], "ISSUE-0001 CRITICAL") {
		t.Fatalf("continuation prompt does not list the received finding:\n%s", provider.prompts[1])
	}
	if !hasIssue(result.Report.Issues, "ISSUE-0001") || !hasIssue(result.Report.Issues, "ISSUE-0002") || len(result.Report.Issues) != 2 {
		t.Fatalf("issues = %#v, want both the kept and the continued finding", result.Report.Issues)
	}
	if result.Report.Summary.Verdict != schema.VerdictInvalid {
		t.Fatalf("verdict = %s, want INVALID from the finding received before the cut", result.Report.Summary.Verdict)
	}
}

func TestCheckerDropsInvalidFindingWithoutRetry(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "fake")
	t.Setenv("SPECCRITIC_LLM_MODEL", "model")

	// One finding has a made-up category; the other is valid but carries an
	// odd ID and someone else's path.
	provider := &fakeProvider{content: `{
		"issues":[
			{"id":"ISSUE-0001","severity":"WARN","category":"VIBES","title":"Bad","description":"d","evidence":[{"line_start":1,"line_end":1,"quote":"q"}],"impact":"i","recommendation":"r","blocking":false,"tags":[]},
			{"id":"finding-two","severity":"CRITICAL","category":"NON_TESTABLE_REQUIREMENT","title":"Good","description":"d","evidence":[{"path":"../other.md","line_start":1,"line_end":1,"quote":"q"}],"impact":"i","recommendation":"r","blocking":true,"tags":[]}
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
		SeverityThreshold: "info",
		MaxTokens:         1000,
		Source:            SourceWeb,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(provider.reqs) != 1 {
		t.Fatalf("provider calls = %d, want no repair call for one bad finding", len(provider.reqs))
	}
	if len(result.Report.Issues) != 1 {
		t.Fatalf("issues = %#v, want only the valid finding", result.Report.Issues)
	}
	issue := result.Report.Issues[0]
	if issue.ID != "ISSUE-0001" || issue.Title != "Good" {
		t.Fatalf("issue = %s %q, want the valid finding renumbered ISSUE-0001", issue.ID, issue.Title)
	}
	if issue.Evidence[0].Path != "SPEC.md" {
		t.Fatalf("evidence path = %q, want the reviewed spec", issue.Evidence[0].Path)
	}
	if result.Report.Meta.DroppedFindings != 1 {
		t.Fatalf("meta.dropped_findings = %d, want 1", result.Report.Meta.DroppedFindings)
	}
	if result.Report.Summary.Verdict != schema.VerdictInvalid {
		t.Fatalf("verdict = %s, want INVALID from the kept finding", result.Report.Summary.Verdict)
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

func TestCheckerPreflightOnlyAddsConvergenceMetadata(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "")
	t.Setenv("SPECCRITIC_LLM_MODEL", "")

	checker := &Checker{NewProvider: func(string) (llm.Provider, error) {
		return nil, errors.New("provider should not be called")
	}}

	result, err := checker.Check(context.Background(), CheckRequest{
		Version:             "test",
		SpecName:            "SPEC.md",
		SpecText:            "TODO define authentication behavior.\n",
		Profile:             "general",
		SeverityThreshold:   "info",
		MaxTokens:           1000,
		Preflight:           true,
		PreflightMode:       "only",
		ConvergenceFromText: previousConvergenceReportJSON(),
		ConvergenceMode:     "auto",
		ConvergenceReport:   true,
		Source:              SourceCLI,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	meta := result.Report.Meta.Convergence
	if meta == nil || !meta.Enabled {
		t.Fatalf("convergence meta = %#v", meta)
	}
	if meta.Current.New == 0 || meta.Status == "" {
		t.Fatalf("convergence meta = %#v", meta)
	}
}

func TestCheckerPreflightOnlyAddsCompletion(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "")
	t.Setenv("SPECCRITIC_LLM_MODEL", "")

	checker := &Checker{NewProvider: func(string) (llm.Provider, error) {
		return nil, errors.New("provider should not be called")
	}}
	result, err := checker.Check(context.Background(), CheckRequest{
		Version:                 "test",
		SpecName:                "SPEC.md",
		SpecText:                "# Spec\n",
		Profile:                 "general",
		SeverityThreshold:       "info",
		MaxTokens:               1000,
		Preflight:               true,
		PreflightMode:           "only",
		CompletionSuggestions:   true,
		CompletionMode:          "auto",
		CompletionTemplate:      "profile",
		CompletionMaxPatches:    8,
		CompletionOpenDecisions: true,
		Source:                  SourceCLI,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if result.Report.Meta.Completion == nil || result.Report.Meta.Completion.GeneratedPatches == 0 {
		t.Fatalf("completion meta = %#v patches=%#v", result.Report.Meta.Completion, result.Report.Patches)
	}
	if !hasIssueTag(result.Report.Issues[0].Tags, "completion-suggested") {
		t.Fatalf("first issue tags = %#v", result.Report.Issues[0].Tags)
	}
}

func TestCheckerCompletionModeOnFailsRequiredUnsafePatch(t *testing.T) {
	checker := NewChecker()
	_, err := checker.Check(context.Background(), CheckRequest{
		Version:                 "test",
		SpecName:                "SPEC.md",
		SpecText:                "same\nsame\n",
		Profile:                 "general",
		SeverityThreshold:       "info",
		Preflight:               true,
		PreflightMode:           "only",
		CompletionMode:          "on",
		CompletionTemplate:      "profile",
		CompletionMaxPatches:    8,
		CompletionOpenDecisions: true,
		Source:                  SourceCLI,
	})
	if err == nil || !strings.Contains(err.Error(), "required completion patch") {
		t.Fatalf("error = %v", err)
	}
}

func TestCheckerRejectsInvalidCompletionMode(t *testing.T) {
	checker := NewChecker()
	_, err := checker.Check(context.Background(), CheckRequest{
		Version:           "test",
		SpecName:          "SPEC.md",
		SpecText:          "# Spec\n",
		Profile:           "general",
		SeverityThreshold: "info",
		CompletionMode:    "bad",
		Source:            SourceCLI,
	})
	if err == nil || !strings.Contains(err.Error(), "completion mode") {
		t.Fatalf("error = %v", err)
	}
}

func TestCheckerConvergenceOnModeRejectsInvalidPreviousReport(t *testing.T) {
	checker := NewChecker()
	_, err := checker.Check(context.Background(), CheckRequest{
		Version:             "test",
		SpecName:            "SPEC.md",
		SpecText:            "TODO define authentication behavior.\n",
		Profile:             "general",
		SeverityThreshold:   "info",
		Preflight:           true,
		PreflightMode:       "only",
		ConvergenceFromText: `{`,
		ConvergenceMode:     "on",
		ConvergenceReport:   true,
		Source:              SourceCLI,
	})
	if err == nil || !strings.Contains(err.Error(), "previous convergence report") {
		t.Fatalf("error = %v", err)
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

func TestCheckerFullReviewAddsCompletion(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "fake")
	t.Setenv("SPECCRITIC_LLM_MODEL", "model")

	provider := &fakeProvider{content: `{"issues":[{"id":"ISSUE-0001","severity":"WARN","category":"MISSING_FAILURE_MODE","title":"Missing timeout behavior","description":"d","evidence":[{"path":"SPEC.md","line_start":3,"line_end":3,"quote":"The system must work."}],"impact":"i","recommendation":"r","blocking":false,"tags":[]}],"questions":[],"patches":[]}`}
	checker := &Checker{NewProvider: func(string) (llm.Provider, error) { return provider, nil }}
	result, err := checker.Check(context.Background(), CheckRequest{
		Version:                 "test",
		SpecName:                "SPEC.md",
		SpecText:                "# Spec\n\n## Purpose\nThe system must work.\n",
		Profile:                 "general",
		SeverityThreshold:       "info",
		MaxTokens:               1000,
		CompletionSuggestions:   true,
		CompletionMode:          "auto",
		CompletionTemplate:      "profile",
		CompletionMaxPatches:    8,
		CompletionOpenDecisions: true,
		Source:                  SourceCLI,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if result.Report.Meta.Completion == nil || result.Report.Meta.Completion.GeneratedPatches != 1 {
		t.Fatalf("completion meta = %#v patches=%#v", result.Report.Meta.Completion, result.Report.Patches)
	}
	if result.Report.Summary.WarnCount != 1 || result.Report.Summary.Verdict != schema.VerdictValidWithGaps {
		t.Fatalf("summary changed: %#v", result.Report.Summary)
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

func TestCheckerIncrementalUnchangedReusesWithoutLLMCall(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "fake")
	t.Setenv("SPECCRITIC_LLM_MODEL", "model")

	specText := "# Spec\n## Behavior\nThe API must return JSON.\n"
	s := specForTest("SPEC.md", specText)
	prevPath := writePreviousReport(t, s.Hash, "general", true, "info", "The API must return JSON.")
	provider := &fakeProvider{content: `{"issues":[],"questions":[],"patches":[]}`}
	checker := &Checker{NewProvider: func(string) (llm.Provider, error) { return provider, nil }}
	result, err := checker.Check(context.Background(), CheckRequest{
		Version:                         "test",
		SpecName:                        "SPEC.md",
		SpecText:                        specText,
		Profile:                         "general",
		Strict:                          true,
		SeverityThreshold:               "info",
		MaxTokens:                       1000,
		Preflight:                       false,
		Chunking:                        "off",
		IncrementalFrom:                 prevPath,
		IncrementalMode:                 "auto",
		IncrementalMaxChangeRatio:       0.35,
		IncrementalMaxRemapFailureRatio: 0.25,
		IncrementalReport:               true,
		Source:                          SourceWeb,
	})
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if len(provider.reqs) != 0 {
		t.Fatalf("provider calls = %d, want no incremental LLM call", len(provider.reqs))
	}
	if !hasIssue(result.Report.Issues, "ISSUE-0001") {
		t.Fatalf("issues = %#v", result.Report.Issues)
	}
	if result.Report.Meta.Incremental == nil || result.Report.Meta.Incremental.ReusedIssues != 1 {
		t.Fatalf("incremental meta = %#v", result.Report.Meta.Incremental)
	}
}

func TestCheckerIncrementalChangedNeedsBaseSpec(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "fake")
	t.Setenv("SPECCRITIC_LLM_MODEL", "model")

	previous := "# Spec\n## Behavior\nThe API must return JSON.\n"
	current := "# Spec\n## Behavior\nThe API must return XML.\n"
	prevPath := writePreviousReport(t, specForTest("SPEC.md", previous).Hash, "general", false, "info", "The API must return JSON.")
	provider := &fakeProvider{content: `{"issues":[],"questions":[],"patches":[]}`}
	checker := &Checker{NewProvider: func(string) (llm.Provider, error) { return provider, nil }}
	_, err := checker.Check(context.Background(), CheckRequest{
		Version:                         "test",
		SpecName:                        "SPEC.md",
		SpecText:                        current,
		Profile:                         "general",
		SeverityThreshold:               "info",
		MaxTokens:                       1000,
		Preflight:                       false,
		Chunking:                        "off",
		IncrementalFrom:                 prevPath,
		IncrementalMode:                 "on",
		IncrementalMaxChangeRatio:       0.35,
		IncrementalMaxRemapFailureRatio: 0.25,
		Source:                          SourceWeb,
	})
	if err == nil || !strings.Contains(err.Error(), "incremental-base") {
		t.Fatalf("error = %v, want missing incremental base", err)
	}
}

func TestCheckerIncrementalModelOutputErrorKind(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "fake")
	t.Setenv("SPECCRITIC_LLM_MODEL", "model")

	previous := "# Spec\n## Behavior\nThe API must return JSON.\n"
	current := "# Spec\n## Behavior\nThe API must return XML.\n"
	prevPath := writePreviousReport(t, specForTest("SPEC.md", previous).Hash, "general", false, "info", "The API must return JSON.")
	provider := &fakeProvider{content: `{"issues":[`}
	checker := &Checker{NewProvider: func(string) (llm.Provider, error) { return provider, nil }}
	_, err := checker.Check(context.Background(), CheckRequest{
		Version:                         "test",
		SpecName:                        "SPEC.md",
		SpecText:                        current,
		Profile:                         "general",
		SeverityThreshold:               "info",
		MaxTokens:                       1000,
		Preflight:                       false,
		Chunking:                        "off",
		IncrementalFrom:                 prevPath,
		IncrementalBaseText:             previous,
		IncrementalMode:                 "on",
		IncrementalMaxChangeRatio:       0.35,
		IncrementalMaxRemapFailureRatio: 0.25,
		IncrementalContextLines:         20,
		Source:                          SourceWeb,
	})
	var appErr *Error
	if err == nil || !errors.As(err, &appErr) || appErr.Kind != ErrorModelOutput {
		t.Fatalf("error = %#v, want ErrorModelOutput", err)
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

func previousConvergenceReportJSON() string {
	return `{
		"tool":"speccritic",
		"version":"test",
		"input":{
			"spec_file":"SPEC.md",
			"spec_hash":"sha256:old",
			"context_files":[],
			"profile":"general",
			"strict":false,
			"severity_threshold":"info"
		},
		"summary":{
			"verdict":"INVALID",
			"score":80,
			"critical_count":1,
			"warn_count":0,
			"info_count":0
		},
		"issues":[{
			"id":"ISSUE-0001",
			"severity":"CRITICAL",
			"category":"AMBIGUOUS_BEHAVIOR",
			"title":"Prior issue",
			"description":"d",
			"evidence":[{"path":"SPEC.md","line_start":1,"line_end":1,"quote":"old"}],
			"impact":"i",
			"recommendation":"r",
			"blocking":true,
			"tags":[]
		}],
		"questions":[],
		"patches":[],
		"meta":{"model":"test:model","temperature":0.2}
	}`
}

func specForTest(name, raw string) *spec.Spec {
	return spec.New(name, raw)
}

func writePreviousReport(t *testing.T, specHash, profile string, strict bool, threshold string, quote string) string {
	t.Helper()
	path := t.TempDir() + "/previous.json"
	strictRaw := "false"
	if strict {
		strictRaw = "true"
	}
	content := fmt.Sprintf(`{
  "tool": "speccritic",
  "version": "1.0",
  "input": {
    "spec_file": "SPEC.md",
    "spec_hash": %q,
    "context_files": [],
    "profile": %q,
    "strict": %s,
    "severity_threshold": %q
  },
  "summary": {"verdict":"VALID_WITH_GAPS","score":93,"critical_count":0,"warn_count":1,"info_count":0},
  "issues": [{
    "id": "ISSUE-0001",
    "severity": "WARN",
    "category": "UNSPECIFIED_CONSTRAINT",
    "title": "Finding",
    "description": "desc",
    "evidence": [{"path":"SPEC.md","line_start":3,"line_end":3,"quote":%q}],
    "impact": "impact",
    "recommendation": "rec",
    "blocking": false,
    "tags": []
  }],
  "questions": [],
  "patches": [],
  "meta": {"model":"fake:model","temperature":0.2}
}`, specHash, profile, strictRaw, threshold, quote)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// chunkAwareProvider is called from concurrent chunk workers, so its counters
// are guarded by mu.
type chunkAwareProvider struct {
	emptyChunks bool

	mu          sync.Mutex
	chunkCalls  int
	synthCalls  int
	singleCalls int
}

func (p *chunkAwareProvider) Complete(_ context.Context, req *llm.Request) (*llm.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch {
	case strings.Contains(req.UserPromptCachedPrefix, "Analyze cross-section risks"):
		p.synthCalls++
		return &llm.Response{Content: `{"issues":[],"questions":[],"patches":[]}`, Model: "fake:synthesis"}, nil
	case strings.Contains(req.UserPrompt, chunkIDAttr):
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

// chunkIDAttr opens the chunk element of a chunk review prompt.
const chunkIDAttr = `<chunk id="`

func chunkIDFromPrompt(prompt string) string {
	start := strings.Index(prompt, chunkIDAttr)
	if start < 0 {
		return "CHUNK-0001-L1-L1"
	}
	start += len(chunkIDAttr)
	end := strings.Index(prompt[start:], `"`)
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

// usageProvider answers each call with the next scripted response and a fixed
// usage report.
type usageProvider struct {
	responses []string
	calls     int
}

func (p *usageProvider) Complete(_ context.Context, _ *llm.Request) (*llm.Response, error) {
	content := p.responses[min(p.calls, len(p.responses)-1)]
	p.calls++
	return &llm.Response{
		Content: content,
		Model:   "fake:model",
		Usage:   llm.Usage{InputTokens: 100, OutputTokens: 10, CacheReadTokens: 5, CacheWriteTokens: 1},
	}, nil
}

func usageCheckRequest(errw *strings.Builder) CheckRequest {
	return CheckRequest{
		Version:           "test",
		SpecName:          "SPEC.md",
		SpecText:          "The system must do one thing.\n",
		Profile:           "general",
		SeverityThreshold: "info",
		MaxTokens:         1000,
		Verbose:           true,
		Source:            SourceWeb,
		ErrWriter:         errw,
	}
}

func TestCheckerReportsUsageForSingleCall(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "fake")
	t.Setenv("SPECCRITIC_LLM_MODEL", "model")

	provider := &usageProvider{responses: []string{`{"issues":[],"questions":[],"patches":[]}`}}
	checker := &Checker{NewProvider: func(string) (llm.Provider, error) { return provider, nil }}
	var errw strings.Builder

	result, err := checker.Check(context.Background(), usageCheckRequest(&errw))
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	usage := result.Report.Meta.Usage
	if usage == nil {
		t.Fatal("meta.usage is nil, want usage for an LLM-backed review")
	}
	if usage.Calls != 1 || usage.RepairCalls != 0 || usage.ContinuationCalls != 0 {
		t.Errorf("calls=%d repair=%d continuation=%d, want 1/0/0", usage.Calls, usage.RepairCalls, usage.ContinuationCalls)
	}
	if usage.InputTokens != 100 || usage.OutputTokens != 10 || usage.CacheReadTokens != 5 || usage.CacheWriteTokens != 1 {
		t.Errorf("tokens = %+v, want 100 input, 10 output, 5 cache read, 1 cache write", *usage)
	}
	if !strings.Contains(errw.String(), "INFO: LLM usage: 1 call(s) (0 repair, 0 continuation, 0 truncated); tokens: 100 input, 5 cache read, 1 cache write, 10 output;") {
		t.Errorf("verbose log missing the usage line:\n%s", errw.String())
	}
}

func TestCheckerReportsRepairCallsInUsage(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "fake")
	t.Setenv("SPECCRITIC_LLM_MODEL", "model")

	provider := &usageProvider{responses: []string{`not json`, `{"issues":[],"questions":[],"patches":[]}`}}
	checker := &Checker{NewProvider: func(string) (llm.Provider, error) { return provider, nil }}
	var errw strings.Builder

	result, err := checker.Check(context.Background(), usageCheckRequest(&errw))
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	usage := result.Report.Meta.Usage
	if usage == nil || usage.Calls != 2 || usage.RepairCalls != 1 || usage.InputTokens != 200 || usage.OutputTokens != 20 {
		t.Fatalf("usage = %+v, want 2 calls, 1 repair, tokens for both calls", usage)
	}
}

func TestCheckerLogsUsageWhenTheReviewFails(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "fake")
	t.Setenv("SPECCRITIC_LLM_MODEL", "model")

	provider := &usageProvider{responses: []string{`not json`}}
	checker := &Checker{NewProvider: func(string) (llm.Provider, error) { return provider, nil }}
	var errw strings.Builder

	if _, err := checker.Check(context.Background(), usageCheckRequest(&errw)); err == nil {
		t.Fatal("expected invalid model output error")
	}
	if !strings.Contains(errw.String(), "LLM usage: 2 call(s) (1 repair,") {
		t.Errorf("verbose log should still report what the failed review used:\n%s", errw.String())
	}
}

func TestCheckerReportsUsageAcrossChunkCalls(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "fake")
	t.Setenv("SPECCRITIC_LLM_MODEL", "model")

	provider := &chunkAwareProvider{}
	checker := &Checker{NewProvider: func(string) (llm.Provider, error) { return provider, nil }}
	var errw strings.Builder
	req := usageCheckRequest(&errw)
	req.SpecText = longSpec(200)
	req.Chunking = "on"
	req.ChunkLines = 40
	req.ChunkConcurrency = 2

	result, err := checker.Check(context.Background(), req)
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if provider.chunkCalls < 2 {
		t.Fatalf("chunk calls = %d, want a multi-chunk review", provider.chunkCalls)
	}
	usage := result.Report.Meta.Usage
	if usage == nil || usage.Calls != provider.chunkCalls+provider.synthCalls {
		t.Fatalf("usage = %+v, want %d chunk + %d synthesis calls", usage, provider.chunkCalls, provider.synthCalls)
	}
}

func TestCheckerOmitsUsageWithoutLLMCalls(t *testing.T) {
	checker := &Checker{NewProvider: func(string) (llm.Provider, error) {
		t.Fatal("preflight-only review must not create a provider")
		return nil, nil
	}}
	var errw strings.Builder
	req := usageCheckRequest(&errw)
	req.Preflight = true
	req.PreflightMode = "only"

	result, err := checker.Check(context.Background(), req)
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	if result.Report.Meta.Usage != nil {
		t.Fatalf("meta.usage = %+v, want nil when no LLM call was made", result.Report.Meta.Usage)
	}
	if strings.Contains(errw.String(), "LLM usage") {
		t.Errorf("verbose log reports usage for a review with no calls:\n%s", errw.String())
	}
}

// settingsProvider records the effort of every request.
type settingsProvider struct {
	inner llm.Provider

	mu      sync.Mutex
	efforts []string
}

func (p *settingsProvider) Complete(ctx context.Context, req *llm.Request) (*llm.Response, error) {
	p.mu.Lock()
	p.efforts = append(p.efforts, req.Effort)
	p.mu.Unlock()
	return p.inner.Complete(ctx, req)
}

func TestCheckerPassesEffortToEveryCall(t *testing.T) {
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "fake")
	t.Setenv("SPECCRITIC_LLM_MODEL", "model")

	cases := map[string]func(*CheckRequest){
		"single call": func(*CheckRequest) {},
		"chunked": func(req *CheckRequest) {
			req.SpecText = longSpec(200)
			req.Chunking = "on"
			req.ChunkLines = 40
		},
	}
	for name, configure := range cases {
		t.Run(name, func(t *testing.T) {
			provider := &settingsProvider{inner: &chunkAwareProvider{}}
			checker := &Checker{NewProvider: func(string) (llm.Provider, error) { return provider, nil }}
			var errw strings.Builder
			req := usageCheckRequest(&errw)
			req.Effort = "high"
			configure(&req)

			result, err := checker.Check(context.Background(), req)
			if err != nil {
				t.Fatalf("Check returned error: %v", err)
			}
			if len(provider.efforts) == 0 {
				t.Fatal("no LLM calls were made")
			}
			for i, effort := range provider.efforts {
				if effort != "high" {
					t.Errorf("call %d effort = %q, want high", i, effort)
				}
			}
			if result.Report.Meta.Effort != "high" {
				t.Errorf("meta.effort = %q, want high", result.Report.Meta.Effort)
			}
		})
	}
}

func TestCheckerRejectsUnknownEffort(t *testing.T) {
	checker := &Checker{NewProvider: func(string) (llm.Provider, error) {
		t.Fatal("an invalid request must not create a provider")
		return nil, nil
	}}
	var errw strings.Builder
	req := usageCheckRequest(&errw)
	req.Effort = "turbo"

	_, err := checker.Check(context.Background(), req)
	var appErr *Error
	if !errors.As(err, &appErr) || appErr.Kind != ErrorInput || !strings.Contains(err.Error(), `effort "turbo"`) {
		t.Fatalf("error = %v, want an input error naming the effort", err)
	}
}
