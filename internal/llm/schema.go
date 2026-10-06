package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/dshills/speccritic/internal/schema"
)

// OutputSchema describes the JSON a response must conform to.
//
// A provider that can constrain a model's output enforces JSON, which rules
// out malformed documents, unknown enum values and missing fields. A provider
// that cannot, for the model in use, appends PromptFallback to the user
// message so the model is at least shown the shape.
type OutputSchema struct {
	// Name identifies the schema to providers that ask for a name.
	Name string
	// JSON is the JSON Schema document. It uses only what every supported
	// provider's strict mode accepts: types, enums, required lists and
	// additionalProperties set to false. Line bounds cannot be expressed that
	// way and are checked locally.
	JSON json.RawMessage
	// Enforce asks the provider to constrain the response to JSON. When false
	// the provider only appends PromptFallback.
	Enforce bool
	// PromptFallback teaches the same shape by example.
	PromptFallback string
}

// evidenceSchema is the shape of one evidence entry. The quote is a short
// anchor rather than the passage: SpecCritic finds it in the spec and fills
// in the full text of the cited lines itself, so the model does not spend
// output tokens copying them.
const evidenceSchema = `{
            "type": "object",
            "additionalProperties": false,
            "required": ["line_start", "line_end", "quote"],
            "properties": {
              "line_start": {"type": "integer", "description": "First line cited, taken from its L<number>: prefix."},
              "line_end": {"type": "integer", "description": "Last line cited. Equal to line_start for a single line."},
              "quote": {"type": "string", "description": "A short phrase, about 4 to 12 words, copied exactly from the cited lines. Not the whole passage: the full text is filled in from the spec."}
            }
          }`

// patchesProperty is the schema of the patches list. A patch names the lines
// it replaces instead of copying them: SpecCritic takes the text it replaces
// from the spec.
const patchesProperty = `,
    "patches": {
      "type": "array",
      "description": "Minimal corrections. Empty when none is safe to suggest.",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["issue_id", "line_start", "line_end", "after"],
        "properties": {
          "issue_id": {"type": "string", "description": "The id of the issue this corrects."},
          "line_start": {"type": "integer", "description": "First line replaced."},
          "line_end": {"type": "integer", "description": "Last line replaced. Keep the range to the lines that change."},
          "after": {"type": "string", "description": "The full new text of those lines, as they should read."}
        }
      }
    }`

// reviewSchemaTemplate is the schema of a review. Properties are listed in
// the order a careful reviewer works: say what is wrong and where before
// judging how severe it is. Prose fields ask for a sentence or two: output
// tokens cost several times input tokens and set the latency of every call.
//
// Placeholders: {{required}} extra required top-level names, {{categories}}
// the category enum, {{evidence}} the evidence schema and {{properties}}
// extra top-level properties.
const reviewSchemaTemplate = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["issues", "questions"{{required}}],
  "properties": {
    "issues": {
      "type": "array",
      "description": "Defects found in the specification. Empty when there are none.",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["id", "category", "title", "description", "evidence", "impact", "recommendation", "severity", "blocking", "tags"],
        "properties": {
          "id": {"type": "string", "description": "ISSUE-0001, ISSUE-0002 and so on, in order."},
          "category": {"type": "string", "enum": [{{categories}}]},
          "title": {"type": "string", "description": "Names the defect in under ten words."},
          "description": {"type": "string", "description": "What is wrong, in one or two sentences."},
          "evidence": {
            "type": "array",
            "description": "Where the defect is. At least one entry.",
            "items": {{evidence}}
          },
          "impact": {"type": "string", "description": "What goes wrong if this is not fixed, in one sentence."},
          "recommendation": {"type": "string", "description": "The smallest change to the specification that fixes it, in one or two sentences. Not an architecture."},
          "severity": {"type": "string", "enum": ["CRITICAL", "WARN", "INFO"]},
          "blocking": {"type": "boolean", "description": "true when implementation cannot begin until this is resolved."},
          "tags": {"type": "array", "items": {"type": "string"}, "description": "Optional labels. Usually empty."}
        }
      }
    },
    "questions": {
      "type": "array",
      "description": "Questions the author must answer before implementation. Empty when there are none.",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["question", "why_needed", "evidence", "blocks", "severity"],
        "properties": {
          "question": {"type": "string", "description": "One specific question."},
          "why_needed": {"type": "string", "description": "Why implementation cannot proceed without the answer, in one sentence."},
          "evidence": {
            "type": "array",
            "description": "The lines that raise the question.",
            "items": {{evidence}}
          },
          "blocks": {"type": "array", "items": {"type": "string"}, "description": "Requirement identifiers from the specification that the question blocks. Empty if it names none."},
          "severity": {"type": "string", "enum": ["CRITICAL", "WARN", "INFO"]}
        }
      }
    }{{properties}}
  }
}`

// schemaExample shows the shape of a review to a model whose output cannot be
// constrained. {{patches}} and {{extra}} take the patches list and any extra
// fields.
const schemaExample = `{
  "issues": [
    {
      "id": "ISSUE-0001",
      "category": "NON_TESTABLE_REQUIREMENT",
      "title": "Response time has no number",
      "description": "One or two sentences on what is wrong.",
      "evidence": [{"line_start": 10, "line_end": 12, "quote": "short exact phrase from those lines"}],
      "impact": "One sentence on what goes wrong if this is not fixed.",
      "recommendation": "The smallest change to the spec that fixes it.",
      "severity": "CRITICAL",
      "blocking": true,
      "tags": []
    }
  ],
  "questions": [
    {
      "question": "Specific question that must be answered before implementation",
      "why_needed": "One sentence on why this blocks implementation",
      "evidence": [{"line_start": 10, "line_end": 12, "quote": "short exact phrase"}],
      "blocks": ["REQ-001"],
      "severity": "CRITICAL"
    }
  ]{{patches}}{{extra}}
}`

// patchesExample adds the patches list to the prompt example.
const patchesExample = `,
  "patches": [
    {"issue_id": "ISSUE-0001", "line_start": 11, "line_end": 11, "after": "full new text of line 11"}
  ]`

// synthesisProperties are the fields the cross-section pass returns besides
// new findings: which chunk findings to fold together and which the spec
// answers.
const synthesisProperties = `,
    "merge": {
      "type": "array",
      "description": "Groups of findings listed in the task that report the same defect. Empty when there are none.",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["issue_ids", "reason"],
        "properties": {
          "issue_ids": {"type": "array", "items": {"type": "string"}, "description": "Two or more ISSUE ids from the task."},
          "reason": {"type": "string", "description": "Why these are one defect, in one sentence."}
        }
      }
    },
    "retract": {
      "type": "array",
      "description": "Findings listed in the task that the specification already answers. Empty when there are none.",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["id", "line_start", "line_end", "quote", "reason"],
        "properties": {
          "id": {"type": "string", "description": "The ISSUE or Q id from the task."},
          "line_start": {"type": "integer", "description": "First line of the text that answers it."},
          "line_end": {"type": "integer", "description": "Last line of the text that answers it."},
          "quote": {"type": "string", "description": "The answering text, copied exactly from those lines."},
          "reason": {"type": "string", "description": "How that text answers the finding, in one sentence."}
        }
      }
    }`

// synthesisExample adds the synthesis fields to the prompt example.
const synthesisExample = `,
  "merge": [{"issue_ids": ["ISSUE-0002", "ISSUE-0007"], "reason": "Both report the undefined retry limit"}],
  "retract": [{"id": "ISSUE-0004", "line_start": 31, "line_end": 31, "quote": "exact answering text from spec", "reason": "Line 31 defines the term"}]`

// schemaKind selects one of the precomputed schemas.
type schemaKind struct{ synthesis, patches bool }

// builtSchema is a schema document and its prompt example.
type builtSchema struct {
	json    json.RawMessage
	example string
}

var builtSchemas = func() map[schemaKind]builtSchema {
	out := map[schemaKind]builtSchema{}
	for _, synthesis := range []bool{false, true} {
		for _, patches := range []bool{false, true} {
			out[schemaKind{synthesis, patches}] = buildReviewSchema(synthesis, patches)
		}
	}
	return out
}()

func buildReviewSchema(synthesis, patches bool) builtSchema {
	categories := schema.Categories()
	quoted := make([]string, len(categories))
	for i, category := range categories {
		quoted[i] = fmt.Sprintf("%q", category)
	}
	var required, properties, examplePatches, exampleExtra string
	if patches {
		required += `, "patches"`
		properties += patchesProperty
		examplePatches = patchesExample
	}
	if synthesis {
		required += `, "merge", "retract"`
		properties += synthesisProperties
		exampleExtra = synthesisExample
	}
	pretty := strings.NewReplacer(
		"{{required}}", required,
		"{{categories}}", strings.Join(quoted, ", "),
		"{{evidence}}", evidenceSchema,
		"{{properties}}", properties,
	).Replace(reviewSchemaTemplate)
	example := strings.NewReplacer("{{patches}}", examplePatches, "{{extra}}", exampleExtra).Replace(schemaExample)
	return builtSchema{json: compactJSON(pretty), example: example}
}

// compactJSON strips the indentation that makes a schema template readable;
// the schema is sent with every request.
func compactJSON(pretty string) json.RawMessage {
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(pretty)); err != nil {
		panic(fmt.Sprintf("schema template is not valid JSON: %v", err))
	}
	return compact.Bytes()
}

// ReviewSchema returns the schema of a review response. enforce is passed
// through to OutputSchema.Enforce. patches says whether the review asks for
// patches; leaving them out saves the output tokens they cost.
func ReviewSchema(enforce, patches bool) *OutputSchema {
	built := builtSchemas[schemaKind{patches: patches}]
	return &OutputSchema{
		Name:           "spec_review",
		JSON:           built.json,
		Enforce:        enforce,
		PromptFallback: "\n\nReturn your findings as JSON with this structure:\n" + built.example,
	}
}

// SynthesisSchema returns the schema of a cross-section synthesis response: a
// review plus the merge and retract lists.
func SynthesisSchema(enforce, patches bool) *OutputSchema {
	built := builtSchemas[schemaKind{synthesis: true, patches: patches}]
	return &OutputSchema{
		Name:           "spec_synthesis",
		JSON:           built.json,
		Enforce:        enforce,
		PromptFallback: "\n\nReturn your findings as JSON with this structure:\n" + built.example,
	}
}

// schemaRejected reports whether an error body says the API will not take the
// structured-output setting it was sent.
func schemaRejected(body string) bool {
	lower := strings.ToLower(body)
	for _, marker := range []string{"output_config", "response_format", "json_schema", "schema"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// modelSet is a set of model names that is safe for concurrent use. Its zero
// value is empty and ready to use.
type modelSet struct{ models sync.Map }

func (s *modelSet) add(model string) { s.models.Store(model, struct{}{}) }

func (s *modelSet) has(model string) bool {
	_, ok := s.models.Load(model)
	return ok
}
