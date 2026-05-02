package completion

import (
	"regexp"
	"strings"
)

var (
	atxHeadingPattern = regexp.MustCompile(`^ {0,3}(#{1,6})(?:[ \t]+(.*?))?[ \t]*#*[ \t]*$`)
	requirementID     = regexp.MustCompile(`\b[A-Z][A-Z0-9]+-\d+\b`)
	requirementCue    = regexp.MustCompile(`(?i)\b(must|shall|open decision)\b`)
	lineEndings       = strings.NewReplacer("\r\n", "\n", "\r", "\n")
)

type Document struct {
	Raw          string
	Lines        []string
	Sections     []SectionNode
	PrimaryLevel int
}

type SectionNode struct {
	Heading   string
	Level     int
	StartLine int
	EndLine   int
	BodyStart int
	Parent    int
}

type PatchTarget struct {
	Before string
	After  string
	Line   int
}

func AnalyzeSections(raw string) Document {
	raw = normalizeLineEndings(raw)
	lines := splitLines(raw)
	sections := make([]SectionNode, 0)
	stack := make([]int, 0, 6)
	fence := ""
	for i, line := range lines {
		if marker, rest, ok := codeFenceMarker(line); ok {
			if fence == "" {
				fence = marker
			} else if marker[0] == fence[0] && len(marker) >= len(fence) && strings.TrimSpace(rest) == "" {
				fence = ""
			}
			continue
		}
		if fence != "" {
			continue
		}
		match := atxHeadingPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		level := len(match[1])
		lineNo := i + 1
		for len(stack) > 0 && sections[stack[len(stack)-1]].Level >= level {
			closing := stack[len(stack)-1]
			sections[closing].EndLine = lineNo - 1
			stack = stack[:len(stack)-1]
		}
		parent := -1
		if len(stack) > 0 {
			parent = stack[len(stack)-1]
		}
		sections = append(sections, SectionNode{
			Heading:   strings.TrimSpace(match[2]),
			Level:     level,
			StartLine: lineNo,
			EndLine:   len(lines),
			BodyStart: lineNo + 1,
			Parent:    parent,
		})
		idx := len(sections) - 1
		stack = append(stack, idx)
	}
	return Document{Raw: raw, Lines: lines, Sections: sections, PrimaryLevel: primarySectionLevel(sections)}
}

func MissingSections(doc Document, tmpl *Template) []TemplateSection {
	missing := make([]TemplateSection, 0)
	for _, section := range tmpl.Sections {
		if len(topLevelSectionsByHeading(doc, section.Heading)) == 0 {
			missing = append(missing, section)
		}
	}
	return missing
}

func IncompleteSections(doc Document, tmpl *Template) []SectionNode {
	var incomplete []SectionNode
	for _, section := range tmpl.Sections {
		matches := topLevelSectionsByHeading(doc, section.Heading)
		if len(matches) != 1 {
			continue
		}
		node := doc.Sections[matches[0]]
		if sectionMateriallyIncomplete(doc, node) {
			incomplete = append(incomplete, node)
		}
	}
	return incomplete
}

func StableInsertionTarget(doc Document, tmpl *Template, target TemplateSection, inserted string) (PatchTarget, Status) {
	if duplicateTopLevelHeading(doc, target.Heading) {
		return PatchTarget{}, StatusSkippedNoSafeLocation
	}
	lower := nearestExistingSection(doc, tmpl, target.Order, true)
	if lower >= 0 {
		before := sectionText(doc, doc.Sections[lower])
		if strings.Count(doc.Raw, before) != 1 {
			return PatchTarget{}, StatusSkippedNoSafeLocation
		}
		return PatchTarget{Before: before, After: before + "\n\n" + inserted, Line: doc.Sections[lower].EndLine + 1}, StatusPatchGenerated
	}
	higher := nearestExistingSection(doc, tmpl, target.Order, false)
	if higher >= 0 {
		before := sectionText(doc, doc.Sections[higher])
		if strings.Count(doc.Raw, before) != 1 {
			return PatchTarget{}, StatusSkippedNoSafeLocation
		}
		return PatchTarget{Before: before, After: inserted + "\n\n" + before, Line: doc.Sections[higher].StartLine}, StatusPatchGenerated
	}
	if len(doc.Lines) == 0 {
		return PatchTarget{}, StatusSkippedNoSafeLocation
	}
	before, line := lastNonEmptyLine(doc)
	if before == "" {
		return PatchTarget{}, StatusSkippedNoSafeLocation
	}
	if strings.Count(doc.Raw, before) != 1 {
		return PatchTarget{}, StatusSkippedNoSafeLocation
	}
	return PatchTarget{Before: before, After: before + "\n\n" + inserted, Line: line}, StatusPatchGenerated
}

func AppendSubsectionTarget(doc Document, node SectionNode, inserted string) (PatchTarget, Status) {
	before := sectionText(doc, node)
	if before == "" || strings.Count(doc.Raw, before) != 1 {
		return PatchTarget{}, StatusSkippedNoSafeLocation
	}
	return PatchTarget{Before: before, After: before + "\n\n" + inserted, Line: node.EndLine}, StatusPatchGenerated
}

func topLevelSectionsByHeading(doc Document, heading string) []int {
	var matches []int
	normalized := normalizeHeading(heading)
	for i, section := range doc.Sections {
		if (section.Level == doc.PrimaryLevel || section.Level == 1) && normalizeHeading(section.Heading) == normalized {
			matches = append(matches, i)
		}
	}
	return matches
}

func duplicateTopLevelHeading(doc Document, heading string) bool {
	return len(topLevelSectionsByHeading(doc, heading)) > 1
}

func nearestExistingSection(doc Document, tmpl *Template, targetOrder int, lower bool) int {
	bestIdx := -1
	bestOrder := 0
	for _, tmplSection := range tmpl.Sections {
		if lower && tmplSection.Order >= targetOrder {
			continue
		}
		if !lower && tmplSection.Order <= targetOrder {
			continue
		}
		matches := topLevelSectionsByHeading(doc, tmplSection.Heading)
		if len(matches) != 1 {
			continue
		}
		if bestIdx == -1 || (lower && tmplSection.Order > bestOrder) || (!lower && tmplSection.Order < bestOrder) {
			bestIdx = matches[0]
			bestOrder = tmplSection.Order
		}
	}
	return bestIdx
}

func sectionMateriallyIncomplete(doc Document, node SectionNode) bool {
	for lineNo := node.BodyStart; lineNo <= node.EndLine && lineNo <= len(doc.Lines); lineNo++ {
		line := strings.TrimSpace(doc.Lines[lineNo-1])
		if line == "" || atxHeadingPattern.MatchString(line) {
			continue
		}
		if requirementCue.MatchString(line) || requirementID.MatchString(line) || (strings.HasPrefix(line, "-") && strings.Contains(line, "?")) {
			return false
		}
	}
	return true
}

func sectionText(doc Document, node SectionNode) string {
	if node.StartLine < 1 || node.EndLine < node.StartLine || node.EndLine > len(doc.Lines) {
		return ""
	}
	return strings.Join(doc.Lines[node.StartLine-1:node.EndLine], "\n")
}

func lastNonEmptyLine(doc Document) (string, int) {
	for i := len(doc.Lines) - 1; i >= 0; i-- {
		line := doc.Lines[i]
		if strings.TrimSpace(line) != "" {
			return line, i + 1
		}
	}
	return "", 0
}

func normalizeHeading(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

func splitLines(raw string) []string {
	raw = strings.TrimSuffix(raw, "\n")
	if raw == "" {
		return nil
	}
	return strings.Split(raw, "\n")
}

func normalizeLineEndings(raw string) string {
	return lineEndings.Replace(raw)
}

func codeFenceMarker(line string) (string, string, bool) {
	trimmed := strings.TrimLeft(line, " ")
	if len(line)-len(trimmed) > 3 {
		return "", "", false
	}
	if marker := fenceRun(trimmed, '`'); len(marker) >= 3 {
		return marker, trimmed[len(marker):], true
	}
	if marker := fenceRun(trimmed, '~'); len(marker) >= 3 {
		return marker, trimmed[len(marker):], true
	}
	return "", "", false
}

func fenceRun(line string, marker byte) string {
	i := 0
	for i < len(line) && line[i] == marker {
		i++
	}
	return line[:i]
}

func primarySectionLevel(sections []SectionNode) int {
	if len(sections) == 0 {
		return 0
	}
	minLevel := sections[0].Level
	for _, section := range sections[1:] {
		if section.Level < minLevel {
			minLevel = section.Level
		}
	}
	if len(sections) > 1 && sections[0].Level == minLevel && countSectionsAtLevel(sections, minLevel) == 1 {
		nextMin := 7
		for _, section := range sections[1:] {
			if section.Level < nextMin {
				nextMin = section.Level
			}
		}
		if nextMin < 7 {
			return nextMin
		}
	}
	return minLevel
}

func countSectionsAtLevel(sections []SectionNode, level int) int {
	count := 0
	for _, section := range sections {
		if section.Level == level {
			count++
		}
	}
	return count
}
