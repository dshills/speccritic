package preflight

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/dshills/speccritic/internal/schema"
)

type sectionGroup struct {
	id       string
	title    string
	profiles []string
	terms    []string
	norms    [][]string
}

func structuralRules() []Rule {
	groups := []sectionGroup{
		{id: "PREFLIGHT-STRUCTURE-001", title: "Missing purpose or goals section", profiles: []string{"*"}, terms: []string{"purpose", "goals", "goal"}},
		{id: "PREFLIGHT-STRUCTURE-002", title: "Missing non-goals or out-of-scope section", profiles: []string{"*"}, terms: []string{"non goals", "non-goals", "out of scope", "out-of-scope"}},
		{id: "PREFLIGHT-STRUCTURE-003", title: "Missing requirements or behavior section", profiles: []string{"*"}, terms: []string{"requirements", "functional behavior", "behavior"}},
		{id: "PREFLIGHT-STRUCTURE-004", title: "Missing acceptance criteria or testability section", profiles: []string{"*"}, terms: []string{"acceptance criteria", "testability", "tests", "testing"}},
		{id: "PREFLIGHT-STRUCTURE-101", title: "Missing authentication or authorization section", profiles: []string{"backend-api"}, terms: []string{"authentication", "authorization", "auth"}},
		{id: "PREFLIGHT-STRUCTURE-102", title: "Missing endpoints or routes section", profiles: []string{"backend-api"}, terms: []string{"endpoints", "routes", "api routes"}},
		{id: "PREFLIGHT-STRUCTURE-103", title: "Missing request or response schemas section", profiles: []string{"backend-api"}, terms: []string{"request schema", "response schema", "schemas", "request response"}},
		{id: "PREFLIGHT-STRUCTURE-104", title: "Missing error responses section", profiles: []string{"backend-api"}, terms: []string{"error responses", "error codes", "errors"}},
		{id: "PREFLIGHT-STRUCTURE-105", title: "Missing rate limits or abuse handling section", profiles: []string{"backend-api"}, terms: []string{"rate limits", "rate limiting", "abuse handling", "abuse"}},
		{id: "PREFLIGHT-STRUCTURE-201", title: "Missing audit trail section", profiles: []string{"regulated-system"}, terms: []string{"audit trail", "audit"}},
		{id: "PREFLIGHT-STRUCTURE-202", title: "Missing data retention section", profiles: []string{"regulated-system"}, terms: []string{"data retention", "retention"}},
		{id: "PREFLIGHT-STRUCTURE-203", title: "Missing access control section", profiles: []string{"regulated-system"}, terms: []string{"access control", "permissions"}},
		{id: "PREFLIGHT-STRUCTURE-204", title: "Missing compliance or regulatory constraints section", profiles: []string{"regulated-system"}, terms: []string{"compliance", "regulatory", "regulation"}},
		{id: "PREFLIGHT-STRUCTURE-301", title: "Missing event schema section", profiles: []string{"event-driven"}, terms: []string{"event schema", "event schemas"}},
		{id: "PREFLIGHT-STRUCTURE-302", title: "Missing delivery guarantees section", profiles: []string{"event-driven"}, terms: []string{"delivery guarantees", "delivery guarantee"}},
		{id: "PREFLIGHT-STRUCTURE-303", title: "Missing retry or dead-letter behavior section", profiles: []string{"event-driven"}, terms: []string{"retry", "dead letter", "dead-letter", "dlq"}},
		{id: "PREFLIGHT-STRUCTURE-304", title: "Missing consumer failure behavior section", profiles: []string{"event-driven"}, terms: []string{"consumer failure", "consumer failures"}},
	}
	rules := make([]Rule, 0, len(groups))
	for _, group := range groups {
		group := group
		group.norms = normalizeTerms(group.terms)
		rules = append(rules, Rule{
			ID:             group.id,
			Group:          "missing-section",
			Title:          group.title,
			Description:    "The spec does not include a required section for the selected profile.",
			Severity:       schema.SeverityCritical,
			Category:       schema.CategoryUnspecifiedConstraint,
			Profiles:       group.profiles,
			Impact:         "The missing section leaves required behavior or constraints unspecified.",
			Recommendation: fmt.Sprintf("Add a section covering one of: %s.", strings.Join(group.terms, ", ")),
			Tags:           []string{"missing-section"},
			Matcher: MatcherFunc(func(doc Document, _ Rule, _ Config) []Finding {
				if hasHeading(doc.Lines, group.norms) {
					return nil
				}
				return []Finding{{LineStart: fallbackEvidenceLine(doc.Lines)}}
			}),
		})
	}
	return rules
}

func hasHeading(lines []string, terms [][]string) bool {
	for _, line := range lines {
		if !isMarkdownHeading(line) {
			continue
		}
		heading := strings.Fields(normalizeHeading(line))
		for _, term := range terms {
			if containsPhrase(heading, term) {
				return true
			}
		}
	}
	return false
}

func normalizeTerms(terms []string) [][]string {
	out := make([][]string, 0, len(terms))
	for _, term := range terms {
		out = append(out, strings.Fields(normalizeText(term)))
	}
	return out
}

func containsPhrase(words, phrase []string) bool {
	if len(phrase) == 0 || len(phrase) > len(words) {
		return false
	}
	for i := 0; i <= len(words)-len(phrase); i++ {
		match := true
		for j := range phrase {
			if words[i+j] != phrase[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}

func normalizeHeading(line string) string {
	trimmed := strings.TrimSpace(line)
	trimmed = strings.TrimLeft(trimmed, "#")
	return normalizeText(trimmed)
}

func normalizeText(value string) string {
	var b strings.Builder
	lastSpace := true
	for _, r := range strings.ToLower(value) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(r)
			lastSpace = false
		default:
			if !lastSpace {
				b.WriteByte(' ')
				lastSpace = true
			}
		}
	}
	return strings.TrimSpace(b.String())
}

func fallbackEvidenceLine(lines []string) int {
	for i, line := range lines {
		if isMarkdownHeading(line) {
			return i + 1
		}
	}
	return 1
}
