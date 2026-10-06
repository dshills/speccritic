package llm

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/dshills/speccritic/internal/profile"
	"github.com/dshills/speccritic/internal/schema"
)

// schemaNode is the part of JSON Schema the review schema uses.
type schemaNode struct {
	Type                 string                 `json:"type"`
	Enum                 []string               `json:"enum"`
	Required             []string               `json:"required"`
	Properties           map[string]*schemaNode `json:"properties"`
	Items                *schemaNode            `json:"items"`
	AdditionalProperties *bool                  `json:"additionalProperties"`
}

// checkStrict walks the schema and applies the rules every provider's strict
// mode shares: an object lists all of its properties as required and allows no
// others. A schema that breaks them is rejected by the API on every call.
func checkStrict(t *testing.T, path string, n *schemaNode) {
	t.Helper()
	switch n.Type {
	case "object":
		if n.AdditionalProperties == nil || *n.AdditionalProperties {
			t.Errorf("%s: additionalProperties must be false", path)
		}
		names := make([]string, 0, len(n.Properties))
		for name, child := range n.Properties {
			names = append(names, name)
			checkStrict(t, path+"."+name, child)
		}
		slices.Sort(names)
		required := slices.Clone(n.Required)
		slices.Sort(required)
		if !slices.Equal(names, required) {
			t.Errorf("%s: required %v must list every property %v", path, required, names)
		}
	case "array":
		if n.Items == nil {
			t.Fatalf("%s: array has no items schema", path)
		}
		checkStrict(t, path+"[]", n.Items)
	case "string", "integer", "boolean":
	default:
		t.Errorf("%s: unexpected type %q", path, n.Type)
	}
}

func TestReviewSchema(t *testing.T) {
	out := ReviewSchema(true, true)
	if !out.Enforce || out.Name == "" {
		t.Fatalf("schema = %+v, want a named schema with Enforce set", out)
	}
	var root schemaNode
	if err := json.Unmarshal(out.JSON, &root); err != nil {
		t.Fatalf("schema is not valid JSON: %v\n%s", err, out.JSON)
	}
	checkStrict(t, "$", &root)

	top := slices.Clone(root.Required)
	slices.Sort(top)
	if want := []string{"issues", "patches", "questions"}; !slices.Equal(top, want) {
		t.Errorf("top-level fields = %v, want %v", top, want)
	}

	issue := root.Properties["issues"].Items
	var categories []string
	for _, category := range schema.Categories() {
		categories = append(categories, string(category))
	}
	if !slices.Equal(issue.Properties["category"].Enum, categories) {
		t.Errorf("category enum = %v, want the defined categories %v", issue.Properties["category"].Enum, categories)
	}
	severities := []string{"CRITICAL", "WARN", "INFO"}
	if !slices.Equal(issue.Properties["severity"].Enum, severities) || !slices.Equal(root.Properties["questions"].Items.Properties["severity"].Enum, severities) {
		t.Error("severity must be an enum of CRITICAL, WARN and INFO on issues and questions")
	}
	// The path is set locally, so the model is not asked for it.
	if _, ok := issue.Properties["evidence"].Items.Properties["path"]; ok {
		t.Error("evidence must not ask for a path")
	}

	// A reviewer says what is wrong and where before judging severity.
	raw := string(out.JSON)
	issues := raw[strings.Index(raw, `"issues":{`):strings.Index(raw, `"questions":{`)]
	evidenceAt, severityAt := strings.Index(issues, `"evidence":`), strings.Index(issues, `"severity":`)
	if evidenceAt < 0 || severityAt < 0 || evidenceAt > severityAt {
		t.Error("in an issue, evidence must come before severity")
	}
	if strings.Contains(raw, "\n") || strings.Contains(raw, "  ") {
		t.Error("the schema sent with every request should be compact")
	}

	if !strings.Contains(out.PromptFallback, `"issues"`) {
		t.Errorf("prompt fallback does not show the shape:\n%s", out.PromptFallback)
	}
	if !json.Valid([]byte(out.PromptFallback[strings.Index(out.PromptFallback, "{"):])) {
		t.Errorf("the example in the prompt fallback is not valid JSON:\n%s", out.PromptFallback)
	}
	if ReviewSchema(false, true).Enforce {
		t.Error("Enforce must follow the argument")
	}

	// Patches name the lines they replace; the tool copies the old text.
	patch := root.Properties["patches"].Items
	for _, field := range []string{"issue_id", "line_start", "line_end", "after"} {
		if patch.Properties[field] == nil {
			t.Errorf("patches must have %s", field)
		}
	}
	if patch.Properties["before"] != nil {
		t.Error("patches must not ask the model to copy the text they replace")
	}
	// Question ids are assigned locally and nothing in a response refers to
	// them, so the model is not asked for them.
	if root.Properties["questions"].Items.Properties["id"] != nil {
		t.Error("questions must not ask for an id")
	}
}

// With patches off the schema and its example leave the patches list out, so
// the model spends no output on them.
func TestReviewSchema_WithoutPatches(t *testing.T) {
	for name, out := range map[string]*OutputSchema{"review": ReviewSchema(true, false), "synthesis": SynthesisSchema(true, false)} {
		var root schemaNode
		if err := json.Unmarshal(out.JSON, &root); err != nil {
			t.Fatalf("%s: schema is not valid JSON: %v", name, err)
		}
		checkStrict(t, "$", &root)
		if root.Properties["patches"] != nil || slices.Contains(root.Required, "patches") {
			t.Errorf("%s: schema asks for patches", name)
		}
		example := out.PromptFallback[strings.Index(out.PromptFallback, "{"):]
		if !json.Valid([]byte(example)) || strings.Contains(example, `"patches"`) {
			t.Errorf("%s: example is not valid JSON without patches:\n%s", name, example)
		}
	}
}

func TestBuildSystemPrompt_LeavesTheOutputShapeToTheSchema(t *testing.T) {
	p, err := profile.Get("general")
	if err != nil {
		t.Fatalf("profile.Get: %v", err)
	}
	sys := BuildSystemPrompt(p, false)
	if strings.Contains(sys, `"issues"`) || strings.Contains(sys, "with this structure") {
		t.Errorf("system prompt still carries the output example:\n%s", sys)
	}
}

func TestSchemaRejected(t *testing.T) {
	cases := map[string]bool{
		`{"error":{"message":"output_config.format: Extra inputs are not permitted"}}`:                        true,
		`{"error":{"message":"Invalid parameter: 'response_format' of type 'json_schema' is not supported"}}`: true,
		`{"error":{"message":"Invalid schema for response_format 'spec_review'"}}`:                            true,
		`{"error":{"message":"max_tokens is too large"}}`:                                                     false,
		`{"error":{"message":"Unsupported parameter: 'max_tokens'"}}`:                                         false,
	}
	for body, want := range cases {
		if got := schemaRejected(body); got != want {
			t.Errorf("schemaRejected(%s) = %v, want %v", body, got, want)
		}
	}
}

func systemText(t *testing.T, body map[string]any) string {
	t.Helper()
	blocks, _ := body["system"].([]any)
	if len(blocks) == 0 {
		return ""
	}
	block, _ := blocks[0].(map[string]any)
	text, _ := block["text"].(string)
	return text
}

func TestAnthropicComplete_Schema(t *testing.T) {
	review := ReviewSchema(true, true)
	described := ReviewSchema(false, true)

	t.Run("enforced", func(t *testing.T) {
		url, bodies := recordingServer(t, anthropicOK)
		useAnthropicURL(t, url)
		resp, err := (&anthropicProvider{model: "claude-opus-5-5", apiKey: "k"}).Complete(context.Background(), &Request{SystemPrompt: "sys", UserPrompt: "spec", Schema: review, Effort: "high"})
		if err != nil {
			t.Fatalf("Complete: %v", err)
		}
		config, _ := (*bodies)[0]["output_config"].(map[string]any)
		format, _ := config["format"].(map[string]any)
		if format["type"] != "json_schema" || format["schema"] == nil || config["effort"] != "high" {
			t.Errorf("output_config = %v, want the schema alongside the effort", config)
		}
		if got := systemText(t, (*bodies)[0]); got != "sys" {
			t.Errorf("system prompt = %q, want it without the example", got)
		}
		if !resp.SchemaEnforced {
			t.Error("SchemaEnforced = false")
		}
	})

	t.Run("described only", func(t *testing.T) {
		url, bodies := recordingServer(t, anthropicOK)
		useAnthropicURL(t, url)
		resp, err := (&anthropicProvider{model: "claude-opus-5-5", apiKey: "k"}).Complete(context.Background(), &Request{SystemPrompt: "sys", UserPrompt: "spec", Schema: described})
		if err != nil {
			t.Fatalf("Complete: %v", err)
		}
		if _, ok := (*bodies)[0]["output_config"]; ok {
			t.Errorf("output_config sent although enforcement is off: %v", (*bodies)[0]["output_config"])
		}
		if got := systemText(t, (*bodies)[0]); got != "sys" {
			t.Errorf("system prompt = %q, want it unchanged", got)
		}
		if got := userText((*bodies)[0]); got != "spec"+described.PromptFallback {
			t.Errorf("user message = %q, want the example after the task", got)
		}
		if resp.SchemaEnforced {
			t.Error("SchemaEnforced = true")
		}
	})

	t.Run("rejected by the model", func(t *testing.T) {
		url, bodies := recordingServer(t,
			`400:{"type":"error","error":{"type":"invalid_request_error","message":"output_config.format is not supported on this model"}}`,
			anthropicOK,
		)
		useAnthropicURL(t, url)
		p := &anthropicProvider{model: "claude-old", apiKey: "k"}
		req := &Request{SystemPrompt: "sys", UserPrompt: "spec", Schema: review}
		resp, err := p.Complete(context.Background(), req)
		if err != nil {
			t.Fatalf("Complete: %v", err)
		}
		if resp.SchemaEnforced {
			t.Error("SchemaEnforced = true after the model rejected the schema")
		}
		if _, err := p.Complete(context.Background(), req); err != nil {
			t.Fatalf("second Complete: %v", err)
		}
		if len(*bodies) != 3 {
			t.Fatalf("requests = %d, want rejected + retry, then one for the second call", len(*bodies))
		}
		for i, wantSchema := range []bool{true, false, false} {
			_, sent := (*bodies)[i]["output_config"]
			if sent != wantSchema {
				t.Errorf("request %d sent a schema = %v, want %v", i, sent, wantSchema)
			}
			if hasExample := strings.Contains(userText((*bodies)[i]), "with this structure"); hasExample == wantSchema {
				t.Errorf("request %d has the example in its prompt = %v, want %v", i, hasExample, !wantSchema)
			}
		}
	})

	t.Run("no schema", func(t *testing.T) {
		url, bodies := recordingServer(t, anthropicOK)
		useAnthropicURL(t, url)
		if _, err := (&anthropicProvider{model: "claude-opus-5-5", apiKey: "k"}).Complete(context.Background(), &Request{SystemPrompt: "sys", UserPrompt: "spec"}); err != nil {
			t.Fatalf("Complete: %v", err)
		}
		if got := systemText(t, (*bodies)[0]); got != "sys" {
			t.Errorf("system prompt = %q, want it untouched", got)
		}
	})
}

// userText returns the user message of an Anthropic request body, joining
// its content blocks.
func userText(body map[string]any) string {
	messages, _ := body["messages"].([]any)
	if len(messages) == 0 {
		return ""
	}
	message, _ := messages[0].(map[string]any)
	switch content := message["content"].(type) {
	case string:
		return content
	case []any:
		var b strings.Builder
		for _, block := range content {
			text, _ := block.(map[string]any)["text"].(string)
			b.WriteString(text)
		}
		return b.String()
	}
	return ""
}

func chatUserText(body map[string]any) string {
	messages, _ := body["messages"].([]any)
	for _, m := range messages {
		message, _ := m.(map[string]any)
		if message["role"] == "user" {
			text, _ := message["content"].(string)
			return text
		}
	}
	return ""
}

func chatSystemText(body map[string]any) string {
	messages, _ := body["messages"].([]any)
	for _, m := range messages {
		message, _ := m.(map[string]any)
		if message["role"] == "system" {
			text, _ := message["content"].(string)
			return text
		}
	}
	return ""
}

func TestChatComplete_Schema(t *testing.T) {
	review := ReviewSchema(true, true)
	described := ReviewSchema(false, true)

	for name := range chatProviders(t, "") {
		t.Run(name+"/enforced", func(t *testing.T) {
			url, bodies := recordingServer(t, chatOK)
			resp, err := chatProviders(t, url)[name].Complete(context.Background(), &Request{SystemPrompt: "sys", UserPrompt: "spec", Schema: review})
			if err != nil {
				t.Fatalf("Complete: %v", err)
			}
			format, _ := (*bodies)[0]["response_format"].(map[string]any)
			jsonSchema, _ := format["json_schema"].(map[string]any)
			if format["type"] != "json_schema" || jsonSchema["name"] != "spec_review" || jsonSchema["strict"] != true || jsonSchema["schema"] == nil {
				t.Errorf("response_format = %v, want a strict, named json_schema", format)
			}
			if got := chatSystemText((*bodies)[0]); got != "sys" || !resp.SchemaEnforced {
				t.Errorf("system = %q enforced = %v, want the bare prompt and an enforced schema", got, resp.SchemaEnforced)
			}
		})

		t.Run(name+"/described only", func(t *testing.T) {
			url, bodies := recordingServer(t, chatOK)
			resp, err := chatProviders(t, url)[name].Complete(context.Background(), &Request{SystemPrompt: "sys", UserPrompt: "spec", Schema: described})
			if err != nil {
				t.Fatalf("Complete: %v", err)
			}
			format, _ := (*bodies)[0]["response_format"].(map[string]any)
			if format["type"] != "json_object" || format["json_schema"] != nil {
				t.Errorf("response_format = %v, want json_object", format)
			}
			if got := chatSystemText((*bodies)[0]); got != "sys" || resp.SchemaEnforced {
				t.Errorf("system = %q enforced = %v, want the system prompt unchanged and no enforcement", got, resp.SchemaEnforced)
			}
			if got := chatUserText((*bodies)[0]); got != "spec"+described.PromptFallback {
				t.Errorf("user message = %q, want the example after the task", got)
			}
		})

		t.Run(name+"/rejected by the model", func(t *testing.T) {
			url, bodies := recordingServer(t,
				`400:{"error":{"type":"invalid_request_error","message":"Invalid parameter: 'response_format' of type 'json_schema' is not supported with this model."}}`,
				chatOK,
			)
			provider := chatProviders(t, url)[name]
			req := &Request{SystemPrompt: "sys", UserPrompt: "spec", Schema: review}
			resp, err := provider.Complete(context.Background(), req)
			if err != nil {
				t.Fatalf("Complete: %v", err)
			}
			if resp.SchemaEnforced {
				t.Error("SchemaEnforced = true after the model rejected the schema")
			}
			if _, err := provider.Complete(context.Background(), req); err != nil {
				t.Fatalf("second Complete: %v", err)
			}
			if len(*bodies) != 3 {
				t.Fatalf("requests = %d, want rejected + retry, then one for the second call", len(*bodies))
			}
			for i, wantType := range []string{"json_schema", "json_object", "json_object"} {
				format, _ := (*bodies)[i]["response_format"].(map[string]any)
				if format["type"] != wantType {
					t.Errorf("request %d response_format type = %v, want %s", i, format["type"], wantType)
				}
				if hasExample := strings.Contains(chatUserText((*bodies)[i]), "with this structure"); hasExample != (wantType == "json_object") {
					t.Errorf("request %d has the example in its prompt = %v", i, hasExample)
				}
			}
		})
	}
}

// One call can need both adjustments: the other token-limit parameter and no
// schema. Each is made once.
func TestOpenAIComplete_AdjustsTokenParameterAndSchemaInOneCall(t *testing.T) {
	url, bodies := recordingServer(t,
		`400:{"error":{"type":"invalid_request_error","message":"Unsupported parameter: 'max_completion_tokens' is not supported with this model. Use 'max_tokens' instead."}}`,
		`400:{"error":{"type":"invalid_request_error","message":"Invalid parameter: 'response_format' of type 'json_schema' is not supported with this model."}}`,
		chatOK,
	)
	original := OpenAIAPIURL()
	SetOpenAIAPIURL(url)
	t.Cleanup(func() { SetOpenAIAPIURL(original) })

	resp, err := (&openaiProvider{model: "some-model", apiKey: "k"}).Complete(context.Background(), &Request{UserPrompt: "spec", MaxTokens: 100, Schema: ReviewSchema(true, true)})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if len(*bodies) != 3 || resp.SchemaEnforced {
		t.Fatalf("requests = %d enforced = %v, want 3 requests ending without enforcement", len(*bodies), resp.SchemaEnforced)
	}
	last := (*bodies)[2]
	format, _ := last["response_format"].(map[string]any)
	if last["max_tokens"] != float64(100) || last["max_completion_tokens"] != nil || format["type"] != "json_object" {
		t.Errorf("final request = %v, want max_tokens and json_object", last)
	}
}

func TestAnthropicComplete_CacheLifetime(t *testing.T) {
	for name, long := range map[string]bool{"default": false, "long": true} {
		t.Run(name, func(t *testing.T) {
			url, bodies := recordingServer(t, anthropicOK)
			useAnthropicURL(t, url)
			req := &Request{SystemPrompt: "sys", UserPromptCachedPrefix: "<spec>...</spec>", UserPrompt: "task", LongCache: long}
			if _, err := (&anthropicProvider{model: "claude-opus-5-5", apiKey: "k"}).Complete(context.Background(), req); err != nil {
				t.Fatalf("Complete: %v", err)
			}
			body := (*bodies)[0]
			system, _ := body["system"].([]any)
			messages, _ := body["messages"].([]any)
			content, _ := messages[0].(map[string]any)["content"].([]any)
			breakpoints := []any{system[0].(map[string]any)["cache_control"], content[0].(map[string]any)["cache_control"]}
			for i, bp := range breakpoints {
				control, _ := bp.(map[string]any)
				ttl, hasTTL := control["ttl"]
				if control["type"] != "ephemeral" || hasTTL != long || (long && ttl != "1h") {
					t.Errorf("breakpoint %d = %v, want ephemeral with ttl 1h only for the long cache", i, control)
				}
			}
			if _, cached := content[1].(map[string]any)["cache_control"]; cached {
				t.Error("the task must not be cached")
			}
		})
	}
}

func TestSynthesisSchema(t *testing.T) {
	out := SynthesisSchema(true, true)
	var root schemaNode
	if err := json.Unmarshal(out.JSON, &root); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	checkStrict(t, "$", &root)
	top := slices.Clone(root.Required)
	slices.Sort(top)
	if want := []string{"issues", "merge", "patches", "questions", "retract"}; !slices.Equal(top, want) {
		t.Errorf("top-level fields = %v, want %v", top, want)
	}
	if root.Properties["merge"].Items.Properties["issue_ids"].Type != "array" {
		t.Error("merge groups must list issue ids")
	}
	for _, field := range []string{"id", "line_start", "line_end", "quote", "reason"} {
		if root.Properties["retract"].Items.Properties[field] == nil {
			t.Errorf("retract entries must have %s", field)
		}
	}
	example := out.PromptFallback[strings.Index(out.PromptFallback, "{"):]
	if !json.Valid([]byte(example)) || !strings.Contains(example, `"retract"`) {
		t.Errorf("the example is not valid JSON with the synthesis fields:\n%s", example)
	}
	if strings.Contains(ReviewSchema(true, true).PromptFallback, `"retract"`) || strings.Contains(string(ReviewSchema(true, true).JSON), `"merge"`) {
		t.Error("the plain review schema must not carry the synthesis fields")
	}
}
