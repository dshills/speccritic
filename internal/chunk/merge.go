package chunk

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dshills/speccritic/internal/evidence"
	"github.com/dshills/speccritic/internal/schema"
)

// tagPreflight marks a finding from the deterministic preflight rules. It
// matches preflight.TagPreflight.
const tagPreflight = "preflight"

type MergeInput struct {
	ChunkResults []ChunkResult
	Preflight    []schema.Issue
	OriginalSpec string
}

type MergeResult struct {
	Issues    []schema.Issue
	Questions []schema.Question
	Patches   []schema.Patch
	// DroppedFindings counts findings the chunk and synthesis parsers left
	// out as invalid.
	DroppedFindings int
}

func MergeReports(input MergeInput) MergeResult {
	var issues []schema.Issue
	issues = appendIssues(issues, input.Preflight...)
	for _, result := range input.ChunkResults {
		if result.Report == nil {
			continue
		}
		for _, issue := range result.Report.Issues {
			issue.Tags = appendUniqueStrings(copyStrings(issue.Tags), "chunked-review")
			issues = append(issues, issue)
		}
	}
	issues, issueIDMap := renumberIssues(sortIssues(dedupeChunkIssues(issues)))

	var questions []schema.Question
	for _, result := range input.ChunkResults {
		if result.Report != nil {
			questions = append(questions, result.Report.Questions...)
		}
	}
	questions = renumberQuestions(sortQuestions(questions))

	var patches []schema.Patch
	for _, result := range input.ChunkResults {
		if result.Report != nil {
			patches = appendValidPatches(patches, input.OriginalSpec, issueIDMap, result.Report.Patches...)
		}
	}

	dropped := 0
	for _, result := range input.ChunkResults {
		if result.Report != nil {
			dropped += result.Report.Meta.DroppedFindings
		}
	}

	return MergeResult{Issues: issues, Questions: questions, Patches: patches, DroppedFindings: dropped}
}

// synthesisIDPrefix renames the new findings from synthesis while they are
// merged, so their IDs cannot collide with the chunk findings' IDs.
const synthesisIDPrefix = "SYNTHESIS-"

// ApplySynthesis folds the result of the cross-section pass into merged:
//
//   - A retraction removes a chunk finding when its quote is found in the
//     spec. Preflight findings are never retracted. Every retraction applied
//     is recorded with the spec text that justified it.
//   - Each merge group keeps the first of its findings, which is the most
//     severe, and folds the others into it.
//   - New findings are added, deduplicated against the rest.
//
// Findings are then renumbered and patches re-pointed at the new numbers; a
// patch for a removed finding is dropped. specText is the spec the model was
// shown and specPath its evidence path.
func ApplySynthesis(merged MergeResult, synthesis *SynthesisResult, specText, specPath string) (MergeResult, *schema.SynthesisMeta) {
	if synthesis == nil {
		return merged, nil
	}
	meta := &schema.SynthesisMeta{}
	removedIssues := map[int]bool{}
	removedQuestions := map[int]bool{}
	issueAt := map[string]int{}
	for i, issue := range merged.Issues {
		issueAt[issue.ID] = i
	}
	questionAt := map[string]int{}
	for i, question := range merged.Questions {
		questionAt[question.ID] = i
	}

	index := evidence.NewIndex(specText)
	for _, r := range synthesis.Retractions {
		answer, outcome := index.Anchor(schema.Evidence{Path: specPath, LineStart: r.LineStart, LineEnd: r.LineEnd, Quote: r.Quote})
		found := outcome == evidence.Verified || outcome == evidence.Moved
		if i, ok := issueAt[r.ID]; ok && found && !removedIssues[i] && !hasTag(merged.Issues[i].Tags, tagPreflight) {
			issue := merged.Issues[i]
			removedIssues[i] = true
			meta.Retracted = append(meta.Retracted, schema.RemovedFinding{Title: issue.Title, Severity: issue.Severity, Category: issue.Category, Reason: r.Reason, AnsweredBy: answer})
			continue
		}
		if i, ok := questionAt[r.ID]; ok && found && !removedQuestions[i] {
			question := merged.Questions[i]
			removedQuestions[i] = true
			meta.Retracted = append(meta.Retracted, schema.RemovedFinding{Title: question.Question, Severity: question.Severity, Reason: r.Reason, AnsweredBy: answer})
			continue
		}
		meta.IgnoredRetractions++
	}

	// Union the merge groups, so findings named in two overlapping groups end
	// up in one.
	parent := make([]int, len(merged.Issues))
	for i := range parent {
		parent[i] = i
	}
	find := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	for _, group := range synthesis.Merges {
		first := -1
		for _, id := range group.IssueIDs {
			i, ok := issueAt[id]
			if !ok || removedIssues[i] {
				continue
			}
			if first < 0 {
				first = i
				continue
			}
			a, b := find(first), find(i)
			if a != b {
				// Issues are sorted most severe first, so the lower index is the
				// one to keep.
				parent[max(a, b)] = min(a, b)
			}
		}
	}

	issues := make([]schema.Issue, 0, len(merged.Issues))
	keptAt := map[int]int{}
	for i, issue := range merged.Issues {
		if removedIssues[i] {
			continue
		}
		root := find(i)
		if root == i {
			keptAt[i] = len(issues)
			issue.Tags = copyStrings(issue.Tags)
			issues = append(issues, issue)
		}
	}
	for i, issue := range merged.Issues {
		if removedIssues[i] || find(i) == i {
			continue
		}
		k := keptAt[find(i)]
		issues[k] = mergeDuplicateIssue(issues[k], issue)
		meta.MergedFindings++
	}

	var newPatches []schema.Patch
	if synthesis.Report != nil {
		renamed := map[string]string{}
		for _, issue := range synthesis.Report.Issues {
			id := synthesisIDPrefix + issue.ID
			renamed[issue.ID] = id
			issue.ID = id
			issue.Tags = appendUniqueStrings(copyStrings(issue.Tags), TagSynthesis)
			issues = append(issues, issue)
		}
		for _, patch := range synthesis.Report.Patches {
			if id, ok := renamed[patch.IssueID]; ok {
				patch.IssueID = id
				newPatches = append(newPatches, patch)
			}
		}
	}
	issues, issueIDMap := renumberIssues(sortIssues(dedupeChunkIssues(issues)))

	var questions []schema.Question
	for i, question := range merged.Questions {
		if !removedQuestions[i] {
			questions = append(questions, question)
		}
	}
	if synthesis.Report != nil {
		questions = append(questions, synthesis.Report.Questions...)
	}
	questions = renumberQuestions(sortQuestions(questions))

	patches := appendValidPatches(nil, specText, issueIDMap, merged.Patches...)
	patches = appendValidPatches(patches, specText, issueIDMap, newPatches...)

	dropped := merged.DroppedFindings
	if synthesis.Report != nil {
		dropped += synthesis.Report.Meta.DroppedFindings
	}
	return MergeResult{Issues: issues, Questions: questions, Patches: patches, DroppedFindings: dropped}, meta
}

func appendIssues(dst []schema.Issue, src ...schema.Issue) []schema.Issue {
	for _, issue := range src {
		issue.Tags = copyStrings(issue.Tags)
		dst = append(dst, issue)
	}
	return dst
}

func dedupeChunkIssues(issues []schema.Issue) []schema.Issue {
	var out []schema.Issue
	buckets := make(map[string][]int)
	for _, issue := range issues {
		key := issueBucketKey(issue)
		if idx := findDuplicateIssue(out, buckets[key], issue); idx >= 0 {
			out[idx] = mergeDuplicateIssue(out[idx], issue)
			continue
		}
		out = append(out, issue)
		buckets[key] = append(buckets[key], len(out)-1)
	}
	return out
}

func findDuplicateIssue(issues []schema.Issue, bucket []int, candidate schema.Issue) int {
	for _, i := range bucket {
		if issueEvidenceOverlaps(issues[i], candidate) {
			return i
		}
	}
	return -1
}

func mergeDuplicateIssue(a, b schema.Issue) schema.Issue {
	a.Blocking = a.Blocking || b.Blocking
	if severityRank(b.Severity) > severityRank(a.Severity) {
		a.Severity = b.Severity
	}
	if b.ID != "" && b.ID != a.ID {
		a.Tags = appendUniqueStrings(copyStrings(a.Tags), issueAliasTag(b.ID))
	} else {
		a.Tags = copyStrings(a.Tags)
	}
	a.Tags = appendUniqueStrings(a.Tags, b.Tags...)
	a.Evidence = mergeEvidence(a.Evidence, b.Evidence)
	if len(b.Recommendation) > len(a.Recommendation) {
		a.Recommendation = b.Recommendation
	}
	if len(b.Description) > len(a.Description) {
		a.Description = b.Description
	}
	return a
}

func sortIssues(issues []schema.Issue) []schema.Issue {
	out := append([]schema.Issue(nil), issues...)
	sort.SliceStable(out, func(i, j int) bool {
		if severityRank(out[i].Severity) != severityRank(out[j].Severity) {
			return severityRank(out[i].Severity) > severityRank(out[j].Severity)
		}
		if firstLine(out[i].Evidence) != firstLine(out[j].Evidence) {
			return firstLine(out[i].Evidence) < firstLine(out[j].Evidence)
		}
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].Title < out[j].Title
	})
	return out
}

func renumberIssues(issues []schema.Issue) ([]schema.Issue, map[string]string) {
	out := append([]schema.Issue(nil), issues...)
	idMap := make(map[string]string, len(out))
	for i := range out {
		newID := fmt.Sprintf("ISSUE-%04d", i+1)
		if out[i].ID != "" {
			idMap[out[i].ID] = newID
		}
		tags := make([]string, 0, len(out[i].Tags))
		for _, tag := range out[i].Tags {
			if originalID, ok := strings.CutPrefix(tag, issueAliasTagPrefix); ok {
				if originalID != "" {
					idMap[originalID] = newID
				}
				continue
			}
			tags = append(tags, tag)
		}
		out[i].Tags = tags
		out[i].ID = newID
	}
	return out, idMap
}

func sortQuestions(questions []schema.Question) []schema.Question {
	out := append([]schema.Question(nil), questions...)
	sort.SliceStable(out, func(i, j int) bool {
		if severityRank(out[i].Severity) != severityRank(out[j].Severity) {
			return severityRank(out[i].Severity) > severityRank(out[j].Severity)
		}
		if firstLine(out[i].Evidence) != firstLine(out[j].Evidence) {
			return firstLine(out[i].Evidence) < firstLine(out[j].Evidence)
		}
		return out[i].Question < out[j].Question
	})
	return out
}

func renumberQuestions(questions []schema.Question) []schema.Question {
	out := append([]schema.Question(nil), questions...)
	for i := range out {
		out[i].ID = fmt.Sprintf("Q-%04d", i+1)
	}
	return out
}

func appendValidPatches(dst []schema.Patch, original string, issueIDMap map[string]string, patches ...schema.Patch) []schema.Patch {
	for _, patch := range patches {
		if patch.Before == "" {
			continue
		}
		if !uniquePatchSite(original, patch.Before) {
			continue
		}
		newIssueID, ok := issueIDMap[patch.IssueID]
		if !ok {
			continue
		}
		patch.IssueID = newIssueID
		dst = append(dst, patch)
	}
	return dst
}

// uniquePatchSite returns true only when before appears exactly once in raw,
// including overlapping occurrences (e.g. "abab" in "ababab" counts as two).
func uniquePatchSite(raw, before string) bool {
	start := strings.Index(raw, before)
	if start < 0 {
		return false
	}
	next := start + 1
	return next >= len(raw) || !strings.Contains(raw[next:], before)
}

func normalizedTitle(title string) string {
	return strings.Join(strings.Fields(strings.ToLower(title)), " ")
}

func issueBucketKey(issue schema.Issue) string {
	return string(issue.Category) + "\x00" + normalizedTitle(issue.Title)
}

func issueEvidenceOverlaps(a, b schema.Issue) bool {
	for _, left := range a.Evidence {
		for _, right := range b.Evidence {
			if left.Path == right.Path && left.LineStart <= right.LineEnd && right.LineStart <= left.LineEnd {
				return true
			}
		}
	}
	return false
}

func mergeEvidence(a, b []schema.Evidence) []schema.Evidence {
	out := append([]schema.Evidence(nil), a...)
	seen := make(map[string]bool, len(out)+len(b))
	for _, evidence := range out {
		seen[evidenceKey(evidence)] = true
	}
	for _, evidence := range b {
		key := evidenceKey(evidence)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, evidence)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].LineStart != out[j].LineStart {
			return out[i].LineStart < out[j].LineStart
		}
		if out[i].LineEnd != out[j].LineEnd {
			return out[i].LineEnd < out[j].LineEnd
		}
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].Quote < out[j].Quote
	})
	return out
}

func evidenceKey(evidence schema.Evidence) string {
	return fmt.Sprintf("%s\x00%d\x00%d\x00%s", evidence.Path, evidence.LineStart, evidence.LineEnd, evidence.Quote)
}

func firstLine(evidence []schema.Evidence) int {
	if len(evidence) == 0 {
		return 0
	}
	return evidence[0].LineStart
}

const issueAliasTagPrefix = "merged-issue-id:"

func issueAliasTag(issueID string) string {
	return issueAliasTagPrefix + issueID
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

func appendUniqueStrings(dst []string, values ...string) []string {
	for _, value := range values {
		exists := false
		for _, existing := range dst {
			if existing == value {
				exists = true
				break
			}
		}
		if !exists {
			dst = append(dst, value)
		}
	}
	return dst
}

func copyStrings(values []string) []string {
	if values == nil {
		return nil
	}
	return append([]string(nil), values...)
}
