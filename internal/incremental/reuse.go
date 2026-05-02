package incremental

import (
	"fmt"
	"strings"

	"github.com/dshills/speccritic/internal/schema"
	"github.com/dshills/speccritic/internal/spec"
)

const TagReused = "incremental-reused"

type ReuseInput struct {
	Plan            Plan
	Previous        *schema.Report
	CurrentRaw      string
	CurrentRedacted string
	Config          Config
	PreflightIssues []schema.Issue
}

type ReuseResult struct {
	Issues          []schema.Issue
	Questions       []schema.Question
	Dropped         []DroppedFinding
	RemapFailures   int
	RemapCandidates int
	Fallback        *FallbackReason
}

type DroppedFinding struct {
	ID     string
	Reason string
}

func ReuseFindings(input ReuseInput) (ReuseResult, error) {
	if input.Previous == nil {
		return ReuseResult{}, fmt.Errorf("previous report is required")
	}
	cfg := input.Config
	if cfg.MaxRemapFailureRatio == 0 {
		cfg.MaxRemapFailureRatio = defaultMaxRemapFailureRatio
	}
	if cfg.SeverityThreshold == "" {
		cfg.SeverityThreshold = input.Previous.Input.SeverityThreshold
	}
	currentLines := spec.Lines(input.CurrentRaw)
	redacted := input.CurrentRedacted
	if redacted == "" {
		redacted = input.CurrentRaw
	}
	redactedLines := spec.Lines(redacted)
	var result ReuseResult
	for _, issue := range input.Previous.Issues {
		reused, drop, candidate, failed := reuseIssue(issue, input.Plan, currentLines, redactedLines, cfg, input.PreflightIssues)
		if candidate {
			result.RemapCandidates++
		}
		if failed {
			result.RemapFailures++
		}
		if drop.Reason != "" {
			result.Dropped = append(result.Dropped, drop)
			continue
		}
		result.Issues = append(result.Issues, reused)
	}
	for _, question := range input.Previous.Questions {
		reused, drop, candidate, failed := reuseQuestion(question, input.Plan, currentLines, redactedLines, cfg)
		if candidate {
			result.RemapCandidates++
		}
		if failed {
			result.RemapFailures++
		}
		if drop.Reason != "" {
			result.Dropped = append(result.Dropped, drop)
			continue
		}
		result.Questions = append(result.Questions, reused)
	}
	if result.RemapCandidates > 0 {
		ratio := float64(result.RemapFailures) / float64(result.RemapCandidates)
		if ratio > cfg.MaxRemapFailureRatio {
			result.Fallback = &FallbackReason{
				Code:    "remap_failure_ratio",
				Message: fmt.Sprintf("prior finding remap failure ratio %.2f exceeds %.2f", ratio, cfg.MaxRemapFailureRatio),
			}
		}
	}
	return result, nil
}

func reuseIssue(issue schema.Issue, plan Plan, currentLines, redactedLines []string, cfg Config, preflight []schema.Issue) (schema.Issue, DroppedFinding, bool, bool) {
	if hasTag(issue.Tags, "preflight") {
		return schema.Issue{}, DroppedFinding{ID: issue.ID, Reason: "preflight"}, false, false
	}
	if !meetsThreshold(issue.Severity, cfg.SeverityThreshold) {
		return schema.Issue{}, DroppedFinding{ID: issue.ID, Reason: "severity_filter"}, false, false
	}
	mapped, drop, candidate, failed := remapEvidence(issue.ID, issue.Evidence, plan, currentLines, redactedLines)
	if drop.Reason != "" {
		return schema.Issue{}, drop, candidate, failed
	}
	issue.Evidence = mapped
	if duplicatePreflight(issue, preflight) {
		return schema.Issue{}, DroppedFinding{ID: issue.ID, Reason: "preflight_duplicate"}, candidate, failed
	}
	issue.Tags = appendUniqueTag(stripIncrementalTags(issue.Tags), TagReused)
	return issue, DroppedFinding{}, candidate, failed
}

func reuseQuestion(question schema.Question, plan Plan, currentLines, redactedLines []string, cfg Config) (schema.Question, DroppedFinding, bool, bool) {
	if !meetsThreshold(question.Severity, cfg.SeverityThreshold) {
		return schema.Question{}, DroppedFinding{ID: question.ID, Reason: "severity_filter"}, false, false
	}
	mapped, drop, candidate, failed := remapEvidence(question.ID, question.Evidence, plan, currentLines, redactedLines)
	if drop.Reason != "" {
		return schema.Question{}, drop, candidate, failed
	}
	question.Evidence = mapped
	return question, DroppedFinding{}, candidate, failed
}

func remapEvidence(id string, evidence []schema.Evidence, plan Plan, currentLines, redactedLines []string) ([]schema.Evidence, DroppedFinding, bool, bool) {
	if len(evidence) == 0 {
		return nil, DroppedFinding{ID: id, Reason: "missing_evidence"}, false, false
	}
	out := make([]schema.Evidence, 0, len(evidence))
	candidate := false
	for _, ev := range evidence {
		if inDeletedRange(ev, plan) {
			return nil, DroppedFinding{ID: id, Reason: "deleted_section"}, false, false
		}
		reuseRange, ok := findReuseRange(ev, plan.ReuseRanges)
		if !ok {
			return nil, DroppedFinding{ID: id, Reason: "not_reusable_section"}, false, false
		}
		candidate = true
		delta := reuseRange.Current.Start - reuseRange.Previous.Start
		mapped := ev
		mapped.LineStart += delta
		mapped.LineEnd += delta
		if !validRange(mapped.LineStart, mapped.LineEnd, len(currentLines)) {
			return nil, DroppedFinding{ID: id, Reason: "remap_out_of_bounds"}, true, true
		}
		if !evidenceQuoteMatches(mapped, redactedLines) {
			return nil, DroppedFinding{ID: id, Reason: "quote_mismatch"}, true, true
		}
		out = append(out, mapped)
	}
	return out, DroppedFinding{}, candidate, false
}

func inDeletedRange(ev schema.Evidence, plan Plan) bool {
	for _, ch := range plan.Sections {
		if ch.Classification != ClassDeleted {
			continue
		}
		if containsRange(ch.PreviousRange, ev.LineStart, ev.LineEnd) {
			return true
		}
	}
	return false
}

func findReuseRange(ev schema.Evidence, ranges []ReuseRange) (ReuseRange, bool) {
	for _, r := range ranges {
		if containsRange(r.Previous, ev.LineStart, ev.LineEnd) {
			return r, true
		}
	}
	return ReuseRange{}, false
}

func containsRange(r LineRange, start, end int) bool {
	return r.Start > 0 && start >= r.Start && end <= r.End
}

func validRange(start, end, lineCount int) bool {
	return start >= 1 && end >= start && end <= lineCount
}

func evidenceQuoteMatches(ev schema.Evidence, redactedLines []string) bool {
	quote := strings.TrimSpace(ev.Quote)
	if quote == "" {
		return false
	}
	if strings.Contains(quote, "[REDACTED]") {
		return false
	}
	if !validRange(ev.LineStart, ev.LineEnd, len(redactedLines)) {
		return false
	}
	text := strings.Join(redactedLines[ev.LineStart-1:ev.LineEnd], "\n")
	return strings.Contains(text, quote)
}

func meetsThreshold(severity schema.Severity, threshold string) bool {
	if threshold == "" {
		threshold = "info"
	}
	rank, ok := severityRank(string(severity))
	if !ok {
		return false
	}
	min, ok := severityRank(threshold)
	if !ok {
		return false
	}
	return rank >= min
}

func hasTag(tags []string, tag string) bool {
	for _, existing := range tags {
		if existing == tag {
			return true
		}
	}
	return false
}

func stripIncrementalTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		if tag == TagReused || tag == "incremental-review" || strings.HasPrefix(tag, "range:") {
			continue
		}
		out = append(out, tag)
	}
	return out
}

func appendUniqueTag(tags []string, tag string) []string {
	if hasTag(tags, tag) {
		return tags
	}
	return append(tags, tag)
}

func duplicatePreflight(issue schema.Issue, preflight []schema.Issue) bool {
	for _, pf := range preflight {
		if issue.Category != pf.Category {
			continue
		}
		for _, ev := range issue.Evidence {
			for _, pfEv := range pf.Evidence {
				if ev.Path == pfEv.Path && ev.LineStart <= pfEv.LineEnd && pfEv.LineStart <= ev.LineEnd {
					return true
				}
			}
		}
	}
	return false
}
