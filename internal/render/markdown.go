package render

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/dshills/speccritic/internal/schema"
)

type markdownRenderer struct{}

var mdTemplate = template.Must(template.New("report").Parse(`# SpecCritic Report

**Verdict:** {{ .Summary.Verdict }}
**Score:** {{ .Summary.Score }}/100
**Critical:** {{ .Summary.CriticalCount }} | **Warn:** {{ .Summary.WarnCount }} | **Info:** {{ .Summary.InfoCount }}
> Note: counts reflect all findings; --severity-threshold may hide some from this output.
{{ if .Issues }}
---

## Issues
{{ range .Issues }}
### {{ .ID }} · {{ .Severity }} · {{ .Category }}
**{{ .Title }}**

{{ .Description }}
{{ range .Evidence }}
> {{ .Path }} L{{ .LineStart }}–{{ .LineEnd }}: "{{ .Quote }}"
{{ end }}
**Impact:** {{ .Impact }}
**Recommendation:** {{ .Recommendation }}
{{ end }}{{ end }}{{ if .Questions }}
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
`))

func (r *markdownRenderer) Render(report *schema.Report) ([]byte, error) {
	var buf bytes.Buffer
	if err := mdTemplate.Execute(&buf, report); err != nil {
		return nil, fmt.Errorf("rendering markdown: %w", err)
	}
	return buf.Bytes(), nil
}
