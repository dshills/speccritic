package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// anthropicAPIURL is a var to allow test overrides via httptest.
var anthropicAPIURL = "https://api.anthropic.com/v1/messages"

// AnthropicAPIURL returns the current Anthropic API endpoint URL.
// Exposed for use by integration tests via httptest servers.
func AnthropicAPIURL() string { return anthropicAPIURL }

// SetAnthropicAPIURL overrides the Anthropic API endpoint URL.
// Intended for use in tests only.
func SetAnthropicAPIURL(u string) { anthropicAPIURL = u }

const anthropicVersion = "2023-06-01"

type anthropicProvider struct {
	model  string
	apiKey string // unexported; never serialized by encoding/json
}

type anthropicRequest struct {
	Model       string             `json:"model"`
	MaxTokens   int                `json:"max_tokens"`
	System      string             `json:"system,omitempty"`
	Messages    []anthropicMessage `json:"messages"`
	Temperature *float64           `json:"temperature,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	ID      string `json:"id"`
	Model   string `json:"model"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
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
		maxTokens = defaultMaxTokens
	}

	body := anthropicRequest{
		Model:     model,
		MaxTokens: maxTokens,
		System:    req.SystemPrompt,
		Messages: []anthropicMessage{
			{Role: "user", Content: req.UserPrompt},
		},
	}
	if req.Temperature != 0 {
		t := req.Temperature
		body.Temperature = &t
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, anthropicAPIURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("creating HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", anthropicVersion)

	resp, err := sharedHTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()

	const maxBodyBytes = 10 * 1024 * 1024 // 10 MiB
	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("reading response body: %w", err)
	}
	respStr := string(respBytes)

	var ar anthropicResponse
	if err := json.Unmarshal(respBytes, &ar); err != nil {
		return nil, fmt.Errorf("parsing response JSON (HTTP %d, body: %s): %w", resp.StatusCode, truncate(respStr, 200), err)
	}

	// Check status code first, then structured error field.
	if resp.StatusCode != http.StatusOK {
		if ar.Error != nil {
			return nil, fmt.Errorf("anthropic: %s: %s", ar.Error.Type, ar.Error.Message)
		}
		return nil, fmt.Errorf("anthropic: HTTP %d: %s", resp.StatusCode, truncate(respStr, 200))
	}

	var content string
	for _, block := range ar.Content {
		if block.Type == "text" {
			content += block.Text
		}
	}
	if content == "" {
		return nil, fmt.Errorf("anthropic: no text content in response (got %d content blocks)", len(ar.Content))
	}

	return &Response{
		Content: content,
		Model:   fmt.Sprintf("anthropic:%s", ar.Model),
	}, nil
}
