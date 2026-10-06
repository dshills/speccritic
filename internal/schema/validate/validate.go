package validate

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/dshills/speccritic/internal/evidence"
	"github.com/dshills/speccritic/internal/schema"
)

var (
	issueIDPattern    = regexp.MustCompile(`^ISSUE-\d{4}$`)
	questionIDPattern = regexp.MustCompile(`^Q-\d{4}$`)
)

// Parse strips markdown fences, unmarshals JSON, and validates the structure
// of an LLM response. lineCount is the number of lines in the spec file and
// is used to validate evidence bounds.
func Parse(raw string, lineCount int) (*schema.Report, error) {
	cleaned := stripFences(raw)

	var report schema.Report
	if err := json.Unmarshal([]byte(cleaned), &report); err != nil {
		return nil, fmt.Errorf("JSON parse failed: %w", err)
	}

	if err := validateReport(&report, lineCount); err != nil {
		return nil, err
	}

	return &report, nil
}

// Options controls how a model response is checked.
type Options struct {
	// LineCount is the number of lines in the spec; evidence must stay inside it.
	LineCount int
	// SpecPath, when set, replaces the path on every evidence entry. The caller
	// knows which file was reviewed, so the model's copy is not used. The path
	// is spelled by schema.EvidencePath, so evidence paths always pass the
	// check Parse applies.
	SpecPath string
	// SpecText, when set, is the spec as the model was shown it. Every quote
	// is then looked up in it: evidence is moved to where its quote really is,
	// quotes are replaced by the spec's exact text, an issue whose quotes are
	// nowhere in the spec is tagged and cannot stay CRITICAL, and an issue with
	// no evidence at all is dropped. See package evidence.
	SpecText string
	// CheckIssue and CheckQuestion apply caller-specific rules to a finding
	// that passed the general checks, and may adjust it. They run after
	// evidence has been checked, so they see corrected line ranges. A non-nil
	// error drops the finding.
	CheckIssue    func(*schema.Issue) error
	CheckQuestion func(*schema.Question) error
	// ExtraFields names top-level fields beyond issues, questions and patches
	// that the caller wants. Each one found whole is returned in Result.Extra.
	ExtraFields []string
}

// Result is the usable part of a model response.
type Result struct {
	Report *schema.Report
	// Incomplete is non-nil when the JSON ended early or broke mid-document.
	// Report then holds only the findings that were read before that point.
	Incomplete error
	// Dropped explains each finding that failed validation and was left out.
	Dropped []string
	// Extra holds the fields named in Options.ExtraFields that were read whole.
	Extra map[string]json.RawMessage
}

// ParseResponse reads a model response one finding at a time. A finding that
// fails validation is dropped and the rest are kept, and a response cut off at
// the output cap still yields the findings that arrived whole. It returns an
// error only when the response holds nothing usable.
//
// IDs are returned as the model wrote them, so they may be malformed or
// repeated; llm.CompleteReport renumbers them. Patches are not matched to
// issues here, because a patch in a continued response may point at an issue
// from an earlier part.
//
// Parse, by contrast, needs the whole document to be valid.
func ParseResponse(raw string, opts Options) (Result, error) {
	opts.SpecPath = schema.EvidencePath(opts.SpecPath)
	c := &collector{opts: opts, report: &schema.Report{}}
	if opts.SpecText != "" {
		c.index = evidence.NewIndex(opts.SpecText)
	}
	incomplete, err := decodeFindings(stripFences(raw), c)
	if err != nil {
		return Result{}, err
	}
	c.report.Meta.DroppedFindings = len(c.dropped)
	if len(c.dropped) > 0 && len(c.report.Issues)+len(c.report.Questions) == 0 {
		return Result{}, fmt.Errorf("no usable findings, %d dropped: %s", len(c.dropped), c.dropped[0])
	}
	return Result{Report: c.report, Incomplete: incomplete, Dropped: c.dropped, Extra: c.extra}, nil
}

// collector gathers the findings of one response that pass validation.
type collector struct {
	opts Options
	// index is set when evidence is to be checked against the spec.
	index   *evidence.Index
	extra   map[string]json.RawMessage
	report  *schema.Report
	dropped []string
}

func (c *collector) issue(idx int, issue schema.Issue) {
	prefix := fmt.Sprintf("issue[%d]", idx)
	c.stampPath(issue.Evidence)
	err := validateIssueContent(issue, prefix, c.opts.LineCount)
	if err == nil && c.index != nil {
		if len(issue.Evidence) == 0 {
			err = fmt.Errorf("%s: no evidence", prefix)
		} else {
			c.index.CheckIssue(&issue)
		}
	}
	if err == nil && c.opts.CheckIssue != nil {
		if checkErr := c.opts.CheckIssue(&issue); checkErr != nil {
			err = fmt.Errorf("%s: %w", prefix, checkErr)
		}
	}
	if err != nil {
		c.dropped = append(c.dropped, err.Error())
		return
	}
	c.report.Issues = append(c.report.Issues, issue)
}

func (c *collector) question(idx int, question schema.Question) {
	prefix := fmt.Sprintf("question[%d]", idx)
	c.stampPath(question.Evidence)
	err := validateQuestionContent(question, prefix, c.opts.LineCount)
	if err == nil && c.index != nil {
		c.index.CheckQuestion(&question)
	}
	if err == nil && c.opts.CheckQuestion != nil {
		if checkErr := c.opts.CheckQuestion(&question); checkErr != nil {
			err = fmt.Errorf("%s: %w", prefix, checkErr)
		}
	}
	if err != nil {
		c.dropped = append(c.dropped, err.Error())
		return
	}
	c.report.Questions = append(c.report.Questions, question)
}

// patch keeps a patch that names an issue and an edit. Patches are advisory,
// so an unusable one is left out without counting as a dropped finding.
func (c *collector) patch(patch schema.Patch) {
	if strings.TrimSpace(patch.IssueID) == "" || strings.TrimSpace(patch.Before) == "" || patch.After == "" {
		return
	}
	c.report.Patches = append(c.report.Patches, patch)
}

func (c *collector) stampPath(evidence []schema.Evidence) {
	if c.opts.SpecPath == "" {
		return
	}
	for i := range evidence {
		evidence[i].Path = c.opts.SpecPath
	}
}

// wrongType handles a decode error for one element. JSON that was well formed
// but of the wrong type leaves the decoder usable, so the element is skipped
// (and recorded under label, if one is given) and decoding goes on. Any other
// error is returned.
func (c *collector) wrongType(label string, err error) error {
	var typeErr *json.UnmarshalTypeError
	if !errors.As(err, &typeErr) {
		return err
	}
	if label != "" {
		c.dropped = append(c.dropped, fmt.Sprintf("%s: %s", label, err))
	}
	return nil
}

// shapeError marks JSON that is well formed but is not shaped like a report.
type shapeError struct{ msg string }

func (e *shapeError) Error() string { return e.msg }

// decodeFindings feeds c every complete issue, question and patch the document
// holds. incomplete reports why reading stopped early; err reports JSON that
// was read whole but is not shaped like a report.
func decodeFindings(cleaned string, c *collector) (incomplete, err error) {
	dec := json.NewDecoder(strings.NewReader(cleaned))
	if err := expectDelim(dec, '{'); err != nil {
		return classifyDecodeError(err)
	}
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return classifyDecodeError(err)
		}
		key, _ := tok.(string)
		switch key {
		case "issues":
			err = decodeArray(dec, func(idx int) error {
				var issue schema.Issue
				if err := dec.Decode(&issue); err != nil {
					return c.wrongType(fmt.Sprintf("issue[%d]", idx), err)
				}
				c.issue(idx, issue)
				return nil
			})
		case "questions":
			err = decodeArray(dec, func(idx int) error {
				var question schema.Question
				if err := dec.Decode(&question); err != nil {
					return c.wrongType(fmt.Sprintf("question[%d]", idx), err)
				}
				c.question(idx, question)
				return nil
			})
		case "patches":
			err = decodeArray(dec, func(int) error {
				var patch schema.Patch
				if err := dec.Decode(&patch); err != nil {
					return c.wrongType("", err)
				}
				c.patch(patch)
				return nil
			})
		default:
			var raw json.RawMessage
			err = dec.Decode(&raw)
			if err == nil && slices.Contains(c.opts.ExtraFields, key) {
				if c.extra == nil {
					c.extra = map[string]json.RawMessage{}
				}
				c.extra[key] = raw
			}
		}
		if err != nil {
			return classifyDecodeError(err)
		}
	}
	if err := expectDelim(dec, '}'); err != nil {
		return classifyDecodeError(err)
	}
	// Text after the report is ignored, but a second JSON value is not: its
	// findings would be lost without anyone noticing.
	rest := strings.TrimSpace(cleaned[dec.InputOffset():])
	if strings.HasPrefix(rest, "{") || strings.HasPrefix(rest, "[") {
		return nil, fmt.Errorf("JSON parse failed: unexpected second JSON value after the report")
	}
	return nil, nil
}

// classifyDecodeError separates JSON with the wrong shape, which is an error,
// from a document that stopped or broke partway, which is merely incomplete.
func classifyDecodeError(err error) (incomplete, fatal error) {
	var shapeErr *shapeError
	if errors.As(err, &shapeErr) {
		return nil, fmt.Errorf("JSON parse failed: %w", err)
	}
	if errors.Is(err, io.EOF) {
		// A document that simply stops is an unexpected EOF whichever token it
		// stopped on.
		err = io.ErrUnexpectedEOF
	}
	return fmt.Errorf("JSON parse failed: %w", err), nil
}

// decodeArray calls elem with the index of each element of the JSON array at
// the decoder's position. A JSON null is treated as an empty array.
func decodeArray(dec *json.Decoder, elem func(idx int) error) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if tok == nil {
		return nil
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '[' {
		return &shapeError{msg: fmt.Sprintf("expected an array, got %v", tok)}
	}
	for idx := 0; dec.More(); idx++ {
		if err := elem(idx); err != nil {
			return err
		}
	}
	return expectDelim(dec, ']')
}

func expectDelim(dec *json.Decoder, want json.Delim) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != want {
		return &shapeError{msg: fmt.Sprintf("expected %q, got %v", want, tok)}
	}
	return nil
}

// stripFences removes leading/trailing markdown code fences (```json ... ``` or ``` ... ```).
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		idx := strings.Index(s, "\n")
		if idx >= 0 {
			// Normal case: fence opener on its own line.
			s = s[idx+1:]
		} else {
			// Fence with no newline (e.g. ```{...}```): strip up to the first { or [.
			rest := s[3:]
			if i := strings.IndexAny(rest, "{["); i >= 0 {
				s = rest[i:]
			}
		}
	}
	if strings.HasSuffix(s, "```") {
		idx := strings.LastIndex(s, "\n```")
		if idx >= 0 {
			s = s[:idx]
		}
	}
	return strings.TrimSpace(s)
}

func validateReport(r *schema.Report, lineCount int) error {
	seenIssueIDs, err := validateFindings(r, lineCount)
	if err != nil {
		return err
	}
	for i, patch := range r.Patches {
		if err := validatePatch(patch, i, seenIssueIDs); err != nil {
			return err
		}
	}
	if err := validateMeta(r.Meta); err != nil {
		return err
	}
	return nil
}

// validateFindings checks every issue and question and returns the issue IDs
// it saw.
func validateFindings(r *schema.Report, lineCount int) (map[string]bool, error) {
	seenIssueIDs := make(map[string]bool, len(r.Issues))
	for i, issue := range r.Issues {
		if err := validateIssue(issue, i, lineCount); err != nil {
			return nil, err
		}
		if seenIssueIDs[issue.ID] {
			return nil, fmt.Errorf("duplicate issue ID %q", issue.ID)
		}
		seenIssueIDs[issue.ID] = true
	}
	seenQuestionIDs := make(map[string]bool, len(r.Questions))
	for i, q := range r.Questions {
		if err := validateQuestion(q, i, lineCount); err != nil {
			return nil, err
		}
		if seenQuestionIDs[q.ID] {
			return nil, fmt.Errorf("duplicate question ID %q", q.ID)
		}
		seenQuestionIDs[q.ID] = true
	}
	return seenIssueIDs, nil
}

func validateMeta(meta schema.Meta) error {
	if meta.Completion != nil {
		switch meta.Completion.Mode {
		case schema.CompletionModeAuto, schema.CompletionModeOn, schema.CompletionModeOff:
		default:
			return fmt.Errorf("meta.completion.mode %q must be auto, on, or off", meta.Completion.Mode)
		}
		if !schema.IsCompletionTemplateName(meta.Completion.Template) {
			return fmt.Errorf("meta.completion.template %q must be one of %s", meta.Completion.Template, strings.Join(schema.CompletionTemplateNames(), ", "))
		}
		if meta.Completion.GeneratedPatches < 0 {
			return fmt.Errorf("meta.completion.generated_patches must be >= 0, got %d", meta.Completion.GeneratedPatches)
		}
		if meta.Completion.SkippedSuggestions < 0 {
			return fmt.Errorf("meta.completion.skipped_suggestions must be >= 0, got %d", meta.Completion.SkippedSuggestions)
		}
		if meta.Completion.OpenDecisions < 0 {
			return fmt.Errorf("meta.completion.open_decisions must be >= 0, got %d", meta.Completion.OpenDecisions)
		}
	}
	if meta.Convergence == nil {
		return nil
	}
	switch meta.Convergence.Mode {
	case "", schema.ConvergenceModeAuto, schema.ConvergenceModeOn, schema.ConvergenceModeOff:
	default:
		return fmt.Errorf("meta.convergence.mode %q must be auto, on, or off", meta.Convergence.Mode)
	}
	switch meta.Convergence.Status {
	case schema.ConvergenceStatusComplete, schema.ConvergenceStatusPartial, schema.ConvergenceStatusUnavailable:
	default:
		return fmt.Errorf("meta.convergence.status %q must be complete, partial, or unavailable", meta.Convergence.Status)
	}
	return nil
}

func validateIssue(issue schema.Issue, idx int, lineCount int) error {
	prefix := fmt.Sprintf("issue[%d]", idx)

	if !issueIDPattern.MatchString(issue.ID) {
		return fmt.Errorf("%s: id %q does not match ISSUE-XXXX format", prefix, issue.ID)
	}
	return validateIssueContent(issue, prefix, lineCount)
}

// validateIssueContent checks everything about an issue except its ID.
func validateIssueContent(issue schema.Issue, prefix string, lineCount int) error {
	if err := validateIssueFields(issue, prefix); err != nil {
		return err
	}
	for j, ev := range issue.Evidence {
		if err := validateEvidence(ev, fmt.Sprintf("%s.evidence[%d]", prefix, j), lineCount); err != nil {
			return err
		}
	}
	return nil
}

// validateIssueFields checks an issue's own fields, leaving out its ID and
// evidence.
func validateIssueFields(issue schema.Issue, prefix string) error {
	if err := validateSeverity(issue.Severity, prefix); err != nil {
		return err
	}
	if !schema.IsValidCategory(issue.Category) {
		return fmt.Errorf("%s: unknown category %q", prefix, issue.Category)
	}
	if issue.Title == "" {
		return fmt.Errorf("%s: title is required", prefix)
	}
	return nil
}

func validateQuestion(q schema.Question, idx int, lineCount int) error {
	prefix := fmt.Sprintf("question[%d]", idx)

	if !questionIDPattern.MatchString(q.ID) {
		return fmt.Errorf("%s: id %q does not match Q-XXXX format", prefix, q.ID)
	}
	return validateQuestionContent(q, prefix, lineCount)
}

// validateQuestionContent checks everything about a question except its ID.
func validateQuestionContent(q schema.Question, prefix string, lineCount int) error {
	if err := validateQuestionFields(q, prefix); err != nil {
		return err
	}
	for j, ev := range q.Evidence {
		if err := validateEvidence(ev, fmt.Sprintf("%s.evidence[%d]", prefix, j), lineCount); err != nil {
			return err
		}
	}
	return nil
}

// validateQuestionFields checks a question's own fields, leaving out its ID
// and evidence.
func validateQuestionFields(q schema.Question, prefix string) error {
	if err := validateSeverity(q.Severity, prefix); err != nil {
		return err
	}
	if q.Question == "" {
		return fmt.Errorf("%s: question text is required", prefix)
	}
	return nil
}

func validatePatch(patch schema.Patch, idx int, seenIssueIDs map[string]bool) error {
	if err := validatePatchShape(patch, idx); err != nil {
		return err
	}
	if !seenIssueIDs[patch.IssueID] {
		return fmt.Errorf("patch[%d]: issue_id %q does not reference a current issue", idx, patch.IssueID)
	}
	return nil
}

func validatePatchShape(patch schema.Patch, idx int) error {
	prefix := fmt.Sprintf("patch[%d]", idx)
	if !issueIDPattern.MatchString(patch.IssueID) {
		return fmt.Errorf("%s: issue_id %q does not match ISSUE-XXXX format", prefix, patch.IssueID)
	}
	if strings.TrimSpace(patch.Before) == "" {
		return fmt.Errorf("%s: before is required", prefix)
	}
	if patch.After == "" {
		return fmt.Errorf("%s: after is required", prefix)
	}
	return nil
}

func validateSeverity(s schema.Severity, prefix string) error {
	switch s {
	case schema.SeverityInfo, schema.SeverityWarn, schema.SeverityCritical:
		return nil
	}
	return fmt.Errorf("%s: invalid severity %q (must be INFO, WARN, or CRITICAL)", prefix, s)
}

func validateEvidence(ev schema.Evidence, prefix string, lineCount int) error {
	if err := validateEvidenceLines(ev, prefix, lineCount); err != nil {
		return err
	}
	if ev.Path != "" && !filepath.IsLocal(ev.Path) {
		return fmt.Errorf("%s: path %q must be a local relative path", prefix, ev.Path)
	}
	return nil
}

// validateEvidenceLines checks the line range of ev. A lineCount of 0 means
// the length of the spec is not known.
func validateEvidenceLines(ev schema.Evidence, prefix string, lineCount int) error {
	if ev.LineStart < 1 {
		return fmt.Errorf("%s: line_start %d must be ≥ 1", prefix, ev.LineStart)
	}
	if ev.LineEnd < ev.LineStart {
		return fmt.Errorf("%s: line_end %d must be ≥ line_start %d", prefix, ev.LineEnd, ev.LineStart)
	}
	if lineCount > 0 && ev.LineEnd > lineCount {
		return fmt.Errorf("%s: line_end %d exceeds spec line count %d", prefix, ev.LineEnd, lineCount)
	}
	return nil
}
