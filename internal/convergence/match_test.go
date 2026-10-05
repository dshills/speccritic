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

// Findings that quote the same text tie on the evidence fallback, so they
// match only while their fingerprints do. The tag an incremental range review
// adds must not break that.
func TestMatchFindingsSharedEvidenceAcrossIncrementalReview(t *testing.T) {
	prev := []TrackedFinding{
		testFinding("ISSUE-0001", "Authentication requirement contradicts itself", "Users must be authenticated."),
		testFinding("ISSUE-0002", "Scope of unauthenticated access is not stated", "Users must be authenticated."),
	}
	prev[0].Tags = []string{"incremental-review", "range:SEC-003"}
	prev[1].Tags = []string{"incremental-review", "range:SEC-003"}
	for name, tags := range map[string][]string{
		"full review": nil,
		"reused":      {"incremental-reused"},
	} {
		cur := []TrackedFinding{
			testFinding("ISSUE-0001", "Authentication requirement contradicts itself", "Users must be authenticated."),
			testFinding("ISSUE-0002", "Scope of unauthenticated access is not stated", "Users must be authenticated."),
		}
		cur[0].Tags = tags
		cur[1].Tags = tags
		matches := MatchFindings(prev, cur)
		if len(matches) != 2 {
			t.Fatalf("%s: matches = %#v, want both findings matched", name, matches)
		}
		for _, match := range matches {
			if match.Method != matchMethodFingerprint || match.Previous.ID != match.Current.ID {
				t.Errorf("%s: %s matched %s by %s, want the same finding by fingerprint", name, match.Current.ID, match.Previous.ID, match.Method)
			}
		}
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
