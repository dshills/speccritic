package preflight

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/dshills/speccritic/internal/schema"
	"github.com/dshills/speccritic/internal/spec"
)

func TestGoldenGoodSpecProducesNoFindings(t *testing.T) {
	s := spec.New("SPEC.md", strings.Join([]string{
		"# Upload Service",
		"",
		"## Purpose",
		"",
		"The service accepts uploaded documents for validation.",
		"",
		"## Non-goals",
		"",
		"The service does not perform billing or user provisioning.",
		"",
		"## Requirements",
		"",
		"- The service SHALL accept files up to 10 MB.",
		"- The service SHALL return HTTP 400 when the file type is unsupported.",
		"- The service SHALL complete validation within 500 ms for files up to 10 MB.",
		"",
		"## Acceptance Criteria",
		"",
		"- A test uploading an 11 MB file receives HTTP 413.",
		"- A test uploading an unsupported file receives HTTP 400.",
	}, "\n"))

	result, err := Run(s, Config{Enabled: true, Mode: ModeOnly, Profile: "general"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(result.Issues) != 0 {
		t.Fatalf("issues = %#v, want none", result.Issues)
	}
}

func TestGoldenBadSpecProducesRepresentativeFindings(t *testing.T) {
	s := spec.New("SPEC.md", strings.Join([]string{
		"# Upload Service",
		"",
		"TODO define the retry behavior.",
		"The service should be fast and user-friendly.",
		"The API uses the QXR token.",
	}, "\n"))

	result, err := Run(s, Config{Enabled: true, Mode: ModeOnly, Profile: "general", Strict: true})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	requireAuditIssue(t, result.Issues, "PREFLIGHT-TODO-001", schema.SeverityCritical)
	requireAuditIssue(t, result.Issues, "PREFLIGHT-VAGUE-001", schema.SeverityWarn)
	requireAuditIssue(t, result.Issues, "PREFLIGHT-WEAK-001", schema.SeverityCritical)
	requireAuditIssue(t, result.Issues, "PREFLIGHT-ACRONYM-001", schema.SeverityWarn)
}

func TestProjectSpecsAudit(t *testing.T) {
	for _, path := range []string{
		filepath.Join("..", "..", "specs", "preflight", "SPEC.md"),
		filepath.Join("..", "..", "specs", "web", "SPEC.md"),
	} {
		s, err := spec.Load(path)
		if err != nil {
			t.Fatalf("load %s: %v", path, err)
		}
		result, err := Run(s, Config{Enabled: true, Mode: ModeOnly, Profile: "general"})
		if err != nil {
			t.Fatalf("run %s: %v", path, err)
		}
		if len(result.Issues) > 60 {
			t.Fatalf("%s produced %d findings, want <= 60", path, len(result.Issues))
		}
		t.Logf("%s produced %d preflight findings", path, len(result.Issues))
	}
}

func requireAuditIssue(t *testing.T, issues []schema.Issue, id string, severity schema.Severity) {
	t.Helper()
	for _, issue := range issues {
		if issue.ID == id && issue.Severity == severity {
			return
		}
	}
	t.Fatalf("missing %s/%s in %#v", id, severity, issues)
}
