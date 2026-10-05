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

func temperature(v float64) *float64 { return &v }

func TestAnthropicAcceptsTemperature(t *testing.T) {
	cases := map[string]bool{
		// Sampling parameters were removed starting with these models.
		"claude-opus-4-7":   false,
		"claude-opus-4-8":   false,
		"claude-opus-5":     false,
		"claude-opus-5-5":   false,
		"claude-sonnet-5":   false,
		"claude-sonnet-5-5": false,
		"claude-fable-5-1":  false,
		"claude-mythos-5":   false,
		"Claude-Opus-5-5":   false,
		// Earlier models still take one.
		"claude-opus-4-6":            true,
		"claude-opus-4-5-20251101":   true,
		"claude-opus-4-1":            true,
		"claude-sonnet-4-6":          true,
		"claude-sonnet-4-5-20250929": true,
		"claude-sonnet-4-20250514":   true,
		"claude-haiku-4-5-20251001":  true,
		"claude-3-5-sonnet-20241022": true,
		// Unknown names are assumed to; the API has the last word.
		"claude-test":    true,
		"claude-nova-9":  true,
		"something-else": true,
	}
	for model, want := range cases {
		if got := anthropicAcceptsTemperature(model); got != want {
			t.Errorf("anthropicAcceptsTemperature(%q) = %v, want %v", model, got, want)
		}
	}
}

func TestAnthropicComplete_TemperatureFollowsTheModel(t *testing.T) {
	cases := map[string]struct {
		model       string
		wantSent    bool
		wantDropped bool
	}{
		"accepted by an earlier model": {"claude-sonnet-4-6", true, false},
		"left out for a current model": {"claude-opus-5-5", false, true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			url, bodies := recordingServer(t, anthropicOK)
			useAnthropicURL(t, url)

			p := &anthropicProvider{model: tc.model, apiKey: "k"}
			resp, err := p.Complete(context.Background(), &Request{UserPrompt: "spec", Temperature: temperature(0.2)})
			if err != nil {
				t.Fatalf("Complete: %v", err)
			}
			if len(*bodies) != 1 {
				t.Fatalf("requests = %d, want 1", len(*bodies))
			}
			if _, sent := (*bodies)[0]["temperature"]; sent != tc.wantSent {
				t.Errorf("temperature sent = %v, want %v", sent, tc.wantSent)
			}
			if resp.TemperatureDropped != tc.wantDropped {
				t.Errorf("TemperatureDropped = %v, want %v", resp.TemperatureDropped, tc.wantDropped)
			}
		})
	}
}

func TestAnthropicComplete_NoTemperatureRequestedIsNotADrop(t *testing.T) {
	url, _ := recordingServer(t, anthropicOK)
	useAnthropicURL(t, url)

	resp, err := (&anthropicProvider{model: "claude-opus-5-5", apiKey: "k"}).Complete(context.Background(), &Request{UserPrompt: "spec"})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.TemperatureDropped {
		t.Error("TemperatureDropped = true for a request that never asked for a temperature")
	}
}

func TestAnthropicComplete_RetriesWithoutTemperatureAnUnknownModelRejects(t *testing.T) {
	url, bodies := recordingServer(t,
		`400:{"type":"error","error":{"type":"invalid_request_error","message":"temperature is not supported for this model"}}`,
		anthropicOK,
	)
	useAnthropicURL(t, url)

	p := &anthropicProvider{model: "claude-nova-9", apiKey: "k"}
	req := &Request{UserPrompt: "spec", Temperature: temperature(0.2)}
	resp, err := p.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if !resp.TemperatureDropped {
		t.Error("TemperatureDropped = false after the API rejected the temperature")
	}
	if _, err := p.Complete(context.Background(), req); err != nil {
		t.Fatalf("second Complete: %v", err)
	}
	if len(*bodies) != 3 {
		t.Fatalf("requests = %d, want rejected + retry, then one for the second call", len(*bodies))
	}
	for i, wantSent := range []bool{true, false, false} {
		if _, sent := (*bodies)[i]["temperature"]; sent != wantSent {
			t.Errorf("request %d temperature sent = %v, want %v", i, sent, wantSent)
		}
	}
}

func TestAnthropicComplete_OtherBadRequestsAreNotRetried(t *testing.T) {
	url, bodies := recordingServer(t, `400:{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens is too large"}}`)
	useAnthropicURL(t, url)

	p := &anthropicProvider{model: "claude-nova-9", apiKey: "k"}
	_, err := p.Complete(context.Background(), &Request{UserPrompt: "spec", Temperature: temperature(0.2)})
	if err == nil || !strings.Contains(err.Error(), "max_tokens is too large") {
		t.Fatalf("error = %v, want the API's message", err)
	}
	if len(*bodies) != 1 {
		t.Fatalf("requests = %d, want no retry", len(*bodies))
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

func TestChatComplete_RetriesWithoutTemperatureTheModelRejects(t *testing.T) {
	rejections := map[string]string{
		"unsupported value":     `400:{"error":{"type":"invalid_request_error","message":"Unsupported value: 'temperature' does not support 0.2 with this model. Only the default (1) value is supported."}}`,
		"unsupported parameter": `400:{"error":{"type":"invalid_request_error","message":"Unsupported parameter: 'temperature' is not supported with this model."}}`,
		"error in an array":     `400:[{"error":{"code":400,"message":"Invalid value at 'temperature'","status":"INVALID_ARGUMENT"}}]`,
	}
	for rejectionName, rejection := range rejections {
		for providerName := range chatProviders(t, "") {
			t.Run(providerName+"/"+rejectionName, func(t *testing.T) {
				url, bodies := recordingServer(t, rejection, chatOK)
				provider := chatProviders(t, url)[providerName]
				req := &Request{UserPrompt: "spec", Temperature: temperature(0.2), MaxTokens: 100}

				resp, err := provider.Complete(context.Background(), req)
				if err != nil {
					t.Fatalf("Complete: %v", err)
				}
				if !resp.TemperatureDropped {
					t.Error("TemperatureDropped = false after the API rejected the temperature")
				}
				if _, err := provider.Complete(context.Background(), req); err != nil {
					t.Fatalf("second Complete: %v", err)
				}
				if len(*bodies) != 3 {
					t.Fatalf("requests = %d, want rejected + retry, then one for the second call", len(*bodies))
				}
				for i, wantSent := range []bool{true, false, false} {
					if _, sent := (*bodies)[i]["temperature"]; sent != wantSent {
						t.Errorf("request %d temperature sent = %v, want %v", i, sent, wantSent)
					}
				}
			})
		}
	}
}

func TestChatComplete_TemperatureIsSentWhenAccepted(t *testing.T) {
	for name := range chatProviders(t, "") {
		t.Run(name, func(t *testing.T) {
			url, bodies := recordingServer(t, chatOK)
			resp, err := chatProviders(t, url)[name].Complete(context.Background(), &Request{UserPrompt: "spec", Temperature: temperature(0.2)})
			if err != nil {
				t.Fatalf("Complete: %v", err)
			}
			if (*bodies)[0]["temperature"] != 0.2 || resp.TemperatureDropped {
				t.Errorf("temperature = %v dropped = %v, want 0.2 sent", (*bodies)[0]["temperature"], resp.TemperatureDropped)
			}
		})
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

func TestChatComplete_RejectedTemperatureIsRememberedPerModel(t *testing.T) {
	url, bodies := recordingServer(t,
		`400:{"error":{"type":"invalid_request_error","message":"Unsupported parameter: 'temperature' is not supported with this model."}}`,
		chatOK,
	)
	original := OpenAIAPIURL()
	SetOpenAIAPIURL(url)
	t.Cleanup(func() { SetOpenAIAPIURL(original) })

	p := &openaiProvider{model: "gpt-6.1-sol", apiKey: "k"}
	if _, err := p.Complete(context.Background(), &Request{UserPrompt: "spec", Temperature: temperature(0.2)}); err != nil {
		t.Fatalf("Complete: %v", err)
	}
	// A different model on the same provider has not rejected anything yet.
	resp, err := p.Complete(context.Background(), &Request{UserPrompt: "spec", Temperature: temperature(0.2), Model: "gpt-4o"})
	if err != nil {
		t.Fatalf("Complete with another model: %v", err)
	}
	last := (*bodies)[len(*bodies)-1]
	if last["model"] != "gpt-4o" || last["temperature"] != 0.2 || resp.TemperatureDropped {
		t.Errorf("request for the other model = %v (dropped=%v), want its temperature sent", last, resp.TemperatureDropped)
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
