package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestListModelsOpenAI(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test" {
			t.Fatalf("authorization = %q", got)
		}
		w.Write([]byte(`{"data":[{"id":"gpt-5"},{"id":"gpt-4o-audio-preview"},{"id":"gpt-image-1"},{"id":"text-embedding-3-large"},{"id":"omni-moderation-latest"},{"id":"tts-1"},{"id":"whisper-1"},{"id":"o4-mini"}]}`))
	}))
	defer server.Close()
	old := openaiModelsAPIURL
	SetOpenAIModelsAPIURL(server.URL)
	t.Cleanup(func() { SetOpenAIModelsAPIURL(old) })

	models, err := ListModels(context.Background(), "openai")
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if got := modelIDs(models); strings.Join(got, ",") != "gpt-5,o4-mini" {
		t.Fatalf("models = %#v", got)
	}
}

func TestListModelsAnthropic(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-ant-test")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-api-key"); got != "sk-ant-test" {
			t.Fatalf("x-api-key = %q", got)
		}
		if got := r.Header.Get("anthropic-version"); got == "" {
			t.Fatal("missing anthropic-version header")
		}
		w.Write([]byte(`{"data":[{"id":"claude-sonnet-4-20250514","display_name":"Claude Sonnet 4"},{"id":"not-claude-test","display_name":"Not Claude"},{"id":"claude-embedding-test","display_name":"Claude Embedding"}]}`))
	}))
	defer server.Close()
	old := anthropicModelsAPIURL
	SetAnthropicModelsAPIURL(server.URL)
	t.Cleanup(func() { SetAnthropicModelsAPIURL(old) })

	models, err := ListModels(context.Background(), "anthropic")
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 1 || models[0].ID != "claude-sonnet-4-20250514" || models[0].DisplayName != "Claude Sonnet 4" {
		t.Fatalf("models = %#v", models)
	}
}

func TestListModelsGemini(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "gemini-test")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("x-goog-api-key"); got != "gemini-test" {
			t.Fatalf("x-goog-api-key = %q", got)
		}
		if got := r.URL.Query().Get("key"); got != "" {
			t.Fatalf("key query should be empty, got %q", got)
		}
		w.Write([]byte(`{"models":[{"name":"models/gemini-2.0-flash","baseModelId":"gemini-2.0-flash","displayName":"Gemini 2.0 Flash","supportedGenerationMethods":["generateContent"]},{"name":"models/gemini-2.0-flash-preview-image-generation","supportedGenerationMethods":["generateContent"]},{"name":"models/imagen-3.0-generate-002","supportedGenerationMethods":["generateContent"]},{"name":"models/veo-2.0-generate-001","supportedGenerationMethods":["generateContent"]},{"name":"models/embedding-001","supportedGenerationMethods":["embedContent"]}]}`))
	}))
	defer server.Close()
	old := geminiModelsAPIURL
	SetGeminiModelsAPIURL(server.URL)
	t.Cleanup(func() { SetGeminiModelsAPIURL(old) })

	models, err := ListModels(context.Background(), "gemini")
	if err != nil {
		t.Fatalf("ListModels: %v", err)
	}
	if len(models) != 1 || models[0].ID != "gemini-2.0-flash" {
		t.Fatalf("models = %#v", models)
	}
}

func TestListModelsOpenAIParsesStructuredHTTPError(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"bad key"}}`))
	}))
	defer server.Close()
	old := openaiModelsAPIURL
	SetOpenAIModelsAPIURL(server.URL)
	t.Cleanup(func() { SetOpenAIModelsAPIURL(old) })

	_, err := ListModels(context.Background(), "openai")
	if err == nil || !strings.Contains(err.Error(), "bad key") {
		t.Fatalf("error = %v, want structured provider error", err)
	}
}

func TestListModelsOpenAIReturnsGenericHTTPError(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`{"temporary":"proxy failure"}`))
	}))
	defer server.Close()
	old := openaiModelsAPIURL
	SetOpenAIModelsAPIURL(server.URL)
	t.Cleanup(func() { SetOpenAIModelsAPIURL(old) })

	_, err := ListModels(context.Background(), "openai")
	if err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("error = %v, want HTTP 502", err)
	}
}

func modelIDs(models []ModelInfo) []string {
	out := make([]string, 0, len(models))
	for _, model := range models {
		out = append(out, model.ID)
	}
	return out
}
