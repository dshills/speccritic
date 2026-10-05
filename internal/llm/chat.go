package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// chatEndpoint describes one OpenAI-compatible chat completions API. OpenAI and
// Gemini both speak this protocol.
type chatEndpoint struct {
	// name prefixes error messages and the reported model, e.g. "openai".
	name   string
	url    string
	apiKey string
	// completionTokens reports whether model takes max_completion_tokens
	// rather than max_tokens. It is nil for an API that only knows max_tokens.
	completionTokens func(model string) bool
}

type chatRequest struct {
	Model               string          `json:"model"`
	Messages            []chatMessage   `json:"messages"`
	MaxTokens           int             `json:"max_tokens,omitempty"`
	MaxCompletionTokens int             `json:"max_completion_tokens,omitempty"`
	ReasoningEffort     string          `json:"reasoning_effort,omitempty"`
	ResponseFormat      *responseFormat `json:"response_format,omitempty"`
}

type responseFormat struct {
	Type string `json:"type"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
	// Refusal is set, with empty content, when the model declines to answer.
	Refusal string `json:"refusal,omitempty"`
}

const (
	// chatFinishLength is the finish_reason reported when a response hits the
	// output token cap.
	chatFinishLength = "length"
	// chatFinishContentFilter is the finish_reason reported when the provider
	// withheld the response.
	chatFinishContentFilter = "content_filter"
)

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message      chatMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	// Usage reports prompt_tokens inclusive of cached tokens; the cached share
	// is broken out in prompt_tokens_details when the provider sends it.
	Usage struct {
		PromptTokens        int `json:"prompt_tokens"`
		CompletionTokens    int `json:"completion_tokens"`
		PromptTokensDetails struct {
			CachedTokens int `json:"cached_tokens"`
		} `json:"prompt_tokens_details"`
	} `json:"usage"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

// usage converts the OpenAI-style counts, where cached tokens are part of
// prompt_tokens, to Usage, where they are counted separately.
func (r chatResponse) usage() Usage {
	cached := r.Usage.PromptTokensDetails.CachedTokens
	return Usage{
		InputTokens:     max(r.Usage.PromptTokens-cached, 0),
		OutputTokens:    r.Usage.CompletionTokens,
		CacheReadTokens: cached,
	}
}

// completeChat sends req to an OpenAI-compatible endpoint.
//
// Which token-limit parameter a model takes is not discoverable in advance:
// newer models want max_completion_tokens and reject max_tokens, older ones the
// reverse. When the API rejects the one that was sent, the request is sent
// once more with the other.
func completeChat(ctx context.Context, ep chatEndpoint, model string, req *Request) (*Response, error) {
	if req.Model != "" {
		model = req.Model
	}

	// Only include a system message when non-empty to avoid unnecessary tokens.
	var messages []chatMessage
	if req.SystemPrompt != "" {
		messages = append(messages, chatMessage{Role: "system", Content: req.SystemPrompt})
	}
	messages = append(messages, chatMessage{Role: "user", Content: req.UserPromptCachedPrefix + req.UserPrompt})

	completionTokens := ep.completionTokens != nil && ep.completionTokens(model)
	resp, rejectedTokenParameter, err := sendChat(ctx, ep, model, messages, req, completionTokens)
	if err != nil && rejectedTokenParameter && ep.completionTokens != nil {
		resp, _, err = sendChat(ctx, ep, model, messages, req, !completionTokens)
	}
	if err != nil {
		return nil, err
	}

	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("%s: empty choices in response", ep.name)
	}
	choice := resp.Choices[0]
	truncated := choice.FinishReason == chatFinishLength
	switch {
	case choice.Message.Content == "" && choice.Message.Refusal != "":
		return nil, fmt.Errorf("%s: the model declined the request: %s", ep.name, truncate(choice.Message.Refusal, 300))
	case choice.FinishReason == chatFinishContentFilter:
		return nil, fmt.Errorf("%s: the model declined the request (finish_reason: %s)", ep.name, chatFinishContentFilter)
	case choice.Message.Content == "" && !truncated:
		// A response can spend its whole budget before emitting any text; that
		// case is left to the caller, which retries with more room. Any other
		// empty response has nothing to parse.
		return nil, fmt.Errorf("%s: empty response (finish_reason: %q)", ep.name, choice.FinishReason)
	}

	return &Response{
		Content:    choice.Message.Content,
		Model:      fmt.Sprintf("%s:%s", ep.name, resp.Model),
		StopReason: choice.FinishReason,
		Truncated:  truncated,
		Usage:      resp.usage(),
	}, nil
}

// sendChat makes one request. On an HTTP 400 it also reports whether the API
// objected to the token-limit parameter that was sent.
func sendChat(ctx context.Context, ep chatEndpoint, model string, messages []chatMessage, req *Request, completionTokens bool) (resp chatResponse, rejectedTokenParameter bool, err error) {
	body := chatRequest{
		Model:           model,
		Messages:        messages,
		ReasoningEffort: req.Effort,
		ResponseFormat:  &responseFormat{Type: "json_object"},
	}
	if req.MaxTokens > 0 {
		if completionTokens {
			body.MaxCompletionTokens = req.MaxTokens
		} else {
			body.MaxTokens = req.MaxTokens
		}
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return resp, false, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.url, bytes.NewReader(bodyBytes))
	if err != nil {
		return resp, false, fmt.Errorf("creating HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+ep.apiKey)

	httpResp, err := sharedHTTPClient.Do(httpReq)
	if err != nil {
		return resp, false, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	const maxBodyBytes = 10 * 1024 * 1024 // 10 MiB
	respBytes, err := io.ReadAll(io.LimitReader(httpResp.Body, maxBodyBytes))
	if err != nil {
		return resp, false, fmt.Errorf("reading response body: %w", err)
	}
	respStr := string(respBytes)

	// The rejection is recognized from the raw body, because error bodies are
	// not shaped the same way by every OpenAI-compatible API.
	rejectedTokenParameter = httpResp.StatusCode == http.StatusBadRequest && unsupportedTokenParameter(respStr)

	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return resp, rejectedTokenParameter, fmt.Errorf("parsing response JSON (HTTP %d, body: %s): %w", httpResp.StatusCode, truncate(respStr, 200), err)
	}
	if httpResp.StatusCode != http.StatusOK {
		if resp.Error != nil {
			return resp, rejectedTokenParameter, fmt.Errorf("%s: %s: %s", ep.name, resp.Error.Type, resp.Error.Message)
		}
		return resp, rejectedTokenParameter, fmt.Errorf("%s: HTTP %d: %s", ep.name, httpResp.StatusCode, truncate(respStr, 200))
	}
	return resp, false, nil
}

// unsupportedTokenParameter reports whether an error body says the API does
// not take the token-limit parameter it was sent.
func unsupportedTokenParameter(body string) bool {
	lower := strings.ToLower(body)
	return strings.Contains(lower, "unsupported parameter") &&
		(strings.Contains(lower, "max_tokens") || strings.Contains(lower, "max_completion_tokens"))
}
