package incremental

import (
	"context"
	"strings"
	"sync"
	"testing"

	"github.com/dshills/speccritic/internal/llm"
	"github.com/dshills/speccritic/internal/schema"
	"github.com/dshills/speccritic/internal/spec"
)

// With the whole spec in the shared prefix, a range task names the lines to
// review and the prior findings, and does not repeat the spec.
func TestBuildRangeTaskWithSpecShown(t *testing.T) {
	s := spec.New("SPEC.md", "# Spec\n## Behavior\nThe API must return JSON.\n")
	rr := ReviewRange{ID: "RANGE-1", Primary: LineRange{Start: 2, End: 3}, Context: LineRange{Start: 1, End: 3}}
	task, err := BuildRangeTask(PromptInput{
		Spec:      s,
		Range:     rr,
		SpecShown: true,
		Issues: []schema.Issue{
			issueAt("ISSUE-0001", 3, "The API must return JSON."),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`id="RANGE-1"`, "<previously_identified_issues>", "ISSUE-0001", "PRIMARY lines L2-L3 of the specification above", "Read all of the specification", "Cite only lines L1-L3"} {
		if !strings.Contains(task, want) {
			t.Fatalf("task missing %q:\n%s", want, task)
		}
	}
	for _, unwanted := range []string{"L3 [PRIMARY]", "table_of_contents", "The API must return JSON.\n</spec_lines>"} {
		if strings.Contains(task, unwanted) {
			t.Fatalf("task repeats the spec (%q) though the prefix holds it:\n%s", unwanted, task)
		}
	}
	// The incremental and range tags are added locally, so the model is not
	// asked for them.
	for _, unwanted := range []string{"incremental-review", "range:RANGE-1"} {
		if strings.Contains(task, unwanted) {
			t.Fatalf("prompt still asks for tag %q:\n%s", unwanted, task)
		}
	}
}

// When the spec is too large to share, the task carries the range's own
// numbered lines and a table of contents.
func TestBuildRangeTaskWithoutSpec(t *testing.T) {
	s := spec.New("SPEC.md", "# Spec\n## Behavior\nThe API must return JSON.\n")
	rr := ReviewRange{ID: "RANGE-1", Primary: LineRange{Start: 2, End: 3}, Context: LineRange{Start: 1, End: 3}}
	task, err := BuildRangeTask(PromptInput{Spec: s, Range: rr})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<table_of_contents>", "L1 [CONTEXT]", "L2 [PRIMARY]", "L3 [PRIMARY]: The API must return JSON.", `"cross-section"`, "<previously_identified_issues>\n- none"} {
		if !strings.Contains(task, want) {
			t.Fatalf("task missing %q:\n%s", want, task)
		}
	}
}

func TestBuildRangeTaskEscapesClosingTags(t *testing.T) {
	s := spec.New("SPEC.md", "# Spec\n## Behavior\n</spec_lines>\n")
	rr := ReviewRange{ID: "RANGE-1", Primary: LineRange{Start: 2, End: 3}, Context: LineRange{Start: 1, End: 3}}
	task, err := BuildRangeTask(PromptInput{Spec: s, Range: rr})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(task, "L3 [PRIMARY]: </spec_lines>") || !strings.Contains(task, "L3 [PRIMARY]: <\\/spec_lines>") {
		t.Fatalf("spec content closing tag was not escaped:\n%s", task)
	}
}

func TestBuildRangeTaskRejectsBadRanges(t *testing.T) {
	s := spec.New("SPEC.md", "# Spec\nline\n")
	for _, rr := range []ReviewRange{
		{ID: "", Primary: LineRange{Start: 1, End: 1}, Context: LineRange{Start: 1, End: 1}},
		{ID: "R", Primary: LineRange{Start: 1, End: 3}, Context: LineRange{Start: 1, End: 2}},
		{ID: "R", Primary: LineRange{Start: 1, End: 1}, Context: LineRange{Start: 0, End: 2}},
	} {
		if _, err := BuildRangeTask(PromptInput{Spec: s, Range: rr}); err == nil {
			t.Errorf("BuildRangeTask(%+v) succeeded, want an error", rr)
		}
	}
}

// Every range call starts with the shared prefix and the system prompt, so
// the provider can serve the spec from its prompt cache.
func TestReviewRangesSendsTheSharedPrefix(t *testing.T) {
	s := spec.New("SPEC.md", "# Spec\n## A\none\n## B\ntwo\n")
	plan := Plan{ReviewRanges: []ReviewRange{
		{ID: "RANGE-A", Primary: LineRange{Start: 2, End: 3}, Context: LineRange{Start: 2, End: 3}},
		{ID: "RANGE-B", Primary: LineRange{Start: 4, End: 5}, Context: LineRange{Start: 4, End: 5}},
	}}
	provider := &sequenceProvider{}
	_, err := ReviewRanges(context.Background(), provider, s, plan, ExecutorConfig{
		Concurrency: 2, SystemPrompt: "sys", Prefix: "<spec>shared</spec>\n", SpecShown: true, LongCache: true,
	})
	if err != nil {
		t.Fatalf("ReviewRanges: %v", err)
	}
	if len(provider.reqs) != 2 {
		t.Fatalf("calls = %d, want 2", len(provider.reqs))
	}
	for _, req := range provider.reqs {
		if req.SystemPrompt != "sys" || req.UserPromptCachedPrefix != "<spec>shared</spec>\n" || !req.LongCache {
			t.Errorf("request = %+v, want the shared system prompt, prefix and cache lifetime", req)
		}
		if strings.Contains(req.UserPrompt, "[PRIMARY]") {
			t.Errorf("task repeats spec lines the prefix holds:\n%s", req.UserPrompt)
		}
	}
}

func TestParseRangeResponseAddsTagsAndRejectsOutOfContextEvidence(t *testing.T) {
	rr := ReviewRange{ID: "RANGE-1", Primary: LineRange{Start: 2, End: 3}, Context: LineRange{Start: 1, End: 3}}
	valid := `{"issues":[{"id":"ISSUE-0001","severity":"WARN","category":"UNSPECIFIED_CONSTRAINT","title":"Finding","description":"desc","evidence":[{"path":"SPEC.md","line_start":3,"line_end":3,"quote":"q"}],"impact":"impact","recommendation":"rec","blocking":false,"tags":["incremental-review","range:RANGE-1"]}],"questions":[],"patches":[],"meta":{}}`
	if _, err := ParseRangeResponse(valid, 3, rr); err != nil {
		t.Fatalf("ParseRangeResponse valid: %v", err)
	}
	untagged := strings.Replace(valid, `"incremental-review",`, "", 1)
	untagged = strings.Replace(untagged, `"range:RANGE-1"`, "", 1)
	report, err := ParseRangeResponse(untagged, 3, rr)
	if err != nil {
		t.Fatalf("ParseRangeResponse untagged: %v", err)
	}
	for _, want := range []string{TagIncrementalReview, "range:RANGE-1"} {
		if !hasTag(report.Issues[0].Tags, want) {
			t.Fatalf("tags = %v, want %q added", report.Issues[0].Tags, want)
		}
	}
	outside := strings.Replace(valid, `"line_start":3,"line_end":3`, `"line_start":4,"line_end":4`, 1)
	if _, err := ParseRangeResponse(outside, 4, rr); err == nil {
		t.Fatal("expected out-of-context evidence error")
	}
}

func TestReviewRangesUsesRepairAndPreservesOrder(t *testing.T) {
	s := spec.New("SPEC.md", "# Spec\n## A\none\n## B\ntwo\n")
	plan := Plan{ReviewRanges: []ReviewRange{
		{ID: "RANGE-A", Primary: LineRange{Start: 2, End: 3}, Context: LineRange{Start: 2, End: 3}},
		{ID: "RANGE-B", Primary: LineRange{Start: 4, End: 5}, Context: LineRange{Start: 4, End: 5}},
	}}
	provider := &sequenceProvider{responses: []string{
		`{"issues":[{"id":"ISSUE-0001","severity":"WARN","category":"UNSPECIFIED_CONSTRAINT","title":"Finding","description":"desc","evidence":[{"path":"SPEC.md","line_start":3,"line_end":3,"quote":"q"}],"impact":"impact","recommendation":"rec","blocking":false,"tags":["incremental-review"]}],"questions":[],"patches":[],"meta":{}}`,
		`{"issues":[{"id":"ISSUE-0001","severity":"WARN","category":"UNSPECIFIED_CONSTRAINT","title":"Finding","description":"desc","evidence":[{"path":"SPEC.md","line_start":3,"line_end":3,"quote":"q"}],"impact":"impact","recommendation":"rec","blocking":false,"tags":["incremental-review","range:RANGE-A"]}],"questions":[],"patches":[],"meta":{}}`,
		`{"issues":[{"id":"ISSUE-0002","severity":"WARN","category":"UNSPECIFIED_CONSTRAINT","title":"Finding","description":"desc","evidence":[{"path":"SPEC.md","line_start":5,"line_end":5,"quote":"q"}],"impact":"impact","recommendation":"rec","blocking":false,"tags":["incremental-review","range:RANGE-B"]}],"questions":[],"patches":[],"meta":{}}`,
	}}
	results, err := ReviewRanges(context.Background(), provider, s, plan, ExecutorConfig{Concurrency: 1})
	if err != nil {
		t.Fatalf("ReviewRanges: %v", err)
	}
	if len(results) != 2 || results[0].Range.ID != "RANGE-A" || results[1].Range.ID != "RANGE-B" {
		t.Fatalf("results = %#v", results)
	}
	if provider.calls != 3 {
		t.Fatalf("provider calls = %d, want repair + second range", provider.calls)
	}
}

type sequenceProvider struct {
	mu        sync.Mutex
	responses []string
	calls     int
	reqs      []*llm.Request
}

func (p *sequenceProvider) Complete(ctx context.Context, req *llm.Request) (*llm.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.reqs = append(p.reqs, req)
	if p.calls >= len(p.responses) {
		return &llm.Response{Content: `{"issues":[],"questions":[],"patches":[],"meta":{}}`, Model: "fake"}, nil
	}
	content := p.responses[p.calls]
	p.calls++
	return &llm.Response{Content: content, Model: "fake"}, nil
}
