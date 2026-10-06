package verify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/dshills/speccritic/internal/llm"
	"github.com/dshills/speccritic/internal/schema"
)

const specText = "# Spec\nA tenant is one customer account.\nRetries are unlimited.\nThe cache is refreshed hourly.\n"

type scripted struct {
	replies []string
	err     error
	reqs    []llm.Request
}

func (p *scripted) Complete(_ context.Context, req *llm.Request) (*llm.Response, error) {
	p.reqs = append(p.reqs, *req)
	if p.err != nil {
		return nil, p.err
	}
	reply := p.replies[min(len(p.reqs), len(p.replies))-1]
	return &llm.Response{Content: reply, Model: "fake"}, nil
}

func critical(id, title string, line int, tags ...string) schema.Issue {
	return schema.Issue{
		ID: id, Severity: schema.SeverityCritical, Category: schema.CategoryUndefinedInterface, Title: title,
		Description: "d", Blocking: true, Tags: tags,
		Evidence: []schema.Evidence{{LineStart: line, LineEnd: line}},
	}
}

func verdictJSON(id, decision, severity string, line int, quote string) string {
	return fmt.Sprintf(`{"id":%q,"reason":"because","decision":%q,"severity":%q,"line_start":%d,"line_end":%d,"quote":%q}`, id, decision, severity, line, line, quote)
}

func reply(verdicts ...string) string {
	return `{"verdicts":[` + strings.Join(verdicts, ",") + `]}`
}

func input(p llm.Provider) Input {
	return Input{Provider: p, SystemPrompt: "sys", Prefix: "<spec>...</spec>", MaxTokens: 1000, EnforceSchema: true, SpecText: specText, SpecPath: "SPEC.md"}
}

func TestCandidates(t *testing.T) {
	cases := map[string]struct {
		report *schema.Report
		want   bool
	}{
		"model critical":     {&schema.Report{Issues: []schema.Issue{critical("ISSUE-0001", "t", 2)}}, true},
		"preflight critical": {&schema.Report{Issues: []schema.Issue{critical("P", "t", 1, "preflight")}}, false},
		"already confirmed":  {&schema.Report{Issues: []schema.Issue{critical("ISSUE-0001", "t", 2, TagConfirmed)}}, false},
		"warn only":          {&schema.Report{Issues: []schema.Issue{{Severity: schema.SeverityWarn}}}, false},
		"critical question":  {&schema.Report{Questions: []schema.Question{{Severity: schema.SeverityCritical}}}, true},
		"nothing":            {&schema.Report{}, false},
	}
	for name, tc := range cases {
		if got := Candidates(tc.report); got != tc.want {
			t.Errorf("%s: Candidates = %v, want %v", name, got, tc.want)
		}
	}
}

func TestRunAppliesVerdicts(t *testing.T) {
	report := &schema.Report{
		Issues: []schema.Issue{
			critical("ISSUE-0001", "Tenant undefined", 2),
			critical("ISSUE-0002", "Retry limit missing", 3),
			critical("ISSUE-0003", "Cache policy vague", 4),
			critical("ISSUE-0004", "Made-up rejection", 4),
			critical("ISSUE-0005", "Not answered", 1),
			critical("PREFLIGHT-X", "Missing section", 1, "preflight"),
			{ID: "ISSUE-0006", Severity: schema.SeverityWarn, Title: "warn"},
		},
		Questions: []schema.Question{
			{ID: "Q-0001", Severity: schema.SeverityCritical, Question: "What is a tenant?", Evidence: []schema.Evidence{{LineStart: 2, LineEnd: 2}}},
			{ID: "Q-0002", Severity: schema.SeverityCritical, Question: "How often is the cache refreshed?"},
		},
		Patches: []schema.Patch{
			{IssueID: "ISSUE-0001", Before: "x", After: "y"},
			{IssueID: "ISSUE-0002", Before: "x", After: "y"},
		},
	}
	p := &scripted{replies: []string{reply(
		verdictJSON("F1", "reject", "CRITICAL", 2, "A tenant is one customer account."),
		verdictJSON("F2", "confirm", "CRITICAL", 0, ""),
		verdictJSON("F2", "reject", "CRITICAL", 2, "A tenant is one customer account."), // repeat: ignored
		verdictJSON("F3", "downgrade", "WARN", 0, ""),
		verdictJSON("F4", "reject", "CRITICAL", 4, "The cache is refreshed every minute."),
		verdictJSON("F6", "downgrade", "INFO", 0, ""),
		verdictJSON("F7", "reject", "CRITICAL", 4, "The cache is refreshed hourly."),
		verdictJSON("F99", "reject", "CRITICAL", 2, "A tenant is one customer account."),
	)}}
	Run(context.Background(), report, input(p))

	if len(p.reqs) != 1 {
		t.Fatalf("calls = %d, want 1", len(p.reqs))
	}
	task := p.reqs[0].UserPrompt
	for _, want := range []string{"F1 UNDEFINED_INTERFACE L2: Tenant undefined", "F5 UNDEFINED_INTERFACE L1: Not answered", "F6 QUESTION L2: What is a tenant?", "F7 QUESTION L?"} {
		if !strings.Contains(task, want) {
			t.Errorf("task missing %q:\n%s", want, task)
		}
	}
	if strings.Contains(task, "Missing section") || strings.Contains(task, "warn") {
		t.Errorf("task lists findings that need no second look:\n%s", task)
	}
	if req := p.reqs[0]; req.UserPromptCachedPrefix != "<spec>...</spec>" || req.SystemPrompt != "sys" || req.Schema == nil || !req.Schema.Enforce {
		t.Errorf("request = %+v, want the shared prefix and an enforced schema", req)
	}

	byID := map[string]schema.Issue{}
	for _, issue := range report.Issues {
		byID[issue.ID] = issue
	}
	if _, ok := byID["ISSUE-0001"]; ok {
		t.Error("the rejected issue is still in the report")
	}
	if got := byID["ISSUE-0002"]; got.Severity != schema.SeverityCritical || !slices.Contains(got.Tags, TagConfirmed) {
		t.Errorf("confirmed issue = %+v", got)
	}
	if got := byID["ISSUE-0003"]; got.Severity != schema.SeverityWarn || got.Blocking || !slices.Contains(got.Tags, TagDowngraded) {
		t.Errorf("downgraded issue = %+v", got)
	}
	if got := byID["ISSUE-0004"]; got.Severity != schema.SeverityCritical || len(got.Tags) != 0 {
		t.Errorf("issue with an unproven rejection = %+v, want it unchanged", got)
	}
	if got := byID["ISSUE-0005"]; got.Severity != schema.SeverityCritical || len(got.Tags) != 0 {
		t.Errorf("unanswered issue = %+v, want it unchanged", got)
	}
	if len(report.Questions) != 1 || report.Questions[0].ID != "Q-0001" || report.Questions[0].Severity != schema.SeverityInfo {
		t.Errorf("questions = %+v, want Q-0001 downgraded and Q-0002 rejected", report.Questions)
	}
	if len(report.Patches) != 1 || report.Patches[0].IssueID != "ISSUE-0002" {
		t.Errorf("patches = %+v, want the rejected issue's patch removed", report.Patches)
	}

	meta := report.Meta.Verification
	if meta == nil || meta.Status != schema.VerificationComplete || meta.Checked != 7 || meta.Confirmed != 1 || meta.Downgraded != 2 ||
		len(meta.Rejected) != 2 || meta.UnverifiedRejections != 1 || meta.Unanswered != 1 {
		t.Fatalf("meta = %+v", meta)
	}
	if r := meta.Rejected[0]; r.Title != "Tenant undefined" || r.AnsweredBy.LineStart != 2 || r.AnsweredBy.Path != "SPEC.md" || r.Reason != "because" {
		t.Errorf("rejection record = %+v", r)
	}
}

func TestRunInStrictModeDoesNotDowngrade(t *testing.T) {
	report := &schema.Report{Issues: []schema.Issue{critical("ISSUE-0001", "t", 2)}}
	p := &scripted{replies: []string{reply(verdictJSON("F1", "downgrade", "WARN", 0, ""))}}
	in := input(p)
	in.Strict = true
	Run(context.Background(), report, in)
	if got := report.Issues[0]; got.Severity != schema.SeverityCritical || !slices.Contains(got.Tags, TagConfirmed) {
		t.Errorf("issue = %+v, want it kept CRITICAL", got)
	}
	if !strings.Contains(p.reqs[0].UserPrompt, "downgrade: not available") {
		t.Errorf("strict task offers a downgrade:\n%s", p.reqs[0].UserPrompt)
	}
}

func TestRunDowngradeMustNameALowerSeverity(t *testing.T) {
	report := &schema.Report{Issues: []schema.Issue{critical("ISSUE-0001", "t", 2)}}
	Run(context.Background(), report, input(&scripted{replies: []string{reply(verdictJSON("F1", "downgrade", "CRITICAL", 0, ""))}}))
	if got := report.Issues[0]; got.Severity != schema.SeverityCritical || !slices.Contains(got.Tags, TagConfirmed) {
		t.Errorf("issue = %+v, want it treated as confirmed", got)
	}
}

func TestRunFailureLeavesFindingsAlone(t *testing.T) {
	cases := map[string]*scripted{
		"provider error":           {err: errors.New("boom")},
		"unreadable twice":         {replies: []string{"not json", "still not json"}},
		"no verdicts field, twice": {replies: []string{`{"issues":[]}`}},
	}
	for name, p := range cases {
		t.Run(name, func(t *testing.T) {
			report := &schema.Report{Issues: []schema.Issue{critical("ISSUE-0001", "t", 2)}}
			Run(context.Background(), report, input(p))
			if got := report.Issues[0]; got.Severity != schema.SeverityCritical || len(got.Tags) != 0 {
				t.Errorf("issue = %+v, want it unchanged", got)
			}
			meta := report.Meta.Verification
			if meta == nil || meta.Status != schema.VerificationFailed || meta.Error == "" || meta.Checked != 0 || meta.Unchecked != 1 {
				t.Errorf("meta = %+v, want a failed verification with the finding unchecked", meta)
			}
		})
	}
}

func TestRunRepairsAnUnreadableResponseOnce(t *testing.T) {
	report := &schema.Report{Issues: []schema.Issue{critical("ISSUE-0001", "t", 2)}}
	p := &scripted{replies: []string{"```json\n{\"verdicts\": [", reply(verdictJSON("F1", "confirm", "CRITICAL", 0, ""))}}
	Run(context.Background(), report, input(p))
	if len(p.reqs) != 2 || p.reqs[1].Attempt != llm.AttemptRepair {
		t.Fatalf("calls = %d, want a repair", len(p.reqs))
	}
	if report.Meta.Verification.Confirmed != 1 {
		t.Errorf("meta = %+v", report.Meta.Verification)
	}
}

func TestRunChecksAtMostTheLimit(t *testing.T) {
	report := &schema.Report{}
	for i := range maxCandidates + 2 {
		report.Issues = append(report.Issues, critical(fmt.Sprintf("ISSUE-%04d", i+1), "t", 2))
	}
	p := &scripted{replies: []string{reply()}}
	Run(context.Background(), report, input(p))
	meta := report.Meta.Verification
	if meta.Checked != maxCandidates || meta.Unchecked != 2 || meta.Unanswered != maxCandidates {
		t.Errorf("meta = %+v", meta)
	}
	if strings.Contains(p.reqs[0].UserPrompt, fmt.Sprintf("F%d ", maxCandidates+1)) {
		t.Error("the task lists findings beyond the limit")
	}
}

func TestRunWithNothingToCheckMakesNoCall(t *testing.T) {
	report := &schema.Report{Issues: []schema.Issue{critical("P", "t", 1, "preflight")}}
	p := &scripted{}
	Run(context.Background(), report, input(p))
	if len(p.reqs) != 0 || report.Meta.Verification != nil {
		t.Errorf("calls = %d meta = %+v, want nothing done", len(p.reqs), report.Meta.Verification)
	}
}

// Every provider's strict mode needs each object to list all its properties
// as required and allow no others.
func TestSchemaIsStrict(t *testing.T) {
	var root struct {
		Required   []string `json:"required"`
		Properties struct {
			Verdicts struct {
				Items struct {
					AdditionalProperties bool                       `json:"additionalProperties"`
					Required             []string                   `json:"required"`
					Properties           map[string]json.RawMessage `json:"properties"`
				} `json:"items"`
			} `json:"verdicts"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(Schema(true).JSON, &root); err != nil {
		t.Fatalf("schema is not valid JSON: %v", err)
	}
	items := root.Properties.Verdicts.Items
	var props []string
	for name := range items.Properties {
		props = append(props, name)
	}
	slices.Sort(props)
	required := slices.Clone(items.Required)
	slices.Sort(required)
	if items.AdditionalProperties || !slices.Equal(props, required) || !slices.Equal(root.Required, []string{"verdicts"}) {
		t.Errorf("schema is not strict: properties %v, required %v", props, required)
	}
	example := Schema(false).PromptFallback
	if !json.Valid([]byte(example[strings.Index(example, "{"):])) {
		t.Errorf("the example is not valid JSON:\n%s", example)
	}
}

func TestRunIgnoresAnUnknownDecision(t *testing.T) {
	report := &schema.Report{Issues: []schema.Issue{critical("ISSUE-0001", "t", 2)}}
	Run(context.Background(), report, input(&scripted{replies: []string{reply(verdictJSON("F1", "maybe", "CRITICAL", 0, ""))}}))
	if got := report.Issues[0]; got.Severity != schema.SeverityCritical || len(got.Tags) != 0 {
		t.Errorf("issue = %+v, want it unchanged and untagged", got)
	}
	if meta := report.Meta.Verification; meta.Confirmed != 0 || meta.Unanswered != 1 {
		t.Errorf("meta = %+v, want the finding counted as unanswered", meta)
	}
}
