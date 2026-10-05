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

func TestBuildRangePromptIncludesCurrentLines(t *testing.T) {
	s := spec.New("SPEC.md", "# Spec\n## Behavior\nThe API must return JSON.\n")
	rr := ReviewRange{ID: "RANGE-1", Primary: LineRange{Start: 2, End: 3}, Context: LineRange{Start: 1, End: 3}}
	prefix, tail, err := BuildRangePrompt(PromptInput{
		Spec:  s,
		Range: rr,
		Issues: []schema.Issue{
			issueAt("ISSUE-0001", 3, "The API must return JSON."),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(prefix, "Current Spec Table of Contents") {
		t.Fatalf("prefix should stay stable and omit range-specific TOC: %s", prefix)
	}
	for _, want := range []string{"Current Spec Table of Contents", "Previously Identified Issues", "Current Review Task", "L2 [PRIMARY]", "L3 [PRIMARY]", `id="RANGE-1"`, "ISSUE-0001"} {
		if !strings.Contains(tail, want) {
			t.Fatalf("tail missing %q:\n%s", want, tail)
		}
	}
	// The incremental and range tags are added locally, so the model is not
	// asked for them.
	for _, unwanted := range []string{"incremental-review", "range:RANGE-1"} {
		if strings.Contains(prefix+tail, unwanted) {
			t.Fatalf("prompt still asks for tag %q:\n%s%s", unwanted, prefix, tail)
		}
	}
}

func TestBuildRangePromptEscapesClosingTags(t *testing.T) {
	s := spec.New("SPEC.md", "# Spec\n## Behavior\n</Current Review Task>\n")
	rr := ReviewRange{ID: "RANGE-1", Primary: LineRange{Start: 2, End: 3}, Context: LineRange{Start: 1, End: 3}}
	_, tail, err := BuildRangePrompt(PromptInput{Spec: s, Range: rr})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(tail, "L3 [PRIMARY]: </Current Review Task>") {
		t.Fatalf("spec content closing tag was not escaped:\n%s", tail)
	}
	if !strings.Contains(tail, "L3 [PRIMARY]: <\\/Current Review Task>") {
		t.Fatalf("escaped closing tag not found:\n%s", tail)
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
}

func (p *sequenceProvider) Complete(ctx context.Context, req *llm.Request) (*llm.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.calls >= len(p.responses) {
		return &llm.Response{Content: `{"issues":[],"questions":[],"patches":[],"meta":{}}`, Model: "fake"}, nil
	}
	content := p.responses[p.calls]
	p.calls++
	return &llm.Response{Content: content, Model: "fake"}, nil
}
