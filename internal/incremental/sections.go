package incremental

import (
	"crypto/sha256"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"github.com/dshills/speccritic/internal/spec"
)

const (
	ClassUnchanged = "unchanged"
	ClassChanged   = "changed"
	ClassAdded     = "added"
	ClassDeleted   = "deleted"
	ClassMoved     = "moved"
	ClassRenamed   = "renamed"
	ClassAmbiguous = "ambiguous"
)

type section struct {
	ID            string
	Level         int
	HeadingText   string
	HeadingPath   []string
	IdentityPath  string
	Range         LineRange
	ContentHash   string
	LocalAnchor   string
	Sample        string
	NonEmptyLines int
}

type heading struct {
	Level int
	Text  string
	Line  int
}

var headingAttributePattern = regexp.MustCompile(`\s+\{#[A-Za-z0-9_.:-]+\}\s*$`)

func buildSections(raw string) []section {
	lines := spec.Lines(raw)
	if len(lines) == 0 {
		return nil
	}
	headings := extractHeadings(lines)
	if len(headings) == 0 {
		return []section{makeSection(lines, 0, "Document", nil, LineRange{Start: 1, End: len(lines)})}
	}
	var sections []section
	if headings[0].Line > 1 {
		sections = append(sections, makeSection(lines, 0, "Introduction", nil, LineRange{Start: 1, End: headings[0].Line - 1}))
	}
	path := make([]heading, 0, 6)
	occurrences := make(map[string]int)
	for i, h := range headings {
		for len(path) > 0 && path[len(path)-1].Level >= h.Level {
			path = path[:len(path)-1]
		}
		path = append(path, h)
		end := len(lines)
		if i+1 < len(headings) {
			end = headings[i+1].Line - 1
		}
		displayPath := make([]string, len(path))
		identityParts := make([]string, len(path))
		for j, item := range path {
			displayPath[j] = item.Text
			parent := strings.Join(identityParts[:j], " > ")
			key := parent + "\x00" + item.Text
			if j == len(path)-1 {
				occurrences[key]++
			}
			idx := occurrences[key]
			if idx == 0 {
				idx = 1
			}
			identityParts[j] = item.Text + "[" + strconv.Itoa(idx) + "]"
		}
		sections = append(sections, makeSection(lines, h.Level, h.Text, displayPath, LineRange{Start: h.Line, End: end}))
		sections[len(sections)-1].IdentityPath = strings.Join(identityParts, " > ")
	}
	return sections
}

func extractHeadings(lines []string) []heading {
	var out []heading
	for i, line := range lines {
		level, text, ok := parseHeading(line)
		if ok {
			out = append(out, heading{Level: level, Text: text, Line: i + 1})
		}
	}
	return out
}

func parseHeading(line string) (int, string, bool) {
	if strings.HasPrefix(line, "    ") || strings.HasPrefix(line, "\t") {
		return 0, "", false
	}
	trimmed := strings.TrimLeft(line, " ")
	if strings.Count(line[:len(line)-len(trimmed)], " ") > 3 {
		return 0, "", false
	}
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level == 0 || level > 6 || level >= len(trimmed) || trimmed[level] != ' ' {
		return 0, "", false
	}
	text := normalizeHeading(trimmed[level+1:])
	if text == "" {
		return 0, "", false
	}
	return level, text, true
}

func normalizeHeading(text string) string {
	text = strings.TrimSpace(text)
	text = headingAttributePattern.ReplaceAllString(text, "")
	text = strings.TrimSpace(text)
	return strings.Join(strings.Fields(text), " ")
}

func makeSection(lines []string, level int, headingText string, path []string, r LineRange) section {
	content := sectionContent(lines, r)
	nonEmpty := nonEmptyLines(content)
	s := section{
		Level:         level,
		HeadingText:   headingText,
		HeadingPath:   append([]string(nil), path...),
		IdentityPath:  strings.Join(path, " > "),
		Range:         r,
		ContentHash:   hashString(content),
		Sample:        sampleContent(content),
		NonEmptyLines: nonEmpty,
	}
	s.LocalAnchor = hashString(s.IdentityPath + "\n" + firstNonEmpty(content, 5))
	s.ID = sectionID(r)
	return s
}

func sectionID(r LineRange) string {
	return fmt.Sprintf("SECTION-L%d-L%d", r.Start, r.End)
}

func sectionContent(lines []string, r LineRange) string {
	if r.Start < 1 || r.End < r.Start || r.Start > len(lines) {
		return ""
	}
	end := r.End
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[r.Start-1:end], "\n")
}

func nonEmptyLines(content string) int {
	count := 0
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) != "" {
			count++
		}
	}
	return count
}

func sampleContent(content string) string {
	var lines []string
	for _, line := range strings.Split(content, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, line)
		}
	}
	if len(lines) <= 30 {
		return strings.Join(lines, "\n")
	}
	var sample []string
	sample = append(sample, lines[:10]...)
	mid := len(lines)/2 - 5
	if mid < 10 {
		mid = 10
	}
	sample = append(sample, lines[mid:mid+10]...)
	sample = append(sample, lines[len(lines)-10:]...)
	return strings.Join(sample, "\n")
}

func firstNonEmpty(content string, limit int) string {
	var out []string
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		out = append(out, line)
		if len(out) == limit {
			break
		}
	}
	return strings.Join(out, "\n")
}

func hashString(s string) string {
	sum := sha256.Sum256([]byte(s))
	return fmt.Sprintf("sha256:%x", sum)
}

func similarity(a, b string) float64 {
	a = strings.TrimSpace(a)
	b = strings.TrimSpace(b)
	if a == "" && b == "" {
		return 1
	}
	if a == "" || b == "" {
		return 0
	}
	ar := []rune(a)
	br := []rune(b)
	dist := levenshtein(ar, br)
	maxLen := len(ar)
	if len(br) > maxLen {
		maxLen = len(br)
	}
	return 1 - float64(dist)/float64(maxLen)
}

func levenshtein(a, b []rune) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	cur := make([]int, len(b)+1)
	for i := 1; i <= len(a); i++ {
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			cur[j] = minInt(cur[j-1]+1, prev[j]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(b)]
}

func tokenJaccard(a, b string) float64 {
	at := tokenSet(a)
	bt := tokenSet(b)
	if len(at) == 0 && len(bt) == 0 {
		return 1
	}
	if len(at) == 0 || len(bt) == 0 {
		return 0
	}
	intersection := 0
	for token := range at {
		if bt[token] {
			intersection++
		}
	}
	union := len(at) + len(bt) - intersection
	return float64(intersection) / float64(union)
}

func tokenSet(s string) map[string]bool {
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	out := make(map[string]bool, len(fields))
	for _, field := range fields {
		if len(field) < 2 {
			continue
		}
		out[field] = true
	}
	return out
}

func minInt(vals ...int) int {
	min := vals[0]
	for _, v := range vals[1:] {
		if v < min {
			min = v
		}
	}
	return min
}

func estimateTokens(text string) int {
	if text == "" {
		return 0
	}
	nonASCII := false
	for _, r := range text {
		if r > unicode.MaxASCII {
			nonASCII = true
			break
		}
	}
	if nonASCII {
		return len([]rune(text))
	}
	return (len(text) + 2) / 3
}
