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
// that cannot, for the model in use, appends PromptFallback to the system
// prompt so the model is at least shown the shape.
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

// evidenceSchema is the shape of one evidence entry.
const evidenceSchema = `{
            "type": "object",
            "additionalProperties": false,
            "required": ["line_start", "line_end", "quote"],
            "properties": {
              "line_start": {"type": "integer", "description": "First line cited, taken from its L<number>: prefix."},
              "line_end": {"type": "integer", "description": "Last line cited. Equal to line_start for a single line."},
              "quote": {"type": "string", "description": "Text copied exactly from the cited lines."}
            }
          }`

// reviewSchemaTemplate is the schema of a review. Properties are listed in
// the order a careful reviewer works: say what is wrong and where before
// judging how severe it is. Its verbs are, in order: the extra required
// top-level names, the category enum, the evidence schema (twice) and the
// extra top-level properties.
const reviewSchemaTemplate = `{
  "type": "object",
  "additionalProperties": false,
  "required": ["issues", "questions", "patches"%s],
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
          "category": {"type": "string", "enum": [%s]},
          "title": {"type": "string", "description": "Short title naming the defect."},
          "description": {"type": "string", "description": "What is wrong."},
          "evidence": {
            "type": "array",
            "description": "Where the defect is. At least one entry.",
            "items": %s
          },
          "impact": {"type": "string", "description": "What goes wrong if this is not fixed."},
          "recommendation": {"type": "string", "description": "The smallest change to the specification that fixes it. Not an architecture."},
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
        "required": ["id", "question", "why_needed", "evidence", "blocks", "severity"],
        "properties": {
          "id": {"type": "string", "description": "Q-0001, Q-0002 and so on, in order."},
          "question": {"type": "string", "description": "One specific question."},
          "why_needed": {"type": "string", "description": "Why implementation cannot proceed without the answer."},
          "evidence": {
            "type": "array",
            "description": "The lines that raise the question.",
            "items": %s
          },
          "blocks": {"type": "array", "items": {"type": "string"}, "description": "Requirement identifiers from the specification that the question blocks. Empty if it names none."},
          "severity": {"type": "string", "enum": ["CRITICAL", "WARN", "INFO"]}
        }
      }
    },
    "patches": {
      "type": "array",
      "description": "Minimal corrections. Empty when none is safe to suggest.",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["issue_id", "before", "after"],
        "properties": {
          "issue_id": {"type": "string", "description": "The id of the issue this corrects."},
          "before": {"type": "string", "description": "Text copied exactly from the specification, long enough to occur only once."},
          "after": {"type": "string", "description": "The replacement text."}
        }
      }
    }%s
  }
}`

// chunkSummarySchema is the extra top-level property a chunk review returns.
const chunkSummarySchema = `,
    "meta": {
      "type": "object",
      "additionalProperties": false,
      "required": ["chunk_summary"],
      "properties": {
        "chunk_summary": {"type": "string", "description": "What the primary range covers, in at most 600 characters. No findings."}
      }
    }`

// schemaExample shows the shape of a review to a model whose output cannot be
// constrained.
const schemaExample = `{
  "issues": [
    {
      "id": "ISSUE-0001",
      "category": "NON_TESTABLE_REQUIREMENT",
      "title": "Short title describing the defect",
      "description": "Detailed explanation of the defect",
      "evidence": [{"line_start": 10, "line_end": 12, "quote": "exact text from spec"}],
      "impact": "What goes wrong if this is not fixed",
      "recommendation": "Minimal corrective action",
      "severity": "CRITICAL",
      "blocking": true,
      "tags": []
    }
  ],
  "questions": [
    {
      "id": "Q-0001",
      "question": "Specific question that must be answered before implementation",
      "why_needed": "Why this question blocks implementation",
      "evidence": [{"line_start": 10, "line_end": 12, "quote": "exact text"}],
      "blocks": ["REQ-001"],
      "severity": "CRITICAL"
    }
  ],
  "patches": [
    {
      "issue_id": "ISSUE-0001",
      "before": "exact text from spec to be replaced",
      "after": "corrected minimal replacement text"
    }
  ]%s
}`

var (
	reviewSchemaJSON      = buildReviewSchema(false)
	chunkReviewSchemaJSON = buildReviewSchema(true)
)

func buildReviewSchema(chunkSummary bool) json.RawMessage {
	categories := schema.Categories()
	quoted := make([]string, len(categories))
	for i, category := range categories {
		quoted[i] = fmt.Sprintf("%q", category)
	}
	required, properties := "", ""
	if chunkSummary {
		required, properties = `, "meta"`, chunkSummarySchema
	}
	pretty := fmt.Sprintf(reviewSchemaTemplate, required, strings.Join(quoted, ", "), evidenceSchema, evidenceSchema, properties)
	// The schema is sent with every request, so the indentation that makes
	// the template readable is stripped before use.
	var compact bytes.Buffer
	if err := json.Compact(&compact, []byte(pretty)); err != nil {
		panic(fmt.Sprintf("review schema template is not valid JSON: %v", err))
	}
	return compact.Bytes()
}

// ReviewSchema returns the schema of a review response. A chunk review also
// returns a summary of its range, so it has its own variant. enforce is passed
// through to OutputSchema.Enforce.
func ReviewSchema(chunkSummary, enforce bool) *OutputSchema {
	out := &OutputSchema{Name: "spec_review", JSON: reviewSchemaJSON, Enforce: enforce}
	exampleMeta := ""
	if chunkSummary {
		out.Name = "spec_chunk_review"
		out.JSON = chunkReviewSchemaJSON
		exampleMeta = `,
  "meta": {"chunk_summary": "What the primary range covers, in at most 600 characters"}`
	}
	out.PromptFallback = "\n\nReturn your findings as JSON with this structure:\n" + fmt.Sprintf(schemaExample, exampleMeta)
	return out
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
