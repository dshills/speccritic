package render

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/dshills/speccritic/internal/schema"
)

type markdownRenderer struct{}

type markdownView struct {
	*schema.Report
	PreflightIssues []schema.Issue
	LLMIssues       []schema.Issue
	HasIssues       bool
}

var mdTemplate = template.Must(template.New("report").Parse(`# SpecCritic Report

**Verdict:** {{ .Summary.Verdict }}
**Score:** {{ .Summary.Score }}/100
**Critical:** {{ .Summary.CriticalCount }} | **Warn:** {{ .Summary.WarnCount }} | **Info:** {{ .Summary.InfoCount }}
> Note: counts reflect all findings; --severity-threshold may hide some from this output.
{{ if .HasIssues }}
---

## Issues
{{ if .PreflightIssues }}
### Preflight Findings
{{ range .PreflightIssues }}
{{ template "issue" . }}
{{ end }}{{ end }}{{ if .LLMIssues }}
### LLM Findings
{{ range .LLMIssues }}
{{ template "issue" . }}
{{ end }}{{ end }}{{ end }}{{ if .Questions }}
---

## Clarification Questions
{{ range .Questions }}
### {{ .ID }} · {{ .Severity }}
{{ .Question }}

*Why needed:* {{ .WhyNeeded }}
{{ range .Evidence }}
> {{ .Path }} L{{ .LineStart }}–{{ .LineEnd }}: "{{ .Quote }}"
{{ end }}{{ end }}{{ end }}{{ if .Patches }}
---

## Suggested Patches
{{ range .Patches }}
**{{ .IssueID }}** (see --patch-out for machine-applicable diff)

Before:
` + "```" + `
{{ .Before }}
` + "```" + `
After:
` + "```" + `
{{ .After }}
` + "```" + `
{{ end }}{{ end }}
---
*Model: {{ .Meta.Model }} | Temperature: {{ .Meta.Temperature }}*
{{ define "issue" }}
#### {{ .ID }} · {{ .Severity }} · {{ .Category }}
**{{ .Title }}**

{{ .Description }}
{{ range .Evidence }}
> {{ .Path }} L{{ .LineStart }}–{{ .LineEnd }}: "{{ .Quote }}"
{{ end }}
**Impact:** {{ .Impact }}
**Recommendation:** {{ .Recommendation }}
{{ end }}
`))

func (r *markdownRenderer) Render(report *schema.Report) ([]byte, error) {
	if report == nil {
		return nil, fmt.Errorf("rendering markdown: report is nil")
	}
	var buf bytes.Buffer
	if err := mdTemplate.Execute(&buf, newMarkdownView(report)); err != nil {
		return nil, fmt.Errorf("rendering markdown: %w", err)
	}
	return buf.Bytes(), nil
}

func newMarkdownView(report *schema.Report) markdownView {
	view := markdownView{Report: report}
	for _, issue := range report.Issues {
		if hasTag(issue.Tags, "preflight") {
			view.PreflightIssues = append(view.PreflightIssues, issue)
			continue
		}
		view.LLMIssues = append(view.LLMIssues, issue)
	}
	view.HasIssues = len(view.PreflightIssues) > 0 || len(view.LLMIssues) > 0
	return view
}

func hasTag(tags []string, want string) bool {
	for _, tag := range tags {
		if tag == want {
			return true
		}
	}
	return false
}
