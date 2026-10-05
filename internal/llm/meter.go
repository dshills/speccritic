package llm

import (
	"context"
	"sync"
	"time"
)

// Totals sums what a set of completion calls used.
type Totals struct {
	// Calls counts every request sent, including the ones below.
	Calls int
	// RepairCalls counts requests that regenerated an unusable response.
	RepairCalls int
	// ContinuationCalls counts requests for the rest of a cut-off response.
	ContinuationCalls int
	// TruncatedResponses counts responses that stopped at the output cap.
	TruncatedResponses int
	// TemperatureDropped reports that a requested temperature was left out of
	// at least one call because the model does not accept one.
	TemperatureDropped bool
	Usage
	// CallDuration is the time spent inside calls, added up. With concurrent
	// calls it exceeds WallDuration.
	CallDuration time.Duration
	// WallDuration runs from the start of the first call to the end of the
	// last one.
	WallDuration time.Duration
}

// Meter wraps a Provider and totals what its calls used. It is safe for
// concurrent use.
type Meter struct {
	provider Provider

	mu     sync.Mutex
	totals Totals
	first  time.Time
	last   time.Time
}

// NewMeter returns a Meter that forwards calls to provider.
func NewMeter(provider Provider) *Meter {
	return &Meter{provider: provider}
}

// Complete forwards req to the wrapped provider and records the outcome. A
// call that fails is still counted, with its duration and no tokens.
func (m *Meter) Complete(ctx context.Context, req *Request) (*Response, error) {
	start := time.Now()
	resp, err := m.provider.Complete(ctx, req)
	end := time.Now()

	m.mu.Lock()
	defer m.mu.Unlock()
	m.totals.Calls++
	switch req.Attempt {
	case AttemptRepair:
		m.totals.RepairCalls++
	case AttemptContinuation:
		m.totals.ContinuationCalls++
	}
	m.totals.CallDuration += end.Sub(start)
	if m.first.IsZero() || start.Before(m.first) {
		m.first = start
	}
	if end.After(m.last) {
		m.last = end
	}
	if resp != nil {
		if resp.Truncated {
			m.totals.TruncatedResponses++
		}
		if resp.TemperatureDropped {
			m.totals.TemperatureDropped = true
		}
		m.totals.InputTokens += resp.Usage.InputTokens
		m.totals.OutputTokens += resp.Usage.OutputTokens
		m.totals.CacheReadTokens += resp.Usage.CacheReadTokens
		m.totals.CacheWriteTokens += resp.Usage.CacheWriteTokens
	}
	return resp, err
}

// Totals returns what the calls made so far used.
func (m *Meter) Totals() Totals {
	m.mu.Lock()
	defer m.mu.Unlock()
	totals := m.totals
	totals.WallDuration = m.last.Sub(m.first)
	return totals
}
