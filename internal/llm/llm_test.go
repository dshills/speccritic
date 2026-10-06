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
	cases := map[string]struct {
		model       string
		firstField  string
		secondField string
	}{
		// A model in a family that predates max_completion_tokens but turns out
		// to require it.
		"legacy family rejects max_tokens": {"gpt-4o-next", "max_tokens", "max_completion_tokens"},
		// An unrecognized model that only knows the older parameter.
		"unknown model rejects max_completion_tokens": {"new-model", "max_completion_tokens", "max_tokens"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			var captured [][]byte
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				body, _ := io.ReadAll(r.Body)
				captured = append(captured, body)
				w.Header().Set("Content-Type", "application/json")
				if len(captured) == 1 {
					w.WriteHeader(http.StatusBadRequest)
					_, _ = w.Write([]byte(`{"error":{"type":"invalid_request_error","message":"Unsupported parameter: '` + tc.firstField + `' is not supported with this model. Use '` + tc.secondField + `' instead."}}`))
					return
				}
				_, _ = w.Write([]byte(`{"model":"m","choices":[{"message":{"role":"assistant","content":"ok"}}]}`))
			}))
			t.Cleanup(srv.Close)

			original := OpenAIAPIURL()
			SetOpenAIAPIURL(srv.URL)
			t.Cleanup(func() { SetOpenAIAPIURL(original) })

			p := &openaiProvider{model: tc.model, apiKey: "k"}
			if _, err := p.Complete(context.Background(), &Request{UserPrompt: "hi", MaxTokens: 1234}); err != nil {
				t.Fatalf("Complete: %v", err)
			}
			if len(captured) != 2 {
				t.Fatalf("calls = %d, want retry with alternate token field", len(captured))
			}
			if !strings.Contains(string(captured[0]), `"`+tc.firstField+`"`) {
				t.Fatalf("first request = %s, want %s", captured[0], tc.firstField)
			}
			if !strings.Contains(string(captured[1]), `"`+tc.secondField+`"`) || strings.Contains(string(captured[1]), `"`+tc.firstField+`"`) {
				t.Fatalf("second request = %s, want only %s", captured[1], tc.secondField)
			}
		})
	}
}

func TestOpenAIUsesMaxCompletionTokens(t *testing.T) {
	cases := map[string]bool{
		"gpt-3.5-turbo": false,
		"gpt-4":         false,
		"gpt-4-turbo":   false,
		"gpt-4o":        false,
		"gpt-4.1-mini":  false,
		"gpt-5":         true,
		"gpt-5.5-pro":   true,
		"gpt-6.1-sol":   true,
		"GPT-6-Sol":     true,
		"o3":            true,
		"some-new-name": true,
	}
	for model, want := range cases {
		if got := openaiUsesMaxCompletionTokens(model); got != want {
			t.Errorf("openaiUsesMaxCompletionTokens(%q) = %v, want %v", model, got, want)
		}
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

func TestBuildSpecPrefix(t *testing.T) {
	s := writeTempSpec(t, "line one\nline two\n")
	files := []ctx.ContextFile{{Path: "glossary.md", Content: "term: definition\n"}}
	const preflight = "<known_preflight_findings>\n- PREFLIGHT-X\n</known_preflight_findings>\n"

	prefix := BuildSpecPrefix(s, files, preflight)
	order := []string{`<context file="glossary.md">`, "term: definition", "<spec file=", "L1: line one", "L2: line two", "</spec>", "<known_preflight_findings>"}
	at := -1
	for _, want := range order {
		i := strings.Index(prefix, want)
		if i < 0 {
			t.Fatalf("prefix is missing %q:\n%s", want, prefix)
		}
		if i < at {
			t.Fatalf("%q is out of order in the prefix:\n%s", want, prefix)
		}
		at = i
	}
	// The prefix is shared by every call, so it must not carry any one task.
	if strings.Contains(prefix, "Analyze") || strings.Contains(prefix, "Review ") {
		t.Errorf("prefix carries a task:\n%s", prefix)
	}
	if again := BuildSpecPrefix(s, files, preflight); again != prefix {
		t.Error("the prefix must be byte-identical for the same inputs")
	}
}

func TestBuildSpecPrefix_NoContextNoPreflight(t *testing.T) {
	s := writeTempSpec(t, "spec content\n")
	prefix := BuildSpecPrefix(s, nil, "")
	if strings.Contains(prefix, "<context") || strings.Contains(prefix, "preflight") {
		t.Errorf("prefix = %q, want only the spec", prefix)
	}
	if !strings.HasPrefix(prefix, "<spec file=") || !strings.HasSuffix(prefix, "</spec>\n") {
		t.Errorf("prefix = %q, want the spec block alone", prefix)
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

// serveJSON starts a test server that answers every request with body.
func serveJSON(t *testing.T, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestAnthropicComplete_ReportsStopReason(t *testing.T) {
	cases := map[string]struct {
		body          string
		wantContent   string
		wantTruncated bool
		wantErr       bool
	}{
		"finished": {
			body:        `{"model":"claude","stop_reason":"end_turn","content":[{"type":"text","text":"ok"}]}`,
			wantContent: "ok",
		},
		"hit output cap": {
			body:          `{"model":"claude","stop_reason":"max_tokens","content":[{"type":"text","text":"{\"issues\":["}]}`,
			wantContent:   `{"issues":[`,
			wantTruncated: true,
		},
		"hit output cap before any text": {
			body:          `{"model":"claude","stop_reason":"max_tokens","content":[{"type":"thinking","thinking":""}]}`,
			wantTruncated: true,
		},
		"no text and not truncated": {
			body:    `{"model":"claude","stop_reason":"end_turn","content":[]}`,
			wantErr: true,
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			original := AnthropicAPIURL()
			SetAnthropicAPIURL(serveJSON(t, tc.body))
			t.Cleanup(func() { SetAnthropicAPIURL(original) })

			p := &anthropicProvider{model: "claude-test", apiKey: "k"}
			resp, err := p.Complete(context.Background(), &Request{UserPrompt: "spec"})
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("Complete: %v", err)
			}
			if resp.Content != tc.wantContent || resp.Truncated != tc.wantTruncated {
				t.Errorf("content=%q truncated=%v, want %q/%v", resp.Content, resp.Truncated, tc.wantContent, tc.wantTruncated)
			}
			if tc.wantTruncated && resp.StopReason != "max_tokens" {
				t.Errorf("stop reason = %q, want max_tokens", resp.StopReason)
			}
		})
	}
}

func TestOpenAICompatibleComplete_ReportsFinishReason(t *testing.T) {
	const finished = `{"model":"m","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`
	const cutOff = `{"model":"m","choices":[{"message":{"role":"assistant","content":"{\"issues\":["},"finish_reason":"length"}]}`

	providers := map[string]struct {
		provider Provider
		setURL   func(string)
		url      func() string
	}{
		"openai": {provider: &openaiProvider{model: "gpt-test", apiKey: "k"}, setURL: SetOpenAIAPIURL, url: OpenAIAPIURL},
		"gemini": {provider: &geminiProvider{model: "gemini-test", apiKey: "k"}, setURL: SetGeminiAPIURL, url: GeminiAPIURL},
	}
	for name, tc := range providers {
		t.Run(name, func(t *testing.T) {
			original := tc.url()
			t.Cleanup(func() { tc.setURL(original) })

			tc.setURL(serveJSON(t, finished))
			resp, err := tc.provider.Complete(context.Background(), &Request{UserPrompt: "spec"})
			if err != nil {
				t.Fatalf("Complete: %v", err)
			}
			if resp.Truncated || resp.StopReason != "stop" {
				t.Errorf("finished response: truncated=%v stop=%q", resp.Truncated, resp.StopReason)
			}

			tc.setURL(serveJSON(t, cutOff))
			resp, err = tc.provider.Complete(context.Background(), &Request{UserPrompt: "spec"})
			if err != nil {
				t.Fatalf("Complete: %v", err)
			}
			if !resp.Truncated || resp.StopReason != "length" {
				t.Errorf("cut-off response: truncated=%v stop=%q", resp.Truncated, resp.StopReason)
			}
		})
	}
}

func TestAnthropicComplete_ReportsUsage(t *testing.T) {
	original := AnthropicAPIURL()
	SetAnthropicAPIURL(serveJSON(t, `{"model":"claude","stop_reason":"end_turn","content":[{"type":"text","text":"ok"}],
		"usage":{"input_tokens":120,"output_tokens":45,"cache_creation_input_tokens":900,"cache_read_input_tokens":3000}}`))
	t.Cleanup(func() { SetAnthropicAPIURL(original) })

	p := &anthropicProvider{model: "claude-test", apiKey: "k"}
	resp, err := p.Complete(context.Background(), &Request{UserPrompt: "spec"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	want := Usage{InputTokens: 120, OutputTokens: 45, CacheReadTokens: 3000, CacheWriteTokens: 900}
	if resp.Usage != want {
		t.Errorf("usage = %+v, want %+v", resp.Usage, want)
	}
}

func TestOpenAICompatibleComplete_ReportsUsage(t *testing.T) {
	cases := map[string]struct {
		usage string
		want  Usage
	}{
		// prompt_tokens includes the cached share, which must not be counted twice.
		"with cached tokens":    {`{"prompt_tokens":1000,"completion_tokens":50,"prompt_tokens_details":{"cached_tokens":800}}`, Usage{InputTokens: 200, OutputTokens: 50, CacheReadTokens: 800}},
		"without cache details": {`{"prompt_tokens":1000,"completion_tokens":50}`, Usage{InputTokens: 1000, OutputTokens: 50}},
	}
	providers := map[string]struct {
		provider Provider
		setURL   func(string)
		url      func() string
	}{
		"openai": {provider: &openaiProvider{model: "gpt-test", apiKey: "k"}, setURL: SetOpenAIAPIURL, url: OpenAIAPIURL},
		"gemini": {provider: &geminiProvider{model: "gemini-test", apiKey: "k"}, setURL: SetGeminiAPIURL, url: GeminiAPIURL},
	}
	for providerName, pc := range providers {
		for name, tc := range cases {
			t.Run(providerName+"/"+name, func(t *testing.T) {
				original := pc.url()
				t.Cleanup(func() { pc.setURL(original) })
				pc.setURL(serveJSON(t, `{"model":"m","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":`+tc.usage+`}`))

				resp, err := pc.provider.Complete(context.Background(), &Request{UserPrompt: "spec"})
				if err != nil {
					t.Fatalf("Complete: %v", err)
				}
				if resp.Usage != tc.want {
					t.Errorf("usage = %+v, want %+v", resp.Usage, tc.want)
				}
			})
		}
	}
}

func TestProviders_NoUsageReportedIsZero(t *testing.T) {
	original := AnthropicAPIURL()
	SetAnthropicAPIURL(serveJSON(t, `{"model":"claude","content":[{"type":"text","text":"ok"}]}`))
	t.Cleanup(func() { SetAnthropicAPIURL(original) })

	resp, err := (&anthropicProvider{model: "claude-test", apiKey: "k"}).Complete(context.Background(), &Request{UserPrompt: "spec"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Usage != (Usage{}) {
		t.Errorf("usage = %+v, want zero", resp.Usage)
	}
}
