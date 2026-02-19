package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// openaiAPIURL is a var to allow test overrides via httptest.
var openaiAPIURL = "https://api.openai.com/v1/chat/completions"

// OpenAIAPIURL returns the current OpenAI API endpoint URL.
// Exposed for use by integration tests via httptest servers.
func OpenAIAPIURL() string { return openaiAPIURL }

// SetOpenAIAPIURL overrides the OpenAI API endpoint URL.
// Intended for use in tests only.
func SetOpenAIAPIURL(u string) { openaiAPIURL = u }

type openaiProvider struct {
	model  string
	apiKey string // unexported; never serialized by encoding/json
}

type openaiRequest struct {
	Model       string          `json:"model"`
	Messages    []openaiMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens,omitempty"`
	Temperature *float64        `json:"temperature,omitempty"`
}

type openaiMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openaiResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message openaiMessage `json:"message"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
		Type    string `json:"type"`
	} `json:"error"`
}

func (p *openaiProvider) Complete(ctx context.Context, req *Request) (*Response, error) {
	model := p.model
	if req.Model != "" {
		model = req.Model
	}

	// Only include system message when non-empty to avoid unnecessary token usage.
	var messages []openaiMessage
	if req.SystemPrompt != "" {
		messages = append(messages, openaiMessage{Role: "system", Content: req.SystemPrompt})
	}
	messages = append(messages, openaiMessage{Role: "user", Content: req.UserPrompt})

	body := openaiRequest{
		Model:    model,
		Messages: messages,
	}
	if req.Temperature != 0 {
		t := req.Temperature
		body.Temperature = &t
	}
	if req.MaxTokens > 0 {
		body.MaxTokens = req.MaxTokens
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, openaiAPIURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("creating HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

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

	var oaiResp openaiResponse
	if err := json.Unmarshal(respBytes, &oaiResp); err != nil {
		return nil, fmt.Errorf("parsing response JSON (HTTP %d, body: %s): %w", resp.StatusCode, truncate(respStr, 200), err)
	}

	// Check status code first, then structured error field.
	if resp.StatusCode != http.StatusOK {
		if oaiResp.Error != nil {
			return nil, fmt.Errorf("openai: %s: %s", oaiResp.Error.Type, oaiResp.Error.Message)
		}
		return nil, fmt.Errorf("openai: HTTP %d: %s", resp.StatusCode, truncate(respStr, 200))
	}

	if len(oaiResp.Choices) == 0 {
		return nil, fmt.Errorf("openai: empty choices in response")
	}

	return &Response{
		Content: oaiResp.Choices[0].Message.Content,
		Model:   fmt.Sprintf("openai:%s", oaiResp.Model),
	}, nil
}
