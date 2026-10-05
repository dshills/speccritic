package validate

import (
	"encoding/json"
	"fmt"
	"regexp"
	"slices"

	"github.com/dshills/speccritic/internal/schema"
)

// preflightTag is the tag every preflight finding carries.
const preflightTag = "preflight"

var (
	// Written IDs are printed with "%04d", which grows past four digits.
	writtenIssueIDPattern    = regexp.MustCompile(`^ISSUE-\d{4,}$`)
	writtenQuestionIDPattern = regexp.MustCompile(`^Q-\d{4,}$`)
	preflightRuleIDPattern   = regexp.MustCompile(`^PREFLIGHT-[A-Z0-9]+(?:-[A-Z0-9]+)*-\d{3}$`)
)

// ParseReport reads a JSON report that SpecCritic wrote, for use as the
// baseline of a later run. It holds the report to what SpecCritic writes,
// which is not what Parse and ParseResponse ask of a model:
//
//   - A finding tagged "preflight" may carry its rule ID
//     (PREFLIGHT-<GROUP>-<NNN>) in place of an ISSUE number, and several
//     findings may share one rule ID because a rule can match more than once.
//     Every other issue needs an ISSUE number of its own.
//   - An evidence path is a label for display and is not checked. Reports
//     written before preflight evidence used schema.EvidencePath hold the
//     spec path as it was given, which may be absolute.
//   - Patches are not checked. Nothing reads the patches of a baseline, and a
//     report filtered by --severity-threshold can keep a patch whose issue was
//     filtered out.
func ParseReport(raw []byte) (*schema.Report, error) {
	var report schema.Report
	if err := json.Unmarshal(raw, &report); err != nil {
		return nil, fmt.Errorf("JSON parse failed: %w", err)
	}

	seenIssueIDs := make(map[string]bool, len(report.Issues))
	for i, issue := range report.Issues {
		prefix := fmt.Sprintf("issue[%d]", i)
		if err := validateWrittenIssueID(issue, prefix, seenIssueIDs); err != nil {
			return nil, err
		}
		if err := validateIssueFields(issue, prefix); err != nil {
			return nil, err
		}
		if err := validateWrittenEvidence(issue.Evidence, prefix); err != nil {
			return nil, err
		}
	}
	seenQuestionIDs := make(map[string]bool, len(report.Questions))
	for i, q := range report.Questions {
		prefix := fmt.Sprintf("question[%d]", i)
		if !writtenQuestionIDPattern.MatchString(q.ID) {
			return nil, fmt.Errorf("%s: id %q does not match Q-XXXX format", prefix, q.ID)
		}
		if seenQuestionIDs[q.ID] {
			return nil, fmt.Errorf("duplicate question ID %q", q.ID)
		}
		seenQuestionIDs[q.ID] = true
		if err := validateQuestionFields(q, prefix); err != nil {
			return nil, err
		}
		if err := validateWrittenEvidence(q.Evidence, prefix); err != nil {
			return nil, err
		}
	}
	if err := validateMeta(report.Meta); err != nil {
		return nil, err
	}
	return &report, nil
}

// validateWrittenIssueID accepts an unused ISSUE number on any issue, and a
// preflight rule ID on an issue tagged as a preflight finding.
func validateWrittenIssueID(issue schema.Issue, prefix string, seen map[string]bool) error {
	if writtenIssueIDPattern.MatchString(issue.ID) {
		if seen[issue.ID] {
			return fmt.Errorf("duplicate issue ID %q", issue.ID)
		}
		seen[issue.ID] = true
		return nil
	}
	if preflightRuleIDPattern.MatchString(issue.ID) && slices.Contains(issue.Tags, preflightTag) {
		return nil
	}
	return fmt.Errorf("%s: id %q is neither ISSUE-XXXX nor the rule ID of a preflight finding", prefix, issue.ID)
}

func validateWrittenEvidence(evidence []schema.Evidence, prefix string) error {
	for j, ev := range evidence {
		if err := validateEvidenceLines(ev, fmt.Sprintf("%s.evidence[%d]", prefix, j), 0); err != nil {
			return err
		}
	}
	return nil
}
