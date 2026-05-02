package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	ctx "github.com/dshills/speccritic/internal/context"
	"github.com/dshills/speccritic/internal/profile"
	"github.com/dshills/speccritic/internal/spec"
)

func TestAnthropicComplete_SendsCacheControlOnSystem(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request body: %v", err)
		}
		captured = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","model":"claude","content":[{"type":"text","text":"ok"}]}`))
	}))
	t.Cleanup(srv.Close)

	original := AnthropicAPIURL()
	SetAnthropicAPIURL(srv.URL)
	t.Cleanup(func() { SetAnthropicAPIURL(original) })

	p := &anthropicProvider{model: "claude-test", apiKey: "k"}
	if _, err := p.Complete(context.Background(), &Request{
		SystemPrompt: "stable system prompt",
		UserPrompt:   "variable spec content",
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	var sent struct {
		System []struct {
			Type         string `json:"type"`
			Text         string `json:"text"`
			CacheControl *struct {
				Type string `json:"type"`
			} `json:"cache_control"`
		} `json:"system"`
	}
	if err := json.Unmarshal(captured, &sent); err != nil {
		t.Fatalf("unmarshal captured body: %v\nbody: %s", err, captured)
	}
	if len(sent.System) != 1 {
		t.Fatalf("expected 1 system block, got %d", len(sent.System))
	}
	if sent.System[0].Text != "stable system prompt" {
		t.Errorf("system text = %q", sent.System[0].Text)
	}
	if sent.System[0].CacheControl == nil || sent.System[0].CacheControl.Type != "ephemeral" {
		t.Errorf("expected cache_control=ephemeral, got %+v", sent.System[0].CacheControl)
	}
}

func TestAnthropicComplete_UserPromptPrefix_EmitsCachedBlock(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","model":"claude","content":[{"type":"text","text":"ok"}]}`))
	}))
	t.Cleanup(srv.Close)

	original := AnthropicAPIURL()
	SetAnthropicAPIURL(srv.URL)
	t.Cleanup(func() { SetAnthropicAPIURL(original) })

	p := &anthropicProvider{model: "claude-test", apiKey: "k"}
	if _, err := p.Complete(context.Background(), &Request{
		SystemPrompt:           "sys",
		UserPromptCachedPrefix: "stable context",
		UserPrompt:             "variable spec",
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	var sent struct {
		Messages []struct {
			Role    string `json:"role"`
			Content []struct {
				Type         string `json:"type"`
				Text         string `json:"text"`
				CacheControl *struct {
					Type string `json:"type"`
				} `json:"cache_control"`
			} `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(captured, &sent); err != nil {
		t.Fatalf("unmarshal captured body: %v\nbody: %s", err, captured)
	}
	if len(sent.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(sent.Messages))
	}
	blocks := sent.Messages[0].Content
	if len(blocks) != 2 {
		t.Fatalf("expected 2 content blocks, got %d: %+v", len(blocks), blocks)
	}
	if blocks[0].Text != "stable context" {
		t.Errorf("block 0 text = %q", blocks[0].Text)
	}
	if blocks[0].CacheControl == nil || blocks[0].CacheControl.Type != "ephemeral" {
		t.Errorf("block 0 missing cache_control=ephemeral: %+v", blocks[0].CacheControl)
	}
	if blocks[1].Text != "variable spec" {
		t.Errorf("block 1 text = %q", blocks[1].Text)
	}
	if blocks[1].CacheControl != nil {
		t.Errorf("block 1 should not have cache_control: %+v", blocks[1].CacheControl)
	}
}

func TestAnthropicComplete_NoUserPromptPrefix_StringContent(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","model":"claude","content":[{"type":"text","text":"ok"}]}`))
	}))
	t.Cleanup(srv.Close)

	original := AnthropicAPIURL()
	SetAnthropicAPIURL(srv.URL)
	t.Cleanup(func() { SetAnthropicAPIURL(original) })

	p := &anthropicProvider{model: "claude-test", apiKey: "k"}
	if _, err := p.Complete(context.Background(), &Request{
		SystemPrompt: "sys",
		UserPrompt:   "just the spec",
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	// Without a prefix, content should be serialized as a plain JSON string,
	// not an array — this keeps the request minimal and avoids an unnecessary
	// cache lookup on small prompts.
	var sent struct {
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(captured, &sent); err != nil {
		t.Fatalf("unmarshal captured body: %v\nbody: %s", err, captured)
	}
	if len(sent.Messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(sent.Messages))
	}
	raw := string(sent.Messages[0].Content)
	if !strings.HasPrefix(raw, `"`) {
		t.Errorf("expected string content, got %s", raw)
	}
}

func TestAnthropicComplete_TemperatureZeroIsForwarded(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","model":"claude","content":[{"type":"text","text":"ok"}]}`))
	}))
	t.Cleanup(srv.Close)

	original := AnthropicAPIURL()
	SetAnthropicAPIURL(srv.URL)
	t.Cleanup(func() { SetAnthropicAPIURL(original) })

	zero := 0.0
	p := &anthropicProvider{model: "claude-test", apiKey: "k"}
	if _, err := p.Complete(context.Background(), &Request{
		UserPrompt:  "hi",
		Temperature: &zero,
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	var sent struct {
		Temperature *float64 `json:"temperature"`
	}
	if err := json.Unmarshal(captured, &sent); err != nil {
		t.Fatalf("unmarshal captured body: %v\nbody: %s", err, captured)
	}
	if sent.Temperature == nil {
		t.Fatalf("expected temperature=0 to be forwarded, got nil; body: %s", captured)
	}
	if *sent.Temperature != 0.0 {
		t.Errorf("temperature = %v, want 0.0", *sent.Temperature)
	}
}

func TestAnthropicComplete_NilTemperatureOmitted(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","model":"claude","content":[{"type":"text","text":"ok"}]}`))
	}))
	t.Cleanup(srv.Close)

	original := AnthropicAPIURL()
	SetAnthropicAPIURL(srv.URL)
	t.Cleanup(func() { SetAnthropicAPIURL(original) })

	p := &anthropicProvider{model: "claude-test", apiKey: "k"}
	if _, err := p.Complete(context.Background(), &Request{UserPrompt: "hi"}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if strings.Contains(string(captured), `"temperature"`) {
		t.Errorf("expected no temperature field when unset, body: %s", captured)
	}
}

func TestAnthropicComplete_OmitsSystemWhenEmpty(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"x","model":"claude","content":[{"type":"text","text":"ok"}]}`))
	}))
	t.Cleanup(srv.Close)

	original := AnthropicAPIURL()
	SetAnthropicAPIURL(srv.URL)
	t.Cleanup(func() { SetAnthropicAPIURL(original) })

	p := &anthropicProvider{model: "claude-test", apiKey: "k"}
	if _, err := p.Complete(context.Background(), &Request{UserPrompt: "hi"}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if strings.Contains(string(captured), `"system"`) {
		t.Errorf("expected no system field when prompt empty, body: %s", captured)
	}
}

func TestOpenAIComplete_UsesMaxCompletionTokens(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"gpt-test","choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	t.Cleanup(srv.Close)

	original := OpenAIAPIURL()
	SetOpenAIAPIURL(srv.URL)
	t.Cleanup(func() { SetOpenAIAPIURL(original) })

	p := &openaiProvider{model: "gpt-5", apiKey: "k"}
	if _, err := p.Complete(context.Background(), &Request{
		UserPrompt: "hi",
		MaxTokens:  1234,
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	var sent map[string]any
	if err := json.Unmarshal(captured, &sent); err != nil {
		t.Fatalf("unmarshal captured body: %v\nbody: %s", err, captured)
	}
	if _, ok := sent["max_tokens"]; ok {
		t.Fatalf("request should not include max_tokens: %s", captured)
	}
	if got := sent["max_completion_tokens"]; got != float64(1234) {
		t.Fatalf("max_completion_tokens = %#v, want 1234; body: %s", got, captured)
	}
}

func TestOpenAIComplete_UsesMaxTokensForLegacyModels(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"gpt-4o","choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	t.Cleanup(srv.Close)

	original := OpenAIAPIURL()
	SetOpenAIAPIURL(srv.URL)
	t.Cleanup(func() { SetOpenAIAPIURL(original) })

	p := &openaiProvider{model: "gpt-4o", apiKey: "k"}
	if _, err := p.Complete(context.Background(), &Request{
		UserPrompt: "hi",
		MaxTokens:  1234,
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	var sent map[string]any
	if err := json.Unmarshal(captured, &sent); err != nil {
		t.Fatalf("unmarshal captured body: %v\nbody: %s", err, captured)
	}
	if _, ok := sent["max_completion_tokens"]; ok {
		t.Fatalf("legacy request should not include max_completion_tokens: %s", captured)
	}
	if got := sent["max_tokens"]; got != float64(1234) {
		t.Fatalf("max_tokens = %#v, want 1234; body: %s", got, captured)
	}
	responseFormat, ok := sent["response_format"].(map[string]any)
	if !ok || responseFormat["type"] != "json_object" {
		t.Fatalf("response_format = %#v, want json_object; body: %s", sent["response_format"], captured)
	}
}

func TestGeminiComplete_RequestsJSONMode(t *testing.T) {
	var captured []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = body
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"gemini-2.0-flash","choices":[{"message":{"role":"assistant","content":"{}"}}]}`))
	}))
	t.Cleanup(srv.Close)

	original := GeminiAPIURL()
	SetGeminiAPIURL(srv.URL)
	t.Cleanup(func() { SetGeminiAPIURL(original) })

	p := &geminiProvider{model: "gemini-2.0-flash", apiKey: "k"}
	if _, err := p.Complete(context.Background(), &Request{
		UserPrompt: "return JSON",
		MaxTokens:  1234,
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}

	var sent map[string]any
	if err := json.Unmarshal(captured, &sent); err != nil {
		t.Fatalf("unmarshal captured body: %v\nbody: %s", err, captured)
	}
	responseFormat, ok := sent["response_format"].(map[string]any)
	if !ok || responseFormat["type"] != "json_object" {
		t.Fatalf("response_format = %#v, want json_object; body: %s", sent["response_format"], captured)
	}
}

func TestOpenAIComplete_RetriesAlternateTokenParameter(t *testing.T) {
	var captured [][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		captured = append(captured, body)
		w.Header().Set("Content-Type", "application/json")
		if len(captured) == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"Unsupported parameter: 'max_tokens' is not supported with this model. Use 'max_completion_tokens' instead."}}`))
			return
		}
		_, _ = w.Write([]byte(`{"model":"new-model","choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
	}))
	t.Cleanup(srv.Close)

	original := OpenAIAPIURL()
	SetOpenAIAPIURL(srv.URL)
	t.Cleanup(func() { SetOpenAIAPIURL(original) })

	p := &openaiProvider{model: "new-model", apiKey: "k"}
	if _, err := p.Complete(context.Background(), &Request{
		UserPrompt: "hi",
		MaxTokens:  1234,
	}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(captured) != 2 {
		t.Fatalf("calls = %d, want retry with alternate token field", len(captured))
	}
	if !strings.Contains(string(captured[0]), `"max_tokens"`) {
		t.Fatalf("first request = %s, want max_tokens", captured[0])
	}
	if !strings.Contains(string(captured[1]), `"max_completion_tokens"`) {
		t.Fatalf("second request = %s, want max_completion_tokens", captured[1])
	}
}

func writeTempSpec(t *testing.T, content string) *spec.Spec {
	t.Helper()
	f, err := os.CreateTemp("", "spec*.md")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Remove(f.Name()) })
	defer func() { _ = f.Close() }()
	if _, err := f.WriteString(content); err != nil {
		t.Fatal(err)
	}
	s, err := spec.Load(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestBuildUserPrompt_SpecGoesInVariableTail(t *testing.T) {
	s := writeTempSpec(t, "line one\nline two\n")
	prefix, tail := BuildUserPrompt(s, nil)

	if !strings.Contains(tail, "L1: line one") {
		t.Errorf("tail missing line-numbered spec content: %q", tail)
	}
	if !strings.Contains(tail, "L2: line two") {
		t.Errorf("tail missing L2: %q", tail)
	}
	if strings.Contains(prefix, "L1:") {
		t.Errorf("spec content leaked into cacheable prefix: %q", prefix)
	}
}

func TestBuildUserPrompt_ContextFilesGoInCachedPrefix(t *testing.T) {
	s := writeTempSpec(t, "spec content\n")
	files := []ctx.ContextFile{
		{Path: "glossary.md", Content: "term: definition\n"},
	}
	prefix, tail := BuildUserPrompt(s, files)

	if !strings.Contains(prefix, `<context file="glossary.md">`) {
		t.Errorf("prefix missing context XML tag: %q", prefix)
	}
	if !strings.Contains(prefix, "term: definition") {
		t.Errorf("prefix missing context content: %q", prefix)
	}
	if strings.Contains(tail, "<context") {
		t.Errorf("context leaked into variable tail: %q", tail)
	}
}

func TestBuildUserPrompt_NoContextFiles_NoXMLTags(t *testing.T) {
	s := writeTempSpec(t, "spec content\n")
	prefix, tail := BuildUserPrompt(s, nil)

	if strings.Contains(prefix, "<context") || strings.Contains(tail, "<context") {
		t.Errorf("should not contain context tags when no context files: prefix=%q tail=%q", prefix, tail)
	}
}

func TestBuildSystemPrompt_ContainsProfileRules(t *testing.T) {
	p, err := profile.Get("backend-api")
	if err != nil {
		t.Fatalf("profile.Get: %v", err)
	}
	sys := BuildSystemPrompt(p, false)

	// Check that the profile's FormatRulesForPrompt output is included.
	rules := p.FormatRulesForPrompt()
	if !strings.Contains(sys, rules) {
		t.Errorf("system prompt does not contain profile rules output")
	}
}

func TestBuildSystemPrompt_StrictModeInjected(t *testing.T) {
	p, err := profile.Get("general")
	if err != nil {
		t.Fatalf("profile.Get: %v", err)
	}
	sys := BuildSystemPrompt(p, true)

	if !strings.Contains(sys, "STRICT MODE ENABLED") {
		t.Errorf("system prompt missing strict mode text: %q", sys)
	}
}

func TestBuildSystemPrompt_NoStrictMode(t *testing.T) {
	p, err := profile.Get("general")
	if err != nil {
		t.Fatalf("profile.Get: %v", err)
	}
	sys := BuildSystemPrompt(p, false)

	if strings.Contains(sys, "STRICT MODE ENABLED") {
		t.Errorf("system prompt should not contain strict mode text when not enabled: %q", sys)
	}
}

func TestNewProvider_UnknownPrefix(t *testing.T) {
	_, err := NewProvider("cohere:command-r")
	if err == nil {
		t.Error("expected error for unknown provider prefix, got nil")
	}
}

func TestNewProvider_Gemini_NoKey(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "")
	_, err := NewProvider("gemini:gemini-2.5-flash")
	if err == nil {
		t.Error("expected error when GEMINI_API_KEY not set, got nil")
	}
}

func TestNewProvider_Gemini_WithKey(t *testing.T) {
	t.Setenv("GEMINI_API_KEY", "test-key-for-construction-only")
	p, err := NewProvider("gemini:gemini-2.5-flash")
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if p == nil {
		t.Error("expected non-nil provider")
	}
}

func TestNewProvider_InvalidFormat(t *testing.T) {
	_, err := NewProvider("nocoIon")
	if err == nil {
		t.Error("expected error for missing colon separator, got nil")
	}
}

func TestNewProvider_Anthropic_NoKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "")
	_, err := NewProvider(DefaultProvider + ":" + DefaultModel)
	if err == nil {
		t.Error("expected error when ANTHROPIC_API_KEY not set, got nil")
	}
}

func TestNewProvider_OpenAI_NoKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	_, err := NewProvider("openai:gpt-4o")
	if err == nil {
		t.Error("expected error when OPENAI_API_KEY not set, got nil")
	}
}

func TestNewProvider_Anthropic_WithKey(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "sk-test-key-for-construction-only")
	p, err := NewProvider(DefaultProvider + ":" + DefaultModel)
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if p == nil {
		t.Error("expected non-nil provider")
	}
}

func TestNewProvider_OpenAI_WithKey(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test-key-for-construction-only")
	p, err := NewProvider("openai:gpt-4o")
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if p == nil {
		t.Error("expected non-nil provider")
	}
}

func TestNewProvider_NormalizesProviderCase(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "sk-test-key-for-construction-only")
	p, err := NewProvider("OpenAI:gpt-4o")
	if err != nil {
		t.Fatalf("NewProvider: %v", err)
	}
	if p == nil {
		t.Error("expected non-nil provider")
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("hello", 10); got != "hello" {
		t.Errorf("truncate short string: got %q", got)
	}
	if got := truncate("hello world", 5); got != "hello..." {
		t.Errorf("truncate long string: got %q", got)
	}
	// Multi-byte: é is 2 bytes but 1 rune; truncating at 3 runes should not cut mid-codepoint.
	if got := truncate("héllo", 3); got != "hél..." {
		t.Errorf("truncate multibyte: got %q, want %q", got, "hél...")
	}
}
