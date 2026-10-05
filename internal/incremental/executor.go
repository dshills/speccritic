package incremental

import (
	"context"
	"fmt"
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
	Effort       string
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
		Effort:                 cfg.Effort,
	}
	report, model, err := llm.CompleteReport(ctx, provider, llm.ReportCall{
		Request: req,
		Label:   fmt.Sprintf("range %s ", rr.ID),
		Parse: func(raw string) (llm.Parsed, error) {
			return parseRangeResponse(raw, s.Path, s.LineCount, rr)
		},
		RepairPrompt: func(reason error, _ string) string {
			return fmt.Sprintf("\n\nYour previous response failed incremental range validation.\n\nValidation error: %s\n\nReturn only valid JSON matching the schema and cite current spec line numbers included in the prompt.", reason)
		},
	})
	if err != nil {
		return RangeResult{}, err
	}
	return RangeResult{Range: rr, Report: report, Model: model}, nil
}

const TagIncrementalReview = "incremental-review"

// ParseRangeResponse reads a complete incremental range response. See
// parseRangeResponse for what is kept and what is dropped.
func ParseRangeResponse(raw string, lineCount int, rr ReviewRange) (*schema.Report, error) {
	parsed, err := parseRangeResponse(raw, "", lineCount, rr)
	if err != nil {
		return nil, err
	}
	if parsed.Incomplete != nil {
		return nil, parsed.Incomplete
	}
	return parsed.Report, nil
}

// parseRangeResponse reads an incremental range response that may have been
// cut off. Issues that cite lines outside the range's context are dropped. The
// incremental and range tags and the evidence path are set here rather than
// asked of the model.
func parseRangeResponse(raw, specPath string, lineCount int, rr ReviewRange) (llm.Parsed, error) {
	res, err := validate.ParseResponse(raw, validate.Options{
		LineCount: lineCount,
		SpecPath:  specPath,
		CheckIssue: func(issue *schema.Issue) error {
			for i, ev := range issue.Evidence {
				if ev.LineStart < rr.Context.Start || ev.LineEnd > rr.Context.End {
					return fmt.Errorf("evidence[%d] cites L%d-L%d outside range context L%d-L%d", i, ev.LineStart, ev.LineEnd, rr.Context.Start, rr.Context.End)
				}
			}
			for _, tag := range []string{TagIncrementalReview, "range:" + rr.ID} {
				if !hasTag(issue.Tags, tag) {
					issue.Tags = append(issue.Tags, tag)
				}
			}
			return nil
		},
	})
	if err != nil {
		return llm.Parsed{}, err
	}
	return llm.Parsed{Report: res.Report, Incomplete: res.Incomplete, Dropped: res.Dropped}, nil
}
