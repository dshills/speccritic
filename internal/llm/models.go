package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
)

var (
	openaiModelsAPIURL    = "https://api.openai.com/v1/models"
	anthropicModelsAPIURL = "https://api.anthropic.com/v1/models"
	geminiModelsAPIURL    = "https://generativelanguage.googleapis.com/v1beta/models"
)

func SetOpenAIModelsAPIURL(u string)    { openaiModelsAPIURL = u }
func SetAnthropicModelsAPIURL(u string) { anthropicModelsAPIURL = u }
func SetGeminiModelsAPIURL(u string)    { geminiModelsAPIURL = u }

func OpenAIModelsAPIURLForTest() string { return openaiModelsAPIURL }

type ModelInfo struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name,omitempty"`
}

type httpStatusError struct {
	statusCode int
	body       string
}

func (e *httpStatusError) Error() string {
	return fmt.Sprintf("HTTP %d: %s", e.statusCode, truncate(e.body, 200))
}

func ListModels(ctx context.Context, provider string) ([]ModelInfo, error) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	switch provider {
	case "anthropic":
		return listAnthropicModels(ctx)
	case "openai":
		return listOpenAIModels(ctx)
	case "gemini":
		return listGeminiModels(ctx)
	default:
		return nil, fmt.Errorf("unknown provider %q", provider)
	}
}

func listOpenAIModels(ctx context.Context) ([]ModelInfo, error) {
	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("OPENAI_API_KEY environment variable not set")
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Error *struct {
			Message string `json:"message"`
			Type    string `json:"type"`
		} `json:"error"`
	}
	err := getJSON(ctx, openaiModelsAPIURL, map[string]string{"Authorization": "Bearer " + apiKey}, &payload)
	if payload.Error != nil {
		return nil, fmt.Errorf("openai: %s: %s", payload.Error.Type, payload.Error.Message)
	}
	if err != nil {
		return nil, err
	}
	models := make([]ModelInfo, 0, len(payload.Data))
	for _, item := range payload.Data {
		if isUsableOpenAIReviewModel(item.ID) {
			models = append(models, ModelInfo{ID: item.ID})
		}
	}
	return sortedModels(models), nil
}

func listAnthropicModels(ctx context.Context) ([]ModelInfo, error) {
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("ANTHROPIC_API_KEY environment variable not set")
	}
	var payload struct {
		Data []struct {
			ID          string `json:"id"`
			DisplayName string `json:"display_name"`
		} `json:"data"`
		Error *struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	headers := map[string]string{"x-api-key": apiKey, "anthropic-version": AnthropicVersion}
	err := getJSON(ctx, anthropicModelsAPIURL, headers, &payload)
	if payload.Error != nil {
		return nil, fmt.Errorf("anthropic: %s: %s", payload.Error.Type, payload.Error.Message)
	}
	if err != nil {
		return nil, err
	}
	models := make([]ModelInfo, 0, len(payload.Data))
	for _, item := range payload.Data {
		if isUsableAnthropicReviewModel(item.ID) {
			models = append(models, ModelInfo{ID: item.ID, DisplayName: item.DisplayName})
		}
	}
	return sortedModels(models), nil
}

func listGeminiModels(ctx context.Context) ([]ModelInfo, error) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY environment variable not set")
	}
	var payload struct {
		Models []struct {
			Name                       string   `json:"name"`
			BaseModelID                string   `json:"baseModelId"`
			DisplayName                string   `json:"displayName"`
			SupportedGenerationMethods []string `json:"supportedGenerationMethods"`
		} `json:"models"`
		Error *struct {
			Message string `json:"message"`
			Status  string `json:"status"`
		} `json:"error"`
	}
	endpoint, err := url.Parse(geminiModelsAPIURL)
	if err != nil {
		return nil, fmt.Errorf("invalid Gemini models URL: %w", err)
	}
	q := endpoint.Query()
	q.Set("pageSize", "1000")
	endpoint.RawQuery = q.Encode()
	err = getJSON(ctx, endpoint.String(), map[string]string{"x-goog-api-key": apiKey}, &payload)
	if payload.Error != nil {
		return nil, fmt.Errorf("gemini: %s: %s", payload.Error.Status, payload.Error.Message)
	}
	if err != nil {
		return nil, err
	}
	models := make([]ModelInfo, 0, len(payload.Models))
	for _, item := range payload.Models {
		id := strings.TrimPrefix(item.Name, "models/")
		if item.BaseModelID != "" {
			id = item.BaseModelID
		}
		if isUsableGeminiReviewModel(id, item.SupportedGenerationMethods) {
			models = append(models, ModelInfo{ID: id, DisplayName: item.DisplayName})
		}
	}
	return sortedModels(dedupeModels(models)), nil
}

func getJSON(ctx context.Context, endpoint string, headers map[string]string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("creating HTTP request: %w", err)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := sharedHTTPClient.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	const maxBodyBytes = 10 * 1024 * 1024
	respBytes, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return fmt.Errorf("reading response body: %w", err)
	}
	if err := json.Unmarshal(respBytes, dst); err != nil {
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return fmt.Errorf("HTTP %d: %s", resp.StatusCode, truncate(string(respBytes), 200))
		}
		return fmt.Errorf("parsing response JSON (HTTP %d, body: %s): %w", resp.StatusCode, truncate(string(respBytes), 200), err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return &httpStatusError{statusCode: resp.StatusCode, body: string(respBytes)}
	}
	return nil
}

func isUsableOpenAIReviewModel(id string) bool {
	id = strings.ToLower(id)
	if hasAnyPrefix(id, "text-embedding-", "embedding-", "dall-e-", "tts-", "whisper-", "omni-moderation-", "text-moderation-") {
		return false
	}
	if strings.Contains(id, "audio") ||
		strings.Contains(id, "realtime") ||
		strings.Contains(id, "transcribe") ||
		strings.Contains(id, "transcription") ||
		strings.Contains(id, "image") ||
		strings.Contains(id, "vision") ||
		strings.Contains(id, "speech") ||
		strings.Contains(id, "moderation") ||
		strings.Contains(id, "embedding") {
		return false
	}
	return strings.HasPrefix(id, "gpt-") || isOpenAIReasoningModel(id)
}

func isUsableAnthropicReviewModel(id string) bool {
	id = strings.ToLower(id)
	return strings.HasPrefix(id, "claude-") &&
		!strings.Contains(id, "embedding") &&
		!strings.Contains(id, "image") &&
		!strings.Contains(id, "audio")
}

func isUsableGeminiReviewModel(id string, methods []string) bool {
	id = strings.ToLower(id)
	if strings.Contains(id, "embedding") ||
		strings.Contains(id, "embed") ||
		strings.Contains(id, "imagen") ||
		strings.Contains(id, "image") ||
		strings.Contains(id, "veo") ||
		strings.Contains(id, "aqa") ||
		strings.Contains(id, "tts") ||
		strings.Contains(id, "audio") {
		return false
	}
	return supportsGenerateContent(methods)
}

func hasAnyPrefix(s string, prefixes ...string) bool {
	for _, prefix := range prefixes {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}

func supportsGenerateContent(methods []string) bool {
	for _, method := range methods {
		if method == "generateContent" || method == "streamGenerateContent" {
			return true
		}
	}
	return false
}

func sortedModels(models []ModelInfo) []ModelInfo {
	sort.Slice(models, func(i, j int) bool {
		return models[i].ID < models[j].ID
	})
	return models
}

func dedupeModels(models []ModelInfo) []ModelInfo {
	seen := map[string]bool{}
	out := make([]ModelInfo, 0, len(models))
	for _, model := range models {
		if model.ID == "" || seen[model.ID] {
			continue
		}
		seen[model.ID] = true
		out = append(out, model)
	}
	return out
}
