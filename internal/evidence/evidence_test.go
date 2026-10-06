package evidence

import (
	"slices"
	"strings"
	"testing"

	"github.com/dshills/speccritic/internal/schema"
)

// specText is a small spec. Line 3 and line 12 share a sentence; lines 6 and 7
// hold text that is unique.
var specText = strings.Join([]string{
	"# Payment Service", // 1
	"",                  // 2
	"The service MUST respond quickly to every request.", // 3
	"",                // 4
	"## Requirements", // 5
	"- **`pay-1`**: A charge MUST be   idempotent when the client sends the same `Idempotency-Key`.", // 6
	"- **`pay-2`**: Refunds are “best effort” — the gateway decides.",                                // 7
	"- **`pay-3`**: Amounts are integers in minor units.",                                            // 8
	"",          // 9
	"## Limits", // 10
	"",          // 11
	"The service MUST respond quickly to every request.", // 12
	"Timeouts are TBD.",                  // 13
	"",                                   // 14
	"## Notes",                           // 15
	"See the gateway guide for details.", // 16
	"",                                   // 17
	"## Appendix",                        // 18
	"Currency codes follow ISO 4217.",    // 19
	"Rounding is half to even.",          // 20
	"Ledger entries are append-only.",    // 21
	"Settlement runs once per day.",      // 22
	"Disputes are out of scope.",         // 23
	"Receipts are sent by email.",        // 24
}, "\n")

func anchor(t *testing.T, start, end int, quote string) (schema.Evidence, Outcome) {
	t.Helper()
	return NewIndex(specText).Anchor(schema.Evidence{Path: "SPEC.md", LineStart: start, LineEnd: end, Quote: quote})
}

func TestAnchor(t *testing.T) {
	cases := map[string]struct {
		start, end int
		quote      string
		want       Outcome
		wantStart  int
		wantEnd    int
		wantQuote  string
	}{
		"exact quote at the cited line": {
			start: 8, end: 8, quote: "Amounts are integers in minor units.",
			want: Verified, wantStart: 8, wantEnd: 8, wantQuote: "Amounts are integers in minor units.",
		},
		"case, spacing and Markdown marks differ": {
			start: 6, end: 6, quote: "pay-1: a charge must be idempotent when the client sends the same Idempotency-Key",
			want: Verified, wantStart: 6, wantEnd: 6,
			wantQuote: "pay-1`**: A charge MUST be   idempotent when the client sends the same `Idempotency-Key",
		},
		"plain quotes and dash for typographic ones": {
			start: 7, end: 7, quote: `Refunds are "best effort" - the gateway decides.`,
			want: Verified, wantStart: 7, wantEnd: 7, wantQuote: "Refunds are “best effort” — the gateway decides.",
		},
		"line labels copied with the quote": {
			start: 7, end: 8, quote: "L7: - **`pay-2`**: Refunds are “best effort” — the gateway decides.\nL8: - **`pay-3`**: Amounts are integers in minor units.",
			want: Verified, wantStart: 7, wantEnd: 8,
			wantQuote: "- **`pay-2`**: Refunds are “best effort” — the gateway decides.\n- **`pay-3`**: Amounts are integers in minor units.",
		},
		"ellipsis stands for omitted text": {
			start: 6, end: 6, quote: "A charge MUST be idempotent ... the same Idempotency-Key",
			want: Verified, wantStart: 6, wantEnd: 6, wantQuote: "A charge MUST be   idempotent when the client sends the same `Idempotency-Key",
		},
		"cited range wider than the quote is kept": {
			start: 5, end: 8, quote: "Amounts are integers in minor units.",
			want: Verified, wantStart: 5, wantEnd: 8, wantQuote: "Amounts are integers in minor units.",
		},
		"quote runs past the cited range, which widens": {
			start: 7, end: 7, quote: "the gateway decides. - pay-3: Amounts are integers",
			want: Verified, wantStart: 7, wantEnd: 8, wantQuote: "the gateway decides.\n- **`pay-3`**: Amounts are integers",
		},
		"unique quote cited on the wrong line moves": {
			start: 13, end: 13, quote: "Amounts are integers in minor units.",
			want: Moved, wantStart: 8, wantEnd: 8, wantQuote: "Amounts are integers in minor units.",
		},
		"repeated quote moves to the nearby occurrence": {
			start: 10, end: 10, quote: "The service MUST respond quickly to every request.",
			want: Moved, wantStart: 12, wantEnd: 12, wantQuote: "The service MUST respond quickly to every request.",
		},
		"repeated quote with no occurrence nearby is unverified": {
			start: 24, end: 24, quote: "The service MUST respond quickly to every request.",
			want: Unverified, wantStart: 24, wantEnd: 24, wantQuote: "The service MUST respond quickly to every request.",
		},
		"quote that is not in the spec is unverified": {
			start: 6, end: 6, quote: "Charges are retried three times before failing.",
			want: Unverified, wantStart: 6, wantEnd: 6, wantQuote: "Charges are retried three times before failing.",
		},
		"short quote at the cited line is verified": {
			start: 13, end: 13, quote: "TBD",
			want: Verified, wantStart: 13, wantEnd: 13, wantQuote: "TBD",
		},
		"short unique quote on the wrong line moves": {
			start: 3, end: 3, quote: "tbd",
			want: Moved, wantStart: 13, wantEnd: 13, wantQuote: "TBD",
		},
		"short quote found in several other places is left alone": {
			start: 15, end: 15, quote: "gateway",
			want: NoQuote, wantStart: 15, wantEnd: 15, wantQuote: "gateway",
		},
		"short placeholder that is nowhere is left alone": {
			start: 5, end: 5, quote: "N/A",
			want: NoQuote, wantStart: 5, wantEnd: 5, wantQuote: "N/A",
		},
		"no quote takes the text of a short range": {
			start: 12, end: 13, quote: "",
			want: NoQuote, wantStart: 12, wantEnd: 13, wantQuote: "The service MUST respond quickly to every request.\nTimeouts are TBD.",
		},
		"punctuation alone is no quote": {
			start: 13, end: 13, quote: " ... ",
			want: NoQuote, wantStart: 13, wantEnd: 13, wantQuote: "Timeouts are TBD.",
		},
		"no quote over a long range stays empty": {
			start: 1, end: 10, quote: "",
			want: NoQuote, wantStart: 1, wantEnd: 10, wantQuote: "",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, outcome := anchor(t, tc.start, tc.end, tc.quote)
			if outcome != tc.want {
				t.Errorf("outcome = %d, want %d", outcome, tc.want)
			}
			if got.LineStart != tc.wantStart || got.LineEnd != tc.wantEnd {
				t.Errorf("lines = %d-%d, want %d-%d", got.LineStart, got.LineEnd, tc.wantStart, tc.wantEnd)
			}
			if got.Quote != tc.wantQuote {
				t.Errorf("quote = %q, want %q", got.Quote, tc.wantQuote)
			}
			if got.Path != "SPEC.md" {
				t.Errorf("path = %q, want it untouched", got.Path)
			}
		})
	}
}

func TestAnchor_LinesOutsideTheSpecAreLeftAlone(t *testing.T) {
	got, outcome := anchor(t, 40, 41, "")
	if outcome != NoQuote || got.Quote != "" || got.LineStart != 40 {
		t.Errorf("got %+v outcome %d, want the entry untouched", got, outcome)
	}
	if _, outcome := NewIndex("").Anchor(schema.Evidence{LineStart: 1, LineEnd: 1, Quote: "a quote long enough to check"}); outcome != Unverified {
		t.Errorf("outcome against an empty spec = %d, want unverified", outcome)
	}
}

// A phrase that fills the spec must still be found at the line that cites it,
// however many times it appears before that line.
func TestAnchor_FindsTheCitedOccurrenceAmongMany(t *testing.T) {
	lines := make([]string, 300)
	for i := range lines {
		lines[i] = "The request is logged before it is handled."
	}
	ix := NewIndex(strings.Join(lines, "\n"))
	got, outcome := ix.Anchor(schema.Evidence{LineStart: 250, LineEnd: 250, Quote: "The request is logged before it is handled."})
	if outcome != Verified || got.LineStart != 250 || got.LineEnd != 250 {
		t.Fatalf("got L%d-%d outcome %d, want it verified at line 250", got.LineStart, got.LineEnd, outcome)
	}
}

// After "start", the first "middle" is a dead end: no "finish" follows it
// before the text runs out of reach. The second "middle" works.
func TestAnchor_EllipsisTriesLaterMatchesOfAMiddleSegment(t *testing.T) {
	text := "start of the clause, middle part one. " + strings.Repeat("filler ", 400) + "\nstart of the clause, then padding, middle part one, middle part two and the finish line."
	ix := NewIndex(text)
	got, outcome := ix.Anchor(schema.Evidence{LineStart: 2, LineEnd: 2, Quote: "start of the clause ... middle part ... the finish line"})
	if outcome != Verified || got.LineStart != 2 || !strings.HasSuffix(got.Quote, "the finish line") {
		t.Fatalf("got L%d-%d %q outcome %d, want the quote verified on line 2", got.LineStart, got.LineEnd, got.Quote, outcome)
	}

	// Within one line: the first "beta" is followed by no "gamma delta", the
	// second is.
	ix = NewIndex("alpha one beta gamma zeta, then beta gamma delta at last")
	got, outcome = ix.Anchor(schema.Evidence{LineStart: 1, LineEnd: 1, Quote: "alpha one ... beta ... gamma delta at last"})
	if outcome != Verified || got.Quote != "alpha one beta gamma zeta, then beta gamma delta at last" {
		t.Fatalf("got %q outcome %d, want the whole line matched", got.Quote, outcome)
	}
}

func TestAnchor_BadLineRangesDoNotPanic(t *testing.T) {
	ix := NewIndex(specText)
	for _, ev := range []schema.Evidence{
		{LineStart: 5, LineEnd: 3},
		{LineStart: 0, LineEnd: 0},
		{LineStart: -2, LineEnd: -1},
		{LineStart: 3, LineEnd: 0},
		{LineStart: 5, LineEnd: 3, Quote: "Amounts are integers in minor units."},
	} {
		got, _ := ix.Anchor(ev)
		if ev.Quote == "" && got.Quote != "" {
			t.Errorf("evidence %+v was given the quote %q from an invalid range", ev, got.Quote)
		}
	}
}

func TestCheckIssue(t *testing.T) {
	const real, fake = "Amounts are integers in minor units.", "Charges are retried three times before failing."
	ev := func(line int, quote string) schema.Evidence {
		return schema.Evidence{LineStart: line, LineEnd: line, Quote: quote}
	}
	cases := map[string]struct {
		severity     schema.Severity
		evidence     []schema.Evidence
		tags         []string
		wantSeverity schema.Severity
		wantBlocking bool
		wantTags     []string
	}{
		"verified": {
			severity: schema.SeverityCritical, evidence: []schema.Evidence{ev(8, real)},
			wantSeverity: schema.SeverityCritical, wantBlocking: true,
		},
		"moved": {
			severity: schema.SeverityCritical, evidence: []schema.Evidence{ev(2, real)},
			wantSeverity: schema.SeverityCritical, wantBlocking: true, wantTags: []string{TagReanchored},
		},
		"critical with no quote in the spec is lowered": {
			severity: schema.SeverityCritical, evidence: []schema.Evidence{ev(6, fake)},
			wantSeverity: schema.SeverityWarn, wantBlocking: false, wantTags: []string{TagUnverified, TagDowngraded},
		},
		"warn with no quote in the spec stays warn": {
			severity: schema.SeverityWarn, evidence: []schema.Evidence{ev(6, fake)}, tags: []string{"assumption"},
			wantSeverity: schema.SeverityWarn, wantBlocking: true, wantTags: []string{"assumption", TagUnverified},
		},
		"one real quote is enough": {
			severity: schema.SeverityCritical, evidence: []schema.Evidence{ev(6, fake), ev(8, real)},
			wantSeverity: schema.SeverityCritical, wantBlocking: true,
		},
		"nothing to check": {
			severity: schema.SeverityCritical, evidence: []schema.Evidence{ev(5, ""), ev(5, "N/A")},
			wantSeverity: schema.SeverityCritical, wantBlocking: true,
		},
		"tags are not repeated": {
			severity: schema.SeverityWarn, evidence: []schema.Evidence{ev(6, fake)}, tags: []string{TagUnverified},
			wantSeverity: schema.SeverityWarn, wantBlocking: true, wantTags: []string{TagUnverified},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			issue := schema.Issue{Severity: tc.severity, Blocking: true, Evidence: tc.evidence, Tags: tc.tags}
			NewIndex(specText).CheckIssue(&issue)
			if issue.Severity != tc.wantSeverity || issue.Blocking != tc.wantBlocking {
				t.Errorf("severity = %s blocking = %v, want %s and %v", issue.Severity, issue.Blocking, tc.wantSeverity, tc.wantBlocking)
			}
			if !slices.Equal(issue.Tags, tc.wantTags) {
				t.Errorf("tags = %v, want %v", issue.Tags, tc.wantTags)
			}
		})
	}
}

func TestCheckIssue_CorrectsItsEvidenceInPlace(t *testing.T) {
	issue := schema.Issue{Severity: schema.SeverityWarn, Evidence: []schema.Evidence{{LineStart: 2, LineEnd: 2, Quote: "amounts are integers in minor units"}}}
	NewIndex(specText).CheckIssue(&issue)
	if got := issue.Evidence[0]; got.LineStart != 8 || got.Quote != "- **`pay-3`**: Amounts are integers in minor units." {
		t.Errorf("evidence = %+v, want it moved to line 8 and quoting the whole line", got)
	}
}

func TestCheckQuestion(t *testing.T) {
	question := schema.Question{Severity: schema.SeverityCritical, Evidence: []schema.Evidence{
		{LineStart: 2, LineEnd: 2, Quote: "amounts are integers in minor units"},
		{LineStart: 6, LineEnd: 6, Quote: "Charges are retried three times before failing."},
	}}
	NewIndex(specText).CheckQuestion(&question)
	if got := question.Evidence[0]; got.LineStart != 8 || got.Quote != "- **`pay-3`**: Amounts are integers in minor units." {
		t.Errorf("first evidence = %+v, want it moved to line 8", got)
	}
	if got := question.Evidence[1]; got.LineStart != 6 || question.Severity != schema.SeverityCritical {
		t.Errorf("second evidence = %+v severity = %s, want both left as they were", got, question.Severity)
	}
}

// Models are asked for a short anchor. A located anchor becomes the full text
// of a short cited range; a long range keeps the located text, and an anchor
// that was not found is left as the model wrote it.
func TestCheckIssue_ExpandsLocatedAnchorsToTheirLines(t *testing.T) {
	cases := map[string]struct {
		ev   schema.Evidence
		want string
	}{
		"single line": {
			ev:   schema.Evidence{LineStart: 6, LineEnd: 6, Quote: "same Idempotency-Key"},
			want: "- **`pay-1`**: A charge MUST be   idempotent when the client sends the same `Idempotency-Key`.",
		},
		"short range": {
			ev:   schema.Evidence{LineStart: 19, LineEnd: 20, Quote: "follow ISO 4217"},
			want: "Currency codes follow ISO 4217.\nRounding is half to even.",
		},
		"long range keeps the anchor": {
			ev:   schema.Evidence{LineStart: 18, LineEnd: 24, Quote: "Ledger entries are append-only"},
			want: "Ledger entries are append-only",
		},
		"not found": {
			ev:   schema.Evidence{LineStart: 6, LineEnd: 6, Quote: "charges are never refunded twice"},
			want: "charges are never refunded twice",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			issue := schema.Issue{Severity: schema.SeverityWarn, Evidence: []schema.Evidence{tc.ev}}
			NewIndex(specText).CheckIssue(&issue)
			if got := issue.Evidence[0].Quote; got != tc.want {
				t.Errorf("quote = %q, want %q", got, tc.want)
			}
		})
	}
}

// Anchor on its own returns the located text, not the whole line: retraction
// and rejection quotes are the answering text, not a finding's evidence.
func TestAnchor_DoesNotExpand(t *testing.T) {
	got, outcome := NewIndex(specText).Anchor(schema.Evidence{LineStart: 6, LineEnd: 6, Quote: "when the client sends"})
	if outcome != Verified || got.Quote != "when the client sends" {
		t.Errorf("Anchor = %q (%v), want only the located text", got.Quote, outcome)
	}
}
