package render

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/dshills/speccritic/internal/llm"
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
		Meta:      schema.Meta{Model: llm.DefaultProvider + ":" + llm.DefaultModel, Temperature: 0.2},
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

func TestNewRenderer_MarkdownSeparatesPreflightFindings(t *testing.T) {
	report := sampleReport()
	report.Issues = append([]schema.Issue{
		{
			ID:             "PREFLIGHT-TODO-001",
			Severity:       schema.SeverityCritical,
			Category:       schema.CategoryUnspecifiedConstraint,
			Title:          "Placeholder text remains in spec",
			Description:    "The spec contains placeholder text.",
			Evidence:       []schema.Evidence{{Path: "SPEC.md", LineStart: 3, LineEnd: 3, Quote: "TODO"}},
			Impact:         "Cannot implement safely.",
			Recommendation: "Replace the placeholder.",
			Blocking:       true,
			Tags:           []string{"preflight", "preflight-rule:PREFLIGHT-TODO-001"},
		},
	}, report.Issues...)

	r, err := NewRenderer("md")
	if err != nil {
		t.Fatalf("NewRenderer md: %v", err)
	}
	out, err := r.Render(report)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	s := string(out)
	if !strings.Contains(s, "### Preflight Findings") {
		t.Fatalf("markdown missing preflight subsection: %q", s)
	}
	if !strings.Contains(s, "### LLM Findings") {
		t.Fatalf("markdown missing LLM subsection: %q", s)
	}
	if strings.Index(s, "PREFLIGHT-TODO-001") > strings.Index(s, "ISSUE-0001") {
		t.Fatalf("preflight finding should render before LLM finding: %q", s)
	}
}

func TestNewRenderer_MarkdownRejectsNilReport(t *testing.T) {
	r, err := NewRenderer("md")
	if err != nil {
		t.Fatalf("NewRenderer md: %v", err)
	}
	if _, err := r.Render(nil); err == nil {
		t.Fatal("expected error for nil report")
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
