package llm

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/dshills/speccritic/internal/schema"
	"github.com/dshills/speccritic/internal/schema/validate"
)

type scriptedProvider struct {
	responses []*Response
	err       error
	reqs      []Request
}

func (p *scriptedProvider) Complete(_ context.Context, req *Request) (*Response, error) {
	p.reqs = append(p.reqs, *req)
	if p.err != nil {
		return nil, p.err
	}
	if len(p.reqs) > len(p.responses) {
		return nil, fmt.Errorf("unexpected call %d", len(p.reqs))
	}
	return p.responses[len(p.reqs)-1], nil
}

func issueJSON(id, title string, line int) string {
	return fmt.Sprintf(`{"id":%q,"severity":"WARN","category":"AMBIGUOUS_BEHAVIOR","title":%q,"description":"d","evidence":[{"path":"SPEC.md","line_start":%d,"line_end":%d,"quote":"q"}],"impact":"i","recommendation":"r","blocking":false,"tags":[]}`, id, title, line, line)
}

func reportJSON(issues []string, patches string) string {
	return `{"issues":[` + strings.Join(issues, ",") + `],"questions":[],"patches":[` + patches + `]}`
}

// cutAfterIssues returns a response that holds the given complete issues and
// then stops partway through another one.
func cutAfterIssues(issues ...string) string {
	return `{"issues":[` + strings.Join(issues, ",") + `,{"id":"ISSUE-0099","severity":"WA`
}

func testCall(req *Request) ReportCall {
	return ReportCall{
		Request: req,
		Label:   "test ",
		Parse: func(raw string) (Parsed, error) {
			res, err := validate.ParseResponse(raw, validate.Options{LineCount: 50})
			if err != nil {
				return Parsed{}, err
			}
			return Parsed{Report: res.Report, Incomplete: res.Incomplete, Dropped: res.Dropped}, nil
		},
		RepairPrompt: func(reason error, _ string) string { return "\nREPAIR: " + reason.Error() },
	}
}

func TestCompleteReport_CompleteResponseNeedsOneCall(t *testing.T) {
	provider := &scriptedProvider{responses: []*Response{
		{Content: reportJSON([]string{issueJSON("ISSUE-0001", "One", 1)}, ""), Model: "m"},
	}}
	report, model, err := CompleteReport(context.Background(), provider, testCall(&Request{UserPrompt: "spec", MaxTokens: 1000}))
	if err != nil {
		t.Fatalf("CompleteReport: %v", err)
	}
	if len(provider.reqs) != 1 || model != "m" || len(report.Issues) != 1 {
		t.Fatalf("calls=%d model=%q issues=%d", len(provider.reqs), model, len(report.Issues))
	}
}

func TestCompleteReport_NumbersFindingsWhateverIDsTheModelUsed(t *testing.T) {
	content := `{"issues":[` + strings.Join([]string{
		issueJSON("ISSUE-0007", "One", 1),
		issueJSON("ISSUE-0007", "Two", 2),
		issueJSON("x", "Three", 3),
	}, ",") + `],"questions":[{"id":"Q-9","severity":"WARN","question":"Why?","evidence":[{"line_start":1,"line_end":1}]}],"patches":[
		{"issue_id":"ISSUE-0007","before":"a","after":"b"},
		{"issue_id":"x","before":"c","after":"d"},
		{"issue_id":"ISSUE-0001","before":"e","after":"f"}
	]}`
	provider := &scriptedProvider{responses: []*Response{{Content: content, Model: "m"}}}
	report, _, err := CompleteReport(context.Background(), provider, testCall(&Request{UserPrompt: "spec", MaxTokens: 1000}))
	if err != nil {
		t.Fatalf("CompleteReport: %v", err)
	}
	for i, issue := range report.Issues {
		if want := fmt.Sprintf("ISSUE-%04d", i+1); issue.ID != want {
			t.Errorf("issue %q id = %s, want %s", issue.Title, issue.ID, want)
		}
	}
	if len(report.Issues) != 3 || report.Questions[0].ID != "Q-0001" {
		t.Fatalf("issues=%d question id=%s", len(report.Issues), report.Questions[0].ID)
	}
	// A reused ID resolves to the first issue that carried it. "ISSUE-0001" is
	// not an ID the model used, so its patch has no issue to attach to.
	var patched []string
	for _, patch := range report.Patches {
		patched = append(patched, patch.IssueID+":"+patch.Before)
	}
	if got := strings.Join(patched, ","); got != "ISSUE-0001:a,ISSUE-0003:c" {
		t.Errorf("patches = %s, want ISSUE-0001:a,ISSUE-0003:c", got)
	}
}

func TestCompleteReport_DropsInvalidFindingWithoutAnotherCall(t *testing.T) {
	bad := strings.Replace(issueJSON("ISSUE-0002", "Bad", 2), `"WARN"`, `"HIGH"`, 1)
	provider := &scriptedProvider{responses: []*Response{
		{Content: reportJSON([]string{issueJSON("ISSUE-0001", "Good", 1), bad, issueJSON("ISSUE-0003", "Also good", 3)}, ""), Model: "m"},
	}}
	var logged []string
	call := testCall(&Request{UserPrompt: "spec", MaxTokens: 1000})
	call.Logf = func(format string, args ...any) { logged = append(logged, fmt.Sprintf(format, args...)) }
	report, _, err := CompleteReport(context.Background(), provider, call)
	if err != nil {
		t.Fatalf("CompleteReport: %v", err)
	}
	if len(provider.reqs) != 1 {
		t.Fatalf("calls = %d, want no repair for one bad finding", len(provider.reqs))
	}
	if len(report.Issues) != 2 || report.Issues[1].ID != "ISSUE-0002" || report.Issues[1].Title != "Also good" {
		t.Fatalf("issues = %#v, want the two valid findings numbered without a gap", report.Issues)
	}
	if report.Meta.DroppedFindings != 1 {
		t.Errorf("dropped = %d, want 1", report.Meta.DroppedFindings)
	}
	if len(logged) != 1 || !strings.Contains(logged[0], "Dropped 1 invalid finding(s)") || !strings.Contains(logged[0], "invalid severity") {
		t.Errorf("log = %q, want the drop and its reason", logged)
	}
}

func TestCompleteReport_CountsDropsAcrossContinuedParts(t *testing.T) {
	bad := strings.Replace(issueJSON("ISSUE-0009", "Bad", 2), `"WARN"`, `"HIGH"`, 1)
	provider := &scriptedProvider{responses: []*Response{
		{Content: cutAfterIssues(issueJSON("ISSUE-0001", "One", 1), bad), Model: "m", Truncated: true},
		{Content: reportJSON([]string{bad, issueJSON("ISSUE-0002", "Two", 3)}, ""), Model: "m"},
	}}
	report, _, err := CompleteReport(context.Background(), provider, testCall(&Request{UserPrompt: "spec", MaxTokens: 1000}))
	if err != nil {
		t.Fatalf("CompleteReport: %v", err)
	}
	if len(report.Issues) != 2 || report.Meta.DroppedFindings != 2 {
		t.Fatalf("issues=%d dropped=%d, want 2 and 2", len(report.Issues), report.Meta.DroppedFindings)
	}
}

func TestCompleteReport_TruncatedResponseIsContinuedNotRegenerated(t *testing.T) {
	provider := &scriptedProvider{responses: []*Response{
		{Content: cutAfterIssues(issueJSON("ISSUE-0001", "One", 1), issueJSON("ISSUE-0002", "Two", 2)), Model: "m1", Truncated: true, StopReason: "max_tokens"},
		// The continuation restarts its numbering and repeats a finding.
		{Content: reportJSON(
			[]string{issueJSON("ISSUE-0001", "Three", 3), issueJSON("ISSUE-0002", "two", 2)},
			`{"issue_id":"ISSUE-0001","before":"a","after":"b"},{"issue_id":"ISSUE-0002","before":"c","after":"d"},{"issue_id":"ISSUE-0042","before":"e","after":"f"}`,
		), Model: "m2"},
	}}
	report, model, err := CompleteReport(context.Background(), provider, testCall(&Request{UserPrompt: "spec", MaxTokens: 1000}))
	if err != nil {
		t.Fatalf("CompleteReport: %v", err)
	}
	if len(provider.reqs) != 2 {
		t.Fatalf("calls = %d, want initial + continuation", len(provider.reqs))
	}
	if model != "m2" {
		t.Errorf("model = %q, want the last responding model", model)
	}

	cont := provider.reqs[1]
	if provider.reqs[0].Attempt != AttemptFirst || cont.Attempt != AttemptContinuation {
		t.Errorf("attempts = %q, %q, want first then continuation", provider.reqs[0].Attempt, cont.Attempt)
	}
	if cont.MaxTokens != 1000 {
		t.Errorf("continuation max tokens = %d, want unchanged 1000", cont.MaxTokens)
	}
	for _, want := range []string{"spec", "<received_findings>", "- ISSUE-0001 WARN AMBIGUOUS_BEHAVIOR L1: One", "- ISSUE-0002 WARN AMBIGUOUS_BEHAVIOR L2: Two", "Number new issues from ISSUE-0003"} {
		if !strings.Contains(cont.UserPrompt, want) {
			t.Errorf("continuation prompt missing %q:\n%s", want, cont.UserPrompt)
		}
	}
	if strings.Contains(cont.UserPrompt, "REPAIR") {
		t.Errorf("continuation used the repair prompt:\n%s", cont.UserPrompt)
	}

	var titles []string
	for i, issue := range report.Issues {
		if want := fmt.Sprintf("ISSUE-%04d", i+1); issue.ID != want {
			t.Errorf("issue %d id = %s, want %s", i, issue.ID, want)
		}
		titles = append(titles, issue.Title)
	}
	if got := strings.Join(titles, ","); got != "One,Two,Three" {
		t.Errorf("issue titles = %s, want One,Two,Three (repeat skipped)", got)
	}
	// "ISSUE-0001" in the continuation is its own new issue (now ISSUE-0003),
	// its repeated "ISSUE-0002" is the earlier ISSUE-0002, and a patch for an
	// issue nobody reported is dropped.
	var patched []string
	for _, patch := range report.Patches {
		patched = append(patched, patch.IssueID)
	}
	if got := strings.Join(patched, ","); got != "ISSUE-0003,ISSUE-0002" {
		t.Errorf("patch issue ids = %s, want ISSUE-0003,ISSUE-0002", got)
	}
}

func TestCompleteReport_NothingUsableIsRepairedWithMoreRoom(t *testing.T) {
	provider := &scriptedProvider{responses: []*Response{
		{Content: `{"issues":[`, Model: "m", Truncated: true},
		{Content: reportJSON(nil, ""), Model: "m"},
	}}
	report, _, err := CompleteReport(context.Background(), provider, testCall(&Request{UserPrompt: "spec", MaxTokens: 1000}))
	if err != nil {
		t.Fatalf("CompleteReport: %v", err)
	}
	if len(provider.reqs) != 2 || len(report.Issues) != 0 {
		t.Fatalf("calls=%d issues=%d", len(provider.reqs), len(report.Issues))
	}
	if provider.reqs[1].MaxTokens <= 1000 {
		t.Errorf("repair max tokens = %d, want more than 1000", provider.reqs[1].MaxTokens)
	}
	if !strings.Contains(provider.reqs[1].UserPrompt, "REPAIR: JSON parse failed") {
		t.Errorf("repair prompt = %q", provider.reqs[1].UserPrompt)
	}
}

func TestCompleteReport_TruncatedFlagAloneRaisesRepairCap(t *testing.T) {
	// The response spent its whole budget before emitting any text.
	provider := &scriptedProvider{responses: []*Response{
		{Content: "", Model: "m", Truncated: true},
		{Content: reportJSON(nil, ""), Model: "m"},
	}}
	if _, _, err := CompleteReport(context.Background(), provider, testCall(&Request{UserPrompt: "spec", MaxTokens: 1000})); err != nil {
		t.Fatalf("CompleteReport: %v", err)
	}
	if provider.reqs[1].MaxTokens <= 1000 {
		t.Errorf("repair max tokens = %d, want more than 1000", provider.reqs[1].MaxTokens)
	}
}

func TestCompleteReport_InvalidFindingIsRepairedAtSameCap(t *testing.T) {
	bad := strings.Replace(issueJSON("ISSUE-0001", "One", 1), `"WARN"`, `"HIGH"`, 1)
	provider := &scriptedProvider{responses: []*Response{
		{Content: reportJSON([]string{bad}, ""), Model: "m"},
		{Content: reportJSON([]string{issueJSON("ISSUE-0001", "One", 1)}, ""), Model: "m"},
	}}
	report, _, err := CompleteReport(context.Background(), provider, testCall(&Request{UserPrompt: "spec", MaxTokens: 1000}))
	if err != nil {
		t.Fatalf("CompleteReport: %v", err)
	}
	if len(report.Issues) != 1 || provider.reqs[1].MaxTokens != 1000 {
		t.Fatalf("issues=%d repair max tokens=%d", len(report.Issues), provider.reqs[1].MaxTokens)
	}
}

func TestCompleteReport_RepairThatIsCutOffIsContinued(t *testing.T) {
	provider := &scriptedProvider{responses: []*Response{
		{Content: "not json", Model: "m"},
		{Content: cutAfterIssues(issueJSON("ISSUE-0001", "One", 1)), Model: "m", Truncated: true},
		{Content: reportJSON([]string{issueJSON("ISSUE-0002", "Two", 2)}, ""), Model: "m"},
	}}
	report, _, err := CompleteReport(context.Background(), provider, testCall(&Request{UserPrompt: "spec", MaxTokens: 1000}))
	if err != nil {
		t.Fatalf("CompleteReport: %v", err)
	}
	if len(provider.reqs) != 3 || len(report.Issues) != 2 {
		t.Fatalf("calls=%d issues=%d, want 3 calls and 2 issues", len(provider.reqs), len(report.Issues))
	}
	repair, cont := provider.reqs[1], provider.reqs[2]
	if repair.Attempt != AttemptRepair || cont.Attempt != AttemptContinuation {
		t.Errorf("attempts = %q, %q, want repair then continuation", repair.Attempt, cont.Attempt)
	}
	if repair.MaxTokens <= 1000 || cont.MaxTokens != repair.MaxTokens {
		t.Errorf("repair max tokens = %d, continuation = %d, want the raised budget kept", repair.MaxTokens, cont.MaxTokens)
	}
	if strings.Contains(cont.UserPrompt, "REPAIR") {
		t.Errorf("continuation prompt carries the repair text:\n%s", cont.UserPrompt)
	}
}

func TestCompleteReport_CutOffPartWithOnlyPatchesCountsAsProgress(t *testing.T) {
	one := issueJSON("ISSUE-0001", "One", 1)
	provider := &scriptedProvider{responses: []*Response{
		{Content: cutAfterIssues(one), Model: "m", Truncated: true},
		// Nothing new to report, and the patch list is cut off after one edit.
		{Content: `{"issues":[],"questions":[],"patches":[{"issue_id":"ISSUE-0001","before":"a","after":"b"},{"issue_id":"ISSUE-0001","bef`, Model: "m", Truncated: true},
		{Content: reportJSON(nil, `{"issue_id":"ISSUE-0001","before":"c","after":"d"}`), Model: "m"},
	}}
	report, _, err := CompleteReport(context.Background(), provider, testCall(&Request{UserPrompt: "spec", MaxTokens: 1000}))
	if err != nil {
		t.Fatalf("CompleteReport: %v", err)
	}
	if len(provider.reqs) != 3 || len(report.Issues) != 1 || len(report.Patches) != 2 {
		t.Fatalf("calls=%d issues=%d patches=%d, want 3/1/2", len(provider.reqs), len(report.Issues), len(report.Patches))
	}
}

func TestCompleteReport_Failures(t *testing.T) {
	cut := func(n int) *Response {
		return &Response{Content: cutAfterIssues(issueJSON("ISSUE-0001", fmt.Sprintf("Finding %d", n), n)), Model: "m", Truncated: true}
	}
	cases := map[string]struct {
		provider  *scriptedProvider
		wantCalls int
		wantErr   string
	}{
		"provider error": {
			provider:  &scriptedProvider{err: errors.New("boom")},
			wantCalls: 1,
			wantErr:   "test LLM call failed: boom",
		},
		"still invalid after repair": {
			provider:  &scriptedProvider{responses: []*Response{{Content: "nope"}, {Content: "still nope"}}},
			wantCalls: 2,
			wantErr:   "test invalid model output after retry",
		},
		"continuation is invalid": {
			provider: &scriptedProvider{responses: []*Response{
				cut(1),
				{Content: reportJSON([]string{strings.Replace(issueJSON("ISSUE-0002", "Two", 2), `"WARN"`, `"HIGH"`, 1)}, "")},
			}},
			wantCalls: 2,
			wantErr:   "test invalid model output in continuation",
		},
		"continuation adds nothing": {
			provider:  &scriptedProvider{responses: []*Response{cut(1), cut(1)}},
			wantCalls: 2,
			wantErr:   "test continuation added nothing new",
		},
		"never completes": {
			provider:  &scriptedProvider{responses: []*Response{cut(1), cut(2), cut(3), cut(4)}},
			wantCalls: 1 + maxContinuations,
			wantErr:   "test response still incomplete after 3 continuation calls",
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, _, err := CompleteReport(context.Background(), tc.provider, testCall(&Request{UserPrompt: "spec", MaxTokens: 1000}))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("error = %v, want %q", err, tc.wantErr)
			}
			if len(tc.provider.reqs) != tc.wantCalls {
				t.Errorf("calls = %d, want %d", len(tc.provider.reqs), tc.wantCalls)
			}
		})
	}
}

func TestAppendPart_SkipsRepeatedPatches(t *testing.T) {
	issue := schema.Issue{ID: "ISSUE-0001", Category: schema.CategoryAmbiguousBehavior, Title: "One", Evidence: []schema.Evidence{{LineStart: 1, LineEnd: 1}}}
	acc := &schema.Report{}
	appendPart(acc, &schema.Report{
		Issues:  []schema.Issue{issue},
		Patches: []schema.Patch{{IssueID: "ISSUE-0001", Before: "a", After: "b"}},
	})
	appendPart(acc, &schema.Report{
		Issues: []schema.Issue{issue},
		Patches: []schema.Patch{
			{IssueID: "ISSUE-0001", Before: "a", After: "b"},
			{IssueID: "ISSUE-0001", Before: "c", After: "d"},
		},
	})
	if len(acc.Issues) != 1 {
		t.Fatalf("issues = %d, want the repeat skipped", len(acc.Issues))
	}
	if len(acc.Patches) != 2 || acc.Patches[1].Before != "c" {
		t.Fatalf("patches = %#v, want the repeated edit skipped and the new one kept", acc.Patches)
	}
}
