package completion

import (
	"testing"

	"github.com/dshills/speccritic/internal/schema"
)

func TestGetTemplateSupportedProfiles(t *testing.T) {
	for _, name := range []string{
		schema.CompletionTemplateGeneral,
		schema.CompletionTemplateBackendAPI,
		schema.CompletionTemplateRegulatedSystem,
		schema.CompletionTemplateEventDriven,
	} {
		t.Run(name, func(t *testing.T) {
			tmpl, err := GetTemplate(name, "general")
			if err != nil {
				t.Fatalf("GetTemplate: %v", err)
			}
			if tmpl.Name != name {
				t.Fatalf("template name = %q, want %q", tmpl.Name, name)
			}
			if len(tmpl.Sections) == 0 {
				t.Fatal("template has no sections")
			}
			assertStrictlyIncreasingOrders(t, tmpl)
			assertNoDuplicateHeadings(t, tmpl)
		})
	}
}

func TestGetTemplateProfileResolution(t *testing.T) {
	tmpl, err := GetTemplate(schema.CompletionTemplateProfile, schema.CompletionTemplateBackendAPI)
	if err != nil {
		t.Fatalf("GetTemplate profile: %v", err)
	}
	if tmpl.Name != schema.CompletionTemplateBackendAPI {
		t.Fatalf("template name = %q", tmpl.Name)
	}
}

func TestGetTemplateDefaultProfileResolution(t *testing.T) {
	tmpl, err := GetTemplate("", "")
	if err != nil {
		t.Fatalf("GetTemplate default: %v", err)
	}
	if tmpl.Name != schema.CompletionTemplateGeneral {
		t.Fatalf("template name = %q", tmpl.Name)
	}
}

func TestGetTemplateUnknown(t *testing.T) {
	if _, err := GetTemplate("custom", "general"); err == nil {
		t.Fatal("expected unknown template error")
	}
}

func TestTemplateCanonicalHeadings(t *testing.T) {
	tests := []struct {
		name     string
		headings []string
	}{
		{schema.CompletionTemplateGeneral, []string{"Purpose", "Non-Goals", "Functional Requirements", "Acceptance Criteria", "Failure Modes", "Open Decisions"}},
		{schema.CompletionTemplateBackendAPI, []string{"Endpoints", "Authentication and Authorization", "Request and Response Schemas", "Error Responses", "Rate Limits and Abuse Handling", "Idempotency and Repeat Submission Behavior", "Observability", "Acceptance Tests"}},
		{schema.CompletionTemplateRegulatedSystem, []string{"Compliance Scope", "Data Classification", "Access Control", "Audit Trail", "Data Lifecycle and Deletion", "Approval and Review Workflow", "Incident and Exception Handling", "Validation Evidence"}},
		{schema.CompletionTemplateEventDriven, []string{"Event Producers and Consumers", "Event Schema", "Delivery Guarantees", "Ordering and Idempotency", "Retry and Failed-Event Queue Behavior", "Consumer Failure Behavior", "Backfill and Replay", "Observability"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tmpl, err := GetTemplate(tc.name, "general")
			if err != nil {
				t.Fatalf("GetTemplate: %v", err)
			}
			if len(tmpl.Sections) != len(tc.headings) {
				t.Fatalf("section count = %d, want %d", len(tmpl.Sections), len(tc.headings))
			}
			for i, want := range tc.headings {
				if tmpl.Sections[i].Heading != want {
					t.Fatalf("heading[%d] = %q, want %q", i, tmpl.Sections[i].Heading, want)
				}
			}
		})
	}
}

func TestTemplateClone(t *testing.T) {
	first, err := GetTemplate(schema.CompletionTemplateGeneral, "general")
	if err != nil {
		t.Fatalf("GetTemplate first: %v", err)
	}
	first.Sections[0].Heading = "Changed"

	second, err := GetTemplate(schema.CompletionTemplateGeneral, "general")
	if err != nil {
		t.Fatalf("GetTemplate second: %v", err)
	}
	if second.Sections[0].Heading == "Changed" {
		t.Fatal("template was not cloned")
	}
}

func assertStrictlyIncreasingOrders(t *testing.T, tmpl *Template) {
	t.Helper()
	last := 0
	for _, section := range tmpl.Sections {
		if section.Order <= last {
			t.Fatalf("%s order %d after %d", section.Heading, section.Order, last)
		}
		last = section.Order
	}
}

func assertNoDuplicateHeadings(t *testing.T, tmpl *Template) {
	t.Helper()
	seen := map[string]bool{}
	for _, section := range tmpl.Sections {
		if seen[section.Heading] {
			t.Fatalf("duplicate heading %q", section.Heading)
		}
		seen[section.Heading] = true
	}
}
