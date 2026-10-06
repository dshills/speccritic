package chunk

import (
	"context"
	"fmt"
	"io"
	"sort"
	"sync"

	"github.com/dshills/speccritic/internal/llm"
	"github.com/dshills/speccritic/internal/schema"
	"github.com/dshills/speccritic/internal/spec"
)

type ExecutorConfig struct {
	SystemPrompt string
	// Prefix is the shared user-message prefix from llm.BuildSpecPrefix. Every
	// chunk call sends it unchanged, so it is cached after the first.
	Prefix string
	// LongCache is passed through to llm.Request.LongCache.
	LongCache bool
	// WithLines puts each chunk's numbered lines in its task, for a Prefix
	// that leaves the spec out because it is too large to share.
	WithLines bool
	MaxTokens int
	Effort    string
	// EnforceSchema asks the provider to constrain responses to the review
	// schema.
	EnforceSchema bool
	Concurrency   int
	Verbose       bool
	ErrWriter     io.Writer
}

type ChunkResult struct {
	Chunk  Chunk
	Report *schema.Report
	Model  string
}

func ReviewChunks(ctx context.Context, provider llm.Provider, s *spec.Spec, plan Plan, cfg ExecutorConfig) ([]ChunkResult, error) {
	if provider == nil {
		return nil, fmt.Errorf("provider is required")
	}
	if s == nil {
		return nil, fmt.Errorf("spec is required")
	}
	if len(plan.Chunks) == 0 {
		return []ChunkResult{}, nil
	}
	concurrency := cfg.Concurrency
	if concurrency <= 0 {
		concurrency = DefaultChunkConcurrency
	}
	if concurrency <= 0 {
		concurrency = 1
	}
	if concurrency > len(plan.Chunks) {
		concurrency = len(plan.Chunks)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make([]ChunkResult, len(plan.Chunks))
	var logMu sync.Mutex

	// Every chunk call starts with the same prefix. A provider can only serve
	// it from cache once the first call has written it, so calls sent together
	// at the start would each pay to write it. The first chunk goes alone; the
	// rest follow in parallel and read the cached prefix.
	// A chunk that fails on a transient error, such as a rate limit that
	// outlasted the provider's own retries, is tried once more after the
	// others finish, when the load it met has passed. The chunks already
	// reviewed are kept.
	var retryMu sync.Mutex
	var retry []int
	deferRetry := func(idx int, err error) bool {
		if !llm.IsTransient(err) {
			return false
		}
		retryMu.Lock()
		retry = append(retry, idx)
		retryMu.Unlock()
		logVerbose(&logMu, cfg.ErrWriter, cfg.Verbose, "Chunk %s failed on a transient error; it will be tried again after the others: %s", plan.Chunks[idx].ID, err)
		return true
	}

	first := 0
	if concurrency > 1 && len(plan.Chunks) > 1 {
		ch := plan.Chunks[0]
		logVerbose(&logMu, cfg.ErrWriter, cfg.Verbose, "Starting chunk %s alone to fill the prompt cache", ch.ID)
		result, err := reviewOneChunk(ctx, provider, s, ch, cfg, &logMu)
		switch {
		case err == nil:
			logVerbose(&logMu, cfg.ErrWriter, cfg.Verbose, "Completed chunk %s", ch.ID)
			results[0] = result
		case !deferRetry(0, err):
			return nil, err
		}
		first = 1
		concurrency = min(concurrency, len(plan.Chunks)-1)
	}

	jobs := make(chan int)
	errs := make(chan error, 1)
	var wg sync.WaitGroup
	for worker := 0; worker < concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				ch := plan.Chunks[idx]
				logVerbose(&logMu, cfg.ErrWriter, cfg.Verbose, "Starting chunk %s", ch.ID)
				result, err := reviewOneChunk(ctx, provider, s, ch, cfg, &logMu)
				if err != nil && deferRetry(idx, err) {
					continue
				}
				if err != nil {
					select {
					case errs <- err:
						cancel()
					default:
					}
					return
				}
				logVerbose(&logMu, cfg.ErrWriter, cfg.Verbose, "Completed chunk %s", ch.ID)
				results[idx] = result
			}
		}()
	}
	go func() {
		defer close(jobs)
		for i := first; i < len(plan.Chunks); i++ {
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
	sort.Ints(retry)
	for _, idx := range retry {
		ch := plan.Chunks[idx]
		logVerbose(&logMu, cfg.ErrWriter, cfg.Verbose, "Retrying chunk %s", ch.ID)
		result, err := reviewOneChunk(ctx, provider, s, ch, cfg, &logMu)
		if err != nil {
			return nil, err
		}
		results[idx] = result
	}
	return results, nil
}

func reviewOneChunk(ctx context.Context, provider llm.Provider, s *spec.Spec, ch Chunk, cfg ExecutorConfig, logMu *sync.Mutex) (ChunkResult, error) {
	task, err := BuildChunkTask(ch, s.LineCount, cfg.WithLines)
	if err != nil {
		return ChunkResult{}, err
	}
	req := &llm.Request{
		SystemPrompt:           cfg.SystemPrompt,
		UserPromptCachedPrefix: cfg.Prefix,
		UserPrompt:             task,
		LongCache:              cfg.LongCache,
		MaxTokens:              cfg.MaxTokens,
		Effort:                 cfg.Effort,
		Schema:                 llm.ReviewSchema(cfg.EnforceSchema),
	}
	report, model, err := llm.CompleteReport(ctx, provider, llm.ReportCall{
		Request: req,
		Label:   fmt.Sprintf("chunk %s ", ch.ID),
		Parse: func(raw string) (llm.Parsed, error) {
			return parseChunkResponse(raw, s.Raw, s.LineCount, ch)
		},
		RepairPrompt: func(reason error, failedOutput string) string {
			return fmt.Sprintf("\n\nYour previous response failed chunk validation.\n\nValidation error: %s\n\n<failed_output>\n%s\n</failed_output>\n\nReturn only valid JSON matching the schema and cite only primary-range lines.", reason, truncate(failedOutput, 4000))
		},
		Logf: func(format string, args ...any) {
			logVerbose(logMu, cfg.ErrWriter, cfg.Verbose, "Chunk %s: "+format, append([]any{ch.ID}, args...)...)
		},
	})
	if err != nil {
		return ChunkResult{}, err
	}
	return ChunkResult{Chunk: ch, Report: report, Model: model}, nil
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

func logVerbose(mu *sync.Mutex, w io.Writer, verbose bool, format string, args ...any) {
	if !verbose || w == nil {
		return
	}
	if mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}
	_, _ = fmt.Fprintf(w, "INFO: "+format+"\n", args...)
}
