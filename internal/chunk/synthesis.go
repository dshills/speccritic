package chunk

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dshills/speccritic/internal/llm"
	"github.com/dshills/speccritic/internal/schema"
	"github.com/dshills/speccritic/internal/schema/validate"
	"github.com/dshills/speccritic/internal/spec"
)

// The synthesis task lists findings one line each, so these caps are far
// above what a spec produces in practice.
const maxSynthesisFindings = 200
const maxSynthesisQuestions = 100

// maxSynthesisDescription bounds the description shown for each finding.
const maxSynthesisDescription = 160

const TagSynthesis = "synthesis"

type SynthesisConfig struct {
	SystemPrompt string
	// Prefix is the shared user-message prefix from llm.BuildSpecPrefix, the
	// same one the chunk calls sent, so synthesis reads the spec from cache.
	Prefix string
	// LongCache is passed through to llm.Request.LongCache.
	LongCache bool
	// SpecShown reports that Prefix holds the whole spec. It is false when the
	// spec was too large to share, and synthesis then works from the findings.
	SpecShown bool
	MaxTokens int
	Effort    string
	// EnforceSchema asks the provider to constrain the response to the review
	// schema.
	EnforceSchema bool
	LineThreshold int
	Enabled       bool
}

type SynthesisInput struct {
	Merged MergeResult
	// SpecShown reports that the shared prefix holds the whole spec.
	SpecShown bool
}

func ShouldRunSynthesis(chunkingEnabled bool, lineCount int, findingCount int, threshold int) bool {
	if !chunkingEnabled {
		return false
	}
	if threshold <= 0 {
		threshold = DefaultSynthesisLineThreshold
	}
	if findingCount == 0 && lineCount < threshold {
		return false
	}
	return true
}

// BuildSynthesisTask returns the task for the cross-section pass. It follows
// the shared prefix, so synthesis reads the whole spec and the preflight
// findings there; the task lists what the chunk reviews found, one line each.
func BuildSynthesisTask(input SynthesisInput) (string, error) {
	var b strings.Builder
	if input.SpecShown {
		b.WriteString("\nThe specification above was reviewed in chunks, each reviewer reporting only on its own lines. Their findings follow.\n")
	} else {
		b.WriteString("\nA specification too large to show in full was reviewed in chunks, each reviewer seeing only its own lines. Their findings follow; they are all you have of the specification.\n")
	}
	b.WriteString("\n<chunk_findings>\n")
	for _, issue := range limitIssues(input.Merged.Issues, maxSynthesisFindings) {
		source := "chunk"
		if hasTag(issue.Tags, tagPreflight) {
			source = "preflight"
		}
		fmt.Fprintf(&b, "- %s %s %s %s [%s]: %s", issue.ID, issue.Severity, issue.Category, lineLabel(issue.Evidence), source, oneLine(issue.Title, 0))
		if issue.Description != "" {
			fmt.Fprintf(&b, " - %s", oneLine(issue.Description, maxSynthesisDescription))
		}
		b.WriteString("\n")
	}
	for _, question := range limitQuestions(input.Merged.Questions, maxSynthesisQuestions) {
		fmt.Fprintf(&b, "- %s %s %s: %s\n", question.ID, question.Severity, lineLabel(question.Evidence), oneLine(question.Question, 0))
	}
	if omitted := max(0, len(input.Merged.Issues)-maxSynthesisFindings) + max(0, len(input.Merged.Questions)-maxSynthesisQuestions); omitted > 0 {
		fmt.Fprintf(&b, "- (%d more not listed)\n", omitted)
	}
	b.WriteString("</chunk_findings>\n\n")

	b.WriteString("Do three things.\n")
	b.WriteString("1. merge: group findings above that report the same defect, such as one gap reported by several chunk reviewers. List each group's ISSUE ids. Do not group findings that are merely related.\n")
	if input.SpecShown {
		b.WriteString("2. retract: list findings above that the specification already answers, typically a term or rule a chunk reviewer reported as missing because it is defined in another section. Give the lines that answer it and copy their text exactly in quote; a retraction whose quote is not in the specification is ignored. Do not retract a finding because it seems minor, and never retract a [preflight] finding.\n")
	} else {
		b.WriteString("2. retract: return an empty list, since you cannot see the specification to quote what answers a finding.\n")
	}
	b.WriteString("3. issues and questions: report new defects that span sections and so could not be seen from one chunk: contradictions between sections, interfaces used in one section and never defined, terms used inconsistently, ordering that depends on several sections, and blocking questions. Do not repeat findings above. Cite specification line numbers. Do not emit score or verdict.\n")
	b.WriteString("Return empty lists for anything you have nothing to report.\n")
	return b.String(), nil
}

// oneLine collapses whitespace so a text fits on one line of the task, and
// shortens it to limit runes when limit is positive.
func oneLine(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if limit > 0 {
		if runes := []rune(text); len(runes) > limit {
			return string(runes[:limit]) + "..."
		}
	}
	return text
}

func lineLabel(evidence []schema.Evidence) string {
	if len(evidence) == 0 {
		return "L?"
	}
	if evidence[0].LineEnd > evidence[0].LineStart {
		return fmt.Sprintf("L%d-L%d", evidence[0].LineStart, evidence[0].LineEnd)
	}
	return fmt.Sprintf("L%d", evidence[0].LineStart)
}

// SynthesisResult is what the cross-section pass returned.
type SynthesisResult struct {
	// Report holds the new cross-section findings.
	Report *schema.Report
	// Merges are groups of chunk findings that report one defect.
	Merges []MergeGroup
	// Retractions are chunk findings the spec answers.
	Retractions []Retraction
}

// MergeGroup names chunk findings that report the same defect.
type MergeGroup struct {
	IssueIDs []string `json:"issue_ids"`
	Reason   string   `json:"reason"`
}

// Retraction names a chunk finding the spec answers, and the text that
// answers it.
type Retraction struct {
	ID        string `json:"id"`
	LineStart int    `json:"line_start"`
	LineEnd   int    `json:"line_end"`
	Quote     string `json:"quote"`
	Reason    string `json:"reason"`
}

func RunSynthesis(ctx context.Context, provider llm.Provider, s *spec.Spec, preflight []schema.Issue, merged MergeResult, cfg SynthesisConfig) (*SynthesisResult, string, error) {
	if provider == nil {
		return nil, "", fmt.Errorf("provider is required")
	}
	if s == nil {
		return nil, "", fmt.Errorf("spec is required")
	}
	if !cfg.Enabled {
		return nil, "", nil
	}
	findingCount := len(merged.Issues) + len(merged.Questions) + len(preflight)
	if !ShouldRunSynthesis(true, s.LineCount, findingCount, cfg.LineThreshold) {
		return nil, "", nil
	}
	tail, err := BuildSynthesisTask(SynthesisInput{Merged: merged, SpecShown: cfg.SpecShown})
	if err != nil {
		return nil, "", err
	}
	req := &llm.Request{
		SystemPrompt:           cfg.SystemPrompt,
		UserPromptCachedPrefix: cfg.Prefix,
		UserPrompt:             tail,
		LongCache:              cfg.LongCache,
		MaxTokens:              cfg.MaxTokens,
		Effort:                 cfg.Effort,
		Schema:                 llm.SynthesisSchema(cfg.EnforceSchema),
	}
	// A response that is cut off and continued arrives in parts. Each part's
	// merge and retract lists are kept, since they refer to the chunk findings
	// in the task, not to the new findings the parts number.
	var extras synthesisExtras
	report, model, err := llm.CompleteReport(ctx, provider, llm.ReportCall{
		Request: req,
		Label:   "synthesis ",
		Parse: func(raw string) (llm.Parsed, error) {
			parsed, extra, err := parseSynthesisResponse(raw, s.Path, s.Raw, s.LineCount)
			if err == nil {
				extras.add(extra)
			}
			return parsed, err
		},
		RepairPrompt: func(reason error, failedOutput string) string {
			return fmt.Sprintf("\n\nYour previous response failed synthesis validation.\n\nValidation error: %s\n\n<failed_output>\n%s\n</failed_output>\n\nReturn only valid JSON matching the schema and cite valid original line numbers.", reason, truncate(failedOutput, 4000))
		},
	})
	if err != nil {
		return nil, "", err
	}
	return &SynthesisResult{Report: report, Merges: extras.merges, Retractions: extras.retractions}, model, nil
}

// synthesisExtras collects the merge and retract lists across the parts of a
// response, ignoring repeats.
type synthesisExtras struct {
	merges      []MergeGroup
	retractions []Retraction
	retracted   map[string]bool
}

func (e *synthesisExtras) add(part synthesisExtras) {
	e.merges = append(e.merges, part.merges...)
	for _, r := range part.retractions {
		if e.retracted == nil {
			e.retracted = map[string]bool{}
		}
		if e.retracted[r.ID] {
			continue
		}
		e.retracted[r.ID] = true
		e.retractions = append(e.retractions, r)
	}
}

// parseSynthesisResponse reads a synthesis response that may have been cut
// off. Synthesis may cite any line of the spec. The synthesis tag and the
// evidence path are set here rather than asked of the model. The merge and
// retract lists are returned when they were read whole; one that is malformed
// is ignored rather than failing the response, since it only removes findings.
func parseSynthesisResponse(raw, specPath, specText string, lineCount int) (llm.Parsed, synthesisExtras, error) {
	res, err := validate.ParseResponse(raw, validate.Options{
		LineCount:   lineCount,
		SpecPath:    specPath,
		SpecText:    specText,
		ExtraFields: []string{"merge", "retract"},
		CheckIssue: func(issue *schema.Issue) error {
			if !hasTag(issue.Tags, TagSynthesis) {
				issue.Tags = appendUniqueStrings(copyStrings(issue.Tags), TagSynthesis)
			}
			return nil
		},
	})
	if err != nil {
		return llm.Parsed{}, synthesisExtras{}, err
	}
	var extras synthesisExtras
	if raw, ok := res.Extra["merge"]; ok {
		var merges []MergeGroup
		if json.Unmarshal(raw, &merges) == nil {
			extras.merges = merges
		}
	}
	if raw, ok := res.Extra["retract"]; ok {
		var retractions []Retraction
		if json.Unmarshal(raw, &retractions) == nil {
			extras.retractions = retractions
		}
	}
	return llm.Parsed{Report: res.Report, Incomplete: res.Incomplete, Dropped: res.Dropped}, extras, nil
}

// ParseSynthesisResponse reads a complete synthesis response.
func ParseSynthesisResponse(raw string, lineCount int) (*schema.Report, error) {
	parsed, _, err := parseSynthesisResponse(raw, "", "", lineCount)
	if err != nil {
		return nil, err
	}
	if parsed.Incomplete != nil {
		return nil, parsed.Incomplete
	}
	return parsed.Report, nil
}

func limitIssues(issues []schema.Issue, limit int) []schema.Issue {
	if len(issues) <= limit {
		return issues
	}
	return issues[:limit]
}

func limitQuestions(questions []schema.Question, limit int) []schema.Question {
	if len(questions) <= limit {
		return questions
	}
	return questions[:limit]
}
