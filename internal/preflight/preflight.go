package preflight

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dshills/speccritic/internal/schema"
	"github.com/dshills/speccritic/internal/spec"
)

const TagPreflight = "preflight"

type Mode string

const (
	ModeWarn Mode = "warn"
	ModeGate Mode = "gate"
	ModeOnly Mode = "only"
)

type Config struct {
	Enabled   bool
	Mode      Mode
	Profile   string
	Strict    bool
	IgnoreIDs []string
}

type Result struct {
	Issues []schema.Issue
}

type Rule struct {
	ID             string
	Group          string
	Title          string
	Description    string
	Severity       schema.Severity
	Category       schema.Category
	Profiles       []string
	Impact         string
	Recommendation string
	Blocking       bool
	Tags           []string
	Matcher        Matcher
}

type Finding struct {
	LineStart      int
	LineEnd        int
	Quote          string
	Title          string
	Description    string
	Severity       schema.Severity
	Category       schema.Category
	Impact         string
	Recommendation string
	Blocking       bool
	Tags           []string
}

type Matcher interface {
	Find(doc Document, rule Rule, cfg Config) []Finding
}

type MatcherFunc func(doc Document, rule Rule, cfg Config) []Finding

func (fn MatcherFunc) Find(doc Document, rule Rule, cfg Config) []Finding {
	return fn(doc, rule, cfg)
}

type Document struct {
	Path      string
	Raw       string
	Lines     []string
	LineCount int
}

func Run(s *spec.Spec, cfg Config) (Result, error) {
	return RunRules(s, cfg, BuiltinRules())
}

func RunRules(s *spec.Spec, cfg Config, rules []Rule) (Result, error) {
	if s == nil {
		return Result{}, fmt.Errorf("spec is required")
	}
	if !cfg.Enabled {
		return Result{}, nil
	}
	if err := validateConfig(cfg); err != nil {
		return Result{}, err
	}
	if cfg.Profile == "" {
		cfg.Profile = "general"
	}
	doc := Document{
		Path:      s.Path,
		Raw:       s.Raw,
		Lines:     spec.Lines(s.Raw),
		LineCount: s.LineCount,
	}
	ignored := ignoreSet(cfg.IgnoreIDs)
	var issues []schema.Issue
	for _, rule := range rules {
		if err := validateRule(rule); err != nil {
			return Result{}, err
		}
		if ignored[rule.ID] || !ruleApplies(rule, cfg.Profile) {
			continue
		}
		for _, finding := range rule.Matcher.Find(doc, rule, cfg) {
			issue, err := issueFromFinding(doc, rule, finding)
			if err != nil {
				return Result{}, err
			}
			issues = append(issues, issue)
		}
	}
	issues = dedupeIssues(issues)
	sortIssues(issues)
	return Result{Issues: issues}, nil
}

func BuiltinRules() []Rule {
	return nil
}

func validateConfig(cfg Config) error {
	switch cfg.Mode {
	case "", ModeWarn, ModeGate, ModeOnly:
		return nil
	default:
		return fmt.Errorf("invalid preflight mode %q", cfg.Mode)
	}
}

func validateRule(rule Rule) error {
	if rule.ID == "" {
		return fmt.Errorf("preflight rule ID is required")
	}
	if rule.Title == "" {
		return fmt.Errorf("preflight rule %s title is required", rule.ID)
	}
	if !schema.IsValidCategory(rule.Category) {
		return fmt.Errorf("preflight rule %s has invalid category %q", rule.ID, rule.Category)
	}
	switch rule.Severity {
	case schema.SeverityInfo, schema.SeverityWarn, schema.SeverityCritical:
	default:
		return fmt.Errorf("preflight rule %s has invalid severity %q", rule.ID, rule.Severity)
	}
	if rule.Matcher == nil {
		return fmt.Errorf("preflight rule %s matcher is required", rule.ID)
	}
	return nil
}

func ruleApplies(rule Rule, profile string) bool {
	if len(rule.Profiles) == 0 {
		return true
	}
	for _, p := range rule.Profiles {
		if p == profile || p == "*" {
			return true
		}
	}
	return false
}

func issueFromFinding(doc Document, rule Rule, finding Finding) (schema.Issue, error) {
	lineStart := finding.LineStart
	if lineStart == 0 {
		lineStart = 1
	}
	lineEnd := finding.LineEnd
	if lineEnd == 0 {
		lineEnd = lineStart
	}
	if lineStart < 1 || lineEnd < lineStart || lineEnd > len(doc.Lines) {
		return schema.Issue{}, fmt.Errorf("preflight rule %s produced invalid evidence range %d-%d for %d line spec", rule.ID, lineStart, lineEnd, len(doc.Lines))
	}
	quote := finding.Quote
	if quote == "" {
		quote = strings.Join(doc.Lines[lineStart-1:lineEnd], "\n")
	}
	severity := finding.Severity
	if severity == "" {
		severity = rule.Severity
	}
	if !isValidSeverity(severity) {
		return schema.Issue{}, fmt.Errorf("preflight rule %s produced invalid severity %q", rule.ID, severity)
	}
	category := finding.Category
	if category == "" {
		category = rule.Category
	}
	if !schema.IsValidCategory(category) {
		return schema.Issue{}, fmt.Errorf("preflight rule %s produced invalid category %q", rule.ID, category)
	}
	title := finding.Title
	if title == "" {
		title = rule.Title
	}
	description := finding.Description
	if description == "" {
		description = rule.Description
	}
	impact := finding.Impact
	if impact == "" {
		impact = rule.Impact
	}
	recommendation := finding.Recommendation
	if recommendation == "" {
		recommendation = rule.Recommendation
	}
	tags := append([]string{TagPreflight, "preflight-rule:" + rule.ID}, rule.Tags...)
	tags = append(tags, finding.Tags...)
	blocking := finding.Blocking || rule.Blocking || rule.Severity == schema.SeverityCritical || severity == schema.SeverityCritical
	return schema.Issue{
		ID:          rule.ID,
		Severity:    severity,
		Category:    category,
		Title:       title,
		Description: description,
		Evidence: []schema.Evidence{{
			Path:      doc.Path,
			LineStart: lineStart,
			LineEnd:   lineEnd,
			Quote:     quote,
		}},
		Impact:         impact,
		Recommendation: recommendation,
		Blocking:       blocking,
		Tags:           uniqueStrings(tags),
	}, nil
}

func dedupeIssues(issues []schema.Issue) []schema.Issue {
	seen := make(map[issueKey]bool, len(issues))
	out := make([]schema.Issue, 0, len(issues))
	for _, issue := range issues {
		key := issueKeyFor(issue)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, issue)
	}
	return out
}

type issueKey struct {
	category  schema.Category
	id        string
	lineStart int
	lineEnd   int
	title     string
	desc      string
	quote     string
}

func issueKeyFor(issue schema.Issue) issueKey {
	lineStart, lineEnd := 0, 0
	if len(issue.Evidence) > 0 {
		lineStart = issue.Evidence[0].LineStart
		lineEnd = issue.Evidence[0].LineEnd
	}
	quote := ""
	if len(issue.Evidence) > 0 {
		quote = issue.Evidence[0].Quote
	}
	return issueKey{category: issue.Category, id: issue.ID, lineStart: lineStart, lineEnd: lineEnd, title: issue.Title, desc: issue.Description, quote: quote}
}

func isValidSeverity(severity schema.Severity) bool {
	switch severity {
	case schema.SeverityInfo, schema.SeverityWarn, schema.SeverityCritical:
		return true
	default:
		return false
	}
}

func sortIssues(issues []schema.Issue) {
	sort.SliceStable(issues, func(i, j int) bool {
		left := issues[i]
		right := issues[j]
		leftLine := firstLine(left)
		rightLine := firstLine(right)
		if leftLine != rightLine {
			return leftLine < rightLine
		}
		if severityRank(left.Severity) != severityRank(right.Severity) {
			return severityRank(left.Severity) > severityRank(right.Severity)
		}
		return left.ID < right.ID
	})
}

func firstLine(issue schema.Issue) int {
	if len(issue.Evidence) == 0 {
		return 0
	}
	return issue.Evidence[0].LineStart
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

func ignoreSet(ids []string) map[string]bool {
	out := make(map[string]bool, len(ids))
	for _, id := range ids {
		out[id] = true
	}
	return out
}

func uniqueStrings(values []string) []string {
	seen := make(map[string]bool, len(values))
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
