package review

import "github.com/dshills/speccritic/internal/schema"

// Score computes the deterministic score from all issues.
// Score is always computed before any --severity-threshold filtering.
// Start: 100, -20 per CRITICAL, -7 per WARN, -2 per INFO, clamped at 0.
func Score(issues []schema.Issue) int {
	score := 100
	for _, issue := range issues {
		switch issue.Severity {
		case schema.SeverityCritical:
			score -= 20
		case schema.SeverityWarn:
			score -= 7
		case schema.SeverityInfo:
			score -= 2
		}
	}
	if score < 0 {
		score = 0
	}
	return score
}

// Verdict computes the deterministic verdict from all issues and questions.
// CRITICAL questions are treated equivalently to CRITICAL issues: a spec
// with only CRITICAL questions (and no issues) receives INVALID.
// Verdict is always computed before any --severity-threshold filtering.
func Verdict(issues []schema.Issue, questions []schema.Question) schema.Verdict {
	for _, issue := range issues {
		if issue.Severity == schema.SeverityCritical {
			return schema.VerdictInvalid
		}
	}
	for _, q := range questions {
		if q.Severity == schema.SeverityCritical {
			return schema.VerdictInvalid
		}
	}
	for _, issue := range issues {
		if issue.Severity == schema.SeverityWarn {
			return schema.VerdictValidWithGaps
		}
	}
	if len(issues) > 0 { // INFO-only
		return schema.VerdictValidWithGaps
	}
	return schema.VerdictValid
}

// Counts returns the pre-filter critical, warn, and info counts from all issues.
func Counts(issues []schema.Issue) (critical, warn, info int) {
	for _, issue := range issues {
		switch issue.Severity {
		case schema.SeverityCritical:
			critical++
		case schema.SeverityWarn:
			warn++
		case schema.SeverityInfo:
			info++
		}
	}
	return
}

// FilterBySeverity returns only issues at or above the given threshold severity.
func FilterBySeverity(issues []schema.Issue, threshold schema.Severity) []schema.Issue {
	if threshold == schema.SeverityInfo {
		return issues
	}
	out := make([]schema.Issue, 0, len(issues))
	for _, issue := range issues {
		if meetsSeverity(issue.Severity, threshold) {
			out = append(out, issue)
		}
	}
	return out
}

func meetsSeverity(s, threshold schema.Severity) bool {
	return severityOrdinal(s) >= severityOrdinal(threshold)
}

func severityOrdinal(s schema.Severity) int {
	switch s {
	case schema.SeverityInfo:
		return 0
	case schema.SeverityWarn:
		return 1
	case schema.SeverityCritical:
		return 2
	}
	return -1
}
