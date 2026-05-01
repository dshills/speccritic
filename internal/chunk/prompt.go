package chunk

import (
	"fmt"
	"strings"

	ctxpkg "github.com/dshills/speccritic/internal/context"
	"github.com/dshills/speccritic/internal/spec"
)

const maxGlossaryLines = 80

type PromptInput struct {
	Spec             *spec.Spec
	Plan             Plan
	Chunk            Chunk
	ContextFiles     []ctxpkg.ContextFile
	PreflightContext string
}

func BuildUserPrompt(input PromptInput) (cachedPrefix, variable string, err error) {
	if input.Spec == nil {
		return "", "", fmt.Errorf("spec is required")
	}
	if input.Chunk.ID == "" {
		return "", "", fmt.Errorf("chunk is required")
	}
	lines := spec.Lines(input.Spec.Raw)
	if input.Chunk.LineStart < 1 || input.Chunk.LineEnd < input.Chunk.LineStart || input.Chunk.LineEnd > len(lines) {
		return "", "", fmt.Errorf("chunk %s has invalid primary range %d-%d", input.Chunk.ID, input.Chunk.LineStart, input.Chunk.LineEnd)
	}
	if input.Chunk.ContextFrom < 1 {
		input.Chunk.ContextFrom = input.Chunk.LineStart
	}
	if input.Chunk.ContextTo < input.Chunk.ContextFrom || input.Chunk.ContextTo > len(lines) {
		input.Chunk.ContextTo = input.Chunk.LineEnd
	}

	var prefix strings.Builder
	prefix.WriteString("Analyze one section chunk of the following specification.\n")
	prefix.WriteString("Return JSON matching the SpecCritic schema. Do not return prose or markdown fences.\n")
	prefix.WriteString("Review only the primary range for defects. Use context-only lines only to interpret the primary range.\n")
	prefix.WriteString("Cite only primary-range line numbers. Add tag \"chunk:<CHUNK-ID>\" using the chunk id from the chunk metadata to every issue. Add tag \"cross-section\" when a finding depends on another section.\n")
	prefix.WriteString("Do not emit score or verdict. Emit meta.chunk_summary as a <=600 character summary of the primary range.\n")
	if len(input.ContextFiles) > 0 {
		prefix.WriteString("\n")
		prefix.WriteString(ctxpkg.FormatForPrompt(input.ContextFiles))
	}
	if input.PreflightContext != "" {
		prefix.WriteString("\n")
		prefix.WriteString(input.PreflightContext)
		if !strings.HasSuffix(input.PreflightContext, "\n") {
			prefix.WriteString("\n")
		}
	}
	prefix.WriteString("\n<spec_table_of_contents>\n")
	prefix.WriteString(TableOfContents(input.Plan))
	prefix.WriteString("</spec_table_of_contents>\n")
	if glossary := glossaryContext(lines, input.Plan); glossary != "" {
		prefix.WriteString("\n<global_definitions_context>\n")
		prefix.WriteString(glossary)
		prefix.WriteString("</global_definitions_context>\n")
	}

	var tail strings.Builder
	fmt.Fprintf(&tail, "\n<chunk id=%q file=%q primary_range=\"L%d-L%d\">\n", input.Chunk.ID, input.Chunk.Path, input.Chunk.LineStart, input.Chunk.LineEnd)
	fmt.Fprintf(&tail, "<chunk_issue_tag>chunk:%s</chunk_issue_tag>\n", input.Chunk.ID)
	if len(input.Chunk.HeadingPath) > 0 {
		fmt.Fprintf(&tail, "<heading_path>%s</heading_path>\n", strings.Join(input.Chunk.HeadingPath, " > "))
	}
	if input.Chunk.ContextFrom < input.Chunk.LineStart {
		tail.WriteString("<context_only_before>\n")
		tail.WriteString(formatRange(lines, input.Chunk.ContextFrom, input.Chunk.LineStart-1))
		tail.WriteString("\n</context_only_before>\n")
	}
	tail.WriteString("<primary_lines>\n")
	tail.WriteString(formatRange(lines, input.Chunk.LineStart, input.Chunk.LineEnd))
	tail.WriteString("\n</primary_lines>\n")
	if input.Chunk.ContextTo > input.Chunk.LineEnd {
		tail.WriteString("<context_only_after>\n")
		tail.WriteString(formatRange(lines, input.Chunk.LineEnd+1, input.Chunk.ContextTo))
		tail.WriteString("\n</context_only_after>\n")
	}
	tail.WriteString("</chunk>\n")
	return prefix.String(), tail.String(), nil
}

func TableOfContents(plan Plan) string {
	var b strings.Builder
	for _, heading := range plan.Headings {
		end := plan.LineCount
		for _, section := range plan.Sections {
			if section.LineStart == heading.Line {
				end = section.LineEnd
				break
			}
		}
		fmt.Fprintf(&b, "L%d-L%d %s%s\n", heading.Line, end, strings.Repeat("#", heading.Level), heading.Text)
	}
	return b.String()
}

func glossaryContext(lines []string, plan Plan) string {
	var b strings.Builder
	written := 0
	for _, section := range plan.Sections {
		if len(section.HeadingPath) == 0 || !isDefinitionsHeading(section.HeadingPath[len(section.HeadingPath)-1]) {
			continue
		}
		sectionLines := section.LineEnd - section.LineStart + 1
		if written+sectionLines > maxGlossaryLines {
			fmt.Fprintf(&b, "L%d-L%d %s\n", section.LineStart, section.LineEnd, strings.Join(section.HeadingPath, " > "))
			continue
		}
		b.WriteString(formatRange(lines, section.LineStart, section.LineEnd))
		b.WriteByte('\n')
		written += sectionLines
	}
	return b.String()
}

func isDefinitionsHeading(value string) bool {
	normalized := strings.ToLower(value)
	return strings.Contains(normalized, "glossary") || strings.Contains(normalized, "definition")
}

func formatRange(lines []string, start, end int) string {
	var b strings.Builder
	for line := start; line <= end; line++ {
		if line > start {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "L%d: %s", line, lines[line-1])
	}
	return b.String()
}
