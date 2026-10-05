package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// anthropicAPIURL is a var to allow test overrides via httptest.
var anthropicAPIURL = "https://api.anthropic.com/v1/messages"

// AnthropicAPIURL returns the current Anthropic API endpoint URL.
// Exposed for use by integration tests via httptest servers.
func AnthropicAPIURL() string { return anthropicAPIURL }

// SetAnthropicAPIURL overrides the Anthropic API endpoint URL.
// Intended for use in tests only.
func SetAnthropicAPIURL(u string) { anthropicAPIURL = u }

const AnthropicVersion = "2023-06-01"

type anthropicProvider struct {
	model  string
	apiKey string // unexported; never serialized by encoding/json
}

type anthropicRequest struct {
	Model        string                 `json:"model"`
	MaxTokens    int                    `json:"max_tokens"`
	System       []anthropicSystemBlock `json:"system,omitempty"`
	Messages     []anthropicMessage     `json:"messages"`
	OutputConfig *anthropicOutputConfig `json:"output_config,omitempty"`
}

// anthropicOutputConfig carries the settings that shape a response.
type anthropicOutputConfig struct {
	Effort string `json:"effort,omitempty"`
}

// anthropicSystemBlock is a single block of the structured system field.
// The array form is required to attach cache_control for prompt caching;
// the Anthropic API also accepts a bare string, but we always use the array
// form here so the caching breakpoint is available.
type anthropicSystemBlock struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

// anthropicCacheControl marks a content block as a prompt-cache breakpoint.
// Type "ephemeral" = 5-minute TTL; Anthropic silently ignores the breakpoint
// if the cumulative prefix is below the per-model minimum (1024 tokens for
// Sonnet/Opus, 2048 for Haiku).
type anthropicCacheControl struct {
	Type string `json:"type"`
}

// anthropicMessage carries a role and either a plain string content or an
// array of content blocks. Content is typed `any` because the Anthropic API
// accepts both forms, and the array form is required to attach cache_control
// to a portion of the message.
type anthropicMessage struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// anthropicContentBlock is one element of the array form of message content.
type anthropicContentBlock struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

const (
	// anthropicStopMaxTokens is the stop_reason reported when a response hits
	// max_tokens.
	anthropicStopMaxTokens = "max_tokens"
	// anthropicStopRefusal is the stop_reason reported when a safety
	// classifier declines the request. The HTTP status is still 200.
	anthropicStopRefusal = "refusal"
)

type anthropicResponse struct {
	ID         string `json:"id"`
	Model      string `json:"model"`
	StopReason string `json:"stop_reason"`
	// StopDetails is populated only when StopReason is a refusal.
	StopDetails *struct {
		Category    string `json:"category"`
		Explanation string `json:"explanation"`
	} `json:"stop_details"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	// Usage reports input_tokens exclusive of cached tokens, which arrive in
	// the two cache fields.
	Usage struct {
		InputTokens              int `json:"input_tokens"`
		OutputTokens             int `json:"output_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	} `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

func (p *anthropicProvider) Complete(ctx context.Context, req *Request) (*Response, error) {
	model := p.model
	if req.Model != "" {
		model = req.Model
	}

	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = DefaultMaxTokens
	}

	body := anthropicRequest{
		Model:     model,
		MaxTokens: maxTokens,
		Messages:  []anthropicMessage{{Role: "user", Content: buildAnthropicUserContent(req)}},
	}
	if req.SystemPrompt != "" {
		body.System = []anthropicSystemBlock{{
			Type:         "text",
			Text:         req.SystemPrompt,
			CacheControl: &anthropicCacheControl{Type: "ephemeral"},
		}}
	}
	if req.Effort != "" {
		body.OutputConfig = &anthropicOutputConfig{Effort: req.Effort}
	}

	ar, err := p.send(ctx, body)
	if err != nil {
		return nil, err
	}

	if ar.StopReason == anthropicStopRefusal {
		return nil, anthropicRefusalError(ar)
	}

	var sb strings.Builder
	for _, block := range ar.Content {
		if block.Type == "text" {
			sb.WriteString(block.Text)
		}
	}
	content := sb.String()
	truncated := ar.StopReason == anthropicStopMaxTokens
	// A response can spend its whole budget before emitting any text. Report
	// that as a truncation so the caller retries with more room instead of
	// treating it as a provider failure.
	if content == "" && !truncated {
		return nil, fmt.Errorf("anthropic: no text content in response (got %d content blocks)", len(ar.Content))
	}

	return &Response{
		Content:    content,
		Model:      fmt.Sprintf("anthropic:%s", ar.Model),
		StopReason: ar.StopReason,
		Truncated:  truncated,
		Usage: Usage{
			InputTokens:      ar.Usage.InputTokens,
			OutputTokens:     ar.Usage.OutputTokens,
			CacheReadTokens:  ar.Usage.CacheReadInputTokens,
			CacheWriteTokens: ar.Usage.CacheCreationInputTokens,
		},
	}, nil
}

// send makes one request and decodes the reply.
func (p *anthropicProvider) send(ctx context.Context, body anthropicRequest) (ar anthropicResponse, err error) {
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return ar, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicAPIURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return ar, fmt.Errorf("creating HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", AnthropicVersion)

	resp, err := sharedHTTPClient.Do(httpReq)
	if err != nil {
		return ar, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	const maxBodyBytes = 10 * 1024 * 1024 // 10 MiB
	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return ar, fmt.Errorf("reading response body: %w", err)
	}
	respStr := string(respBytes)

	if err := json.Unmarshal(respBytes, &ar); err != nil {
		return ar, fmt.Errorf("parsing response JSON (HTTP %d, body: %s): %w", resp.StatusCode, truncate(respStr, 200), err)
	}

	// Check status code first, then structured error field.
	if resp.StatusCode != http.StatusOK {
		if ar.Error != nil {
			return ar, fmt.Errorf("anthropic: %s: %s", ar.Error.Type, ar.Error.Message)
		}
		return ar, fmt.Errorf("anthropic: HTTP %d: %s", resp.StatusCode, truncate(respStr, 200))
	}
	return ar, nil
}

// anthropicRefusalError explains a request the model's safety classifiers
// declined. Nothing in the response is a review, so it must not be parsed as
// one.
func anthropicRefusalError(ar anthropicResponse) error {
	msg := "anthropic: the model declined the request (stop_reason: refusal"
	if ar.StopDetails != nil {
		if ar.StopDetails.Category != "" {
			msg += ", category: " + ar.StopDetails.Category
		}
		msg += ")"
		if ar.StopDetails.Explanation != "" {
			msg += ": " + truncate(ar.StopDetails.Explanation, 300)
		}
		return errors.New(msg)
	}
	return errors.New(msg + ")")
}

// buildAnthropicUserContent returns the user message content in the minimal
// form the API requires: a bare string when there's no cacheable prefix, and
// an array of content blocks with cache_control on the prefix otherwise.
//
// A second breakpoint on the prefix complements the system-prompt breakpoint:
// when callers pass --context files (e.g. a prior SPEC.md for feature work),
// the grounding docs sit in the prefix and become cached alongside the
// instructions, so only the variable spec block is billed at full rate on
// each iteration.
func buildAnthropicUserContent(req *Request) any {
	if req.UserPromptCachedPrefix == "" {
		return req.UserPrompt
	}
	return []anthropicContentBlock{
		{
			Type:         "text",
			Text:         req.UserPromptCachedPrefix,
			CacheControl: &anthropicCacheControl{Type: "ephemeral"},
		},
		{Type: "text", Text: req.UserPrompt},
	}
}
