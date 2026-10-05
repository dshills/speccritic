package llm

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type usageProvider struct {
	delay time.Duration
	fail  bool
}

func (p *usageProvider) Complete(_ context.Context, req *Request) (*Response, error) {
	time.Sleep(p.delay)
	if p.fail {
		return nil, errors.New("provider down")
	}
	return &Response{
		Content:   "{}",
		Truncated: req.Attempt == AttemptContinuation,
		Usage:     Usage{InputTokens: 100, OutputTokens: 20, CacheReadTokens: 7, CacheWriteTokens: 3},
	}, nil
}

func TestMeter_TotalsCallsByAttempt(t *testing.T) {
	meter := NewMeter(&usageProvider{delay: 2 * time.Millisecond})
	for _, attempt := range []Attempt{AttemptFirst, AttemptFirst, AttemptRepair, AttemptContinuation} {
		if _, err := meter.Complete(context.Background(), &Request{Attempt: attempt}); err != nil {
			t.Fatalf("Complete: %v", err)
		}
	}
	got := meter.Totals()
	if got.Calls != 4 || got.RepairCalls != 1 || got.ContinuationCalls != 1 || got.TruncatedResponses != 1 {
		t.Errorf("calls=%d repair=%d continuation=%d truncated=%d, want 4/1/1/1", got.Calls, got.RepairCalls, got.ContinuationCalls, got.TruncatedResponses)
	}
	want := Usage{InputTokens: 400, OutputTokens: 80, CacheReadTokens: 28, CacheWriteTokens: 12}
	if got.Usage != want {
		t.Errorf("usage = %+v, want %+v", got.Usage, want)
	}
	if got.CallDuration < 8*time.Millisecond {
		t.Errorf("call duration = %s, want at least the four 2ms calls", got.CallDuration)
	}
	// Sequential calls: the elapsed time covers at least the time in calls.
	if got.WallDuration < got.CallDuration {
		t.Errorf("wall duration %s is shorter than call duration %s for sequential calls", got.WallDuration, got.CallDuration)
	}
}

func TestMeter_CountsFailedCallsWithoutTokens(t *testing.T) {
	meter := NewMeter(&usageProvider{fail: true})
	if _, err := meter.Complete(context.Background(), &Request{}); err == nil {
		t.Fatal("expected the provider error to pass through")
	}
	got := meter.Totals()
	if got.Calls != 1 || got.Usage != (Usage{}) {
		t.Fatalf("totals = %+v, want one call and no tokens", got)
	}
}

func TestMeter_ReportsADroppedTemperature(t *testing.T) {
	meter := NewMeter(&scriptedProvider{responses: []*Response{{Content: "{}"}, {Content: "{}", TemperatureDropped: true}, {Content: "{}"}}})
	if _, err := meter.Complete(context.Background(), &Request{}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if meter.Totals().TemperatureDropped {
		t.Fatal("TemperatureDropped = true before any call dropped it")
	}
	for range 2 {
		if _, err := meter.Complete(context.Background(), &Request{}); err != nil {
			t.Fatalf("Complete: %v", err)
		}
	}
	if !meter.Totals().TemperatureDropped {
		t.Fatal("TemperatureDropped = false after a call dropped it")
	}
}

func TestMeter_NoCalls(t *testing.T) {
	if got := NewMeter(&usageProvider{}).Totals(); got != (Totals{}) {
		t.Fatalf("totals = %+v, want zero", got)
	}
}

func TestMeter_ConcurrentCallsOverlap(t *testing.T) {
	const calls = 8
	meter := NewMeter(&usageProvider{delay: 20 * time.Millisecond})
	var wg sync.WaitGroup
	for range calls {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = meter.Complete(context.Background(), &Request{})
		}()
	}
	wg.Wait()
	got := meter.Totals()
	if got.Calls != calls || got.InputTokens != calls*100 {
		t.Fatalf("calls=%d input=%d, want %d and %d", got.Calls, got.InputTokens, calls, calls*100)
	}
	// Eight overlapping 20ms calls add up to far more than the time they took
	// side by side.
	if got.CallDuration <= got.WallDuration {
		t.Errorf("call duration %s should exceed wall duration %s for concurrent calls", got.CallDuration, got.WallDuration)
	}
}
