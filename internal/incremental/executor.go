package incremental

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/dshills/speccritic/internal/llm"
	"github.com/dshills/speccritic/internal/schema"
	"github.com/dshills/speccritic/internal/schema/validate"
	"github.com/dshills/speccritic/internal/spec"
)

type ExecutorConfig struct {
	SystemPrompt string
	Temperature  float64
	MaxTokens    int
	Concurrency  int
	Issues       []schema.Issue
	Questions    []schema.Question
}

type RangeResult struct {
	Range  ReviewRange
	Report *schema.Report
	Model  string
}

func ReviewRanges(ctx context.Context, provider llm.Provider, s *spec.Spec, plan Plan, cfg ExecutorConfig) ([]RangeResult, error) {
	if provider == nil {
		return nil, fmt.Errorf("provider is required")
	}
	if s == nil {
		return nil, fmt.Errorf("spec is required")
	}
	if len(plan.ReviewRanges) == 0 {
		return []RangeResult{}, nil
	}
	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = 1
	}
	if concurrency > len(plan.ReviewRanges) {
		concurrency = len(plan.ReviewRanges)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make([]RangeResult, len(plan.ReviewRanges))
	jobs := make(chan int)
	errs := make(chan error, 1)
	var wg sync.WaitGroup
	for worker := 0; worker < concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				result, err := reviewOneRange(ctx, provider, s, plan, plan.ReviewRanges[idx], cfg)
				if err != nil {
					select {
					case errs <- err:
						cancel()
					default:
					}
					return
				}
				results[idx] = result
			}
		}()
	}
	go func() {
		defer close(jobs)
		for i := range plan.ReviewRanges {
			select {
			case <-ctx.Done():
				return
			case jobs <- i:
			}
		}
	}()
	wg.Wait()
	select {
	case err := <-errs:
		return nil, err
	default:
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

func reviewOneRange(ctx context.Context, provider llm.Provider, s *spec.Spec, plan Plan, rr ReviewRange, cfg ExecutorConfig) (RangeResult, error) {
	prefix, tail, err := BuildRangePrompt(PromptInput{
		Spec:      s,
		Plan:      plan,
		Range:     rr,
		Issues:    cfg.Issues,
		Questions: cfg.Questions,
	})
	if err != nil {
		return RangeResult{}, err
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
		return RangeResult{}, fmt.Errorf("range %s LLM call failed: %w", rr.ID, err)
	}
	report, parseErr := ParseRangeResponse(resp.Content, s.LineCount, rr)
	model := resp.Model
	if parseErr != nil {
		repairReq := *req
		repairReq.UserPrompt = req.UserPrompt + fmt.Sprintf("\n\nYour previous response failed incremental range validation.\n\nValidation error: %s\n\nReturn only valid JSON matching the schema, add tags %q and %q to every issue, and cite current spec line numbers included in the prompt.", parseErr, TagIncrementalReview, "range:"+rr.ID)
		resp, err = provider.Complete(ctx, &repairReq)
		if err != nil {
			return RangeResult{}, fmt.Errorf("range %s LLM repair call failed: %w", rr.ID, err)
		}
		model = resp.Model
		report, parseErr = ParseRangeResponse(resp.Content, s.LineCount, rr)
		if parseErr != nil {
			return RangeResult{}, fmt.Errorf("range %s invalid model output after retry: %w", rr.ID, parseErr)
		}
	}
	return RangeResult{Range: rr, Report: report, Model: model}, nil
}

const TagIncrementalReview = "incremental-review"

func ParseRangeResponse(raw string, lineCount int, rr ReviewRange) (*schema.Report, error) {
	report, err := validate.Parse(raw, lineCount)
	if err != nil {
		return nil, err
	}
	for i, issue := range report.Issues {
		if !hasTag(issue.Tags, TagIncrementalReview) {
			return nil, fmt.Errorf("issue[%d] missing %q tag", i, TagIncrementalReview)
		}
		if !hasTag(issue.Tags, "range:"+rr.ID) {
			return nil, fmt.Errorf("issue[%d] missing range tag %q", i, "range:"+rr.ID)
		}
		for j, ev := range issue.Evidence {
			if ev.LineStart < rr.Context.Start || ev.LineEnd > rr.Context.End {
				return nil, fmt.Errorf("issue[%d].evidence[%d] cites L%d-L%d outside range context L%d-L%d", i, j, ev.LineStart, ev.LineEnd, rr.Context.Start, rr.Context.End)
			}
		}
	}
	return report, nil
}

func truncate(value string, max int) string {
	count := 0
	for idx := range value {
		if count == max {
			return value[:idx] + "...[truncated]"
		}
		count++
	}
	return value
}

func incompleteJSON(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "unexpected end of JSON input") ||
		strings.Contains(msg, "unexpected EOF")
}
