package chunk

import (
	"fmt"
	"strings"
)

// BuildChunkTask returns the task for reviewing one chunk. It follows the
// shared prefix built by llm.BuildSpecPrefix.
//
// Normally that prefix holds the whole numbered spec: the reviewer reads every
// section, so a term defined elsewhere or a rule stated in another section is
// not mistaken for a gap, and reports only on the chunk's own lines. With
// withLines set the spec was too large to share and the prefix leaves it out,
// so the task carries the chunk's own numbered lines instead.
func BuildChunkTask(ch Chunk, lineCount int, withLines bool) (string, error) {
	if ch.ID == "" {
		return "", fmt.Errorf("chunk is required")
	}
	if ch.LineStart < 1 || ch.LineEnd < ch.LineStart || ch.LineEnd > lineCount {
		return "", fmt.Errorf("chunk %s has invalid primary range %d-%d", ch.ID, ch.LineStart, ch.LineEnd)
	}
	var b strings.Builder
	where := fmt.Sprintf("lines L%d-L%d", ch.LineStart, ch.LineEnd)
	if len(ch.HeadingPath) > 0 {
		where += fmt.Sprintf(" (%s)", strings.Join(ch.HeadingPath, " > "))
	}
	if withLines {
		fmt.Fprintf(&b, "\nReview %s of a specification for defects. The specification is too large to show in full; these are the lines to review:\n", where)
		fmt.Fprintf(&b, "<spec_lines file=%q>\n%s\n</spec_lines>\n", ch.Path, ch.Numbered)
		b.WriteString("Other reviewers cover the rest of the specification. A term or rule you cannot find here may be defined elsewhere: say so in the finding and add the tag \"cross-section\" rather than asserting that it is missing.\n")
		fmt.Fprintf(&b, "Cite only lines L%d-L%d in evidence.\n", ch.LineStart, ch.LineEnd)
		return b.String(), nil
	}
	fmt.Fprintf(&b, "\nReview %s of the specification above for defects.\n", where)
	b.WriteString("Other reviewers cover the rest of the specification. Read all of it, so that you do not report as missing something stated elsewhere, but report only defects in these lines.\n")
	fmt.Fprintf(&b, "Cite only lines L%d-L%d in evidence. Add the tag \"cross-section\" to a finding that depends on another section.\n", ch.LineStart, ch.LineEnd)
	return b.String(), nil
}
