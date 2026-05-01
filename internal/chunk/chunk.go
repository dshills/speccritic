package chunk

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/dshills/speccritic/internal/spec"
)

const (
	DefaultChunkLines             = 180
	DefaultChunkOverlap           = 20
	DefaultChunkMinLines          = 120
	DefaultChunkTokenThreshold    = 4000
	DefaultChunkConcurrency       = 3
	DefaultSynthesisLineThreshold = 240
)

type Mode string

const (
	ModeAuto Mode = "auto"
	ModeOn   Mode = "on"
	ModeOff  Mode = "off"
)

type Config struct {
	Mode                   Mode
	ChunkLines             int
	ChunkOverlap           int
	ChunkMinLines          int
	ChunkTokenThreshold    int
	ChunkConcurrency       int
	SynthesisLineThreshold int
}

type Heading struct {
	Level int
	Text  string
	Line  int
}

type Section struct {
	LineStart   int
	LineEnd     int
	HeadingPath []string
}

type Chunk struct {
	ID          string
	Path        string
	LineStart   int
	LineEnd     int
	ContextFrom int
	ContextTo   int
	HeadingPath []string
	Numbered    string
}

type Plan struct {
	Chunks    []Chunk
	Headings  []Heading
	Sections  []Section
	LineCount int
}

func WithDefaults(cfg Config) Config {
	if cfg.Mode == "" {
		cfg.Mode = ModeAuto
	}
	if cfg.ChunkLines == 0 {
		cfg.ChunkLines = DefaultChunkLines
	}
	if cfg.ChunkMinLines == 0 {
		cfg.ChunkMinLines = DefaultChunkMinLines
	}
	if cfg.ChunkTokenThreshold == 0 {
		cfg.ChunkTokenThreshold = DefaultChunkTokenThreshold
	}
	if cfg.ChunkConcurrency == 0 {
		cfg.ChunkConcurrency = DefaultChunkConcurrency
	}
	if cfg.SynthesisLineThreshold == 0 {
		cfg.SynthesisLineThreshold = DefaultSynthesisLineThreshold
	}
	return cfg
}

func ValidateConfig(cfg Config) error {
	switch cfg.Mode {
	case "", ModeAuto, ModeOn, ModeOff:
	default:
		return fmt.Errorf("invalid chunking mode %q", cfg.Mode)
	}
	if cfg.ChunkLines <= 0 {
		return fmt.Errorf("--chunk-lines must be > 0, got %d", cfg.ChunkLines)
	}
	if cfg.ChunkOverlap < 0 || cfg.ChunkOverlap >= cfg.ChunkLines {
		return fmt.Errorf("--chunk-overlap must be >= 0 and less than --chunk-lines, got %d", cfg.ChunkOverlap)
	}
	if cfg.ChunkMinLines < 0 {
		return fmt.Errorf("--chunk-min-lines must be >= 0, got %d", cfg.ChunkMinLines)
	}
	if cfg.ChunkTokenThreshold <= 0 {
		return fmt.Errorf("--chunk-token-threshold must be > 0, got %d", cfg.ChunkTokenThreshold)
	}
	if cfg.ChunkConcurrency < 1 || cfg.ChunkConcurrency > 16 {
		return fmt.Errorf("--chunk-concurrency must be between 1 and 16, got %d", cfg.ChunkConcurrency)
	}
	if cfg.SynthesisLineThreshold < 0 {
		return fmt.Errorf("--synthesis-line-threshold must be >= 0, got %d", cfg.SynthesisLineThreshold)
	}
	return nil
}

func PlanSpec(s *spec.Spec, cfg Config) (Plan, error) {
	if s == nil {
		return Plan{}, fmt.Errorf("spec is required")
	}
	cfg = WithDefaults(cfg)
	if err := ValidateConfig(cfg); err != nil {
		return Plan{}, err
	}
	lines := spec.Lines(s.Raw)
	headings := ExtractHeadings(lines)
	sections := BuildSections(lines, headings)
	if len(sections) == 0 && len(lines) > 0 {
		sections = []Section{{LineStart: 1, LineEnd: len(lines)}}
	}
	candidates := candidateSections(lines, headings, cfg.ChunkLines)
	if len(candidates) == 0 {
		candidates = sections
	}
	chunks := buildChunks(s.Path, lines, candidates, cfg)
	return Plan{
		Chunks:    chunks,
		Headings:  headings,
		Sections:  sections,
		LineCount: len(lines),
	}, nil
}

func ExtractHeadings(lines []string) []Heading {
	var headings []Heading
	for i, line := range lines {
		level, text, ok := parseHeading(line)
		if !ok {
			continue
		}
		headings = append(headings, Heading{Level: level, Text: text, Line: i + 1})
	}
	return headings
}

func BuildSections(lines []string, headings []Heading) []Section {
	if len(headings) == 0 {
		return nil
	}
	sections := make([]Section, 0, len(headings))
	if headings[0].Line > 1 {
		sections = append(sections, Section{LineStart: 1, LineEnd: headings[0].Line - 1})
	}
	path := make([]Heading, 0, 6)
	for i, heading := range headings {
		for len(path) > 0 && path[len(path)-1].Level >= heading.Level {
			path = path[:len(path)-1]
		}
		path = append(path, heading)
		end := len(lines)
		for j := i + 1; j < len(headings); j++ {
			if headings[j].Level <= heading.Level {
				end = headings[j].Line - 1
				break
			}
		}
		sections = append(sections, Section{
			LineStart: heading.Line,
			LineEnd:   end,
			HeadingPath: func() []string {
				out := make([]string, len(path))
				for i, h := range path {
					out[i] = h.Text
				}
				return out
			}(),
		})
	}
	return sections
}

func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	return (len(text) + 3) / 4
}

func ShouldChunk(lineCount, estimatedTokens int, cfg Config) bool {
	cfg = WithDefaults(cfg)
	switch cfg.Mode {
	case ModeOff:
		return false
	case ModeOn:
		return true
	default:
		return lineCount >= cfg.ChunkMinLines || estimatedTokens >= cfg.ChunkTokenThreshold
	}
}

func buildChunks(path string, lines []string, sections []Section, cfg Config) []Chunk {
	var chunks []Chunk
	for _, section := range sections {
		for _, span := range splitSection(lines, section, cfg.ChunkLines) {
			chunks = append(chunks, makeChunk(path, lines, span, cfg.ChunkOverlap, len(chunks)+1))
		}
	}
	return chunks
}

func candidateSections(lines []string, headings []Heading, target int) []Section {
	if len(headings) == 0 {
		return nil
	}
	var out []Section
	if headings[0].Line > 1 {
		out = append(out, Section{LineStart: 1, LineEnd: headings[0].Line - 1})
	}
	baseLevel := headings[0].Level
	for i, heading := range headings {
		if i > 0 && heading.Level > baseLevel {
			continue
		}
		baseLevel = heading.Level
		end := len(lines)
		for j := i + 1; j < len(headings); j++ {
			if headings[j].Level <= heading.Level {
				end = headings[j].Line - 1
				break
			}
		}
		out = append(out, splitByNested(lines, headings, Section{
			LineStart:   heading.Line,
			LineEnd:     end,
			HeadingPath: []string{heading.Text},
		}, target)...)
	}
	return out
}

func splitByNested(lines []string, headings []Heading, section Section, target int) []Section {
	if section.LineEnd-section.LineStart+1 <= target*2 {
		return []Section{section}
	}
	children := immediateChildHeadings(headings, section)
	if len(children) == 0 {
		return splitSection(lines, section, target)
	}
	var out []Section
	if section.LineStart < children[0].Line {
		out = append(out, sectionWithRange(section, section.LineStart, children[0].Line-1))
	}
	for i, child := range children {
		end := section.LineEnd
		if i < len(children)-1 {
			end = children[i+1].Line - 1
		}
		childSection := Section{
			LineStart:   child.Line,
			LineEnd:     end,
			HeadingPath: append(append([]string(nil), section.HeadingPath...), child.Text),
		}
		out = append(out, splitByNested(lines, headings, childSection, target)...)
	}
	return out
}

func immediateChildHeadings(headings []Heading, section Section) []Heading {
	parentLevel := 0
	for _, heading := range headings {
		if heading.Line == section.LineStart {
			parentLevel = heading.Level
			break
		}
	}
	if parentLevel == 0 {
		return nil
	}
	childLevel := 0
	for _, heading := range headings {
		if heading.Line <= section.LineStart || heading.Line > section.LineEnd || heading.Level <= parentLevel {
			continue
		}
		if childLevel == 0 || heading.Level < childLevel {
			childLevel = heading.Level
		}
	}
	if childLevel == 0 {
		return nil
	}
	var children []Heading
	for _, heading := range headings {
		if heading.Line > section.LineStart && heading.Line <= section.LineEnd && heading.Level == childLevel {
			children = append(children, heading)
		}
	}
	return children
}

func splitSection(lines []string, section Section, target int) []Section {
	if section.LineEnd-section.LineStart+1 <= target*2 {
		return []Section{section}
	}
	var spans []Section
	start := section.LineStart
	for start <= section.LineEnd {
		end := start + target - 1
		if end >= section.LineEnd {
			spans = append(spans, sectionWithRange(section, start, section.LineEnd))
			break
		}
		if boundary := nearestBoundary(lines, start, end, section.LineEnd); boundary >= start {
			end = boundary
		}
		spans = append(spans, sectionWithRange(section, start, end))
		start = end + 1
	}
	return spans
}

func nearestBoundary(lines []string, start, targetEnd, maxEnd int) int {
	floor := start + 1
	for line := targetEnd; line >= floor; line-- {
		if line > len(lines) {
			continue
		}
		if isSplitBoundary(lines[line-1]) {
			return line
		}
	}
	for line := targetEnd + 1; line <= maxEnd && line <= targetEnd+20; line++ {
		if line > len(lines) {
			continue
		}
		if isSplitBoundary(lines[line-1]) {
			return line
		}
	}
	return targetEnd
}

func sectionWithRange(section Section, start, end int) Section {
	return Section{LineStart: start, LineEnd: end, HeadingPath: append([]string(nil), section.HeadingPath...)}
}

func makeChunk(path string, lines []string, section Section, overlap, ordinal int) Chunk {
	lineCount := len(lines)
	if section.LineStart < 1 {
		section.LineStart = 1
	}
	if section.LineEnd > lineCount {
		section.LineEnd = lineCount
	}
	if section.LineEnd < section.LineStart {
		section.LineEnd = section.LineStart
	}
	contextFrom := section.LineStart - overlap
	if contextFrom < 1 {
		contextFrom = 1
	}
	contextTo := section.LineEnd + overlap
	if contextTo > lineCount {
		contextTo = lineCount
	}
	return Chunk{
		ID:          fmt.Sprintf("CHUNK-%04d-L%d-L%d", ordinal, section.LineStart, section.LineEnd),
		Path:        path,
		LineStart:   section.LineStart,
		LineEnd:     section.LineEnd,
		ContextFrom: contextFrom,
		ContextTo:   contextTo,
		HeadingPath: append([]string(nil), section.HeadingPath...),
		Numbered:    numberRange(lines, section.LineStart, section.LineEnd),
	}
}

func numberRange(lines []string, start, end int) string {
	var b strings.Builder
	for line := start; line <= end; line++ {
		if line > start {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "L%d: %s", line, lines[line-1])
	}
	return b.String()
}

func parseHeading(line string) (int, string, bool) {
	if leadingSpaces(line) >= 4 {
		return 0, "", false
	}
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "#") {
		return 0, "", false
	}
	level := 0
	for level < len(trimmed) && trimmed[level] == '#' {
		level++
	}
	if level == 0 || level > 6 || level >= len(trimmed) || !unicode.IsSpace(rune(trimmed[level])) {
		return 0, "", false
	}
	text := strings.TrimSpace(trimmed[level:])
	if text == "" {
		return 0, "", false
	}
	return level, text, true
}

func leadingSpaces(line string) int {
	count := 0
	for _, r := range line {
		if r != ' ' {
			return count
		}
		count++
	}
	return count
}

func isSplitBoundary(line string) bool {
	trimmed := strings.TrimSpace(line)
	return trimmed == "" || strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") || parseHeadingOK(trimmed)
}

func parseHeadingOK(line string) bool {
	_, _, ok := parseHeading(line)
	return ok
}
