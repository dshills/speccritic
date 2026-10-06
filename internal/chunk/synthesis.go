package chunk

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/dshills/speccritic/internal/llm"
	"github.com/dshills/speccritic/internal/schema"
	"github.com/dshills/speccritic/internal/schema/validate"
	"github.com/dshills/speccritic/internal/spec"
)

const maxSynthesisFindings = 80
const maxSynthesisQuestions = 80
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
// findings there; the task adds what the chunk reviews found.
func BuildSynthesisTask(input SynthesisInput) (string, error) {
	var tail strings.Builder
	if input.SpecShown {
		tail.WriteString("\nThe specification above was reviewed in chunks, each reviewer reporting only on its own lines. Their findings follow.\n")
	} else {
		tail.WriteString("\nA specification too large to show in full was reviewed in chunks, each reviewer seeing only its own lines. Their findings follow; they are all you have of the specification.\n")
	}
	tail.WriteString("Look for defects that span sections and so could not be seen from one chunk: contradictions between sections, interfaces used in one section and never defined, terms used inconsistently, ordering that depends on several sections, and blocking questions.\n")
	tail.WriteString("Do not repeat the findings below. Cite specification line numbers. Do not emit score or verdict.\n")
	for _, block := range []struct {
		name  string
		value any
	}{
		{name: "merged_chunk_findings", value: limitIssues(input.Merged.Issues, maxSynthesisFindings)},
		{name: "merged_chunk_questions", value: limitQuestions(input.Merged.Questions, maxSynthesisQuestions)},
	} {
		if emptyBlock(block.value) {
			continue
		}
		if err := writeJSONBlock(&tail, block.name, block.value); err != nil {
			return "", err
		}
	}
	tail.WriteString("\nIf there are no additional cross-section findings or questions, return empty issues, questions, and patches arrays.\n")
	return tail.String(), nil
}

func RunSynthesis(ctx context.Context, provider llm.Provider, s *spec.Spec, preflight []schema.Issue, merged MergeResult, cfg SynthesisConfig) (*schema.Report, string, error) {
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
		Schema:                 llm.ReviewSchema(cfg.EnforceSchema),
	}
	return llm.CompleteReport(ctx, provider, llm.ReportCall{
		Request: req,
		Label:   "synthesis ",
		Parse: func(raw string) (llm.Parsed, error) {
			return parseSynthesisResponse(raw, s.Path, s.Raw, s.LineCount)
		},
		RepairPrompt: func(reason error, failedOutput string) string {
			return fmt.Sprintf("\n\nYour previous response failed synthesis validation.\n\nValidation error: %s\n\n<failed_output>\n%s\n</failed_output>\n\nReturn only valid JSON matching the schema and cite valid original line numbers.", reason, truncate(failedOutput, 4000))
		},
	})
}

// parseSynthesisResponse reads a synthesis response that may have been cut
// off. Synthesis may cite any line of the spec. The synthesis tag and the
// evidence path are set here rather than asked of the model.
func parseSynthesisResponse(raw, specPath, specText string, lineCount int) (llm.Parsed, error) {
	res, err := validate.ParseResponse(raw, validate.Options{
		LineCount: lineCount,
		SpecPath:  specPath,
		SpecText:  specText,
		CheckIssue: func(issue *schema.Issue) error {
			if !hasTag(issue.Tags, TagSynthesis) {
				issue.Tags = appendUniqueStrings(copyStrings(issue.Tags), TagSynthesis)
			}
			return nil
		},
	})
	if err != nil {
		return llm.Parsed{}, err
	}
	return llm.Parsed{Report: res.Report, Incomplete: res.Incomplete, Dropped: res.Dropped}, nil
}

// ParseSynthesisResponse reads a complete synthesis response.
func ParseSynthesisResponse(raw string, lineCount int) (*schema.Report, error) {
	parsed, err := parseSynthesisResponse(raw, "", "", lineCount)
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

func writeJSONBlock(b *strings.Builder, name string, value any) error {
	b.WriteString("\n<")
	b.WriteString(name)
	b.WriteString(">\n")
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("marshal synthesis prompt block %s: %w", name, err)
	}
	b.Write(data)
	b.WriteString("\n</")
	b.WriteString(name)
	b.WriteString(">\n")
	return nil
}

func emptyBlock(value any) bool {
	if value == nil {
		return true
	}
	v := reflect.ValueOf(value)
	switch v.Kind() {
	case reflect.Array, reflect.Chan, reflect.Map, reflect.Slice, reflect.String:
		return v.Len() == 0
	default:
		return false
	}
}
