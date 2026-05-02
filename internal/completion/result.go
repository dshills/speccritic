package completion

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dshills/speccritic/internal/redact"
	"github.com/dshills/speccritic/internal/schema"
)

const completionSuggestedTag = "completion-suggested"
const openDecisionPrefix = "OPEN DECISION:"
const redactedMarker = "[REDACTED]"

type BuildInput struct {
	OriginalSpec    string
	Candidates      []Candidate
	ExistingPatches []schema.Patch
	Issues          []schema.Issue
	Config          Config
	Template        string
}

func BuildResult(input BuildInput) (Result, []schema.Issue, error) {
	candidates := append([]Candidate(nil), input.Candidates...)
	sortResultCandidates(candidates)
	patches := append([]schema.Patch(nil), input.ExistingPatches...)
	issues := cloneIssues(input.Issues)
	issueByID := make(map[string]schema.Issue, len(issues))
	issueIndexByID := make(map[string]int, len(issues))
	for i, issue := range issues {
		issueByID[issue.ID] = issue
		issueIndexByID[issue.ID] = i
	}
	occupied := make([]editRange, 0, len(candidates))
	generatedByIssue := make(map[string]bool)
	generated := 0
	for i := range candidates {
		if candidates[i].Status != StatusPatchGenerated {
			continue
		}
		rng, ok := safeEditRange(input.OriginalSpec, candidates[i])
		if !ok || candidates[i].After == "" {
			candidates[i].Status = StatusSkippedNoSafeLocation
			continue
		}
		if strings.Contains(candidates[i].Before, redactedMarker) || strings.Contains(candidates[i].After, redactedMarker) || redact.ContainsSecret(candidates[i].Before) || redact.ContainsSecret(candidates[i].After) {
			candidates[i].Status = StatusSkippedRedaction
			continue
		}
		if overlapsAny(rng, occupied) {
			candidates[i].Status = StatusSkippedOverlap
			continue
		}
		if generated >= input.Config.MaxPatches {
			candidates[i].Status = StatusSkippedLimit
			continue
		}
		occupied = append(occupied, rng)
		generated++
		patches = append(patches, schema.Patch{
			IssueID: candidates[i].SourceIssueID,
			Before:  candidates[i].Before,
			After:   candidates[i].After,
		})
		generatedByIssue[candidates[i].SourceIssueID] = true
	}
	for issueID := range generatedByIssue {
		addCompletionTag(issues, issueIndexByID, issueID)
	}
	if string(input.Config.Mode) == schema.CompletionModeOn {
		if err := requiredCandidateError(candidates, issueByID); err != nil {
			return Result{}, issues, err
		}
	}
	meta := schema.CompletionMeta{
		Enabled:            true,
		Mode:               string(input.Config.Mode),
		Template:           input.Template,
		GeneratedPatches:   generated,
		SkippedSuggestions: countSkipped(candidates),
		OpenDecisions:      countOpenDecisions(candidates),
	}
	return Result{Candidates: candidates, Patches: patches, Meta: meta}, issues, nil
}

type editRange struct {
	start int
	end   int
}

func safeEditRange(original string, candidate Candidate) (editRange, bool) {
	if candidate.Before == "" {
		return editRange{}, false
	}
	start := strings.Index(original, candidate.Before)
	if start < 0 {
		return editRange{}, false
	}
	nextStart := start + 1
	if nextStart < len(original) && strings.Contains(original[nextStart:], candidate.Before) {
		return editRange{}, false
	}
	return editRange{start: start, end: start + len(candidate.Before)}, true
}

func overlapsAny(rng editRange, occupied []editRange) bool {
	for _, existing := range occupied {
		if rng.start < existing.end && existing.start < rng.end {
			return true
		}
	}
	return false
}

func cloneIssues(issues []schema.Issue) []schema.Issue {
	out := append([]schema.Issue(nil), issues...)
	for i := range out {
		out[i].Tags = append([]string(nil), out[i].Tags...)
	}
	return out
}

func addCompletionTag(issues []schema.Issue, issueIndexByID map[string]int, issueID string) {
	i, ok := issueIndexByID[issueID]
	if !ok || hasCompletionTag(issues[i].Tags) {
		return
	}
	issues[i].Tags = append(issues[i].Tags, completionSuggestedTag)
	sort.Strings(issues[i].Tags)
}

func hasCompletionTag(tags []string) bool {
	for _, tag := range tags {
		if tag == completionSuggestedTag {
			return true
		}
	}
	return false
}

func countSkipped(candidates []Candidate) int {
	count := 0
	for _, candidate := range candidates {
		if candidate.Status != StatusPatchGenerated {
			count++
		}
	}
	return count
}

func countOpenDecisions(candidates []Candidate) int {
	count := 0
	for _, candidate := range candidates {
		count += strings.Count(candidate.Text, openDecisionPrefix)
	}
	return count
}

func sortResultCandidates(candidates []Candidate) {
	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.TargetLine != right.TargetLine {
			return lineSortValue(left.TargetLine) < lineSortValue(right.TargetLine)
		}
		if severityRank(left.Severity) != severityRank(right.Severity) {
			return severityRank(left.Severity) < severityRank(right.Severity)
		}
		if left.SourceIssueID != right.SourceIssueID {
			return left.SourceIssueID < right.SourceIssueID
		}
		if left.SectionOrder != right.SectionOrder {
			return left.SectionOrder < right.SectionOrder
		}
		return left.Text < right.Text
	})
}

func requiredCandidateError(candidates []Candidate, issueByID map[string]schema.Issue) error {
	for _, candidate := range candidates {
		if candidate.Status == StatusPatchGenerated {
			continue
		}
		issue, ok := issueByID[candidate.SourceIssueID]
		if !ok || !issue.Blocking || !isMissingSectionIssue(issue) {
			continue
		}
		return fmt.Errorf("required completion patch for %s could not be safely generated: %s", candidate.SourceIssueID, candidate.Status)
	}
	return nil
}

func isMissingSectionIssue(issue schema.Issue) bool {
	if strings.HasPrefix(issue.ID, "PREFLIGHT-STRUCTURE-") {
		return true
	}
	for _, tag := range issue.Tags {
		if tag == "missing-section" {
			return true
		}
	}
	return false
}
