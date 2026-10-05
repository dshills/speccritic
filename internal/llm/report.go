package llm

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/dshills/speccritic/internal/schema"
)

// maxContinuations bounds how many times a cut-off response is extended
// before the call is reported as failed.
const maxContinuations = 3

// Parsed is what a parser recovered from one model response.
type Parsed struct {
	Report *schema.Report
	// Incomplete is non-nil when the JSON ended early or broke mid-document.
	// Report then holds only the findings that were read before that point.
	Incomplete error
}

// ReportCall describes a model call that must return a findings report.
type ReportCall struct {
	Request *Request
	// Label prefixes error messages, e.g. "chunk CHUNK-0001-L1-L80 ".
	Label string
	// Parse validates one response. It returns an error when the response
	// holds nothing usable.
	Parse func(raw string) (Parsed, error)
	// RepairPrompt returns the text appended to the user prompt when a
	// response has to be regenerated from scratch.
	RepairPrompt func(reason error, failedOutput string) string
	// Logf, when set, receives progress messages.
	Logf func(format string, args ...any)
}

// CompleteReport runs call and returns the parsed report and the model that
// produced it.
//
// A response that is cut off after at least one complete finding is extended:
// the findings already received are kept and the model is asked only for the
// rest. A response with nothing usable is regenerated once with the repair
// prompt.
func CompleteReport(ctx context.Context, provider Provider, call ReportCall) (*schema.Report, string, error) {
	resp, err := provider.Complete(ctx, call.Request)
	if err != nil {
		return nil, "", fmt.Errorf("%sLLM call failed: %w", call.Label, err)
	}
	parsed, parseErr := call.Parse(resp.Content)
	if parseErr == nil {
		if parsed.Incomplete == nil {
			return parsed.Report, resp.Model, nil
		}
		if findingCount(parsed.Report) > 0 {
			return continueReport(ctx, provider, call, call.Request.MaxTokens, parsed.Report)
		}
	}

	reason := parseErr
	if reason == nil {
		reason = parsed.Incomplete
	}
	call.logf("Validation failed, retrying: %s", reason)
	repairReq := *call.Request
	if resp.Truncated || parsed.Incomplete != nil || IncompleteJSON(parseErr) {
		repairReq.MaxTokens = RepairMaxTokens(call.Request.MaxTokens)
	}
	repairReq.UserPrompt = call.Request.UserPrompt + call.RepairPrompt(reason, resp.Content)
	resp, err = provider.Complete(ctx, &repairReq)
	if err != nil {
		return nil, "", fmt.Errorf("%sLLM repair call failed: %w", call.Label, err)
	}
	parsed, parseErr = call.Parse(resp.Content)
	switch {
	case parseErr != nil:
		return nil, "", fmt.Errorf("%sinvalid model output after retry: %w", call.Label, parseErr)
	case parsed.Incomplete == nil:
		return parsed.Report, resp.Model, nil
	case findingCount(parsed.Report) == 0:
		return nil, "", fmt.Errorf("%sinvalid model output after retry: %w", call.Label, parsed.Incomplete)
	}
	return continueReport(ctx, provider, call, repairReq.MaxTokens, parsed.Report)
}

// continueReport asks for the findings that follow first until a response
// arrives whole, keeping everything received along the way. maxTokens is the
// cap of the request that produced first, so a budget raised for a repair
// carries over.
func continueReport(ctx context.Context, provider Provider, call ReportCall, maxTokens int, first *schema.Report) (*schema.Report, string, error) {
	acc := &schema.Report{}
	appendPart(acc, first)
	for range maxContinuations {
		call.logf("Response cut off after %d finding(s), requesting the rest", findingCount(acc))
		contReq := *call.Request
		contReq.MaxTokens = maxTokens
		contReq.UserPrompt = call.Request.UserPrompt + continuationPrompt(acc)
		resp, err := provider.Complete(ctx, &contReq)
		if err != nil {
			return nil, "", fmt.Errorf("%sLLM continuation call failed: %w", call.Label, err)
		}
		parsed, parseErr := call.Parse(resp.Content)
		if parseErr != nil {
			return nil, "", fmt.Errorf("%sinvalid model output in continuation: %w", call.Label, parseErr)
		}
		before := findingCount(acc) + len(acc.Patches)
		appendPart(acc, parsed.Report)
		if parsed.Incomplete == nil {
			return acc, resp.Model, nil
		}
		// A cut-off part that held only patches still moved things forward.
		if findingCount(acc)+len(acc.Patches) == before {
			return nil, "", fmt.Errorf("%scontinuation added nothing new: %w", call.Label, parsed.Incomplete)
		}
	}
	return nil, "", fmt.Errorf("%sresponse still incomplete after %d continuation calls; raise the max tokens setting", call.Label, maxContinuations)
}

// appendPart adds part's findings to acc, renumbering them to follow the
// findings acc already holds. Findings and patches the model repeated are
// skipped, and patches are re-pointed at the renumbered issues.
func appendPart(acc, part *schema.Report) {
	if part == nil {
		return
	}
	known := make(map[string]bool, len(acc.Issues))
	for _, issue := range acc.Issues {
		known[issue.ID] = true
	}
	renumbered := make(map[string]string, len(part.Issues))
	for _, issue := range part.Issues {
		if idx := repeatedIssue(acc.Issues, issue); idx >= 0 {
			renumbered[issue.ID] = acc.Issues[idx].ID
			continue
		}
		id := fmt.Sprintf("ISSUE-%04d", len(acc.Issues)+1)
		renumbered[issue.ID] = id
		issue.ID = id
		acc.Issues = append(acc.Issues, issue)
	}
	for _, question := range part.Questions {
		if repeatedQuestion(acc.Questions, question) {
			continue
		}
		question.ID = fmt.Sprintf("Q-%04d", len(acc.Questions)+1)
		acc.Questions = append(acc.Questions, question)
	}
	for _, patch := range part.Patches {
		// An ID from this part wins over an identical one from an earlier
		// part: a model that restarts its numbering means its own issue.
		if id, ok := renumbered[patch.IssueID]; ok {
			patch.IssueID = id
		} else if !known[patch.IssueID] {
			continue
		}
		if !slices.Contains(acc.Patches, patch) {
			acc.Patches = append(acc.Patches, patch)
		}
	}
	if acc.Meta.ChunkSummary == "" {
		acc.Meta.ChunkSummary = part.Meta.ChunkSummary
	}
}

func repeatedIssue(existing []schema.Issue, issue schema.Issue) int {
	for i, candidate := range existing {
		if candidate.Category == issue.Category &&
			normalizeText(candidate.Title) == normalizeText(issue.Title) &&
			evidenceOverlaps(candidate.Evidence, issue.Evidence) {
			return i
		}
	}
	return -1
}

func repeatedQuestion(existing []schema.Question, question schema.Question) bool {
	for _, candidate := range existing {
		if normalizeText(candidate.Question) == normalizeText(question.Question) {
			return true
		}
	}
	return false
}

func evidenceOverlaps(a, b []schema.Evidence) bool {
	for _, left := range a {
		for _, right := range b {
			if left.LineStart <= right.LineEnd && right.LineStart <= left.LineEnd {
				return true
			}
		}
	}
	return false
}

func normalizeText(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

// continuationPrompt lists what has been received so the model can pick up
// where the cut-off response stopped.
func continuationPrompt(acc *schema.Report) string {
	var b strings.Builder
	b.WriteString("\n\nYour previous response was cut off before the JSON was complete. These findings were received and are already recorded:\n")
	b.WriteString("<received_findings>\n")
	for _, issue := range acc.Issues {
		fmt.Fprintf(&b, "- %s %s %s %s: %s\n", issue.ID, issue.Severity, issue.Category, lineLabel(issue.Evidence), compactText(issue.Title, 120))
	}
	for _, question := range acc.Questions {
		fmt.Fprintf(&b, "- %s %s %s: %s\n", question.ID, question.Severity, lineLabel(question.Evidence), compactText(question.Question, 120))
	}
	b.WriteString("</received_findings>\n")
	b.WriteString("Do not repeat them. Return one JSON object in the same schema holding only the remaining issues, questions and patches. Patches may reference the issue IDs above.\n")
	fmt.Fprintf(&b, "Number new issues from ISSUE-%04d and new questions from Q-%04d. If nothing remains, return empty arrays.", len(acc.Issues)+1, len(acc.Questions)+1)
	return b.String()
}

func lineLabel(evidence []schema.Evidence) string {
	if len(evidence) == 0 {
		return "L?"
	}
	if evidence[0].LineEnd > evidence[0].LineStart {
		return fmt.Sprintf("L%d-L%d", evidence[0].LineStart, evidence[0].LineEnd)
	}
	return fmt.Sprintf("L%d", evidence[0].LineStart)
}

func compactText(s string, limit int) string {
	s = strings.Join(strings.Fields(s), " ")
	return truncate(s, limit)
}

func findingCount(report *schema.Report) int {
	if report == nil {
		return 0
	}
	return len(report.Issues) + len(report.Questions)
}

func (c ReportCall) logf(format string, args ...any) {
	if c.Logf != nil {
		c.Logf(format, args...)
	}
}
