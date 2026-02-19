package validate

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/dshills/speccritic/internal/schema"
)

var (
	issueIDPattern   = regexp.MustCompile(`^ISSUE-\d{4}$`)
	questionIDPattern = regexp.MustCompile(`^Q-\d{4}$`)
)

// Parse strips markdown fences, unmarshals JSON, and validates the structure
// of an LLM response. lineCount is the number of lines in the spec file and
// is used to validate evidence bounds.
func Parse(raw string, lineCount int) (*schema.Report, error) {
	cleaned := stripFences(raw)

	var report schema.Report
	if err := json.Unmarshal([]byte(cleaned), &report); err != nil {
		return nil, fmt.Errorf("JSON parse failed: %w", err)
	}

	if err := validateReport(&report, lineCount); err != nil {
		return nil, err
	}

	return &report, nil
}

// stripFences removes leading/trailing markdown code fences (```json ... ``` or ``` ... ```).
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		// Remove first line (the fence opener)
		idx := strings.Index(s, "\n")
		if idx >= 0 {
			s = s[idx+1:]
		}
	}
	if strings.HasSuffix(s, "```") {
		idx := strings.LastIndex(s, "\n```")
		if idx >= 0 {
			s = s[:idx]
		}
	}
	return strings.TrimSpace(s)
}

func validateReport(r *schema.Report, lineCount int) error {
	for i, issue := range r.Issues {
		if err := validateIssue(issue, i, lineCount); err != nil {
			return err
		}
	}
	for i, q := range r.Questions {
		if err := validateQuestion(q, i, lineCount); err != nil {
			return err
		}
	}
	return nil
}

func validateIssue(issue schema.Issue, idx int, lineCount int) error {
	prefix := fmt.Sprintf("issue[%d]", idx)

	if !issueIDPattern.MatchString(issue.ID) {
		return fmt.Errorf("%s: id %q does not match ISSUE-XXXX format", prefix, issue.ID)
	}
	if err := validateSeverity(issue.Severity, prefix); err != nil {
		return err
	}
	if !schema.IsValidCategory(issue.Category) {
		return fmt.Errorf("%s: unknown category %q", prefix, issue.Category)
	}
	if issue.Title == "" {
		return fmt.Errorf("%s: title is required", prefix)
	}
	for j, ev := range issue.Evidence {
		if err := validateEvidence(ev, fmt.Sprintf("%s.evidence[%d]", prefix, j), lineCount); err != nil {
			return err
		}
	}
	return nil
}

func validateQuestion(q schema.Question, idx int, lineCount int) error {
	prefix := fmt.Sprintf("question[%d]", idx)

	if !questionIDPattern.MatchString(q.ID) {
		return fmt.Errorf("%s: id %q does not match Q-XXXX format", prefix, q.ID)
	}
	if err := validateSeverity(q.Severity, prefix); err != nil {
		return err
	}
	if q.Question == "" {
		return fmt.Errorf("%s: question text is required", prefix)
	}
	for j, ev := range q.Evidence {
		if err := validateEvidence(ev, fmt.Sprintf("%s.evidence[%d]", prefix, j), lineCount); err != nil {
			return err
		}
	}
	return nil
}

func validateSeverity(s schema.Severity, prefix string) error {
	switch s {
	case schema.SeverityInfo, schema.SeverityWarn, schema.SeverityCritical:
		return nil
	}
	return fmt.Errorf("%s: invalid severity %q (must be INFO, WARN, or CRITICAL)", prefix, s)
}

func validateEvidence(ev schema.Evidence, prefix string, lineCount int) error {
	if ev.LineStart < 1 {
		return fmt.Errorf("%s: line_start %d must be ≥ 1", prefix, ev.LineStart)
	}
	if ev.LineEnd < ev.LineStart {
		return fmt.Errorf("%s: line_end %d must be ≥ line_start %d", prefix, ev.LineEnd, ev.LineStart)
	}
	if lineCount > 0 && ev.LineEnd > lineCount {
		return fmt.Errorf("%s: line_end %d exceeds spec line count %d", prefix, ev.LineEnd, lineCount)
	}
	return nil
}
