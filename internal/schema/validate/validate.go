package validate

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dshills/speccritic/internal/schema"
)

var (
	issueIDPattern    = regexp.MustCompile(`^ISSUE-\d{4}$`)
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
		idx := strings.Index(s, "\n")
		if idx >= 0 {
			// Normal case: fence opener on its own line.
			s = s[idx+1:]
		} else {
			// Fence with no newline (e.g. ```{...}```): strip up to the first { or [.
			rest := s[3:]
			if i := strings.IndexAny(rest, "{["); i >= 0 {
				s = rest[i:]
			}
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
	seenIssueIDs := make(map[string]bool, len(r.Issues))
	for i, issue := range r.Issues {
		if err := validateIssue(issue, i, lineCount); err != nil {
			return err
		}
		if seenIssueIDs[issue.ID] {
			return fmt.Errorf("duplicate issue ID %q", issue.ID)
		}
		seenIssueIDs[issue.ID] = true
	}
	seenQuestionIDs := make(map[string]bool, len(r.Questions))
	for i, q := range r.Questions {
		if err := validateQuestion(q, i, lineCount); err != nil {
			return err
		}
		if seenQuestionIDs[q.ID] {
			return fmt.Errorf("duplicate question ID %q", q.ID)
		}
		seenQuestionIDs[q.ID] = true
	}
	for i, patch := range r.Patches {
		if err := validatePatch(patch, i, seenIssueIDs); err != nil {
			return err
		}
	}
	if err := validateMeta(r.Meta); err != nil {
		return err
	}
	return nil
}

func validateMeta(meta schema.Meta) error {
	if meta.Completion != nil {
		switch meta.Completion.Mode {
		case schema.CompletionModeAuto, schema.CompletionModeOn, schema.CompletionModeOff:
		default:
			return fmt.Errorf("meta.completion.mode %q must be auto, on, or off", meta.Completion.Mode)
		}
		if !schema.IsCompletionTemplateName(meta.Completion.Template) {
			return fmt.Errorf("meta.completion.template %q must be one of %s", meta.Completion.Template, strings.Join(schema.CompletionTemplateNames(), ", "))
		}
		if meta.Completion.GeneratedPatches < 0 {
			return fmt.Errorf("meta.completion.generated_patches must be >= 0, got %d", meta.Completion.GeneratedPatches)
		}
		if meta.Completion.SkippedSuggestions < 0 {
			return fmt.Errorf("meta.completion.skipped_suggestions must be >= 0, got %d", meta.Completion.SkippedSuggestions)
		}
		if meta.Completion.OpenDecisions < 0 {
			return fmt.Errorf("meta.completion.open_decisions must be >= 0, got %d", meta.Completion.OpenDecisions)
		}
	}
	if meta.Convergence == nil {
		return nil
	}
	switch meta.Convergence.Mode {
	case "", schema.ConvergenceModeAuto, schema.ConvergenceModeOn, schema.ConvergenceModeOff:
	default:
		return fmt.Errorf("meta.convergence.mode %q must be auto, on, or off", meta.Convergence.Mode)
	}
	switch meta.Convergence.Status {
	case schema.ConvergenceStatusComplete, schema.ConvergenceStatusPartial, schema.ConvergenceStatusUnavailable:
	default:
		return fmt.Errorf("meta.convergence.status %q must be complete, partial, or unavailable", meta.Convergence.Status)
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

func validatePatch(patch schema.Patch, idx int, seenIssueIDs map[string]bool) error {
	prefix := fmt.Sprintf("patch[%d]", idx)
	if !issueIDPattern.MatchString(patch.IssueID) {
		return fmt.Errorf("%s: issue_id %q does not match ISSUE-XXXX format", prefix, patch.IssueID)
	}
	if !seenIssueIDs[patch.IssueID] {
		return fmt.Errorf("%s: issue_id %q does not reference a current issue", prefix, patch.IssueID)
	}
	if strings.TrimSpace(patch.Before) == "" {
		return fmt.Errorf("%s: before is required", prefix)
	}
	if patch.After == "" {
		return fmt.Errorf("%s: after is required", prefix)
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
	if ev.Path != "" && !filepath.IsLocal(ev.Path) {
		return fmt.Errorf("%s: path %q must be a local relative path", prefix, ev.Path)
	}
	return nil
}
