package llm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	// cacheKey reports that the API takes prompt_cache_key, a hint that routes
	// requests sharing a prefix to the same cache.
	cacheKey bool
}

type chatRequest struct {
	Model               string          `json:"model"`
	Messages            []chatMessage   `json:"messages"`
	MaxTokens           int             `json:"max_tokens,omitempty"`
	MaxCompletionTokens int             `json:"max_completion_tokens,omitempty"`
	ReasoningEffort     string          `json:"reasoning_effort,omitempty"`
	PromptCacheKey      string          `json:"prompt_cache_key,omitempty"`
	ResponseFormat      *responseFormat `json:"response_format,omitempty"`
}

// responseFormat asks for JSON: any JSON object, or one constrained to a
// schema.
type responseFormat struct {
	Type       string          `json:"type"`
	JSONSchema *chatJSONSchema `json:"json_schema,omitempty"`
}

type chatJSONSchema struct {
	Name   string          `json:"name"`
	Strict bool            `json:"strict"`
	Schema json.RawMessage `json:"schema"`
}

const (
	chatFormatJSONObject = "json_object"
	chatFormatJSONSchema = "json_schema"
)

// chatState remembers, for the life of one provider, request settings the API
// has rejected, so later calls do not repeat a request that is known to fail.
type chatState struct {
	// noSchema holds the models that turned a JSON schema down.
	noSchema modelSet
}

// chatRejection names a request setting the API refused, when the refusal is
// one the caller can work around by sending the request differently.
type chatRejection int

const (
	rejectedNothing chatRejection = iota
	rejectedTokenParameter
	rejectedSchema
)

// chatSettings are the parts of a request that are adjusted when the API
// rejects them.
type chatSettings struct {
	// completionTokens sends max_completion_tokens instead of max_tokens.
	completionTokens bool
	// schema constrains the response to the request's schema instead of
	// asking for any JSON object.
	schema bool
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
// Models differ in what they accept, and the differences are not discoverable
// in advance: newer models want max_completion_tokens and reject max_tokens,
// older ones the reverse, and not every model takes a JSON schema. When the API
// rejects one of those settings the request is sent again with the
// alternative. Each setting is adjusted at most once, which bounds the retries.
func completeChat(ctx context.Context, ep chatEndpoint, state *chatState, model string, req *Request) (*Response, error) {
	if req.Model != "" {
		model = req.Model
	}

	settings := chatSettings{
		completionTokens: ep.completionTokens != nil && ep.completionTokens(model),
		schema:           req.Schema != nil && req.Schema.Enforce && !state.noSchema.has(model),
	}
	swappedTokenParameter := false

	var resp chatResponse
	retries := 0
	for {
		got, rejection, more, err := sendChat(ctx, ep, model, req, settings)
		retries += more
		if err == nil {
			resp = got
			break
		}
		switch {
		case rejection == rejectedTokenParameter && ep.completionTokens != nil && !swappedTokenParameter:
			settings.completionTokens = !settings.completionTokens
			swappedTokenParameter = true
		case rejection == rejectedSchema && settings.schema:
			settings.schema = false
			state.noSchema.add(model)
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
		Content:        choice.Message.Content,
		Model:          fmt.Sprintf("%s:%s", ep.name, resp.Model),
		StopReason:     choice.FinishReason,
		Truncated:      truncated,
		Usage:          resp.usage(),
		SchemaEnforced: settings.schema,
		Retries:        retries,
	}, nil
}

// sendChat makes one request. On an HTTP 400 it also reports which setting,
// if any, the API objected to.
func sendChat(ctx context.Context, ep chatEndpoint, model string, req *Request, settings chatSettings) (resp chatResponse, rejection chatRejection, retries int, err error) {
	// The example goes after the task, not into the system prompt, so the
	// start of the prompt stays identical across calls that expect different
	// shapes and the provider's prefix cache can serve it.
	//
	// The spec and context files stay in the user message even where a
	// provider would cache them only in the system message: they are material
	// under review, and the system message would give their text the weight of
	// instructions.
	user := req.UserPromptCachedPrefix + req.UserPrompt
	if req.Schema != nil && !settings.schema {
		user += req.Schema.PromptFallback
	}
	// Only include a system message when non-empty to avoid unnecessary tokens.
	var messages []chatMessage
	if req.SystemPrompt != "" {
		messages = append(messages, chatMessage{Role: "system", Content: req.SystemPrompt})
	}
	messages = append(messages, chatMessage{Role: "user", Content: user})

	body := chatRequest{
		Model:           model,
		Messages:        messages,
		ReasoningEffort: req.Effort,
		ResponseFormat:  &responseFormat{Type: chatFormatJSONObject},
	}
	if settings.schema {
		body.ResponseFormat = &responseFormat{
			Type:       chatFormatJSONSchema,
			JSONSchema: &chatJSONSchema{Name: req.Schema.Name, Strict: true, Schema: req.Schema.JSON},
		}
	}
	if ep.cacheKey && req.UserPromptCachedPrefix != "" {
		body.PromptCacheKey = promptCacheKey(req)
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
		return resp, rejectedNothing, 0, fmt.Errorf("marshaling request: %w", err)
	}
	res, err := postJSON(ctx, ep.url, map[string]string{"Authorization": "Bearer " + ep.apiKey}, bodyBytes)
	if err != nil {
		return resp, rejectedNothing, res.Retries, err
	}
	respStr := string(res.Body)

	// A rejected setting is recognized from the raw body, because error bodies
	// are not shaped the same way by every OpenAI-compatible API.
	if res.Status == http.StatusBadRequest {
		rejection = classifyChatRejection(respStr)
	}

	if err := json.Unmarshal(res.Body, &resp); err != nil {
		return resp, rejection, res.Retries, statusError(res.Status, fmt.Errorf("parsing response JSON (HTTP %d, body: %s): %w", res.Status, truncate(respStr, 200), err))
	}
	if res.Status != http.StatusOK {
		if resp.Error != nil {
			return resp, rejection, res.Retries, statusError(res.Status, fmt.Errorf("%s: %s: %s", ep.name, resp.Error.Type, resp.Error.Message))
		}
		return resp, rejection, res.Retries, statusError(res.Status, fmt.Errorf("%s: HTTP %d: %s", ep.name, res.Status, truncate(respStr, 200)))
	}
	return resp, rejectedNothing, res.Retries, nil
}

// promptCacheKey names the cacheable start of a request. Requests that share a
// system prompt and prefix get the same key, so the provider sends them to the
// machine that already holds that prefix in cache. Without it, calls made in
// parallel tend to land on machines that do not.
func promptCacheKey(req *Request) string {
	sum := sha256.Sum256([]byte(req.SystemPrompt + "\x00" + req.UserPromptCachedPrefix))
	return "speccritic-" + hex.EncodeToString(sum[:12])
}

// classifyChatRejection reads a 400 response body for a setting the API will
// not take.
func classifyChatRejection(body string) chatRejection {
	lower := strings.ToLower(body)
	switch {
	case strings.Contains(lower, "unsupported parameter") &&
		(strings.Contains(lower, "max_tokens") || strings.Contains(lower, "max_completion_tokens")):
		return rejectedTokenParameter
	case schemaRejected(body):
		return rejectedSchema
	}
	return rejectedNothing
}
