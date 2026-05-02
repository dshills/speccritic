package convergence

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"

	"github.com/dshills/speccritic/internal/schema"
)

// TrackIssues converts schema issues into deterministic tracked findings.
func TrackIssues(issues []schema.Issue) []TrackedFinding {
	out := make([]TrackedFinding, 0, len(issues))
	for i, issue := range issues {
		f := TrackedFinding{
			Kind:        KindIssue,
			ID:          issue.ID,
			Severity:    issue.Severity,
			Category:    string(issue.Category),
			Text:        issue.Title,
			Evidence:    append([]schema.Evidence(nil), issue.Evidence...),
			Tags:        append([]string(nil), issue.Tags...),
			SourceIndex: i,
		}
		out = append(out, f)
	}
	return out
}

// TrackQuestions converts schema questions into deterministic tracked findings.
func TrackQuestions(questions []schema.Question) []TrackedFinding {
	out := make([]TrackedFinding, 0, len(questions))
	for i, question := range questions {
		f := TrackedFinding{
			Kind:        KindQuestion,
			ID:          question.ID,
			Severity:    question.Severity,
			Category:    "QUESTION",
			Text:        question.Question,
			Evidence:    append([]schema.Evidence(nil), question.Evidence...),
			SourceIndex: i,
		}
		out = append(out, f)
	}
	return out
}

// ComputeFingerprints returns a copy of findings with Fingerprint populated.
func ComputeFingerprints(findings []TrackedFinding) []TrackedFinding {
	out := make([]TrackedFinding, len(findings))
	copy(out, findings)
	for i := range out {
		out[i].SectionPath = append([]string(nil), out[i].SectionPath...)
		out[i].Evidence = append([]schema.Evidence(nil), out[i].Evidence...)
		out[i].Tags = append([]string(nil), out[i].Tags...)
		out[i].Fingerprint = Fingerprint(out[i])
	}
	return out
}

// Fingerprint computes the stable identity hash for a tracked finding.
func Fingerprint(f TrackedFinding) string {
	parts := []string{
		"kind:" + string(f.Kind),
		"severity:" + normalizeToken(string(f.Severity)),
		"category:" + normalizeToken(f.Category),
		"text:" + normalizeText(f.Text),
		"evidence:" + evidenceText(f.Evidence),
	}
	for _, pathPart := range f.SectionPath {
		parts = append(parts, "section:"+normalizeText(pathPart))
	}
	tags := stableTags(f.Tags)
	for _, tag := range tags {
		parts = append(parts, "tag:"+tag)
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func normalizeToken(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	s = strings.ReplaceAll(s, "\x1f", "")
	return strings.ToLower(strings.TrimSpace(s))
}

func normalizeText(s string) string {
	s = strings.ReplaceAll(s, "\x00", "")
	s = strings.ReplaceAll(s, "\x1f", "")
	s = strings.TrimSpace(s)
	s = strings.Join(strings.Fields(s), " ")
	return strings.ToLower(s)
}

func evidenceText(evidence []schema.Evidence) string {
	if len(evidence) == 0 {
		return ""
	}
	unique := make(map[string]struct{}, len(evidence))
	for _, ev := range evidence {
		if strings.TrimSpace(ev.Quote) != "" {
			unique[normalizeText(ev.Quote)] = struct{}{}
		}
	}
	quotes := make([]string, 0, len(unique))
	for quote := range unique {
		quotes = append(quotes, quote)
	}
	sort.Strings(quotes)
	for i, quote := range quotes {
		quotes[i] = "quote:" + quote
	}
	return strings.Join(quotes, "\x1f")
}

func stableTags(tags []string) []string {
	out := make([]string, 0, len(tags))
	for _, tag := range tags {
		normalized := normalizeToken(tag)
		if normalized == "" || isVolatileTag(normalized) {
			continue
		}
		out = append(out, normalized)
	}
	sort.Strings(out)
	return out
}

func isVolatileTag(tag string) bool {
	switch tag {
	case "incremental-reused", "llm-repaired", "provider-repaired", "repair":
		return true
	}
	return strings.HasPrefix(tag, "chunk:") || strings.HasPrefix(tag, "range:")
}
