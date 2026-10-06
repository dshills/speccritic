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

// A previous report written with completion suggestions carries the
// completion-suggested tag; the current findings never do when they are compared.
func TestTrackIssuesFingerprintIgnoresCompletionSuggestedTag(t *testing.T) {
	current := schema.Issue{
		ID:       "PREFLIGHT-STRUCTURE-001",
		Severity: schema.SeverityCritical,
		Category: schema.CategoryUnspecifiedConstraint,
		Title:    "Missing purpose or goals section",
		Evidence: []schema.Evidence{{Quote: "# Bad Specification"}},
		Tags:     []string{"missing-section", "preflight"},
	}
	previous := current
	previous.Tags = []string{"completion-suggested", "missing-section", "preflight"}
	trackedCurrent := ComputeFingerprints(TrackIssues([]schema.Issue{current}))[0]
	trackedPrevious := ComputeFingerprints(TrackIssues([]schema.Issue{previous}))[0]
	if trackedCurrent.Fingerprint != trackedPrevious.Fingerprint {
		t.Fatalf("fingerprints differ:\n%s\n%s", trackedCurrent.Fingerprint, trackedPrevious.Fingerprint)
	}
}

// A finding produced by an incremental range review carries incremental-review
// in that run's report only: a reuse swaps the tag for incremental-reused and a
// full review never sets it.
func TestTrackIssuesFingerprintIgnoresIncrementalReviewTag(t *testing.T) {
	reviewed := schema.Issue{
		ID:       "ISSUE-0001",
		Severity: schema.SeverityCritical,
		Category: schema.CategoryContradiction,
		Title:    "Authentication requirement contradicts itself",
		Evidence: []schema.Evidence{{Quote: "Users must be authenticated."}},
		Tags:     []string{"auth", "incremental-review", "range:SEC-003"},
	}
	want := ComputeFingerprints(TrackIssues([]schema.Issue{reviewed}))[0].Fingerprint
	for name, tags := range map[string][]string{
		"full review": {"auth"},
		"reused":      {"auth", "incremental-reused"},
		// A reused finding the range review produced again keeps both tags.
		"reused and reviewed": {"auth", "incremental-reused", "incremental-review", "range:SEC-003"},
	} {
		other := reviewed
		other.Tags = tags
		if got := ComputeFingerprints(TrackIssues([]schema.Issue{other}))[0].Fingerprint; got != want {
			t.Errorf("%s: fingerprint = %s, want %s", name, got, want)
		}
	}
}

// Evidence checks and the second look at CRITICAL findings tag what one run's
// model output was found to be. The same finding can be tagged differently in
// the next run, so the tags must not change its fingerprint.
func TestTrackIssuesFingerprintIgnoresCheckTags(t *testing.T) {
	plain := schema.Issue{
		ID:       "ISSUE-0001",
		Severity: schema.SeverityWarn,
		Category: schema.CategoryAmbiguousBehavior,
		Title:    "Retry limit missing",
		Evidence: []schema.Evidence{{Quote: "Retries are unlimited."}},
		Tags:     []string{"cross-section"},
	}
	want := ComputeFingerprints(TrackIssues([]schema.Issue{plain}))[0].Fingerprint
	for _, tag := range []string{"evidence-reanchored", "evidence-unverified", "severity-downgraded", "critical-confirmed", "critical-downgraded"} {
		tagged := plain
		tagged.Tags = []string{"cross-section", tag}
		if got := ComputeFingerprints(TrackIssues([]schema.Issue{tagged}))[0].Fingerprint; got != want {
			t.Errorf("tag %s changed the fingerprint", tag)
		}
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
