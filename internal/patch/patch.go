package patch

import (
	"fmt"
	"io"
	"strings"

	"github.com/dshills/speccritic/internal/schema"
	"github.com/sergi/go-diff/diffmatchpatch"
)

// diffPatch is the internal processing type for patch generation.
// schema.Patch is the JSON-serializable type from the LLM output.
type diffPatch struct {
	issueID string
	before  string // text to use as diff source
	after   string // text to use as diff target
}

// GenerateDiff converts schema.Patch entries into a unified diff string
// suitable for writing to --patch-out. Patches that cannot be located in
// the spec are skipped with a warning written to w (may be nil).
// Both before and after are normalized before diffing to avoid spurious
// whitespace diffs.
func GenerateDiff(specRaw string, patches []schema.Patch, w io.Writer) string {
	if len(patches) == 0 {
		return ""
	}

	// Pre-normalize the spec once for all patches.
	normSpec := normalize(specRaw)

	dmp := diffmatchpatch.New()
	var out strings.Builder

	for _, p := range patches {
		dp, ok := resolve(p, specRaw, normSpec)
		if !ok {
			if w != nil {
				fmt.Fprintf(w, "WARN: patch for %s could not be located in spec (before text not matched)\n", p.IssueID)
			}
			continue
		}

		diffs := dmp.DiffMain(dp.before, dp.after, false)
		patchList := dmp.PatchMake(dp.before, diffs)
		patchText := dmp.PatchToText(patchList)
		if patchText == "" {
			continue
		}

		out.WriteString(fmt.Sprintf("# patch for %s\n", dp.issueID))
		out.WriteString(patchText)
		out.WriteString("\n")
	}

	return out.String()
}

// resolve attempts to locate p.Before in specRaw using exact or normalized matching.
// normSpec is the pre-normalized version of specRaw (passed in to avoid re-computation).
// Returns a zero diffPatch and false if the before text cannot be found.
func resolve(p schema.Patch, specRaw, normSpec string) (diffPatch, bool) {
	normBefore := normalize(p.Before)
	normAfter := normalize(p.After)

	// Step 1: exact match — use original text so the patch applies to the original spec.
	if strings.Contains(specRaw, p.Before) {
		return diffPatch{issueID: p.IssueID, before: p.Before, after: p.After}, true
	}

	// Step 2: normalized match (trim trailing whitespace, normalize CRLF).
	if strings.Contains(normSpec, normBefore) {
		return diffPatch{issueID: p.IssueID, before: normBefore, after: normAfter}, true
	}

	return diffPatch{}, false
}

// normalize trims trailing whitespace from each line and converts CRLF to LF.
func normalize(s string) string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t")
	}
	return strings.Join(lines, "\n")
}

