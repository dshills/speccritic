package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
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

// chatState remembers, for the life of one provider, request settings the API
// has rejected, so later calls do not repeat a request that is known to fail.
type chatState struct {
	// noTemperature holds the models that turned a temperature down.
	noTemperature modelSet
}

// modelSet is a set of model names that is safe for concurrent use. Its zero
// value is empty and ready to use.
type modelSet struct{ models sync.Map }

func (s *modelSet) add(model string)      { s.models.Store(model, struct{}{}) }
func (s *modelSet) has(model string) bool { _, ok := s.models.Load(model); return ok }

type chatRequest struct {
	Model               string          `json:"model"`
	Messages            []chatMessage   `json:"messages"`
	MaxTokens           int             `json:"max_tokens,omitempty"`
	MaxCompletionTokens int             `json:"max_completion_tokens,omitempty"`
	Temperature         *float64        `json:"temperature,omitempty"`
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

// chatRejection names the request setting an API refused, when the refusal is
// one the caller can work around by sending the request differently.
type chatRejection int

const (
	rejectedNothing chatRejection = iota
	rejectedTokenParameter
	rejectedTemperature
)

// chatSettings are the parts of a request that are adjusted when an API
// rejects them.
type chatSettings struct {
	completionTokens bool
	temperature      bool
}

// completeChat sends req to an OpenAI-compatible endpoint.
//
// Models differ in which settings they accept, and the differences are not
// discoverable in advance: newer models want max_completion_tokens instead of
// max_tokens, and some reject any temperature but their default. When the API
// rejects one of those settings the request is sent again without it. Each
// setting is adjusted at most once, which bounds the retries.
func completeChat(ctx context.Context, ep chatEndpoint, state *chatState, model string, req *Request) (*Response, error) {
	if req.Model != "" {
		model = req.Model
	}

	// Only include a system message when non-empty to avoid unnecessary tokens.
	var messages []chatMessage
	if req.SystemPrompt != "" {
		messages = append(messages, chatMessage{Role: "system", Content: req.SystemPrompt})
	}
	messages = append(messages, chatMessage{Role: "user", Content: req.UserPromptCachedPrefix + req.UserPrompt})

	settings := chatSettings{
		completionTokens: ep.completionTokens != nil && ep.completionTokens(model),
		temperature:      req.Temperature != nil && !state.noTemperature.has(model),
	}
	swappedTokenParameter := false

	var resp chatResponse
	for {
		got, rejection, err := sendChat(ctx, ep, model, messages, req, settings)
		if err == nil {
			resp = got
			break
		}
		switch {
		case rejection == rejectedTokenParameter && ep.completionTokens != nil && !swappedTokenParameter:
			settings.completionTokens = !settings.completionTokens
			swappedTokenParameter = true
		case rejection == rejectedTemperature && settings.temperature:
			settings.temperature = false
			state.noTemperature.add(model)
		default:
			return nil, err
		}
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
		Content:            choice.Message.Content,
		Model:              fmt.Sprintf("%s:%s", ep.name, resp.Model),
		StopReason:         choice.FinishReason,
		Truncated:          truncated,
		Usage:              resp.usage(),
		TemperatureDropped: req.Temperature != nil && !settings.temperature,
	}, nil
}

// sendChat makes one request. On an HTTP error it also reports which setting,
// if any, the API objected to.
func sendChat(ctx context.Context, ep chatEndpoint, model string, messages []chatMessage, req *Request, settings chatSettings) (chatResponse, chatRejection, error) {
	body := chatRequest{
		Model:           model,
		Messages:        messages,
		ReasoningEffort: req.Effort,
		ResponseFormat:  &responseFormat{Type: "json_object"},
	}
	if settings.temperature {
		body.Temperature = req.Temperature
	}
	if req.MaxTokens > 0 {
		if settings.completionTokens {
			body.MaxCompletionTokens = req.MaxTokens
		} else {
			body.MaxTokens = req.MaxTokens
		}
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return chatResponse{}, rejectedNothing, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, ep.url, bytes.NewReader(bodyBytes))
	if err != nil {
		return chatResponse{}, rejectedNothing, fmt.Errorf("creating HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+ep.apiKey)

	httpResp, err := sharedHTTPClient.Do(httpReq)
	if err != nil {
		return chatResponse{}, rejectedNothing, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() { _ = httpResp.Body.Close() }()

	const maxBodyBytes = 10 * 1024 * 1024 // 10 MiB
	respBytes, err := io.ReadAll(io.LimitReader(httpResp.Body, maxBodyBytes))
	if err != nil {
		return chatResponse{}, rejectedNothing, fmt.Errorf("reading response body: %w", err)
	}
	respStr := string(respBytes)

	// A rejected setting is recognized from the raw body, because error bodies
	// are not shaped the same way by every OpenAI-compatible API.
	rejection := rejectedNothing
	if httpResp.StatusCode == http.StatusBadRequest {
		rejection = classifyChatRejection(respStr)
	}

	var resp chatResponse
	if err := json.Unmarshal(respBytes, &resp); err != nil {
		return chatResponse{}, rejection, fmt.Errorf("parsing response JSON (HTTP %d, body: %s): %w", httpResp.StatusCode, truncate(respStr, 200), err)
	}
	if httpResp.StatusCode != http.StatusOK {
		if resp.Error != nil {
			return chatResponse{}, rejection, fmt.Errorf("%s: %s: %s", ep.name, resp.Error.Type, resp.Error.Message)
		}
		return chatResponse{}, rejection, fmt.Errorf("%s: HTTP %d: %s", ep.name, httpResp.StatusCode, truncate(respStr, 200))
	}
	return resp, rejectedNothing, nil
}

// classifyChatRejection reads a 400 response body for a setting the API will
// not take.
func classifyChatRejection(body string) chatRejection {
	lower := strings.ToLower(body)
	switch {
	case strings.Contains(lower, "unsupported parameter") &&
		(strings.Contains(lower, "max_tokens") || strings.Contains(lower, "max_completion_tokens")):
		return rejectedTokenParameter
	case strings.Contains(lower, "temperature"):
		return rejectedTemperature
	}
	return rejectedNothing
}
