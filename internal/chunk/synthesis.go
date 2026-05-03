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

const maxSynthesisSummaries = 40
const maxSynthesisFindings = 80
const maxSynthesisQuestions = 80
const maxSynthesisPreflight = 40
const TagSynthesis = "synthesis"

type SynthesisConfig struct {
	SystemPrompt  string
	Temperature   float64
	MaxTokens     int
	LineThreshold int
	Enabled       bool
}

type SynthesisInput struct {
	Spec         *spec.Spec
	Plan         Plan
	ChunkResults []ChunkResult
	Preflight    []schema.Issue
	Merged       MergeResult
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

func BuildSynthesisPrompt(input SynthesisInput) (cachedPrefix, variable string, err error) {
	if input.Spec == nil {
		return "", "", fmt.Errorf("spec is required")
	}
	var prefix strings.Builder
	prefix.WriteString("Analyze cross-section risks in an already chunk-reviewed specification.\n")
	prefix.WriteString("Return JSON matching the SpecCritic schema. Do not return prose or markdown fences.\n")
	prefix.WriteString("Do not re-review the whole spec. Use the table of contents, existing findings, preflight findings, and chunk summaries only to identify cross-section defects that chunk-local reviews may miss.\n")
	prefix.WriteString("You may identify duplicates, contradictions between findings, missing interfaces referenced across sections, terminology inconsistencies, ordering gaps, and blocking clarification questions.\n")
	prefix.WriteString("Cite valid original spec line numbers. Add tag \"")
	prefix.WriteString(TagSynthesis)
	prefix.WriteString("\" to every issue. Do not emit score or verdict.\n")
	prefix.WriteString("\n<spec_table_of_contents>\n")
	prefix.WriteString(escapePromptBlock(TableOfContents(input.Plan)))
	prefix.WriteString("</spec_table_of_contents>\n")

	var tail strings.Builder
	for _, block := range []struct {
		name  string
		value any
	}{
		{name: "merged_chunk_findings", value: limitIssues(input.Merged.Issues, maxSynthesisFindings)},
		{name: "merged_chunk_questions", value: limitQuestions(input.Merged.Questions, maxSynthesisQuestions)},
		{name: "preflight_findings", value: limitIssues(input.Preflight, maxSynthesisPreflight)},
		{name: "chunk_summaries", value: collectChunkSummaries(input.ChunkResults, maxSynthesisSummaries)},
	} {
		if emptyBlock(block.value) {
			continue
		}
		if err := writeJSONBlock(&tail, block.name, block.value); err != nil {
			return "", "", err
		}
	}
	tail.WriteString("\nReturn only the synthesis JSON report. If there are no additional cross-section findings or questions, return empty issues, questions, and patches arrays.\n")
	return prefix.String(), tail.String(), nil
}

func RunSynthesis(ctx context.Context, provider llm.Provider, s *spec.Spec, plan Plan, chunkResults []ChunkResult, preflight []schema.Issue, merged MergeResult, cfg SynthesisConfig) (*schema.Report, string, error) {
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
	prefix, tail, err := BuildSynthesisPrompt(SynthesisInput{
		Spec:         s,
		Plan:         plan,
		ChunkResults: chunkResults,
		Preflight:    preflight,
		Merged:       merged,
	})
	if err != nil {
		return nil, "", err
	}
	req := &llm.Request{
		SystemPrompt:           cfg.SystemPrompt,
		UserPromptCachedPrefix: prefix,
		UserPrompt:             tail,
		Temperature:            &cfg.Temperature,
		MaxTokens:              cfg.MaxTokens,
	}
	resp, err := provider.Complete(ctx, req)
	if err != nil {
		return nil, "", fmt.Errorf("synthesis LLM call failed: %w", err)
	}
	report, parseErr := parseSynthesisResponse(resp.Content, s.LineCount)
	model := resp.Model
	if parseErr != nil {
		repairReq := *req
		if req.Temperature != nil {
			temp := *req.Temperature
			repairReq.Temperature = &temp
		}
		if llm.IncompleteJSON(parseErr) {
			repairReq.MaxTokens = llm.RepairMaxTokens(req.MaxTokens)
		}
		repairReq.UserPrompt = req.UserPrompt + fmt.Sprintf("\n\nYour previous response failed synthesis validation.\n\nValidation error: %s\n\n<failed_output>\n%s\n</failed_output>\n\nReturn only valid JSON matching the schema, cite valid original line numbers, and add tag %q to every issue.", parseErr, truncate(resp.Content, 4000), TagSynthesis)
		resp, err = provider.Complete(ctx, &repairReq)
		if err != nil {
			return nil, "", fmt.Errorf("synthesis LLM repair call failed: %w", err)
		}
		model = resp.Model
		report, parseErr = parseSynthesisResponse(resp.Content, s.LineCount)
		if parseErr != nil {
			return nil, "", fmt.Errorf("synthesis invalid model output after retry: %w", parseErr)
		}
	}
	return report, model, nil
}

func parseSynthesisResponse(raw string, lineCount int) (*schema.Report, error) {
	report, err := ParseSynthesisResponse(raw, lineCount)
	if err != nil {
		return nil, err
	}
	for i := range report.Issues {
		if !hasTag(report.Issues[i].Tags, TagSynthesis) {
			report.Issues[i].Tags = appendUniqueStrings(copyStrings(report.Issues[i].Tags), TagSynthesis)
		}
	}
	return report, nil
}

func ParseSynthesisResponse(raw string, lineCount int) (*schema.Report, error) {
	report, err := validate.Parse(raw, lineCount)
	if err != nil {
		return nil, err
	}
	if err := ValidateSynthesisReport(report, lineCount); err != nil {
		return nil, err
	}
	return report, nil
}

type chunkSummary struct {
	ChunkID     string   `json:"chunk_id"`
	LineStart   int      `json:"line_start"`
	LineEnd     int      `json:"line_end"`
	HeadingPath []string `json:"heading_path,omitempty"`
	Summary     string   `json:"summary"`
}

func collectChunkSummaries(results []ChunkResult, limit int) []chunkSummary {
	if limit <= 0 {
		return nil
	}
	capacity := len(results)
	if capacity > limit {
		capacity = limit
	}
	summaries := make([]chunkSummary, 0, capacity)
	for _, result := range results {
		if result.Report == nil || strings.TrimSpace(result.Report.Meta.ChunkSummary) == "" {
			continue
		}
		summaries = append(summaries, chunkSummary{
			ChunkID:     result.Chunk.ID,
			LineStart:   result.Chunk.LineStart,
			LineEnd:     result.Chunk.LineEnd,
			HeadingPath: append([]string(nil), result.Chunk.HeadingPath...),
			Summary:     result.Report.Meta.ChunkSummary,
		})
		if len(summaries) == limit {
			break
		}
	}
	return summaries
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

func escapePromptBlock(value string) string {
	value = strings.ReplaceAll(value, "&", "&amp;")
	value = strings.ReplaceAll(value, "<", "&lt;")
	value = strings.ReplaceAll(value, ">", "&gt;")
	return value
}
