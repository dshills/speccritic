package llm

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// fastRetries shortens the backoff for the duration of a test.
func fastRetries(t *testing.T) {
	t.Helper()
	original := retryBaseDelay
	retryBaseDelay = time.Millisecond
	t.Cleanup(func() { retryBaseDelay = original })
}

// statusServer answers with the given statuses in turn, then 200 with body.
func statusServer(t *testing.T, body string, headers map[string]string, statuses ...int) (string, *atomic.Int32) {
	t.Helper()
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := int(calls.Add(1))
		w.Header().Set("Content-Type", "application/json")
		if n <= len(statuses) {
			for k, v := range headers {
				w.Header().Set(k, v)
			}
			w.WriteHeader(statuses[n-1])
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"overloaded_error","message":"try later"}}`))
			return
		}
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &calls
}

func TestAnthropicComplete_RetriesTransientFailures(t *testing.T) {
	fastRetries(t)
	for _, status := range []int{429, 500, 502, 503, 504, 529} {
		url, calls := statusServer(t, anthropicOK, nil, status, status)
		useAnthropicURL(t, url)
		resp, err := (&anthropicProvider{model: "claude-opus-5-5", apiKey: "k"}).Complete(context.Background(), &Request{UserPrompt: "spec"})
		if err != nil {
			t.Fatalf("status %d: Complete: %v", status, err)
		}
		if calls.Load() != 3 || resp.Retries != 2 {
			t.Errorf("status %d: calls = %d retries = %d, want 3 and 2", status, calls.Load(), resp.Retries)
		}
	}
}

func TestChatComplete_RetriesTransientFailures(t *testing.T) {
	fastRetries(t)
	for name := range chatProviders(t, "") {
		url, calls := statusServer(t, chatOK, nil, 429)
		resp, err := chatProviders(t, url)[name].Complete(context.Background(), &Request{UserPrompt: "spec"})
		if err != nil {
			t.Fatalf("%s: Complete: %v", name, err)
		}
		if calls.Load() != 2 || resp.Retries != 1 {
			t.Errorf("%s: calls = %d retries = %d, want 2 and 1", name, calls.Load(), resp.Retries)
		}
	}
}

func TestComplete_GivesUpAfterTheLastAttempt(t *testing.T) {
	fastRetries(t)
	url, calls := statusServer(t, anthropicOK, nil, 529, 529, 529, 529, 529)
	useAnthropicURL(t, url)
	_, err := (&anthropicProvider{model: "claude-opus-5-5", apiKey: "k"}).Complete(context.Background(), &Request{UserPrompt: "spec"})
	if err == nil || !IsTransient(err) || !strings.Contains(err.Error(), "overloaded_error") {
		t.Fatalf("error = %v, want a transient error carrying the provider's message", err)
	}
	if calls.Load() != maxAttempts {
		t.Errorf("calls = %d, want %d", calls.Load(), maxAttempts)
	}
}

func TestComplete_DoesNotRetryOtherFailures(t *testing.T) {
	fastRetries(t)
	for _, status := range []int{400, 401, 403, 404} {
		url, calls := statusServer(t, anthropicOK, nil, status)
		useAnthropicURL(t, url)
		_, err := (&anthropicProvider{model: "claude-opus-5-5", apiKey: "k"}).Complete(context.Background(), &Request{UserPrompt: "spec"})
		if err == nil || IsTransient(err) {
			t.Errorf("status %d: error = %v, want a permanent error", status, err)
		}
		if calls.Load() != 1 {
			t.Errorf("status %d: calls = %d, want 1", status, calls.Load())
		}
	}
}

func TestComplete_HonorsRetryAfter(t *testing.T) {
	fastRetries(t)
	url, calls := statusServer(t, anthropicOK, map[string]string{"Retry-After": "0.2"}, 429)
	useAnthropicURL(t, url)
	start := time.Now()
	if _, err := (&anthropicProvider{model: "claude-opus-5-5", apiKey: "k"}).Complete(context.Background(), &Request{UserPrompt: "spec"}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 200*time.Millisecond || calls.Load() != 2 {
		t.Errorf("elapsed = %s calls = %d, want the 200ms Retry-After honored", elapsed, calls.Load())
	}

	// A provider asking for longer than the cap is not waited for.
	url, calls = statusServer(t, anthropicOK, map[string]string{"Retry-After": "600"}, 429)
	useAnthropicURL(t, url)
	_, err := (&anthropicProvider{model: "claude-opus-5-5", apiKey: "k"}).Complete(context.Background(), &Request{UserPrompt: "spec"})
	if !IsTransient(err) || calls.Load() != 1 {
		t.Errorf("error = %v calls = %d, want an immediate transient error", err, calls.Load())
	}
}

func TestComplete_RetriesAreCanceledWithTheContext(t *testing.T) {
	original := retryBaseDelay
	retryBaseDelay = time.Hour
	t.Cleanup(func() { retryBaseDelay = original })
	url, _ := statusServer(t, anthropicOK, nil, 503)
	useAnthropicURL(t, url)
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := (&anthropicProvider{model: "claude-opus-5-5", apiKey: "k"}).Complete(ctx, &Request{UserPrompt: "spec"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want the context's deadline", err)
	}
}

func TestComplete_RetriesDroppedConnections(t *testing.T) {
	fastRetries(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			conn, _, _ := w.(http.Hijacker).Hijack()
			_ = conn.Close()
			return
		}
		_, _ = w.Write([]byte(anthropicOK))
	}))
	t.Cleanup(srv.Close)
	useAnthropicURL(t, srv.URL)
	resp, err := (&anthropicProvider{model: "claude-opus-5-5", apiKey: "k"}).Complete(context.Background(), &Request{UserPrompt: "spec"})
	if err != nil || resp.Retries != 1 {
		t.Fatalf("resp = %+v err = %v, want success after one resend", resp, err)
	}
}

func TestParseRetryAfter(t *testing.T) {
	cases := map[string]struct {
		want time.Duration
		ok   bool
	}{
		"":                              {0, false},
		"3":                             {3 * time.Second, true},
		"1.5":                           {1500 * time.Millisecond, true},
		"-1":                            {0, false},
		"soon":                          {0, false},
		"Wed, 21 Oct 2015 07:28:00 GMT": {0, true},
		"NaN":                           {0, false},
		"Inf":                           {maxRetryWait + time.Second, true},
		"1e300":                         {maxRetryWait + time.Second, true},
	}
	for value, tc := range cases {
		got, ok := parseRetryAfter(value)
		if ok != tc.ok || got != tc.want {
			t.Errorf("parseRetryAfter(%q) = %s, %v; want %s, %v", value, got, ok, tc.want, tc.ok)
		}
	}
}

func TestRetryWaitBacksOffWithJitter(t *testing.T) {
	for attempt := 1; attempt <= 3; attempt++ {
		base := retryBaseDelay << (attempt - 1)
		wait, ok := retryWait(attempt, http.Header{})
		if !ok || wait < base/2 || wait > base {
			t.Errorf("attempt %d: wait = %s, want between %s and %s", attempt, wait, base/2, base)
		}
	}
}

func TestComplete_DoesNotRetryARequestThatCannotBeSent(t *testing.T) {
	fastRetries(t)
	useAnthropicURL(t, "notascheme://example.invalid")
	_, err := (&anthropicProvider{model: "claude-opus-5-5", apiKey: "k"}).Complete(context.Background(), &Request{UserPrompt: "spec"})
	if err == nil || IsTransient(err) {
		t.Fatalf("error = %v, want a permanent error", err)
	}
}

func TestComplete_RejectsAnOversizedResponse(t *testing.T) {
	fastRetries(t)
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		_, _ = w.Write([]byte(strings.Repeat("x", maxResponseBytes+10)))
	}))
	t.Cleanup(srv.Close)
	useAnthropicURL(t, srv.URL)
	_, err := (&anthropicProvider{model: "claude-opus-5-5", apiKey: "k"}).Complete(context.Background(), &Request{UserPrompt: "spec"})
	if err == nil || IsTransient(err) || !strings.Contains(err.Error(), "exceeds") || calls.Load() != 1 {
		t.Fatalf("error = %v calls = %d, want one permanent size error", err, calls.Load())
	}
}
