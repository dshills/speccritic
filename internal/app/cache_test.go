package app

import (
	"context"
	"strings"
	"testing"

	"github.com/dshills/speccritic/internal/cache"
	"github.com/dshills/speccritic/internal/llm"
	"github.com/dshills/speccritic/internal/schema"
)

const cacheTestCritical = `{"issues":[{"id":"ISSUE-0001","severity":"CRITICAL","category":"NON_TESTABLE_REQUIREMENT","title":"Not testable","description":"d","evidence":[{"line_start":1,"line_end":1,"quote":"The system must do one thing."}],"impact":"i","recommendation":"r","blocking":true,"tags":[]}],"questions":[],"patches":[]}`

func cachingChecker(t *testing.T, fn func(*llm.Request) string) (*Checker, *recordingProvider) {
	t.Helper()
	t.Setenv("SPECCRITIC_LLM_PROVIDER", "fake")
	t.Setenv("SPECCRITIC_LLM_MODEL", "model")
	provider := &recordingProvider{fn: fn}
	return &Checker{
		NewProvider: func(string) (llm.Provider, error) { return provider, nil },
		Cache:       cache.New(t.TempDir()),
	}, provider
}

func mustCheck(t *testing.T, c *Checker, req CheckRequest) *schema.Report {
	t.Helper()
	result, err := c.Check(context.Background(), req)
	if err != nil {
		t.Fatalf("Check returned error: %v", err)
	}
	return result.Report
}

func TestCheckerServesRepeatReviewFromCache(t *testing.T) {
	checker, provider := cachingChecker(t, func(*llm.Request) string { return cacheTestCritical })
	var errw strings.Builder
	req := usageCheckRequest(&errw)
	req.Verify = VerifyOff

	first := mustCheck(t, checker, req)
	if first.Meta.Cache == nil || first.Meta.Cache.Hit || first.Meta.Cache.Key == "" {
		t.Fatalf("first meta.cache = %+v, want a stored miss", first.Meta.Cache)
	}
	second := mustCheck(t, checker, req)
	if len(provider.reqs) != 1 {
		t.Fatalf("calls = %d, want the repeat served without a model call", len(provider.reqs))
	}
	if c := second.Meta.Cache; c == nil || !c.Hit || c.Key != first.Meta.Cache.Key || c.StoredAt == "" {
		t.Fatalf("second meta.cache = %+v, want a hit on the same key", c)
	}
	if second.Summary != first.Summary || len(second.Issues) != 1 || second.Meta.Usage != nil {
		t.Fatalf("cached report = %+v usage %+v, want the stored review and no usage", second.Summary, second.Meta.Usage)
	}
	if !strings.Contains(errw.String(), "Review cache hit") {
		t.Errorf("verbose log does not mention the hit:\n%s", errw.String())
	}
}

func TestCheckerCacheKeyCoversWhatShapesTheReview(t *testing.T) {
	checker, provider := cachingChecker(t, func(*llm.Request) string { return `{"issues":[],"questions":[],"patches":[]}` })
	var errw strings.Builder
	base := usageCheckRequest(&errw)
	base.Verify = VerifyOff
	mustCheck(t, checker, base)

	changes := map[string]func(*CheckRequest){
		"spec text":  func(r *CheckRequest) { r.SpecText = "The system must do two things.\n" },
		"model":      func(r *CheckRequest) { r.LLMModel = "other" },
		"effort":     func(r *CheckRequest) { r.Effort = "high" },
		"max tokens": func(r *CheckRequest) { r.MaxTokens = 2000 },
		"profile":    func(r *CheckRequest) { r.Profile = "backend-api" },
		"strict":     func(r *CheckRequest) { r.Strict = true },
		"verify":     func(r *CheckRequest) { r.Verify = VerifyAuto },
		"patches":    func(r *CheckRequest) { r.Patches = PatchesOff },
	}
	for name, change := range changes {
		before := len(provider.reqs)
		req := base
		change(&req)
		report := mustCheck(t, checker, req)
		if len(provider.reqs) == before || report.Meta.Cache == nil || report.Meta.Cache.Hit {
			t.Errorf("changing %s was served from the cache", name)
		}
	}

	// How many chunk calls run at once does not change the review.
	before := len(provider.reqs)
	req := base
	req.ChunkConcurrency = 7
	if report := mustCheck(t, checker, req); len(provider.reqs) != before || !report.Meta.Cache.Hit {
		t.Error("changing chunk concurrency missed the cache")
	}
}

func TestCheckerNoCacheNeitherReadsNorWrites(t *testing.T) {
	checker, provider := cachingChecker(t, func(*llm.Request) string { return `{"issues":[],"questions":[],"patches":[]}` })
	var errw strings.Builder
	req := usageCheckRequest(&errw)
	req.NoCache = true

	if r := mustCheck(t, checker, req); r.Meta.Cache != nil {
		t.Fatalf("meta.cache = %+v with caching off", r.Meta.Cache)
	}
	req.NoCache = false
	if r := mustCheck(t, checker, req); len(provider.reqs) != 2 || r.Meta.Cache.Hit {
		t.Fatalf("calls = %d, want the --no-cache run to have stored nothing", len(provider.reqs))
	}
	req.NoCache = true
	mustCheck(t, checker, req)
	if len(provider.reqs) != 3 {
		t.Fatalf("calls = %d, want --no-cache to ignore the stored review", len(provider.reqs))
	}
}

func TestCheckerDoesNotCacheAFailedVerification(t *testing.T) {
	checker, provider := cachingChecker(t, func(req *llm.Request) string {
		if strings.Contains(req.UserPrompt, "<critical_findings>") {
			return "not json"
		}
		return cacheTestCritical
	})
	var errw strings.Builder
	req := usageCheckRequest(&errw)
	req.Verify = VerifyAuto

	first := mustCheck(t, checker, req)
	if v := first.Meta.Verification; v == nil || v.Status != schema.VerificationFailed {
		t.Fatalf("meta.verification = %+v, want a failed verification", v)
	}
	if first.Meta.Cache != nil {
		t.Fatalf("meta.cache = %+v, want the unverified review left out of the cache", first.Meta.Cache)
	}
	calls := len(provider.reqs)
	mustCheck(t, checker, req)
	if len(provider.reqs) == calls {
		t.Fatal("a review whose verification failed was served from the cache")
	}
	if !strings.Contains(errw.String(), "Review not cached") {
		t.Errorf("verbose log does not say why the review was not cached:\n%s", errw.String())
	}
}

func TestCheckerIncrementalRunsBypassTheCache(t *testing.T) {
	specText := "# Spec\n## Behavior\nThe API must return JSON.\n"
	s := specForTest("SPEC.md", specText)
	prevPath := writePreviousReport(t, s.Hash, "general", true, "info", "The API must return JSON.")
	checker, _ := cachingChecker(t, func(*llm.Request) string { return `{"issues":[],"questions":[],"patches":[]}` })
	req := CheckRequest{
		Version:                         "test",
		SpecName:                        "SPEC.md",
		SpecText:                        specText,
		Profile:                         "general",
		Strict:                          true,
		SeverityThreshold:               "info",
		MaxTokens:                       1000,
		Chunking:                        "off",
		IncrementalFrom:                 prevPath,
		IncrementalMode:                 "auto",
		IncrementalMaxChangeRatio:       0.35,
		IncrementalMaxRemapFailureRatio: 0.25,
		Source:                          SourceWeb,
	}
	if r := mustCheck(t, checker, req); r.Meta.Cache != nil {
		t.Fatalf("meta.cache = %+v, want incremental runs kept out of the cache", r.Meta.Cache)
	}
}
