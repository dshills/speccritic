package convergence

import (
	"testing"

	"github.com/dshills/speccritic/internal/schema"
)

func TestMatchFindingsExactFingerprint(t *testing.T) {
	prev := []TrackedFinding{testFinding("old", "Undefined behavior", "TBD")}
	cur := []TrackedFinding{testFinding("new", "Undefined behavior", "TBD")}
	matches := MatchFindings(prev, cur)
	if len(matches) != 1 || matches[0].Method != matchMethodFingerprint {
		t.Fatalf("matches = %#v", matches)
	}
}

func TestMatchFindingsStableIdentity(t *testing.T) {
	prev := []TrackedFinding{testFinding("old", "Old title", "old")}
	cur := []TrackedFinding{testFinding("new", "New title", "new")}
	prev[0].Tags = []string{"stable:abc"}
	cur[0].Tags = []string{"stable:abc"}
	matches := MatchFindings(prev, cur)
	if len(matches) != 1 || matches[0].Method != matchMethodStableID {
		t.Fatalf("matches = %#v", matches)
	}
}

func TestMatchFindingsStableIdentityWinsOverFingerprint(t *testing.T) {
	prev := []TrackedFinding{
		testFinding("stable", "Different title", "different"),
		testFinding("fingerprint", "Undefined behavior", "TBD"),
	}
	cur := []TrackedFinding{testFinding("new", "Undefined behavior", "TBD")}
	prev[0].Tags = []string{"stable:abc"}
	cur[0].Tags = []string{"stable:abc"}
	matches := MatchFindings(prev, cur)
	if len(matches) != 1 || matches[0].Previous.ID != "stable" {
		t.Fatalf("matches = %#v", matches)
	}
}

func TestMatchFindingsAllowsSeverityDrift(t *testing.T) {
	prev := []TrackedFinding{testFinding("old", "Undefined behavior", "TBD")}
	cur := []TrackedFinding{testFinding("new", "Undefined behavior", "TBD")}
	prev[0].Severity = schema.SeverityWarn
	cur[0].Severity = schema.SeverityCritical
	matches := MatchFindings(prev, cur)
	if len(matches) != 1 {
		t.Fatalf("matches = %#v", matches)
	}
}

func TestMatchFindingsRejectsAmbiguousCandidate(t *testing.T) {
	prev := []TrackedFinding{
		testFinding("a", "Undefined behavior", "TBD"),
		testFinding("b", "Undefined behavior", "TBD"),
	}
	cur := []TrackedFinding{testFinding("new", "Undefined behavior", "TBD")}
	matches := MatchFindings(prev, cur)
	if len(matches) != 0 {
		t.Fatalf("matches = %#v", matches)
	}
}

func TestMatchFindingsOneToOne(t *testing.T) {
	prev := []TrackedFinding{testFinding("old", "Undefined behavior", "TBD")}
	cur := []TrackedFinding{
		testFinding("new1", "Undefined behavior", "TBD"),
		testFinding("new2", "Undefined behavior", "TBD"),
	}
	matches := MatchFindings(prev, cur)
	if len(matches) != 0 {
		t.Fatalf("matches = %#v", matches)
	}
}

func TestMatchFindingsHighSimilarity(t *testing.T) {
	prev := []TrackedFinding{testFinding("old", "Undefined timeout behavior", "request timeout")}
	cur := []TrackedFinding{testFinding("new", "Undefined timeout behaviour", "request timeout")}
	matches := MatchFindings(prev, cur)
	if len(matches) != 1 {
		t.Fatalf("matches = %#v", matches)
	}
}

func TestMatchFindingsDifferentEvidenceIsNew(t *testing.T) {
	prev := []TrackedFinding{testFinding("old", "Undefined behavior", "timeout")}
	cur := []TrackedFinding{testFinding("new", "Undefined behavior", "authentication")}
	matches := MatchFindings(prev, cur)
	if len(matches) != 0 {
		t.Fatalf("matches = %#v", matches)
	}
}

func testFinding(id, text, quote string) TrackedFinding {
	return TrackedFinding{
		Kind:        KindIssue,
		ID:          id,
		Severity:    schema.SeverityWarn,
		Category:    string(schema.CategoryAmbiguousBehavior),
		Text:        text,
		Evidence:    []schema.Evidence{{Quote: quote, LineStart: 1, LineEnd: 1}},
		SourceIndex: 0,
	}
}
