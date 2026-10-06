package llm

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// recordingServer answers each request with the next scripted reply and keeps
// the request bodies. A reply starting with "400:" is sent as a bad request.
func recordingServer(t *testing.T, replies ...string) (url string, bodies *[]map[string]any) {
	t.Helper()
	var captured []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		if err := json.Unmarshal(raw, &body); err != nil {
			t.Errorf("request body is not JSON: %v", err)
		}
		captured = append(captured, body)
		reply := replies[min(len(captured), len(replies))-1]
		w.Header().Set("Content-Type", "application/json")
		if rest, ok := strings.CutPrefix(reply, "400:"); ok {
			w.WriteHeader(http.StatusBadRequest)
			reply = rest
		}
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &captured
}

const (
	anthropicOK = `{"model":"claude","stop_reason":"end_turn","content":[{"type":"text","text":"ok"}]}`
	chatOK      = `{"model":"m","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`
)

func useAnthropicURL(t *testing.T, url string) {
	t.Helper()
	original := AnthropicAPIURL()
	SetAnthropicAPIURL(url)
	t.Cleanup(func() { SetAnthropicAPIURL(original) })
}

// Current models reject a sampling temperature, so no provider sends one.
func TestProviders_NeverSendATemperature(t *testing.T) {
	anthropicURL, anthropicBodies := recordingServer(t, anthropicOK)
	useAnthropicURL(t, anthropicURL)
	if _, err := (&anthropicProvider{model: "claude-sonnet-4-6", apiKey: "k"}).Complete(context.Background(), &Request{SystemPrompt: "sys", UserPrompt: "spec", MaxTokens: 100}); err != nil {
		t.Fatalf("anthropic Complete: %v", err)
	}
	if _, sent := (*anthropicBodies)[0]["temperature"]; sent {
		t.Errorf("anthropic request carries a temperature: %v", (*anthropicBodies)[0])
	}

	for name := range chatProviders(t, "") {
		url, bodies := recordingServer(t, chatOK)
		if _, err := chatProviders(t, url)[name].Complete(context.Background(), &Request{SystemPrompt: "sys", UserPrompt: "spec", MaxTokens: 100}); err != nil {
			t.Fatalf("%s Complete: %v", name, err)
		}
		if _, sent := (*bodies)[0]["temperature"]; sent {
			t.Errorf("%s request carries a temperature: %v", name, (*bodies)[0])
		}
	}
}

func TestAnthropicComplete_Effort(t *testing.T) {
	url, bodies := recordingServer(t, anthropicOK)
	useAnthropicURL(t, url)
	p := &anthropicProvider{model: "claude-opus-5-5", apiKey: "k"}

	if _, err := p.Complete(context.Background(), &Request{UserPrompt: "spec"}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if _, ok := (*bodies)[0]["output_config"]; ok {
		t.Errorf("output_config sent without an effort: %v", (*bodies)[0])
	}

	if _, err := p.Complete(context.Background(), &Request{UserPrompt: "spec", Effort: "xhigh"}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	config, _ := (*bodies)[1]["output_config"].(map[string]any)
	if config["effort"] != "xhigh" {
		t.Errorf("output_config = %v, want effort xhigh", (*bodies)[1]["output_config"])
	}
}

func TestAnthropicComplete_RefusalIsAnError(t *testing.T) {
	cases := map[string]struct {
		body string
		want []string
	}{
		"with details": {
			body: `{"model":"claude","stop_reason":"refusal","stop_details":{"type":"refusal","category":"cyber","explanation":"The request was flagged."},"content":[{"type":"text","text":"{\"issues\":[]}"}]}`,
			want: []string{"declined the request", "category: cyber", "The request was flagged."},
		},
		"without details": {
			body: `{"model":"claude","stop_reason":"refusal","content":[]}`,
			want: []string{"declined the request", "stop_reason: refusal"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			url, _ := recordingServer(t, tc.body)
			useAnthropicURL(t, url)

			_, err := (&anthropicProvider{model: "claude-opus-5-5", apiKey: "k"}).Complete(context.Background(), &Request{UserPrompt: "spec"})
			if err == nil {
				t.Fatal("expected an error for a refused request")
			}
			for _, want := range tc.want {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}

// chatProviders returns the two OpenAI-compatible providers pointed at url.
func chatProviders(t *testing.T, url string) map[string]Provider {
	t.Helper()
	openaiURL, geminiURL := OpenAIAPIURL(), GeminiAPIURL()
	SetOpenAIAPIURL(url)
	SetGeminiAPIURL(url)
	t.Cleanup(func() {
		SetOpenAIAPIURL(openaiURL)
		SetGeminiAPIURL(geminiURL)
	})
	return map[string]Provider{
		"openai": &openaiProvider{model: "gpt-6.1-sol", apiKey: "k"},
		"gemini": &geminiProvider{model: "gemini-3.8-flash", apiKey: "k"},
	}
}

func TestChatComplete_Effort(t *testing.T) {
	for name := range chatProviders(t, "") {
		t.Run(name, func(t *testing.T) {
			url, bodies := recordingServer(t, chatOK)
			provider := chatProviders(t, url)[name]

			if _, err := provider.Complete(context.Background(), &Request{UserPrompt: "spec"}); err != nil {
				t.Fatalf("Complete: %v", err)
			}
			if _, ok := (*bodies)[0]["reasoning_effort"]; ok {
				t.Errorf("reasoning_effort sent without an effort: %v", (*bodies)[0])
			}
			if _, err := provider.Complete(context.Background(), &Request{UserPrompt: "spec", Effort: "high"}); err != nil {
				t.Fatalf("Complete: %v", err)
			}
			if (*bodies)[1]["reasoning_effort"] != "high" {
				t.Errorf("reasoning_effort = %v, want high", (*bodies)[1]["reasoning_effort"])
			}
		})
	}
}

func TestChatComplete_RefusalIsAnError(t *testing.T) {
	const refused = `{"model":"m","choices":[{"message":{"role":"assistant","content":"","refusal":"I can't help with that."},"finish_reason":"stop"}]}`
	for name := range chatProviders(t, "") {
		t.Run(name, func(t *testing.T) {
			url, _ := recordingServer(t, refused)
			_, err := chatProviders(t, url)[name].Complete(context.Background(), &Request{UserPrompt: "spec"})
			if err == nil || !strings.Contains(err.Error(), "declined the request") || !strings.Contains(err.Error(), "I can't help with that.") {
				t.Fatalf("error = %v, want the refusal reported", err)
			}
		})
	}
}

func TestChatComplete_EmptyAndBlockedResponses(t *testing.T) {
	cases := map[string]struct {
		body          string
		wantErr       string
		wantTruncated bool
	}{
		"content filter": {
			body:    `{"model":"m","choices":[{"message":{"role":"assistant","content":"partial"},"finish_reason":"content_filter"}]}`,
			wantErr: "declined the request (finish_reason: content_filter)",
		},
		"empty and finished": {
			body:    `{"model":"m","choices":[{"message":{"role":"assistant","content":""},"finish_reason":"stop"}]}`,
			wantErr: "empty response",
		},
		// The budget ran out before any text: the caller retries with more room.
		"empty and cut off": {
			body:          `{"model":"m","choices":[{"message":{"role":"assistant","content":""},"finish_reason":"length"}]}`,
			wantTruncated: true,
		},
	}
	for name, tc := range cases {
		for providerName := range chatProviders(t, "") {
			t.Run(providerName+"/"+name, func(t *testing.T) {
				url, _ := recordingServer(t, tc.body)
				resp, err := chatProviders(t, url)[providerName].Complete(context.Background(), &Request{UserPrompt: "spec"})
				if tc.wantErr != "" {
					if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
						t.Fatalf("error = %v, want %q", err, tc.wantErr)
					}
					return
				}
				if err != nil {
					t.Fatalf("Complete: %v", err)
				}
				if resp.Truncated != tc.wantTruncated || resp.Content != "" {
					t.Errorf("truncated=%v content=%q, want truncated=%v and no content", resp.Truncated, resp.Content, tc.wantTruncated)
				}
			})
		}
	}
}

func TestDefaultModelForProvider(t *testing.T) {
	cases := map[string]string{
		"anthropic": "claude-opus-5-5",
		"":          "claude-opus-5-5",
		"openai":    "gpt-4o",
		"OpenAI":    "gpt-4o",
		"gemini":    "gemini-3.8-flash",
	}
	for provider, want := range cases {
		if got := DefaultModelForProvider(provider); got != want {
			t.Errorf("DefaultModelForProvider(%q) = %q, want %q", provider, got, want)
		}
	}
}

func TestChatComplete_PromptCacheKey(t *testing.T) {
	for name, tc := range map[string]struct {
		provider string
		prefix   string
		want     bool
	}{
		"openai with a cacheable prefix": {"openai", "<spec>...</spec>", true},
		"openai without a prefix":        {"openai", "", false},
		"gemini does not take the key":   {"gemini", "<spec>...</spec>", false},
	} {
		t.Run(name, func(t *testing.T) {
			url, bodies := recordingServer(t, chatOK, chatOK, chatOK)
			provider := chatProviders(t, url)[tc.provider]
			for _, task := range []string{"task one", "task two"} {
				if _, err := provider.Complete(context.Background(), &Request{SystemPrompt: "sys", UserPromptCachedPrefix: tc.prefix, UserPrompt: task}); err != nil {
					t.Fatalf("Complete: %v", err)
				}
			}
			key, sent := (*bodies)[0]["prompt_cache_key"].(string)
			if sent != tc.want {
				t.Fatalf("prompt_cache_key sent = %v, want %v", sent, tc.want)
			}
			if tc.want && (key == "" || (*bodies)[1]["prompt_cache_key"] != key) {
				t.Errorf("keys = %v and %v, want one non-empty key for calls sharing a prefix", key, (*bodies)[1]["prompt_cache_key"])
			}
		})
	}
	other := promptCacheKey(&Request{SystemPrompt: "sys", UserPromptCachedPrefix: "<spec>other</spec>"})
	if other == promptCacheKey(&Request{SystemPrompt: "sys", UserPromptCachedPrefix: "<spec>...</spec>"}) {
		t.Error("different prefixes must get different keys")
	}
}

// The spec is material under review. It must never be sent as part of the
// system message, where its text would carry the weight of instructions.
func TestChatComplete_KeepsTheSpecOutOfTheSystemMessage(t *testing.T) {
	for name := range chatProviders(t, "") {
		t.Run(name, func(t *testing.T) {
			url, bodies := recordingServer(t, chatOK)
			req := &Request{SystemPrompt: "sys", UserPromptCachedPrefix: "<spec>\nL1: x\n</spec>\n", UserPrompt: "\nReview the spec.\n"}
			if _, err := chatProviders(t, url)[name].Complete(context.Background(), req); err != nil {
				t.Fatalf("Complete: %v", err)
			}
			system, user := chatSystemText((*bodies)[0]), chatUserText((*bodies)[0])
			if system != "sys" || user != req.UserPromptCachedPrefix+req.UserPrompt {
				t.Errorf("system = %q user = %q, want the spec only in the user message", system, user)
			}
		})
	}
}
