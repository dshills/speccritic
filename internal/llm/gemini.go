package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// geminiAPIURL is a var to allow test overrides via httptest.
var geminiAPIURL = "https://generativelanguage.googleapis.com/v1beta/openai/chat/completions"

// GeminiAPIURL returns the current Gemini API endpoint URL.
// Exposed for use by integration tests via httptest servers.
func GeminiAPIURL() string { return geminiAPIURL }

// SetGeminiAPIURL overrides the Gemini API endpoint URL.
// Intended for use in tests only.
func SetGeminiAPIURL(u string) { geminiAPIURL = u }

type geminiProvider struct {
	model  string
	apiKey string // unexported; never serialized by encoding/json
}

func (p *geminiProvider) Complete(ctx context.Context, req *Request) (*Response, error) {
	model := p.model
	if req.Model != "" {
		model = req.Model
	}

	var messages []openaiMessage
	if req.SystemPrompt != "" {
		messages = append(messages, openaiMessage{Role: "system", Content: req.SystemPrompt})
	}
	messages = append(messages, openaiMessage{Role: "user", Content: req.UserPromptCachedPrefix + req.UserPrompt})

	body := openaiRequest{
		Model:    model,
		Messages: messages,
	}
	if req.Temperature != nil {
		body.Temperature = req.Temperature
	}
	if req.MaxTokens > 0 {
		body.MaxTokens = req.MaxTokens
	}

	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, geminiAPIURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("creating HTTP request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	resp, err := sharedHTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

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

	if resp.StatusCode != http.StatusOK {
		if oaiResp.Error != nil {
			return nil, fmt.Errorf("gemini: %s: %s", oaiResp.Error.Type, oaiResp.Error.Message)
		}
		return nil, fmt.Errorf("gemini: HTTP %d: %s", resp.StatusCode, truncate(respStr, 200))
	}

	if len(oaiResp.Choices) == 0 {
		return nil, fmt.Errorf("gemini: empty choices in response")
	}

	return &Response{
		Content: oaiResp.Choices[0].Message.Content,
		Model:   fmt.Sprintf("gemini:%s", oaiResp.Model),
	}, nil
}
