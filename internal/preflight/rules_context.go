package preflight

import (
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/dshills/speccritic/internal/schema"
)

var (
	acronymRe          = regexp.MustCompile(`\b[A-Z][A-Z0-9]{1,}\b`)
	parentheticalRe    = regexp.MustCompile(`\(([A-Z][A-Z0-9]{1,})\)`)
	measurableValueRe  = regexp.MustCompile(`(?i)(\b\d+(\.\d+)?\s*(ns|us|µs|ms|s|sec|secs|second|seconds|min|mins|minute|minutes|hour|hours|day|days|kb|mb|gb|kib|mib|gib|bytes?|percent|rps|qps|requests?|queries|cores?)\b|\b\d+(\.\d+)?\s*%|\bP\d+[DT][A-Z0-9]+\b|\b(at least|at most|no more than|less than|greater than|exactly)\s+\d+(\.\d+)?)`)
	measurablePatterns = compileWordishPatterns([]string{"latency", "throughput", "availability", "retry", "timeout", "retention", "rate limit", "file size"})
)

var allowedAcronyms = map[string]bool{
	"API": true, "CLI": true, "CPU": true, "CSS": true, "CSV": true, "DNS": true,
	"HTML": true, "HTTP": true, "HTTPS": true, "ID": true, "IP": true, "JSON": true,
	"LLM": true, "SQL": true, "UI": true, "URL": true, "UTF": true, "UUID": true,
	"XML": true, "TCP": true, "UDP": true, "TLS": true, "RAM": true, "SSH": true,
	"UTC": true, "SDK": true, "JWT": true, "CORS": true, "CRUD": true, "AI": true,
	"AS": true, "DB": true, "IF": true, "IN": true, "OR": true, "OS": true, "TO": true,
}

func contextRules() []Rule {
	return []Rule{undefinedAcronymRule(), measurableCriteriaRule()}
}

func undefinedAcronymRule() Rule {
	return Rule{
		ID:             "PREFLIGHT-ACRONYM-001",
		Group:          "acronym",
		Title:          "Acronym is used without definition",
		Description:    "The spec uses an acronym that is not defined locally.",
		Severity:       schema.SeverityWarn,
		Category:       schema.CategoryTerminologyInconsistent,
		Impact:         "Readers may interpret the acronym differently.",
		Recommendation: "Define the acronym on first use or add it to a glossary.",
		Tags:           []string{"acronym"},
		Matcher: MatcherFunc(func(doc Document, _ Rule, _ Config) []Finding {
			defined := collectDefinedAcronyms(doc.Lines)
			seen := make(map[string]bool)
			var findings []Finding
			for i, line := range doc.Lines {
				for _, token := range acronymRe.FindAllString(line, -1) {
					if allowedAcronyms[token] || defined[token] || seen[token] {
						continue
					}
					seen[token] = true
					findings = append(findings, Finding{
						LineStart: i + 1,
						Quote:     strings.TrimSpace(line),
						Tags:      []string{"acronym", "term:" + token},
					})
				}
			}
			return findings
		}),
	}
}

func measurableCriteriaRule() Rule {
	return Rule{
		ID:             "PREFLIGHT-MEASURABLE-001",
		Group:          "measurable",
		Title:          "Measurable requirement lacks concrete criteria",
		Description:    "The spec mentions a measurable domain without a concrete value or enum.",
		Severity:       schema.SeverityWarn,
		Category:       schema.CategoryNonTestableRequirement,
		Impact:         "No objective acceptance test can verify the requirement.",
		Recommendation: "Add a number, duration, percentage, size, or explicit enum for the measurable requirement.",
		Tags:           []string{"measurable"},
		Matcher: MatcherFunc(func(doc Document, _ Rule, _ Config) []Finding {
			var findings []Finding
			for i, line := range doc.Lines {
				for _, pattern := range measurablePatterns {
					if pattern.pattern.MatchString(line) {
						if hasMeasurableValueNearTerm(line, pattern.pattern) {
							continue
						}
						findings = append(findings, Finding{
							LineStart: i + 1,
							Quote:     strings.TrimSpace(line),
							Tags:      []string{"measurable", "term:" + pattern.term},
						})
						break
					}
				}
			}
			return findings
		}),
	}
}

func hasMeasurableValueNearTerm(line string, pattern *regexp.Regexp) bool {
	for _, loc := range pattern.FindAllStringIndex(line, -1) {
		start := loc[0] - 10
		if start < 0 {
			start = 0
		}
		end := loc[1] + 120
		if end > len(line) {
			end = len(line)
		}
		start = nearestRuneStart(line, start, -1)
		end = nearestRuneStart(line, end, 1)
		if measurableValueRe.MatchString(line[start:end]) {
			return true
		}
	}
	return false
}

func nearestRuneStart(s string, index, direction int) int {
	if index <= 0 {
		return 0
	}
	if index >= len(s) {
		return len(s)
	}
	for index > 0 && index < len(s) && !utf8.RuneStart(s[index]) {
		index += direction
	}
	if index < 0 {
		return 0
	}
	if index > len(s) {
		return len(s)
	}
	return index
}

func collectDefinedAcronyms(lines []string) map[string]bool {
	defined := make(map[string]bool)
	inGlossary := false
	for _, line := range lines {
		if isMarkdownHeading(line) {
			heading := normalizeHeading(line)
			inGlossary = strings.Contains(heading, "glossary") || strings.Contains(heading, "definitions")
		}
		for _, match := range parentheticalRe.FindAllStringSubmatch(line, -1) {
			defined[match[1]] = true
		}
		if inGlossary {
			for _, token := range acronymRe.FindAllString(line, -1) {
				defined[token] = true
			}
		}
	}
	return defined
}
