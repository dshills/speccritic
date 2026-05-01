package preflight

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dshills/speccritic/internal/schema"
)

type textPattern struct {
	term    string
	pattern *regexp.Regexp
	lower   string
	wordish bool
}

var (
	placeholderPatterns = compileLiteralPatterns([]string{"TODO", "TBD", "FIXME", "???", "[placeholder]", "coming soon", "to be defined"})
	vaguePatterns       = compileWordishPatterns([]string{"fast", "quick", "reasonable", "as needed", "where appropriate", "user-friendly", "robust", "scalable", "secure", "intuitive"})
	weakPatterns        = compileWordishPatterns([]string{"should", "may", "might", "can", "try to", "best effort"})
)

func BuiltinRules() []Rule {
	rules := []Rule{placeholderRule(), vagueRule(), weakRequirementRule()}
	rules = append(rules, structuralRules()...)
	rules = append(rules, contextRules()...)
	return rules
}

func placeholderRule() Rule {
	return Rule{
		ID:             "PREFLIGHT-TODO-001",
		Group:          "placeholder",
		Title:          "Placeholder text remains in spec",
		Description:    "The spec contains placeholder text.",
		Severity:       schema.SeverityCritical,
		Category:       schema.CategoryUnspecifiedConstraint,
		Impact:         "The requirement is incomplete and cannot be safely implemented.",
		Recommendation: "Replace the placeholder with concrete behavior, constraints, or an explicit non-goal.",
		Tags:           []string{"placeholder"},
		Matcher:        linePatternMatcher(placeholderPatterns, false, nil),
	}
}

func vagueRule() Rule {
	return Rule{
		ID:             "PREFLIGHT-VAGUE-001",
		Group:          "vague",
		Title:          "Vague language is not testable",
		Description:    "The spec contains subjective language without measurable criteria.",
		Severity:       schema.SeverityWarn,
		Category:       schema.CategoryNonTestableRequirement,
		Impact:         "Two implementers could satisfy the requirement differently and both believe they complied.",
		Recommendation: "Replace vague terms with measurable acceptance criteria.",
		Tags:           []string{"vague"},
		Matcher:        linePatternMatcher(vaguePatterns, true, nil),
	}
}

func weakRequirementRule() Rule {
	return Rule{
		ID:             "PREFLIGHT-WEAK-001",
		Group:          "weak-requirement",
		Title:          "Requirement uses non-binding language",
		Description:    "The spec uses non-binding language that leaves behavior optional.",
		Severity:       schema.SeverityWarn,
		Category:       schema.CategoryAmbiguousBehavior,
		Impact:         "The implementation cannot know whether the behavior is required.",
		Recommendation: "Use mandatory language such as must, or explicitly mark the behavior as optional with consequences.",
		Tags:           []string{"weak-requirement"},
		Matcher: linePatternMatcher(weakPatterns, true, func(f Finding, cfg Config) Finding {
			if cfg.Strict {
				f.Severity = schema.SeverityCritical
				f.Blocking = true
			}
			return f
		}),
	}
}

func linePatternMatcher(patterns []textPattern, suppressExamples bool, adjust func(Finding, Config) Finding) Matcher {
	return MatcherFunc(func(doc Document, rule Rule, cfg Config) []Finding {
		var findings []Finding
		inSuppressedSection := false
		for i, line := range doc.Lines {
			if isMarkdownHeading(line) {
				inSuppressedSection = suppressExamples && isExampleHeading(line)
			}
			if inSuppressedSection {
				continue
			}
			lowerLine := strings.ToLower(line)
			for _, pattern := range patterns {
				if !pattern.match(line, lowerLine) {
					continue
				}
				finding := Finding{
					LineStart: i + 1,
					Quote:     strings.TrimSpace(line),
					Tags:      []string{rule.Group, "term:" + pattern.term},
				}
				if adjust != nil {
					finding = adjust(finding, cfg)
				}
				findings = append(findings, finding)
			}
		}
		return findings
	})
}

func literalPattern(term string) *regexp.Regexp {
	return regexp.MustCompile(`(?i)` + regexp.QuoteMeta(term))
}

func wordishPattern(term string) *regexp.Regexp {
	parts := strings.Fields(term)
	for i, part := range parts {
		parts[i] = regexp.QuoteMeta(part)
	}
	return regexp.MustCompile(`(?i)(^|[^[:alnum:]_])` + strings.Join(parts, `\s+`) + `([^[:alnum:]_]|$)`)
}

func compileLiteralPatterns(terms []string) []textPattern {
	patterns := make([]textPattern, 0, len(terms))
	for _, term := range terms {
		patterns = append(patterns, textPattern{term: term, pattern: literalPattern(term), lower: strings.ToLower(term)})
	}
	return patterns
}

func compileWordishPatterns(terms []string) []textPattern {
	patterns := make([]textPattern, 0, len(terms))
	for _, term := range terms {
		patterns = append(patterns, textPattern{term: term, pattern: wordishPattern(term), lower: strings.ToLower(term), wordish: true})
	}
	return patterns
}

func (p textPattern) match(line, lowerLine string) bool {
	if p.lower == "" {
		return p.pattern.MatchString(line)
	}
	if !p.wordish {
		return strings.Contains(lowerLine, p.lower)
	}
	return containsWordish(lowerLine, p.lower)
}

func containsWordish(line, term string) bool {
	start := 0
	for {
		idx := strings.Index(line[start:], term)
		if idx < 0 {
			return false
		}
		idx += start
		end := idx + len(term)
		if isBoundary(line, idx-1) && isBoundary(line, end) {
			return true
		}
		start = idx + 1
	}
}

func isBoundary(s string, idx int) bool {
	if idx < 0 || idx >= len(s) {
		return true
	}
	var r rune
	if !utf8.RuneStart(s[idx]) {
		r, _ = utf8.DecodeLastRuneInString(s[:idx+1])
	} else {
		r, _ = utf8.DecodeRuneInString(s[idx:])
	}
	return !(r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r))
}

func isMarkdownHeading(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "#")
}

func isExampleHeading(line string) bool {
	heading := strings.ToLower(strings.TrimLeft(strings.TrimSpace(line), "# "))
	return strings.Contains(heading, "example") ||
		strings.Contains(heading, "anti-pattern") ||
		strings.Contains(heading, "antipattern") ||
		strings.Contains(heading, "bad wording")
}
