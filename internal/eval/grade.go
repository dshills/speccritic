package eval

import (
	"slices"
	"strings"

	"github.com/dshills/speccritic/internal/schema"
)

// tagPreflight marks a finding produced by the deterministic preflight rules
// rather than by the model.
const tagPreflight = "preflight"

// Finding is one issue or question from a report, reduced to what grading
// needs.
type Finding struct {
	// ID is the report's ID for the issue or question.
	ID       string          `json:"id"`
	Question bool            `json:"question,omitempty"`
	Severity schema.Severity `json:"severity"`
	// Category is empty for a question.
	Category schema.Category `json:"category,omitempty"`
	Title    string          `json:"title"`
	Ranges   []LineRange     `json:"ranges"`
	// Preflight is true for a finding the preflight rules produced.
	Preflight bool `json:"preflight,omitempty"`
	// Defect is the ID of the seeded defect this finding was matched to, or
	// empty when it matches none.
	Defect string `json:"defect,omitempty"`
	// CategoryOK is true when Category is one the matched defect accepts.
	CategoryOK bool `json:"category_ok,omitempty"`

	// text is what keywords are looked for in.
	text string
}

// DefectGrade says how a review did on one seeded defect.
type DefectGrade struct {
	ID string `json:"id"`
	// Category is the defect's primary category.
	Category schema.Category `json:"category"`
	Expected schema.Severity `json:"expected_severity"`
	// Found is true when at least one finding matched the defect.
	Found bool `json:"found"`
	// FoundByModel is true when a finding from the model, not preflight,
	// matched.
	FoundByModel bool `json:"found_by_model"`
	// CategoryOK is true when a matching issue carried an accepted category.
	CategoryOK bool `json:"category_ok"`
	// Reported is the highest severity among the matching findings.
	Reported schema.Severity `json:"reported_severity,omitempty"`
}

// Grade is the result of comparing one report with one case.
type Grade struct {
	Defects  []DefectGrade `json:"defects"`
	Findings []Finding     `json:"findings"`
}

// GradeReport matches the findings of report to the seeded defects of c.
//
// A finding matches a defect when it cites a line within tolerance lines of
// the defect's evidence and is about the same thing: its category is one the
// defect accepts, or its text contains one of the defect's keywords. Location
// alone is not enough, because an unrelated complaint about the same line
// would otherwise earn credit. Several findings may match one defect; they are
// repeats, not errors.
func GradeReport(c Case, report *schema.Report, tolerance int) Grade {
	findings := collectFindings(report)
	grade := Grade{Defects: make([]DefectGrade, len(c.Defects))}
	for i, defect := range c.Defects {
		grade.Defects[i] = DefectGrade{ID: defect.ID, Category: defect.Categories[0], Expected: defect.Severity}
	}

	for i := range findings {
		f := &findings[i]
		best, bestCategory, bestSpan := -1, false, 0
		for d, defect := range c.Defects {
			if !overlapsAny(f.Ranges, defect.Ranges, tolerance) {
				continue
			}
			categoryOK := !f.Question && slices.Contains(defect.Categories, f.Category)
			if !categoryOK && !containsAny(f.text, defect.Keywords) {
				continue
			}
			// Prefer a defect whose category the finding names, then the one
			// with the tightest evidence, which is the more specific label.
			span := totalSpan(defect.Ranges)
			if best < 0 || (categoryOK && !bestCategory) || (categoryOK == bestCategory && span < bestSpan) {
				best, bestCategory, bestSpan = d, categoryOK, span
			}
		}
		if best < 0 {
			continue
		}
		f.Defect = c.Defects[best].ID
		f.CategoryOK = bestCategory
		g := &grade.Defects[best]
		g.Found = true
		g.FoundByModel = g.FoundByModel || !f.Preflight
		g.CategoryOK = g.CategoryOK || bestCategory
		if severityRank(f.Severity) > severityRank(g.Reported) {
			g.Reported = f.Severity
		}
	}
	grade.Findings = findings
	return grade
}

func collectFindings(report *schema.Report) []Finding {
	if report == nil {
		return nil
	}
	findings := make([]Finding, 0, len(report.Issues)+len(report.Questions))
	for _, issue := range report.Issues {
		findings = append(findings, Finding{
			ID:        issue.ID,
			Severity:  issue.Severity,
			Category:  issue.Category,
			Title:     issue.Title,
			Ranges:    evidenceRanges(issue.Evidence),
			Preflight: slices.Contains(issue.Tags, tagPreflight),
			text:      strings.ToLower(issue.Title + "\n" + issue.Description + "\n" + issue.Impact + "\n" + issue.Recommendation),
		})
	}
	for _, question := range report.Questions {
		findings = append(findings, Finding{
			ID:       question.ID,
			Question: true,
			Severity: question.Severity,
			Title:    question.Question,
			Ranges:   evidenceRanges(question.Evidence),
			text:     strings.ToLower(question.Question + "\n" + question.WhyNeeded),
		})
	}
	return findings
}

func evidenceRanges(evidence []schema.Evidence) []LineRange {
	ranges := make([]LineRange, 0, len(evidence))
	for _, ev := range evidence {
		ranges = append(ranges, LineRange{Start: ev.LineStart, End: ev.LineEnd})
	}
	return ranges
}

func overlapsAny(a, b []LineRange, tolerance int) bool {
	for _, left := range a {
		for _, right := range b {
			if left.Start <= right.End+tolerance && right.Start-tolerance <= left.End {
				return true
			}
		}
	}
	return false
}

func totalSpan(ranges []LineRange) int {
	span := 0
	for _, r := range ranges {
		span += r.End - r.Start + 1
	}
	return span
}

func containsAny(text string, keywords []string) bool {
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

func severityRank(severity schema.Severity) int {
	switch severity {
	case schema.SeverityCritical:
		return 3
	case schema.SeverityWarn:
		return 2
	case schema.SeverityInfo:
		return 1
	}
	return 0
}
