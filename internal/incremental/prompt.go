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
	// SpecShown says the shared prefix built by llm.BuildSpecPrefix holds the
	// whole numbered spec, as it does for a full or chunked review. The task
	// then names the lines to review. Without it the spec was too large to
	// share, and the task carries the range's own numbered lines and a table
	// of contents.
	SpecShown bool
}

// BuildRangeTask returns the task for reviewing one changed range. It follows
// the prefix every call about the spec shares (context files, the numbered
// spec and the known preflight findings), so a range review judges its lines
// with the same material as a full review, and reads the spec from the
// provider's prompt cache.
func BuildRangeTask(input PromptInput) (string, error) {
	if input.Spec == nil {
		return "", fmt.Errorf("spec is required")
	}
	if input.Range.ID == "" {
		return "", fmt.Errorf("review range is required")
	}
	lines := spec.Lines(input.Spec.Raw)
	rr := input.Range
	if !validRange(rr.Context.Start, rr.Context.End, len(lines)) || !validRange(rr.Primary.Start, rr.Primary.End, len(lines)) {
		return "", fmt.Errorf("range %s has invalid bounds: primary %d-%d, context %d-%d", rr.ID, rr.Primary.Start, rr.Primary.End, rr.Context.Start, rr.Context.End)
	}
	var tail strings.Builder
	fmt.Fprintf(&tail, "\n<incremental_range id=%q primary=\"L%d-L%d\" context=\"L%d-L%d\">\n",
		rr.ID, rr.Primary.Start, rr.Primary.End, rr.Context.Start, rr.Context.End)
	tail.WriteString("The specification changed since its last review. Only the changed lines are reviewed now; findings on the rest are kept from that review.\n")
	tail.WriteString("\n<previously_identified_issues>\n")
	tail.WriteString(formatPriorFindings(input.Issues, input.Questions, rr))
	tail.WriteString("</previously_identified_issues>\n")
	tail.WriteString("These are already known. Do not report them again as new findings.\n\n")
	if input.SpecShown {
		fmt.Fprintf(&tail, "Review the PRIMARY lines L%d-L%d of the specification above for defects. Read all of the specification, so that you do not report as missing something stated elsewhere.\n", rr.Primary.Start, rr.Primary.End)
	} else {
		tail.WriteString("The specification is too large to show in full. Its table of contents:\n")
		tail.WriteString("<table_of_contents>\n")
		tail.WriteString(tableOfContents(input.Spec.Raw, rr))
		tail.WriteString("</table_of_contents>\n")
		tail.WriteString("The lines to review, PRIMARY lines with CONTEXT lines around them:\n")
		fmt.Fprintf(&tail, "<spec_lines file=%q>\n", input.Spec.Path)
		tail.WriteString(numberedRange(lines, rr))
		tail.WriteString("</spec_lines>\n")
		fmt.Fprintf(&tail, "Review the PRIMARY lines L%d-L%d for defects. A term or rule you cannot find here may be defined elsewhere: say so in the finding and add the tag \"cross-section\" rather than asserting that it is missing.\n", rr.Primary.Start, rr.Primary.End)
	}
	fmt.Fprintf(&tail, "Cite only lines L%d-L%d in evidence. Lines outside L%d-L%d may be cited only when the changed text creates or exposes the defect there.\n",
		rr.Context.Start, rr.Context.End, rr.Primary.Start, rr.Primary.End)
	tail.WriteString("</incremental_range>\n")
	return tail.String(), nil
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
