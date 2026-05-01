package chunk

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dshills/speccritic/internal/llm"
	"github.com/dshills/speccritic/internal/spec"
)

func TestReviewChunksPreservesOrder(t *testing.T) {
	s, plan := executorFixture(t, 3)
	provider := &delayedProvider{}
	results, err := ReviewChunks(context.Background(), provider, s, plan, ExecutorConfig{Concurrency: 3, Temperature: 0.2, MaxTokens: 1000})
	if err != nil {
		t.Fatalf("ReviewChunks: %v", err)
	}
	if len(results) != len(plan.Chunks) {
		t.Fatalf("results = %d, want %d", len(results), len(plan.Chunks))
	}
	for i := range results {
		if results[i].Chunk.ID != plan.Chunks[i].ID {
			t.Fatalf("result %d chunk = %s, want %s", i, results[i].Chunk.ID, plan.Chunks[i].ID)
		}
	}
}

func TestReviewChunksHonorsConcurrency(t *testing.T) {
	s, plan := executorFixture(t, 5)
	provider := &blockingProvider{release: make(chan struct{})}
	done := make(chan error, 1)
	go func() {
		_, err := ReviewChunks(context.Background(), provider, s, plan, ExecutorConfig{Concurrency: 2, Temperature: 0.2, MaxTokens: 1000})
		done <- err
	}()
	provider.waitForCalls(t, 2)
	if provider.maxActiveValue() > 2 {
		t.Fatalf("max active = %d, want <= 2", provider.maxActiveValue())
	}
	close(provider.release)
	if err := <-done; err != nil {
		t.Fatalf("ReviewChunks: %v", err)
	}
	if provider.maxActiveValue() > 2 {
		t.Fatalf("max active = %d, want <= 2", provider.maxActiveValue())
	}
}

func TestReviewChunksRepairsInvalidOutputOnce(t *testing.T) {
	s, plan := executorFixture(t, 1)
	provider := &sequentialProvider{responses: []string{`{"issues":[`, responseForChunk(plan.Chunks[0])}}
	_, err := ReviewChunks(context.Background(), provider, s, plan, ExecutorConfig{Concurrency: 1, Temperature: 0.2, MaxTokens: 1000})
	if err != nil {
		t.Fatalf("ReviewChunks: %v", err)
	}
	if provider.calls != 2 {
		t.Fatalf("calls = %d, want initial + repair", provider.calls)
	}
}

func TestReviewChunksFailsWholeReviewOnChunkError(t *testing.T) {
	s, plan := executorFixture(t, 1)
	provider := &errorProvider{}
	_, err := ReviewChunks(context.Background(), provider, s, plan, ExecutorConfig{Concurrency: 1, Temperature: 0.2, MaxTokens: 1000})
	if err == nil {
		t.Fatal("expected chunk error")
	}
}

type delayedProvider struct{}

func (p *delayedProvider) Complete(_ context.Context, req *llm.Request) (*llm.Response, error) {
	return &llm.Response{Content: responseForPrompt(req.UserPrompt), Model: "fake:model"}, nil
}

type blockingProvider struct {
	mu        sync.Mutex
	calls     int
	active    int
	maxActive int
	release   chan struct{}
}

func (p *blockingProvider) Complete(_ context.Context, req *llm.Request) (*llm.Response, error) {
	p.mu.Lock()
	p.calls++
	p.active++
	if p.active > p.maxActive {
		p.maxActive = p.active
	}
	p.mu.Unlock()
	<-p.release
	p.mu.Lock()
	p.active--
	p.mu.Unlock()
	return &llm.Response{Content: responseForPrompt(req.UserPrompt), Model: "fake:model"}, nil
}

func (p *blockingProvider) waitForCalls(t *testing.T, want int) {
	t.Helper()
	for i := 0; i < 1000; i++ {
		p.mu.Lock()
		got := p.calls
		p.mu.Unlock()
		if got >= want {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("calls did not reach %d", want)
}

func (p *blockingProvider) maxActiveValue() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.maxActive
}

type sequentialProvider struct {
	calls     int
	responses []string
}

func (p *sequentialProvider) Complete(_ context.Context, _ *llm.Request) (*llm.Response, error) {
	p.calls++
	return &llm.Response{Content: p.responses[p.calls-1], Model: "fake:model"}, nil
}

type errorProvider struct{}

func (p *errorProvider) Complete(_ context.Context, _ *llm.Request) (*llm.Response, error) {
	return nil, errors.New("provider failed")
}

func executorFixture(t *testing.T, chunks int) (*spec.Spec, Plan) {
	t.Helper()
	var text string
	for i := 1; i <= chunks; i++ {
		text += fmt.Sprintf("## Section %d\nLine %d\n", i, i)
	}
	s := spec.New("SPEC.md", text)
	plan, err := PlanSpec(s, Config{ChunkLines: 2, ChunkOverlap: 0, ChunkConcurrency: 1})
	if err != nil {
		t.Fatalf("PlanSpec: %v", err)
	}
	if len(plan.Chunks) != chunks {
		t.Fatalf("chunks = %d, want %d: %#v", len(plan.Chunks), chunks, plan.Chunks)
	}
	return s, plan
}

func responseForPrompt(prompt string) string {
	id := "CHUNK-0001-L1-L2"
	for _, part := range []string{"CHUNK-0001-L1-L2", "CHUNK-0002-L3-L4", "CHUNK-0003-L5-L6", "CHUNK-0004-L7-L8", "CHUNK-0005-L9-L10"} {
		if strings.Contains(prompt, part) {
			id = part
			break
		}
	}
	return responseForID(id)
}

func responseForChunk(ch Chunk) string {
	return responseForID(ch.ID)
}

func responseForID(id string) string {
	return fmt.Sprintf(`{"issues":[{"id":"ISSUE-0001","severity":"WARN","category":"AMBIGUOUS_BEHAVIOR","title":"Ambiguous","description":"desc","evidence":[{"path":"SPEC.md","line_start":%d,"line_end":%d,"quote":"q"}],"impact":"impact","recommendation":"rec","blocking":false,"tags":["chunk:%s"]}],"questions":[],"patches":[],"meta":{"chunk_summary":"summary"}}`, lineStartForID(id), lineStartForID(id), id)
}

func lineStartForID(id string) int {
	switch id {
	case "CHUNK-0002-L3-L4":
		return 3
	case "CHUNK-0003-L5-L6":
		return 5
	case "CHUNK-0004-L7-L8":
		return 7
	case "CHUNK-0005-L9-L10":
		return 9
	default:
		return 1
	}
}
