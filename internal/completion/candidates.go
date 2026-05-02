package completion

import (
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/dshills/speccritic/internal/schema"
)

type Input struct {
	SpecText  string
	Profile   string
	Template  *Template
	Issues    []schema.Issue
	Questions []schema.Question
	Config    Config
}

type sourceRecord struct {
	issue       schema.Issue
	questionIDs []string
}

func GenerateCandidates(input Input) []Candidate {
	if input.Template == nil || len(input.Issues) == 0 {
		return nil
	}
	doc := AnalyzeSections(input.SpecText)
	missing := sectionsByHeading(MissingSections(doc, input.Template))
	incomplete := incompleteByHeading(doc, input.Template)
	sources := sourceRecords(input.Issues, input.Questions)
	phraseCache := make(map[string]string)
	candidates := make([]Candidate, 0, len(sources))
	for _, source := range sources {
		section, ok := mapIssueToSection(source.issue, input.Template, phraseCache)
		if !ok {
			continue
		}
		if _, ok := missing[normalizeHeading(section.Heading)]; ok {
			candidate := candidateForMissingSection(doc, input.Template, section, source, input)
			candidates = append(candidates, candidate)
			continue
		}
		if node, ok := incomplete[normalizeHeading(section.Heading)]; ok {
			candidate := candidateForIncompleteSection(doc, section, node, source, input)
			candidates = append(candidates, candidate)
		}
	}
	sortCandidates(candidates)
	return dedupeCandidates(candidates)
}

func sourceRecords(issues []schema.Issue, questions []schema.Question) []sourceRecord {
	questionIDsByIssue := make(map[string][]string)
	for _, question := range questions {
		for _, issueID := range question.Blocks {
			questionIDsByIssue[issueID] = append(questionIDsByIssue[issueID], question.ID)
		}
	}
	sources := make([]sourceRecord, 0, len(issues))
	for _, issue := range issues {
		questionIDs := append([]string(nil), questionIDsByIssue[issue.ID]...)
		sort.Strings(questionIDs)
		sources = append(sources, sourceRecord{issue: issue, questionIDs: questionIDs})
	}
	return sources
}

func mapIssueToSection(issue schema.Issue, tmpl *Template, phraseCache map[string]string) (TemplateSection, bool) {
	for _, section := range tmpl.Sections {
		for _, ruleID := range section.PreflightRuleIDs {
			if issue.ID == ruleID || hasRuleTag(issue.Tags, ruleID) {
				return section, true
			}
		}
	}
	for _, section := range tmpl.Sections {
		for _, category := range section.Categories {
			if issue.Category == category {
				return section, true
			}
		}
	}
	text := normalizedTokens(strings.Join([]string{issue.Title, issue.Description, issue.Recommendation}, " "))
	containsPhrase := func(phrase string) bool {
		normalized, ok := phraseCache[phrase]
		if !ok {
			normalized = normalizedTokens(phrase)
			phraseCache[phrase] = normalized
		}
		return containsTokenPhrase(text, normalized)
	}
	for _, section := range tmpl.Sections {
		if containsPhrase(section.Heading) {
			return section, true
		}
		for _, placeholder := range section.Placeholders {
			for _, keyword := range placeholder.RelevanceKeywords {
				if containsPhrase(keyword) {
					return section, true
				}
			}
		}
	}
	return TemplateSection{}, false
}

func candidateForMissingSection(doc Document, tmpl *Template, section TemplateSection, source sourceRecord, input Input) Candidate {
	text := sectionSkeleton(doc, section)
	candidate := newCandidate(tmpl, section, source, text)
	if skipOpenDecisions(&candidate, input.Config) {
		return candidate
	}
	patch, status := StableInsertionTarget(doc, tmpl, section, text)
	candidate.TargetLine = patch.Line
	candidate.Status = status
	candidate.Before = patch.Before
	candidate.After = patch.After
	return candidate
}

func candidateForIncompleteSection(doc Document, section TemplateSection, node SectionNode, source sourceRecord, input Input) Candidate {
	text := subsectionPlaceholder(node, section)
	candidate := newCandidate(input.Template, section, source, text)
	if skipOpenDecisions(&candidate, input.Config) {
		return candidate
	}
	patch, status := AppendSubsectionTarget(doc, node, text)
	candidate.TargetLine = patch.Line
	candidate.Status = status
	candidate.Before = patch.Before
	candidate.After = patch.After
	return candidate
}

func newCandidate(tmpl *Template, section TemplateSection, source sourceRecord, text string) Candidate {
	return Candidate{
		SourceIssueID:     source.issue.ID,
		SourceQuestionIDs: source.questionIDs,
		Template:          tmpl.Name,
		Section:           section.Heading,
		Status:            StatusSkippedNoSafeLocation,
		Text:              text,
		Severity:          source.issue.Severity,
		SectionOrder:      section.Order,
	}
}

func skipOpenDecisions(candidate *Candidate, cfg Config) bool {
	if cfg.OpenDecisions || !strings.Contains(candidate.Text, openDecisionPrefix) {
		return false
	}
	candidate.Status = StatusSkippedOpenDecisionsDisabled
	return true
}

func sectionSkeleton(doc Document, section TemplateSection) string {
	level := doc.PrimaryLevel
	if level <= 0 {
		level = 2
	}
	if level > 6 {
		level = 6
	}
	lines := []string{strings.Repeat("#", level) + " " + section.Heading}
	lines = append(lines, placeholderLines(section)...)
	return strings.Join(lines, "\n")
}

func subsectionPlaceholder(node SectionNode, section TemplateSection) string {
	level := node.Level + 1
	if level > 6 {
		level = 6
	}
	lines := []string{strings.Repeat("#", level) + " Completion Placeholders"}
	lines = append(lines, placeholderLines(section)...)
	return strings.Join(lines, "\n")
}

func placeholderLines(section TemplateSection) []string {
	lines := make([]string, 0, len(section.Placeholders))
	for _, placeholder := range section.Placeholders {
		lines = append(lines, placeholder.Text)
	}
	return lines
}

func sectionsByHeading(sections []TemplateSection) map[string]TemplateSection {
	out := make(map[string]TemplateSection, len(sections))
	for _, section := range sections {
		out[normalizeHeading(section.Heading)] = section
	}
	return out
}

func incompleteByHeading(doc Document, tmpl *Template) map[string]SectionNode {
	out := make(map[string]SectionNode)
	for _, node := range IncompleteSections(doc, tmpl) {
		out[normalizeHeading(node.Heading)] = node
	}
	return out
}

func sortCandidates(candidates []Candidate) {
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

func dedupeCandidates(candidates []Candidate) []Candidate {
	type candidateKey struct {
		section string
		line    int
		text    string
		status  Status
	}
	seen := make(map[candidateKey]bool, len(candidates))
	out := make([]Candidate, 0, len(candidates))
	for _, candidate := range candidates {
		key := candidateKey{
			section: normalizeHeading(candidate.Section),
			line:    candidate.TargetLine,
			text:    candidate.Text,
			status:  candidate.Status,
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, candidate)
	}
	return out
}

func lineSortValue(line int) int {
	if line <= 0 {
		return math.MaxInt
	}
	return line
}

func severityRank(severity schema.Severity) int {
	switch severity {
	case schema.SeverityCritical:
		return 0
	case schema.SeverityWarn:
		return 1
	case schema.SeverityInfo:
		return 2
	default:
		return 3
	}
}

func hasRuleTag(tags []string, ruleID string) bool {
	target := "rule:" + ruleID
	for _, tag := range tags {
		if tag == target {
			return true
		}
	}
	return false
}

func normalizedTokens(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	inToken := false
	wroteToken := false
	for _, r := range s {
		r = unicode.ToLower(r)
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if !inToken {
				if wroteToken {
					b.WriteByte(' ')
				}
				inToken = true
				wroteToken = true
			}
			b.WriteRune(r)
			continue
		}
		inToken = false
	}
	return " " + b.String() + " "
}

func containsTokenPhrase(normalizedText, normalizedPhrase string) bool {
	if strings.TrimSpace(normalizedPhrase) == "" {
		return false
	}
	return strings.Contains(normalizedText, normalizedPhrase)
}
