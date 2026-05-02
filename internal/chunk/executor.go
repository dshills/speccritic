package chunk

import (
	"context"
	"fmt"
	"io"
	"strings"
	"sync"

	ctxpkg "github.com/dshills/speccritic/internal/context"
	"github.com/dshills/speccritic/internal/llm"
	"github.com/dshills/speccritic/internal/schema"
	"github.com/dshills/speccritic/internal/spec"
)

type ExecutorConfig struct {
	SystemPrompt     string
	ContextFiles     []ctxpkg.ContextFile
	PreflightContext string
	Temperature      float64
	MaxTokens        int
	Concurrency      int
	Verbose          bool
	ErrWriter        io.Writer
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
	jobs := make(chan int)
	errs := make(chan error, 1)
	var logMu sync.Mutex
	var wg sync.WaitGroup
	for worker := 0; worker < concurrency; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				ch := plan.Chunks[idx]
				logVerbose(&logMu, cfg.ErrWriter, cfg.Verbose, "Starting chunk %s", ch.ID)
				result, err := reviewOneChunk(ctx, provider, s, plan, ch, cfg)
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
		for i := range plan.Chunks {
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

func reviewOneChunk(ctx context.Context, provider llm.Provider, s *spec.Spec, plan Plan, ch Chunk, cfg ExecutorConfig) (ChunkResult, error) {
	prefix, tail, err := BuildUserPrompt(PromptInput{
		Spec:             s,
		Plan:             plan,
		Chunk:            ch,
		ContextFiles:     cfg.ContextFiles,
		PreflightContext: cfg.PreflightContext,
	})
	if err != nil {
		return ChunkResult{}, err
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
		return ChunkResult{}, fmt.Errorf("chunk %s LLM call failed: %w", ch.ID, err)
	}
	report, parseErr := ParseChunkResponse(resp.Content, s.LineCount, ch)
	model := resp.Model
	if parseErr != nil {
		repairReq := llm.Request{
			SystemPrompt:           req.SystemPrompt,
			UserPromptCachedPrefix: req.UserPromptCachedPrefix,
			Temperature:            req.Temperature,
			MaxTokens:              req.MaxTokens,
			Model:                  req.Model,
		}
		if req.Temperature != nil {
			temp := *req.Temperature
			repairReq.Temperature = &temp
		}
		if incompleteJSON(parseErr) {
			repairReq.MaxTokens = repairMaxTokens(req.MaxTokens)
		}
		repairReq.UserPrompt = req.UserPrompt + fmt.Sprintf("\n\nYour previous response failed chunk validation.\n\nValidation error: %s\n\n<failed_output>\n%s\n</failed_output>\n\nReturn only valid JSON matching the schema, include meta.chunk_summary, add the required chunk tag, and cite only primary-range lines.", parseErr, truncate(resp.Content, 4000))
		resp, err = provider.Complete(ctx, &repairReq)
		if err != nil {
			return ChunkResult{}, fmt.Errorf("chunk %s LLM repair call failed: %w", ch.ID, err)
		}
		model = resp.Model
		report, parseErr = ParseChunkResponse(resp.Content, s.LineCount, ch)
		if parseErr != nil {
			return ChunkResult{}, fmt.Errorf("chunk %s invalid model output after retry: %w", ch.ID, parseErr)
		}
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

func incompleteJSON(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "unexpected end of JSON input") ||
		strings.Contains(msg, "unexpected EOF")
}

func repairMaxTokens(current int) int {
	const (
		defaultMaxRepairTokens = 8192
		maxRepairTokens        = 32768
	)
	if current <= 0 {
		return defaultMaxRepairTokens
	}
	next := current + 2048
	if doubled := current * 2; doubled > next {
		next = doubled
	}
	if next > maxRepairTokens && current < maxRepairTokens {
		return maxRepairTokens
	}
	return next
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
