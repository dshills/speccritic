package web

import (
	"fmt"
	"sort"

	"github.com/dshills/speccritic/internal/schema"
	"github.com/dshills/speccritic/internal/spec"
)

type AnnotatedSpec struct {
	Lines []AnnotatedLine
}

type AnnotatedLine struct {
	Number          int
	Text            string
	HighestSeverity schema.Severity
	FindingRefs     []FindingRef
	ElementID       string
}

type FindingRef struct {
	ID       string
	Kind     string
	Severity schema.Severity
	Title    string
}

func BuildAnnotatedSpec(specText string, report *schema.Report, threshold schema.Severity) (AnnotatedSpec, error) {
	textLines := splitSpecLines(specText)
	lines := make([]AnnotatedLine, len(textLines))
	for i, text := range textLines {
		n := i + 1
		lines[i] = AnnotatedLine{
			Number:    n,
			Text:      text,
			ElementID: fmt.Sprintf("line-%d", n),
		}
	}
	if report == nil {
		return AnnotatedSpec{Lines: lines}, nil
	}
	lineCount := len(lines)
	for _, issue := range report.Issues {
		if !meetsThreshold(issue.Severity, threshold) {
			continue
		}
		ref := FindingRef{ID: issue.ID, Kind: "issue", Severity: issue.Severity, Title: issue.Title}
		for _, ev := range issue.Evidence {
			if err := addRef(lines, lineCount, ev, ref); err != nil {
				return AnnotatedSpec{}, err
			}
		}
	}
	for _, question := range report.Questions {
		if !meetsThreshold(question.Severity, threshold) {
			continue
		}
		ref := FindingRef{ID: question.ID, Kind: "question", Severity: question.Severity, Title: question.Question}
		for _, ev := range question.Evidence {
			if err := addRef(lines, lineCount, ev, ref); err != nil {
				return AnnotatedSpec{}, err
			}
		}
	}
	for i := range lines {
		if len(lines[i].FindingRefs) > 1 {
			sort.SliceStable(lines[i].FindingRefs, func(a, b int) bool {
				left := lines[i].FindingRefs[a]
				right := lines[i].FindingRefs[b]
				if severityRank(left.Severity) != severityRank(right.Severity) {
					return severityRank(left.Severity) > severityRank(right.Severity)
				}
				return left.ID < right.ID
			})
		}
		if len(lines[i].FindingRefs) > 0 {
			lines[i].HighestSeverity = lines[i].FindingRefs[0].Severity
		}
	}
	return AnnotatedSpec{Lines: lines}, nil
}

func splitSpecLines(content string) []string {
	return spec.Lines(content)
}

func addRef(lines []AnnotatedLine, lineCount int, ev schema.Evidence, ref FindingRef) error {
	if ev.LineStart < 1 || ev.LineEnd < ev.LineStart || ev.LineEnd > lineCount {
		return fmt.Errorf("invalid evidence line range %d-%d for %d line spec", ev.LineStart, ev.LineEnd, lineCount)
	}
	for n := ev.LineStart; n <= ev.LineEnd; n++ {
		line := &lines[n-1]
		if !hasRef(line.FindingRefs, ref.ID) {
			line.FindingRefs = append(line.FindingRefs, ref)
		}
	}
	return nil
}

func hasRef(refs []FindingRef, id string) bool {
	for _, ref := range refs {
		if ref.ID == id {
			return true
		}
	}
	return false
}

func meetsThreshold(severity, threshold schema.Severity) bool {
	return severityRank(severity) >= severityRank(threshold)
}

func severityRank(severity schema.Severity) int {
	switch severity {
	case schema.SeverityCritical:
		return 2
	case schema.SeverityWarn:
		return 1
	case schema.SeverityInfo:
		return 0
	default:
		return -1
	}
}
