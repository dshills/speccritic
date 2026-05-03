package incremental

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dshills/speccritic/internal/redact"
	"github.com/dshills/speccritic/internal/schema"
	"github.com/dshills/speccritic/internal/spec"
)

type PromptInput struct {
	Spec      *spec.Spec
	Plan      Plan
	Range     ReviewRange
	Issues    []schema.Issue
	Questions []schema.Question
}

func BuildRangePrompt(input PromptInput) (cachedPrefix, variable string, err error) {
	if input.Spec == nil {
		return "", "", fmt.Errorf("spec is required")
	}
	if input.Range.ID == "" {
		return "", "", fmt.Errorf("review range is required")
	}
	lines := spec.Lines(input.Spec.Raw)
	if !validRange(input.Range.Context.Start, input.Range.Context.End, len(lines)) {
		return "", "", fmt.Errorf("range %s has invalid context bounds %d-%d", input.Range.ID, input.Range.Context.Start, input.Range.Context.End)
	}
	var prefix strings.Builder
	prefix.WriteString("Analyze changed sections of the current specification for defects.\n")
	prefix.WriteString("Return JSON only using the SpecCritic schema. Do not include score or verdict.\n")
	prefix.WriteString("Cite current spec line numbers only.\n")
	prefix.WriteString("Previously identified issues are context only and must not be reported again as new findings.\n")

	var tail strings.Builder
	fmt.Fprintf(&tail, "\n<incremental_range id=%q primary=\"L%d-L%d\" context=\"L%d-L%d\">\n",
		input.Range.ID,
		input.Range.Primary.Start,
		input.Range.Primary.End,
		input.Range.Context.Start,
		input.Range.Context.End,
	)
	tail.WriteString("\n<Current Spec Table of Contents>\n")
	tail.WriteString(tableOfContents(input.Spec.Raw, input.Range))
	tail.WriteString("</Current Spec Table of Contents>\n")
	tail.WriteString("\n<Previously Identified Issues>\n")
	tail.WriteString(formatPriorFindings(input.Issues, input.Questions, input.Range))
	tail.WriteString("</Previously Identified Issues>\n")
	tail.WriteString("\n<Current Review Task>\n")
	tail.WriteString("Review the PRIMARY lines. Context lines may be cited only when the changed text creates or exposes the defect there.\n")
	tail.WriteString("Every new issue must include tags \"incremental-review\" and \"range:")
	tail.WriteString(input.Range.ID)
	tail.WriteString("\".\n")
	tail.WriteString(numberedRange(lines, input.Range))
	tail.WriteString("\n</Current Review Task>\n")
	tail.WriteString("</incremental_range>\n")
	return prefix.String(), tail.String(), nil
}

func tableOfContents(raw string, rr ReviewRange) string {
	sections := buildSections(raw)
	if len(sections) == 0 {
		return "- Document\n"
	}
	var b strings.Builder
	for _, sec := range sections {
		if sec.Level == 0 {
			continue
		}
		if sec.Range.End < rr.Context.Start || sec.Range.Start > rr.Context.End {
			if sec.Level > 2 {
				continue
			}
		}
		indent := strings.Repeat("  ", sec.Level-1)
		fmt.Fprintf(&b, "%s- L%d %s\n", indent, sec.Range.Start, strings.Join(sec.HeadingPath, " > "))
	}
	if b.Len() == 0 {
		return "- Document\n"
	}
	return b.String()
}

func formatPriorFindings(issues []schema.Issue, questions []schema.Question, rr ReviewRange) string {
	type item struct {
		severity schema.Severity
		line     int
		text     string
	}
	var items []item
	for _, issue := range issues {
		line := firstEvidenceLine(issue.Evidence)
		if line == 0 {
			continue
		}
		items = append(items, item{
			severity: issue.Severity,
			line:     line,
			text:     fmt.Sprintf("- %s %s %s L%d: %s\n", issue.Severity, issue.ID, redact.Redact(issue.Title), line, compact(redact.Redact(issue.Description), 180)),
		})
	}
	for _, q := range questions {
		line := firstEvidenceLine(q.Evidence)
		if line == 0 {
			continue
		}
		items = append(items, item{
			severity: q.Severity,
			line:     line,
			text:     fmt.Sprintf("- %s %s L%d: %s\n", q.Severity, q.ID, line, compact(redact.Redact(q.Question), 180)),
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		di := distanceToRange(items[i].line, rr.Primary)
		dj := distanceToRange(items[j].line, rr.Primary)
		if di != dj {
			return di < dj
		}
		if severityOrder(items[i].severity) != severityOrder(items[j].severity) {
			return severityOrder(items[i].severity) > severityOrder(items[j].severity)
		}
		return items[i].line < items[j].line
	})
	var b strings.Builder
	if len(items) == 0 {
		b.WriteString("- none\n")
		return b.String()
	}
	const maxItems = 20
	for i, item := range items {
		if i == maxItems {
			fmt.Fprintf(&b, "- omitted %d additional prior findings due to token budget\n", len(items)-maxItems)
			break
		}
		b.WriteString(item.text)
	}
	return b.String()
}

func numberedRange(lines []string, rr ReviewRange) string {
	var b strings.Builder
	for lineNo := rr.Context.Start; lineNo <= rr.Context.End; lineNo++ {
		label := "CONTEXT"
		if lineNo >= rr.Primary.Start && lineNo <= rr.Primary.End {
			label = "PRIMARY"
		}
		fmt.Fprintf(&b, "L%d [%s]: %s\n", lineNo, label, escapePromptText(lines[lineNo-1]))
	}
	return b.String()
}

func escapePromptText(s string) string {
	s = strings.ReplaceAll(s, "</", "<\\/")
	return s
}

func firstEvidenceLine(evidence []schema.Evidence) int {
	if len(evidence) == 0 {
		return 0
	}
	return evidence[0].LineStart
}

func distanceToRange(line int, r LineRange) int {
	if line >= r.Start && line <= r.End {
		return 0
	}
	if line < r.Start {
		return r.Start - line
	}
	return line - r.End
}

func severityOrder(severity schema.Severity) int {
	switch severity {
	case schema.SeverityCritical:
		return 3
	case schema.SeverityWarn:
		return 2
	case schema.SeverityInfo:
		return 1
	default:
		return 0
	}
}

func compact(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "..."
}
