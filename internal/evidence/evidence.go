// Package evidence checks the evidence a model cites against the spec it was
// shown.
//
// A model reports where a defect is with a line range and a quote. The line
// numbers are the weaker of the two: they drift by a line or so, and nothing
// stops a model from citing a line that says something else. The quote can be
// checked. If it is in the spec, the finding is anchored to where the quote
// really is; if it is nowhere in the spec, the finding rests on text that does
// not exist.
package evidence

import (
	"bytes"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/dshills/speccritic/internal/schema"
	"github.com/dshills/speccritic/internal/spec"
)

// Tags added to an issue to record what checking its evidence found.
const (
	// TagReanchored marks an issue with evidence whose quote was found on
	// other lines than the ones cited. Its line range was moved to the quote.
	TagReanchored = "evidence-reanchored"
	// TagUnverified marks an issue none of whose quotes is in the spec.
	TagUnverified = "evidence-unverified"
	// TagDowngraded marks an issue lowered from CRITICAL because its evidence
	// could not be verified.
	TagDowngraded = "severity-downgraded"
)

// Outcome says what checking one evidence entry found.
type Outcome int

const (
	// NoQuote means the entry had no quote to check. Its lines are taken on
	// trust.
	NoQuote Outcome = iota
	// Verified means the quote is at the cited lines.
	Verified
	// Moved means the quote is elsewhere in the spec and the entry now points
	// there.
	Moved
	// Unverified means the quote is long enough to be checkable and is not in
	// the spec, or is in several places none of which is near the cited lines.
	Unverified
)

const (
	// nearLines is how far from the cited lines a quote that occurs more than
	// once may be and still be taken as the one the model meant.
	nearLines = 5
	// maxGap bounds, in normalized bytes, the text an ellipsis in a quote may
	// stand for.
	maxGap = 2000
	// maxFilledLines is the largest cited range whose text is copied in as the
	// quote when the model gave none.
	maxFilledLines = 5
	// minQuoteBytes is the least normalized text a quote needs before its
	// absence, or a nearby match, means anything. A word or two turns up by
	// chance all over a spec, and a placeholder such as "N/A" is no claim
	// about the spec's text.
	minQuoteBytes = 12
)

// Index is a spec prepared for looking quotes up in it.
type Index struct {
	lines []string
	// text is the spec with case, whitespace, typographic punctuation and
	// Markdown emphasis folded away, so a quote matches despite such
	// differences. The other slices give, for each byte of text, the line it
	// came from and the byte range of its source character in that line.
	text       []byte
	line       []int32
	start, end []int32
}

// NewIndex prepares specText, which must be the text the model was shown.
func NewIndex(specText string) *Index {
	ix := &Index{lines: spec.Lines(specText)}
	for n, line := range ix.lines {
		if n > 0 {
			ix.emit(' ', n, 0, 0)
		}
		for col, r := range line {
			ix.fold(r, n, col, col+utf8.RuneLen(r))
		}
	}
	return ix
}

// fold appends the normalized form of r, if it has one.
func (ix *Index) fold(r rune, line, start, end int) {
	for _, c := range foldRune(r) {
		ix.emit(c, line, start, end)
	}
}

func (ix *Index) emit(r rune, line, start, end int) {
	if r == ' ' && (len(ix.text) == 0 || ix.text[len(ix.text)-1] == ' ') {
		return
	}
	var buf [utf8.UTFMax]byte
	n := utf8.EncodeRune(buf[:], r)
	for i := range n {
		ix.text = append(ix.text, buf[i])
		ix.line = append(ix.line, int32(line))
		ix.start = append(ix.start, int32(start))
		ix.end = append(ix.end, int32(end))
	}
}

// foldRune returns the normalized form of r: nothing for Markdown emphasis
// and code marks, a space for any whitespace, plain punctuation for its
// typographic variants, and the lowercase letter otherwise.
func foldRune(r rune) []rune {
	switch {
	case r == '*' || r == '`':
		return nil
	case unicode.IsSpace(r) || r == ' ':
		return []rune{' '}
	case r == '‘' || r == '’':
		return []rune{'\''}
	case r == '“' || r == '”':
		return []rune{'"'}
	case r == '–' || r == '—':
		return []rune{'-'}
	case r == '…':
		return []rune{'.', '.', '.'}
	}
	return []rune{unicode.ToLower(r)}
}

// linePrefix matches the line label a model may copy along with a quote.
var linePrefix = regexp.MustCompile(`(?m)^[ \t]*L\d+:[ \t]?`)

// quoteSegments normalizes a quote the same way as the spec and splits it at
// each ellipsis, which stands for text the model left out.
func quoteSegments(quote string) [][]byte {
	quote = linePrefix.ReplaceAllString(quote, "")
	var folded []rune
	for _, r := range quote {
		for _, c := range foldRune(r) {
			if c == ' ' && (len(folded) == 0 || folded[len(folded)-1] == ' ') {
				continue
			}
			folded = append(folded, c)
		}
	}
	var segments [][]byte
	for _, part := range strings.Split(string(folded), "...") {
		if part = strings.TrimSpace(part); part != "" {
			segments = append(segments, []byte(part))
		}
	}
	return segments
}

// occurrence is one place a quote was found, as a byte range of Index.text.
type occurrence struct{ from, to int }

// matchRest finds segments in order starting at or after from, each within
// maxGap of the end of the one before, and returns where the last one ends.
// When a later segment cannot be found after one match of an earlier segment,
// the next match of that earlier segment is tried.
func (ix *Index) matchRest(from int, segments [][]byte) (end int, ok bool) {
	if len(segments) == 0 {
		return from, true
	}
	limit := min(from+maxGap, len(ix.text))
	for at := from; at < limit; {
		i := bytes.Index(ix.text[at:limit], segments[0])
		if i < 0 {
			break
		}
		if end, ok := ix.matchRest(at+i+len(segments[0]), segments[1:]); ok {
			return end, true
		}
		at += i + 1
	}
	return 0, false
}

// nearest scans every place the quote appears and returns the one closest to
// the cited lines, how many lines away it is, and how many places there were.
func (ix *Index) nearest(segments [][]byte, ev schema.Evidence) (best occurrence, distance, count int) {
	for from := 0; from < len(ix.text); {
		i := bytes.Index(ix.text[from:], segments[0])
		if i < 0 {
			break
		}
		start := from + i
		from = start + 1
		end, ok := ix.matchRest(start+len(segments[0]), segments[1:])
		if !ok {
			continue
		}
		o := occurrence{from: start, to: end}
		first, last := ix.linesOf(o)
		d := 0
		switch {
		case last < ev.LineStart:
			d = ev.LineStart - last
		case first > ev.LineEnd:
			d = first - ev.LineEnd
		}
		if count == 0 || d < distance {
			best, distance = o, d
		}
		count++
	}
	return best, distance, count
}

// linesOf returns the 1-based first and last line an occurrence covers.
func (ix *Index) linesOf(o occurrence) (first, last int) {
	return int(ix.line[o.from]) + 1, int(ix.line[o.to-1]) + 1
}

// exact returns the spec's own text for an occurrence, with its original
// case, spacing and punctuation.
func (ix *Index) exact(o occurrence) string {
	first, last := int(ix.line[o.from]), int(ix.line[o.to-1])
	startCol, endCol := int(ix.start[o.from]), int(ix.end[o.to-1])
	if first == last {
		return ix.lines[first][startCol:endCol]
	}
	parts := []string{ix.lines[first][startCol:]}
	parts = append(parts, ix.lines[first+1:last]...)
	parts = append(parts, ix.lines[last][:endCol])
	return strings.Join(parts, "\n")
}

// Anchor checks one evidence entry and returns it corrected.
//
// A quote found at the cited lines is replaced by the spec's exact text. A
// quote found only elsewhere moves the line range to where it is: always when
// it occurs exactly once, and to the nearest occurrence when that is within a
// few lines. A quote that is not in the spec leaves the entry as it was.
//
// A quote too short to be distinctive is only ever confirmed or, when it
// occurs exactly once, moved; it is never reported as unverified. An entry
// with no quote is given the text of its lines when the range is short.
func (ix *Index) Anchor(ev schema.Evidence) (schema.Evidence, Outcome) {
	segments := quoteSegments(ev.Quote)
	if len(segments) == 0 {
		if ev.LineStart >= 1 && ev.LineEnd >= ev.LineStart && ev.LineEnd <= len(ix.lines) && ev.LineEnd-ev.LineStart < maxFilledLines {
			ev.Quote = strings.Join(ix.lines[ev.LineStart-1:ev.LineEnd], "\n")
		}
		return ev, NoQuote
	}
	length := 0
	for _, segment := range segments {
		length += len(segment)
	}
	distinctive := length >= minQuoteBytes

	found, distance, count := ix.nearest(segments, ev)
	if count == 0 {
		if distinctive {
			return ev, Unverified
		}
		return ev, NoQuote
	}

	first, last := ix.linesOf(found)
	switch {
	case distance == 0:
		// The quote is at the cited lines. Widen the range if the quote runs
		// past it; never narrow it, since a model may cite more than it quotes.
		ev.LineStart = min(ev.LineStart, first)
		ev.LineEnd = max(ev.LineEnd, last)
		ev.Quote = ix.exact(found)
		return ev, Verified
	case count == 1 || (distinctive && distance <= nearLines):
		ev.LineStart, ev.LineEnd = first, last
		ev.Quote = ix.exact(found)
		return ev, Moved
	case !distinctive:
		return ev, NoQuote
	}
	return ev, Unverified
}

// CheckIssue checks every evidence entry of issue and records the result in
// its tags. An issue none of whose quotes is in the spec is tagged unverified,
// and if it was CRITICAL it is lowered to WARN: a finding that cannot point at
// real text should not be able to fail a spec on its own.
func (ix *Index) CheckIssue(issue *schema.Issue) {
	quoted, located, moved := 0, 0, false
	for i, ev := range issue.Evidence {
		corrected, outcome := ix.Anchor(ev)
		issue.Evidence[i] = corrected
		switch outcome {
		case Verified:
			quoted++
			located++
		case Moved:
			quoted++
			located++
			moved = true
		case Unverified:
			quoted++
		}
	}
	if moved {
		issue.Tags = appendTag(issue.Tags, TagReanchored)
	}
	if quoted == 0 || located > 0 {
		return
	}
	issue.Tags = appendTag(issue.Tags, TagUnverified)
	if issue.Severity == schema.SeverityCritical {
		issue.Severity = schema.SeverityWarn
		issue.Blocking = false
		issue.Tags = appendTag(issue.Tags, TagDowngraded)
	}
}

// CheckQuestion checks the evidence of a question. A question has no tags and
// asks about what is missing rather than asserting what is there, so its
// evidence is corrected where it can be and otherwise left alone.
func (ix *Index) CheckQuestion(question *schema.Question) {
	for i, ev := range question.Evidence {
		question.Evidence[i], _ = ix.Anchor(ev)
	}
}

func appendTag(tags []string, tag string) []string {
	for _, existing := range tags {
		if existing == tag {
			return tags
		}
	}
	return append(tags, tag)
}
