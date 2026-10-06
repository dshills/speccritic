package llm

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand/v2"
	"net"
	"net/http"
	"strconv"
	"syscall"
	"time"
)

// Retry policy for transient failures: rate limits, overloaded or failing
// servers, and dropped connections. Parallel chunk calls make rate limits
// likely, and a review should not fail for a hiccup a few seconds would cure.
const (
	// maxAttempts is the number of times one request is sent, the first
	// included.
	maxAttempts = 4
	// maxRetryWait caps a single wait. A provider asking for a longer one is
	// not waited for: the request fails with a transient error instead.
	maxRetryWait = 60 * time.Second
)

// retryBaseDelay is the wait before the first retry, doubled for each one
// after. Tests shorten it.
var retryBaseDelay = time.Second

// maxResponseBytes bounds how much of a response body is read.
const maxResponseBytes = 10 * 1024 * 1024 // 10 MiB

// TransientError is a failure that might not happen if the same request were
// sent again later: what remained after the retries ran out.
type TransientError struct{ Err error }

func (e *TransientError) Error() string { return e.Err.Error() }
func (e *TransientError) Unwrap() error { return e.Err }

// IsTransient reports whether err is, or wraps, a TransientError.
func IsTransient(err error) bool {
	var transient *TransientError
	return errors.As(err, &transient)
}

// httpResult is a response read in full.
type httpResult struct {
	Status int
	Body   []byte
	// Retries counts the requests resent because of a transient failure.
	Retries int
}

// postJSON sends body to url and reads the response, resending it after a
// wait when the failure is transient. The final response is returned whatever
// its status; only a transport failure that outlasts the retries is an error.
func postJSON(ctx context.Context, url string, headers map[string]string, body []byte) (httpResult, error) {
	var result httpResult
	for attempt := 1; ; attempt++ {
		status, respBody, header, err := postOnce(ctx, url, headers, body)
		if ctx.Err() != nil {
			return result, ctx.Err()
		}
		if err != nil && !isTransientNetError(err) {
			return result, err
		}
		if err == nil && !isTransientStatus(status) {
			result.Status, result.Body = status, respBody
			return result, nil
		}
		wait, ok := retryWait(attempt, header)
		if attempt >= maxAttempts || !ok {
			if err != nil {
				return result, &TransientError{Err: fmt.Errorf("HTTP request failed after %d attempt(s): %w", attempt, err)}
			}
			result.Status, result.Body = status, respBody
			return result, nil
		}
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return result, ctx.Err()
		case <-timer.C:
		}
		result.Retries++
	}
}

func postOnce(ctx context.Context, url string, headers map[string]string, body []byte) (int, []byte, http.Header, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return 0, nil, nil, fmt.Errorf("creating HTTP request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes+1))
	if err != nil {
		// The headers arrived, so a Retry-After among them still applies.
		return resp.StatusCode, nil, resp.Header, fmt.Errorf("reading response body: %w", err)
	}
	if len(respBody) > maxResponseBytes {
		// Not transient: the same request would bring back the same body.
		return 0, nil, nil, fmt.Errorf("response body exceeds %d bytes", maxResponseBytes)
	}
	return resp.StatusCode, respBody, resp.Header, nil
}

// isTransientNetError reports whether a failure to complete an HTTP exchange
// might not recur: a timeout, a refused or reset connection, or a response cut
// off. A request that could not be built or a body that is too large is not.
func isTransientNetError(err error) bool {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) ||
		errors.Is(err, syscall.ECONNRESET) || errors.Is(err, syscall.ECONNREFUSED) ||
		errors.Is(err, syscall.ECONNABORTED) || errors.Is(err, syscall.EPIPE) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	var opErr *net.OpError
	return errors.As(err, &opErr)
}

// isTransientStatus reports whether a status says to try again later. 529 is
// Anthropic's "overloaded".
func isTransientStatus(status int) bool {
	switch status {
	case http.StatusRequestTimeout, http.StatusTooManyRequests,
		http.StatusInternalServerError, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout, 529:
		return true
	}
	return false
}

// retryWait returns how long to wait before the next attempt, and false when
// the provider asked for a wait longer than maxRetryWait. A Retry-After header
// is honored; otherwise the wait doubles each attempt, with jitter so parallel
// calls that failed together do not retry together.
func retryWait(attempt int, header http.Header) (time.Duration, bool) {
	if after, ok := parseRetryAfter(header.Get("Retry-After")); ok {
		if after > maxRetryWait {
			return 0, false
		}
		return after, true
	}
	backoff := retryBaseDelay << (attempt - 1)
	backoff = min(backoff, maxRetryWait)
	return backoff/2 + rand.N(backoff/2+1), true
}

// parseRetryAfter reads a Retry-After value: a number of seconds or an HTTP
// date.
func parseRetryAfter(value string) (time.Duration, bool) {
	if value == "" {
		return 0, false
	}
	if seconds, err := strconv.ParseFloat(value, 64); err == nil {
		switch {
		case math.IsNaN(seconds) || seconds < 0:
			return 0, false
		case seconds > maxRetryWait.Seconds():
			// Too long to convert safely, and too long to wait for anyway.
			return maxRetryWait + time.Second, true
		}
		return time.Duration(seconds * float64(time.Second)), true
	}
	if when, err := http.ParseTime(value); err == nil {
		return max(time.Until(when), 0), true
	}
	return 0, false
}

// statusError builds the error for a response that is not a success, marking
// it transient when sending it again later might succeed.
func statusError(status int, err error) error {
	if isTransientStatus(status) {
		return &TransientError{Err: err}
	}
	return err
}
