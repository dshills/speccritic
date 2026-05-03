package incremental

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/dshills/speccritic/internal/review"
	"github.com/dshills/speccritic/internal/schema"
	"github.com/dshills/speccritic/internal/spec"
)

type MergeInput struct {
	Spec                *spec.Spec
	PreflightIssues     []schema.Issue
	ReusedIssues        []schema.Issue
	ReusedQuestions     []schema.Question
	RangeResults        []RangeResult
	Patches             []schema.Patch
	Model               string
	Temperature         float64
	Profile             string
	Strict              bool
	SeverityThreshold   string
	ContextFiles        []string
	IncrementalMetadata *schema.IncrementalMeta
	IncludeMetadata     bool
}

func MergeReport(input MergeInput) (*schema.Report, error) {
	if input.Spec == nil {
		return nil, fmt.Errorf("spec is required")
	}
	issues := append([]schema.Issue(nil), input.PreflightIssues...)
	for _, issue := range input.ReusedIssues {
		if idx := duplicateIssueIndex(issue, issues); idx >= 0 {
			issues[idx] = mergeDuplicateIssue(issues[idx], issue)
			continue
		}
		issues = append(issues, issue)
	}
	questions := append([]schema.Question(nil), input.ReusedQuestions...)
	patches := append([]schema.Patch(nil), input.Patches...)

	nextIssue := maxIssueID(issues) + 1
	nextQuestion := maxQuestionID(questions) + 1
	for _, result := range input.RangeResults {
		if result.Report == nil {
			continue
		}
		issueIDMap := make(map[string]string, len(result.Report.Issues))
		for _, issue := range result.Report.Issues {
			originalID := issue.ID
			if idx := duplicateIssueIndex(issue, issues); idx >= 0 {
				issues[idx] = mergeDuplicateIssue(issues[idx], issue)
				issueIDMap[originalID] = issues[idx].ID
				continue
			}
			issue.ID = fmt.Sprintf("ISSUE-%04d", nextIssue)
			nextIssue++
			issueIDMap[originalID] = issue.ID
			issues = append(issues, issue)
		}
		for _, question := range result.Report.Questions {
			question.ID = fmt.Sprintf("Q-%04d", nextQuestion)
			nextQuestion++
			questions = append(questions, question)
		}
		patches = append(patches, remapPatches(result.Report.Patches, issueIDMap)...)
	}
	issues = sortIssues(issues)
	questions = sortQuestions(questions)
	patches = validPatches(input.Spec.Raw, patches, issues)
	critical, warn, info := review.Counts(issues)
	meta := schema.Meta{Model: input.Model, Temperature: input.Temperature}
	if input.IncludeMetadata && input.IncrementalMetadata != nil {
		meta.Incremental = input.IncrementalMetadata
	}
	return &schema.Report{
		Tool:    "speccritic",
		Version: "1.0",
		Input: schema.Input{
			SpecFile:          input.Spec.Path,
			SpecHash:          input.Spec.Hash,
			ContextFiles:      append([]string(nil), input.ContextFiles...),
			Profile:           input.Profile,
			Strict:            input.Strict,
			SeverityThreshold: input.SeverityThreshold,
		},
		Summary: schema.Summary{
			Verdict:       review.Verdict(issues, questions),
			Score:         review.Score(issues, questions),
			CriticalCount: critical,
			WarnCount:     warn,
			InfoCount:     info,
		},
		Issues:    issues,
		Questions: questions,
		Patches:   patches,
		Meta:      meta,
	}, nil
}

func duplicateIssueIndex(issue schema.Issue, existing []schema.Issue) int {
	for i, candidate := range existing {
		if candidate.Category != issue.Category || candidate.Severity != issue.Severity {
			continue
		}
		if evidenceOverlaps(candidate.Evidence, issue.Evidence) && evidenceQuoteSimilar(candidate.Evidence, issue.Evidence) {
			return i
		}
		if strings.EqualFold(candidate.Title, issue.Title) && evidenceQuoteSimilar(candidate.Evidence, issue.Evidence) {
			return i
		}
	}
	return -1
}

func evidenceOverlaps(a, b []schema.Evidence) bool {
	for _, left := range a {
		for _, right := range b {
			if left.Path != right.Path {
				continue
			}
			if left.LineStart <= right.LineEnd && right.LineStart <= left.LineEnd {
				return true
			}
		}
	}
	return false
}

func evidenceQuoteSimilar(a, b []schema.Evidence) bool {
	for _, left := range a {
		for _, right := range b {
			lq := strings.TrimSpace(left.Quote)
			rq := strings.TrimSpace(right.Quote)
			if lq != "" && rq != "" && lq == rq {
				return true
			}
		}
	}
	return false
}

func mergeDuplicateIssue(existing, current schema.Issue) schema.Issue {
	existing.Tags = mergeTags(existing.Tags, current.Tags)
	if len(current.Description) > len(existing.Description) {
		existing.Description = current.Description
	}
	if len(current.Recommendation) > len(existing.Recommendation) {
		existing.Recommendation = current.Recommendation
	}
	existing.Evidence = mergeEvidence(existing.Evidence, current.Evidence)
	return existing
}

func mergeEvidence(a, b []schema.Evidence) []schema.Evidence {
	out := append([]schema.Evidence(nil), a...)
	seen := make(map[string]bool, len(a)+len(b))
	for _, ev := range out {
		seen[evidenceKey(ev)] = true
	}
	for _, ev := range b {
		key := evidenceKey(ev)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, ev)
	}
	return out
}

func evidenceKey(ev schema.Evidence) string {
	return fmt.Sprintf("%s:%d:%d:%s", ev.Path, ev.LineStart, ev.LineEnd, ev.Quote)
}

func mergeTags(a, b []string) []string {
	seen := make(map[string]bool, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, tag := range append(append([]string(nil), a...), b...) {
		if seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	sort.Strings(out)
	return out
}

func sortIssues(issues []schema.Issue) []schema.Issue {
	out := append([]schema.Issue(nil), issues...)
	sort.SliceStable(out, func(i, j int) bool {
		li := firstMergeEvidenceLine(out[i].Evidence)
		lj := firstMergeEvidenceLine(out[j].Evidence)
		if li != lj {
			return li < lj
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func sortQuestions(questions []schema.Question) []schema.Question {
	out := append([]schema.Question(nil), questions...)
	sort.SliceStable(out, func(i, j int) bool {
		li := firstMergeEvidenceLine(out[i].Evidence)
		lj := firstMergeEvidenceLine(out[j].Evidence)
		if li != lj {
			return li < lj
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func firstMergeEvidenceLine(evidence []schema.Evidence) int {
	if len(evidence) == 0 {
		return 0
	}
	return evidence[0].LineStart
}

func remapPatches(patches []schema.Patch, issueIDMap map[string]string) []schema.Patch {
	out := make([]schema.Patch, 0, len(patches))
	for _, patch := range patches {
		mappedID, ok := issueIDMap[patch.IssueID]
		if !ok {
			continue
		}
		patch.IssueID = mappedID
		out = append(out, patch)
	}
	return out
}

func validPatches(raw string, patches []schema.Patch, issues []schema.Issue) []schema.Patch {
	out := make([]schema.Patch, 0, len(patches))
	issueIDs := make(map[string]bool, len(issues))
	for _, issue := range issues {
		issueIDs[issue.ID] = true
	}
	occupied := make([]editRange, 0, len(patches))
	for _, patch := range patches {
		if !issueIDs[patch.IssueID] || strings.TrimSpace(patch.Before) == "" || patch.After == "" {
			continue
		}
		rng, ok := exactUniqueRange(raw, patch.Before)
		if !ok || overlapsAny(rng, occupied) {
			continue
		}
		occupied = append(occupied, rng)
		out = append(out, patch)
	}
	return out
}

type editRange struct {
	start int
	end   int
}

func exactUniqueRange(raw string, before string) (editRange, bool) {
	start := strings.Index(raw, before)
	if start < 0 {
		return editRange{}, false
	}
	next := start + 1
	if next < len(raw) && strings.Contains(raw[next:], before) {
		return editRange{}, false
	}
	return editRange{start: start, end: start + len(before)}, true
}

func overlapsAny(rng editRange, existing []editRange) bool {
	for _, candidate := range existing {
		if rng.start < candidate.end && candidate.start < rng.end {
			return true
		}
	}
	return false
}

var (
	issueIDPatternForMerge    = regexp.MustCompile(`^ISSUE-(\d+)$`)
	questionIDPatternForMerge = regexp.MustCompile(`^Q-(\d+)$`)
)

func maxIssueID(issues []schema.Issue) int {
	maxID := 0
	for _, issue := range issues {
		if n := parseNumericID(issueIDPatternForMerge, issue.ID); n > maxID {
			maxID = n
		}
	}
	return maxID
}

func maxQuestionID(questions []schema.Question) int {
	maxID := 0
	for _, question := range questions {
		if n := parseNumericID(questionIDPatternForMerge, question.ID); n > maxID {
			maxID = n
		}
	}
	return maxID
}

func parseNumericID(re *regexp.Regexp, id string) int {
	matches := re.FindStringSubmatch(id)
	if len(matches) != 2 {
		return 0
	}
	n, _ := strconv.Atoi(matches[1])
	return n
}
