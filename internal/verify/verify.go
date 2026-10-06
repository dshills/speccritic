// Package verify gives every CRITICAL finding a second look before it can
// decide a verdict.
//
// One CRITICAL makes a spec INVALID and, under --fail-on, fails the caller's
// gate, so a false CRITICAL is the most expensive mistake a review can make.
// After the review, one more call puts every CRITICAL finding the model
// produced to the model again, against the same cached spec. Each is
// confirmed, downgraded, or rejected with the spec text that shows it is
// wrong. A rejection is applied only when that text is in the spec. When the
// call fails, the findings stand as they were.
package verify

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/dshills/speccritic/internal/evidence"
	"github.com/dshills/speccritic/internal/llm"
	"github.com/dshills/speccritic/internal/schema"
	"github.com/dshills/speccritic/internal/schema/validate"
)

// Tags recording the outcome on an issue.
const (
	// TagConfirmed marks a CRITICAL issue the second look upheld. A reused
	// issue that carries it is not checked again.
	TagConfirmed = "critical-confirmed"
	// TagDowngraded marks an issue the second look lowered from CRITICAL.
	TagDowngraded = "critical-downgraded"
)

// tagPreflight marks a finding from the deterministic preflight rules, which
// need no second look. It matches preflight.TagPreflight.
const tagPreflight = "preflight"

// maxCandidates bounds how many findings one call checks. Any beyond it keep
// their severity and are counted as unchecked.
const maxCandidates = 50

// maxDescription bounds the description shown for each finding.
const maxDescription = 400

// Decisions a verdict may carry.
const (
	decisionConfirm   = "confirm"
	decisionDowngrade = "downgrade"
	decisionReject    = "reject"
)

// Input is what a verification call needs.
type Input struct {
	Provider llm.Provider
	// SystemPrompt and Prefix are the ones the review used, so the call reads
	// the spec from the provider's cache.
	SystemPrompt  string
	Prefix        string
	LongCache     bool
	MaxTokens     int
	Effort        string
	EnforceSchema bool
	// Strict forbids downgrades: in strict mode any required assumption is
	// CRITICAL, so "an implementer could assume" is no reason to lower one.
	Strict bool
	// SpecText is the spec the model was shown, used to check the quote a
	// rejection gives. SpecPath is its evidence path.
	SpecText string
	SpecPath string
	// Logf, when set, receives progress messages.
	Logf func(format string, args ...any)
}

// Candidates reports whether report holds any finding that would be checked.
func Candidates(report *schema.Report) bool {
	for _, issue := range report.Issues {
		if isCandidateIssue(issue) {
			return true
		}
	}
	for _, question := range report.Questions {
		if question.Severity == schema.SeverityCritical {
			return true
		}
	}
	return false
}

func isCandidateIssue(issue schema.Issue) bool {
	return issue.Severity == schema.SeverityCritical &&
		!slices.Contains(issue.Tags, tagPreflight) &&
		!slices.Contains(issue.Tags, TagConfirmed)
}

// verdict is one decision returned by the model.
type verdict struct {
	ID        string          `json:"id"`
	Decision  string          `json:"decision"`
	Severity  schema.Severity `json:"severity"`
	LineStart int             `json:"line_start"`
	LineEnd   int             `json:"line_end"`
	Quote     string          `json:"quote"`
	Reason    string          `json:"reason"`
}

type response struct {
	Verdicts []verdict `json:"verdicts"`
}

// Run checks the CRITICAL findings in report and applies the outcome to it in
// place: severities change, rejected findings and their patches are removed,
// and report.Meta.Verification records what happened. The summary is not
// recomputed; the caller does that. Run returns no error: a failed call is
// recorded in the metadata and leaves the findings as they were.
func Run(ctx context.Context, report *schema.Report, in Input) {
	var issueIdx, questionIdx []int
	for i, issue := range report.Issues {
		if isCandidateIssue(issue) {
			issueIdx = append(issueIdx, i)
		}
	}
	for i, question := range report.Questions {
		if question.Severity == schema.SeverityCritical {
			questionIdx = append(questionIdx, i)
		}
	}
	total := len(issueIdx) + len(questionIdx)
	if total == 0 {
		return
	}
	meta := &schema.VerificationMeta{Status: schema.VerificationComplete}
	report.Meta.Verification = meta
	if total > maxCandidates {
		meta.Unchecked = total - maxCandidates
		if len(issueIdx) > maxCandidates {
			issueIdx = issueIdx[:maxCandidates]
		}
		questionIdx = questionIdx[:min(len(questionIdx), maxCandidates-len(issueIdx))]
	}
	meta.Checked = len(issueIdx) + len(questionIdx)

	// Candidates get IDs of their own so a question and an issue cannot be
	// confused and the model cannot name a finding it was not asked about.
	issueByKey := map[string]int{}
	questionByKey := map[string]int{}
	var b strings.Builder
	b.WriteString(task(in.Strict))
	b.WriteString("\n<critical_findings>\n")
	for n, i := range issueIdx {
		key := fmt.Sprintf("F%d", n+1)
		issueByKey[key] = i
		issue := report.Issues[i]
		fmt.Fprintf(&b, "- %s %s %s: %s\n  %s\n", key, issue.Category, lineLabel(issue.Evidence), oneLine(issue.Title, 0), oneLine(issue.Description, maxDescription))
	}
	for n, i := range questionIdx {
		key := fmt.Sprintf("F%d", len(issueIdx)+n+1)
		questionByKey[key] = i
		question := report.Questions[i]
		fmt.Fprintf(&b, "- %s QUESTION %s: %s\n  %s\n", key, lineLabel(question.Evidence), oneLine(question.Question, 0), oneLine(question.WhyNeeded, maxDescription))
	}
	b.WriteString("</critical_findings>\n")

	verdicts, err := call(ctx, in, b.String())
	if err != nil {
		meta.Status = schema.VerificationFailed
		meta.Error = err.Error()
		meta.Unchecked += meta.Checked
		meta.Checked = 0
		logf(in, "Verification of CRITICAL findings failed, keeping them as they are: %s", err)
		return
	}

	index := evidence.NewIndex(in.SpecText)
	answered := map[string]bool{}
	rejectedIssues := map[int]bool{}
	rejectedQuestions := map[int]bool{}
	for _, v := range verdicts {
		i, isIssue := issueByKey[v.ID]
		q, isQuestion := questionByKey[v.ID]
		if (!isIssue && !isQuestion) || answered[v.ID] {
			continue
		}
		switch v.Decision {
		case decisionConfirm, decisionDowngrade, decisionReject:
		default:
			// Not a decision this call offers; the finding counts as
			// unanswered and keeps its severity, untagged.
			continue
		}
		answered[v.ID] = true
		decision := v.Decision
		if decision == decisionDowngrade && (in.Strict || (v.Severity != schema.SeverityWarn && v.Severity != schema.SeverityInfo)) {
			decision = decisionConfirm
		}
		if decision == decisionReject {
			answer, outcome := index.Anchor(schema.Evidence{Path: in.SpecPath, LineStart: v.LineStart, LineEnd: v.LineEnd, Quote: v.Quote})
			if outcome != evidence.Verified && outcome != evidence.Moved {
				// The model could not point at text that shows the finding is
				// wrong, so the finding stands, unmarked: it was not confirmed
				// either, and a later run may check it again.
				meta.UnverifiedRejections++
				continue
			}
			if isIssue {
				issue := report.Issues[i]
				rejectedIssues[i] = true
				meta.Rejected = append(meta.Rejected, schema.RemovedFinding{Title: issue.Title, Severity: issue.Severity, Category: issue.Category, Reason: v.Reason, AnsweredBy: answer})
				continue
			}
			question := report.Questions[q]
			rejectedQuestions[q] = true
			meta.Rejected = append(meta.Rejected, schema.RemovedFinding{Title: question.Question, Severity: question.Severity, Reason: v.Reason, AnsweredBy: answer})
			continue
		}
		switch {
		case decision == decisionDowngrade && isIssue:
			issue := &report.Issues[i]
			issue.Severity = v.Severity
			issue.Blocking = false
			issue.Tags = appendTag(issue.Tags, TagDowngraded)
			meta.Downgraded++
		case decision == decisionDowngrade:
			report.Questions[q].Severity = v.Severity
			meta.Downgraded++
		case decision == decisionConfirm && isIssue:
			report.Issues[i].Tags = appendTag(report.Issues[i].Tags, TagConfirmed)
			meta.Confirmed++
		case decision == decisionConfirm:
			meta.Confirmed++
		}
	}
	meta.Unanswered = meta.Checked - len(answered)

	if len(rejectedIssues) > 0 {
		kept := report.Issues[:0:0]
		removedIDs := map[string]bool{}
		for i, issue := range report.Issues {
			if rejectedIssues[i] {
				removedIDs[issue.ID] = true
				continue
			}
			kept = append(kept, issue)
		}
		report.Issues = kept
		patches := report.Patches[:0:0]
		for _, patch := range report.Patches {
			if !removedIDs[patch.IssueID] {
				patches = append(patches, patch)
			}
		}
		report.Patches = patches
	}
	if len(rejectedQuestions) > 0 {
		kept := report.Questions[:0:0]
		for i, question := range report.Questions {
			if !rejectedQuestions[i] {
				kept = append(kept, question)
			}
		}
		report.Questions = kept
	}
	logf(in, "Verified %d CRITICAL finding(s): %d confirmed, %d downgraded, %d rejected, %d unanswered", meta.Checked, meta.Confirmed, meta.Downgraded, len(meta.Rejected), meta.Unanswered)
}

// task returns the instructions for the verification call. It follows the
// shared prefix, so the model reads the spec there.
func task(strict bool) string {
	var b strings.Builder
	b.WriteString("\nA reviewer marked the findings below CRITICAL: implementation cannot begin until each is resolved. Check each one against the specification above, as a second reviewer who has not seen the first one's reasoning.\n")
	b.WriteString("For each finding, decide:\n")
	b.WriteString("- confirm: the defect is real and does block implementation.\n")
	if strict {
		b.WriteString("- downgrade: not available. The review runs in strict mode, where any assumption an implementer would have to make is CRITICAL.\n")
	} else {
		b.WriteString("- downgrade: the defect is real, but an implementer could proceed on a low-risk assumption. Give severity WARN or INFO.\n")
	}
	b.WriteString("- reject: the finding is wrong, because the specification states what it says is missing or it misreads the text. Give the lines that show this and copy their text exactly in quote. A rejection whose quote is not in the specification is ignored.\n")
	b.WriteString("When unsure, confirm. Return one verdict per finding, using its F id.\n")
	return b.String()
}

// call makes the verification call, with one retry if the response cannot be
// read.
func call(ctx context.Context, in Input, tail string) ([]verdict, error) {
	req := &llm.Request{
		SystemPrompt:           in.SystemPrompt,
		UserPromptCachedPrefix: in.Prefix,
		UserPrompt:             tail,
		LongCache:              in.LongCache,
		MaxTokens:              in.MaxTokens,
		Effort:                 in.Effort,
		Schema:                 Schema(in.EnforceSchema),
	}
	resp, err := in.Provider.Complete(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("verification call failed: %w", err)
	}
	verdicts, parseErr := parse(resp.Content)
	if parseErr == nil {
		return verdicts, nil
	}
	repair := *req
	repair.Attempt = llm.AttemptRepair
	if resp.Truncated {
		repair.MaxTokens = llm.RepairMaxTokens(req.MaxTokens)
	}
	repair.UserPrompt = tail + fmt.Sprintf("\n\nYour previous response could not be read (%s). Return only JSON matching the schema.", parseErr)
	resp, err = in.Provider.Complete(ctx, &repair)
	if err != nil {
		return nil, fmt.Errorf("verification repair call failed: %w", err)
	}
	verdicts, parseErr = parse(resp.Content)
	if parseErr != nil {
		return nil, fmt.Errorf("verification response unreadable after retry: %w", parseErr)
	}
	return verdicts, nil
}

func parse(raw string) ([]verdict, error) {
	var r response
	dec := json.NewDecoder(strings.NewReader(validate.StripFences(raw)))
	if err := dec.Decode(&r); err != nil {
		return nil, err
	}
	if r.Verdicts == nil {
		return nil, fmt.Errorf("no verdicts field")
	}
	return r.Verdicts, nil
}

// schemaJSON is the shape of a verification response.
const schemaJSON = `{"type":"object","additionalProperties":false,"required":["verdicts"],"properties":{"verdicts":{"type":"array","items":{"type":"object","additionalProperties":false,"required":["id","reason","decision","severity","line_start","line_end","quote"],"properties":{"id":{"type":"string","description":"The F id of the finding."},"reason":{"type":"string","description":"Why, in one or two sentences."},"decision":{"type":"string","enum":["confirm","downgrade","reject"]},"severity":{"type":"string","enum":["CRITICAL","WARN","INFO"],"description":"CRITICAL to confirm, WARN or INFO to downgrade. Ignored for a rejection."},"line_start":{"type":"integer","description":"For a rejection, the first line that shows the finding is wrong. Otherwise 0."},"line_end":{"type":"integer","description":"For a rejection, the last such line. Otherwise 0."},"quote":{"type":"string","description":"For a rejection, the text of those lines copied exactly. Otherwise empty."}}}}}}`

const schemaExample = `{
  "verdicts": [
    {"id": "F1", "reason": "Nothing states the retry limit.", "decision": "confirm", "severity": "CRITICAL", "line_start": 0, "line_end": 0, "quote": ""},
    {"id": "F2", "reason": "Line 14 defines the term.", "decision": "reject", "severity": "CRITICAL", "line_start": 14, "line_end": 14, "quote": "exact text from spec"}
  ]
}`

// Schema returns the schema of a verification response.
func Schema(enforce bool) *llm.OutputSchema {
	return &llm.OutputSchema{
		Name:           "critical_verification",
		JSON:           json.RawMessage(schemaJSON),
		Enforce:        enforce,
		PromptFallback: "\n\nReturn your verdicts as JSON with this structure:\n" + schemaExample,
	}
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

func oneLine(text string, limit int) string {
	text = strings.Join(strings.Fields(text), " ")
	if limit > 0 {
		if runes := []rune(text); len(runes) > limit {
			return string(runes[:limit]) + "..."
		}
	}
	return text
}

func appendTag(tags []string, tag string) []string {
	if slices.Contains(tags, tag) {
		return tags
	}
	return append(append([]string(nil), tags...), tag)
}

func logf(in Input, format string, args ...any) {
	if in.Logf != nil {
		in.Logf(format, args...)
	}
}
