package render

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dshills/speccritic/internal/schema"
)

func sampleReport() *schema.Report {
	return &schema.Report{
		Tool:    "speccritic",
		Version: "1.0",
		Summary: schema.Summary{
			Verdict:       schema.VerdictInvalid,
			Score:         60,
			CriticalCount: 2,
			WarnCount:     1,
			InfoCount:     0,
		},
		Issues: []schema.Issue{
			{
				ID:             "ISSUE-0001",
				Severity:       schema.SeverityCritical,
				Category:       schema.CategoryNonTestableRequirement,
				Title:          "Performance requirement not measurable",
				Description:    "The spec requires 'fast' without metrics.",
				Evidence:       []schema.Evidence{{Path: "SPEC.md", LineStart: 10, LineEnd: 10, Quote: "must be fast"}},
				Impact:         "Cannot verify.",
				Recommendation: "Define latency target.",
				Blocking:       true,
				Tags:           []string{},
			},
		},
		Questions: []schema.Question{},
		Patches:   []schema.Patch{},
		Meta:      schema.Meta{Model: "anthropic:claude-sonnet-4-6", Temperature: 0.2},
	}
}

func TestNewRenderer_JSON(t *testing.T) {
	r, err := NewRenderer("json")
	if err != nil {
		t.Fatalf("NewRenderer json: %v", err)
	}
	out, err := r.Render(sampleReport())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	var decoded schema.Report
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("output is not valid JSON: %v\noutput: %s", err, out)
	}
	if decoded.Summary.Verdict != schema.VerdictInvalid {
		t.Errorf("verdict mismatch: got %q", decoded.Summary.Verdict)
	}
}

func TestNewRenderer_Markdown(t *testing.T) {
	r, err := NewRenderer("md")
	if err != nil {
		t.Fatalf("NewRenderer md: %v", err)
	}
	out, err := r.Render(sampleReport())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "# SpecCritic Report") {
		t.Errorf("markdown missing header: %q", s)
	}
	if !strings.Contains(s, "INVALID") {
		t.Errorf("markdown missing verdict: %q", s)
	}
	if !strings.Contains(s, "ISSUE-0001") {
		t.Errorf("markdown missing issue ID: %q", s)
	}
}

func TestNewRenderer_JSONProducesValidJSON(t *testing.T) {
	r, err := NewRenderer("json")
	if err != nil {
		t.Fatalf("NewRenderer json: %v", err)
	}
	out, err := r.Render(sampleReport())
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(out) {
		t.Errorf("json renderer produced invalid JSON: %s", out)
	}
}

func TestNewRenderer_UnknownFormat(t *testing.T) {
	_, err := NewRenderer("xml")
	if err == nil {
		t.Error("expected error for unknown format, got nil")
	}
}
