package convergence

import (
	"testing"

	"github.com/dshills/speccritic/internal/schema"
)

func TestTrackIssuesFingerprintIgnoresIDAndVolatileTags(t *testing.T) {
	a := schema.Issue{
		ID:       "ISSUE-0001",
		Severity: schema.SeverityCritical,
		Category: schema.CategoryAmbiguousBehavior,
		Title:    "Undefined behavior",
		Evidence: []schema.Evidence{{Quote: "The system is fast"}},
		Tags:     []string{"chunk:CHUNK-1", "range:RANGE-1", "domain"},
	}
	b := a
	b.ID = "ISSUE-0099"
	b.Tags = []string{"domain", "incremental-reused"}
	trackedA := ComputeFingerprints(TrackIssues([]schema.Issue{a}))[0]
	trackedB := ComputeFingerprints(TrackIssues([]schema.Issue{b}))[0]
	if trackedA.Fingerprint != trackedB.Fingerprint {
		t.Fatalf("fingerprints differ:\n%s\n%s", trackedA.Fingerprint, trackedB.Fingerprint)
	}
}

func TestTrackQuestionsFingerprint(t *testing.T) {
	q := schema.Question{
		ID:       "Q-0001",
		Severity: schema.SeverityWarn,
		Question: " What happens   on failure? ",
		Evidence: []schema.Evidence{{Quote: "Failure behavior TBD"}},
	}
	tracked := ComputeFingerprints(TrackQuestions([]schema.Question{q}))
	if len(tracked) != 1 {
		t.Fatalf("tracked len = %d", len(tracked))
	}
	if tracked[0].Kind != KindQuestion || tracked[0].Category != "QUESTION" || tracked[0].Fingerprint == "" {
		t.Fatalf("tracked question = %#v", tracked[0])
	}
}

func TestFingerprintNormalizesWhitespaceSeverityAndCategory(t *testing.T) {
	a := TrackedFinding{
		Kind:     KindIssue,
		Severity: schema.Severity("CRITICAL"),
		Category: "AMBIGUOUS_BEHAVIOR",
		Text:     "Undefined   behavior",
		Evidence: []schema.Evidence{{Quote: "Line   one"}},
	}
	b := TrackedFinding{
		Kind:     KindIssue,
		Severity: schema.Severity("critical"),
		Category: "ambiguous_behavior",
		Text:     " undefined behavior ",
		Evidence: []schema.Evidence{{Quote: " line one "}},
	}
	if Fingerprint(a) != Fingerprint(b) {
		t.Fatalf("fingerprints differ")
	}
}

func TestFingerprintIncludesSectionPathWhenProvided(t *testing.T) {
	base := TrackedFinding{
		Kind:     KindIssue,
		Severity: schema.SeverityWarn,
		Category: "AMBIGUOUS_BEHAVIOR",
		Text:     "Undefined behavior",
		Evidence: []schema.Evidence{{Quote: "TBD"}},
	}
	a := base
	a.SectionPath = []string{"A"}
	b := base
	b.SectionPath = []string{"B"}
	if Fingerprint(a) == Fingerprint(b) {
		t.Fatalf("section path did not affect fingerprint")
	}
}

func TestFingerprintAvoidsJoinedSectionPathCollision(t *testing.T) {
	base := TrackedFinding{
		Kind:     KindIssue,
		Severity: schema.SeverityWarn,
		Category: "AMBIGUOUS_BEHAVIOR",
		Text:     "Undefined behavior",
	}
	a := base
	a.SectionPath = []string{"A", "B > C"}
	b := base
	b.SectionPath = []string{"A > B", "C"}
	if Fingerprint(a) == Fingerprint(b) {
		t.Fatalf("section path collision")
	}
}

func TestFingerprintAvoidsJoinedTagCollision(t *testing.T) {
	base := TrackedFinding{
		Kind:     KindIssue,
		Severity: schema.SeverityWarn,
		Category: "AMBIGUOUS_BEHAVIOR",
		Text:     "Undefined behavior",
	}
	a := base
	a.Tags = []string{"a,b", "c"}
	b := base
	b.Tags = []string{"a", "b,c"}
	if Fingerprint(a) == Fingerprint(b) {
		t.Fatalf("tag collision")
	}
}

func TestFingerprintAvoidsSectionPathTagCollision(t *testing.T) {
	base := TrackedFinding{
		Kind:     KindIssue,
		Severity: schema.SeverityWarn,
		Category: "AMBIGUOUS_BEHAVIOR",
		Text:     "Undefined behavior",
	}
	a := base
	a.SectionPath = []string{"same"}
	b := base
	b.Tags = []string{"same"}
	if Fingerprint(a) == Fingerprint(b) {
		t.Fatalf("section path/tag collision")
	}
}

func TestFingerprintEvidenceOrderIndependent(t *testing.T) {
	base := TrackedFinding{
		Kind:     KindIssue,
		Severity: schema.SeverityWarn,
		Category: "AMBIGUOUS_BEHAVIOR",
		Text:     "Undefined behavior",
	}
	a := base
	a.Evidence = []schema.Evidence{{Quote: "Second"}, {Quote: "First"}}
	b := base
	b.Evidence = []schema.Evidence{{Quote: "First"}, {Quote: "Second"}}
	if Fingerprint(a) != Fingerprint(b) {
		t.Fatalf("evidence order changed fingerprint")
	}
}

func TestFingerprintDeduplicatesEvidenceQuotes(t *testing.T) {
	base := TrackedFinding{
		Kind:     KindIssue,
		Severity: schema.SeverityWarn,
		Category: "AMBIGUOUS_BEHAVIOR",
		Text:     "Undefined behavior",
	}
	a := base
	a.Evidence = []schema.Evidence{{Quote: "Same"}, {Quote: " same "}}
	b := base
	b.Evidence = []schema.Evidence{{Quote: "same"}}
	if Fingerprint(a) != Fingerprint(b) {
		t.Fatalf("duplicate evidence changed fingerprint")
	}
}

func TestComputeFingerprintsDeepCopiesSlices(t *testing.T) {
	input := []TrackedFinding{{
		Kind:        KindIssue,
		Severity:    schema.SeverityWarn,
		Category:    "AMBIGUOUS_BEHAVIOR",
		Text:        "Undefined behavior",
		SectionPath: []string{"A"},
		Evidence:    []schema.Evidence{{Quote: "TBD"}},
		Tags:        []string{"domain"},
	}}
	out := ComputeFingerprints(input)
	out[0].SectionPath[0] = "B"
	out[0].Evidence[0].Quote = "changed"
	out[0].Tags[0] = "changed"
	if input[0].SectionPath[0] != "A" || input[0].Evidence[0].Quote != "TBD" || input[0].Tags[0] != "domain" {
		t.Fatalf("input slices were mutated: %#v", input[0])
	}
}

func TestFingerprintStripsNulls(t *testing.T) {
	base := TrackedFinding{
		Kind:     KindIssue,
		Severity: schema.SeverityWarn,
		Category: "AMBIGUOUS_BEHAVIOR",
		Text:     "Undefined\x00 behavior",
	}
	clean := base
	clean.Text = "Undefined behavior"
	if Fingerprint(base) != Fingerprint(clean) {
		t.Fatalf("null byte affected fingerprint")
	}
}

func TestFingerprintStripsUnitSeparator(t *testing.T) {
	base := TrackedFinding{
		Kind:     KindIssue,
		Severity: schema.SeverityWarn,
		Category: "AMBIGUOUS_BEHAVIOR",
		Text:     "Undefined\x1f behavior",
	}
	clean := base
	clean.Text = "Undefined behavior"
	if Fingerprint(base) != Fingerprint(clean) {
		t.Fatalf("unit separator affected fingerprint")
	}
}

func TestFingerprintPreservesEvidenceBoundaries(t *testing.T) {
	base := TrackedFinding{
		Kind:     KindIssue,
		Severity: schema.SeverityWarn,
		Category: "AMBIGUOUS_BEHAVIOR",
		Text:     "Undefined behavior",
	}
	a := base
	a.Evidence = []schema.Evidence{{Quote: "a"}, {Quote: "b c"}}
	b := base
	b.Evidence = []schema.Evidence{{Quote: "a b"}, {Quote: "c"}}
	if Fingerprint(a) == Fingerprint(b) {
		t.Fatalf("evidence boundary collision")
	}
}
