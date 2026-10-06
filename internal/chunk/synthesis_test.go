package chunk

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/dshills/speccritic/internal/llm"
	"github.com/dshills/speccritic/internal/schema"
)

func TestShouldRunSynthesis(t *testing.T) {
	if ShouldRunSynthesis(false, 500, 1, DefaultSynthesisLineThreshold) {
		t.Fatal("disabled chunking should skip synthesis")
	}
	if ShouldRunSynthesis(true, 239, 0, DefaultSynthesisLineThreshold) {
		t.Fatal("no findings below threshold should skip synthesis")
	}
	if !ShouldRunSynthesis(true, 239, 1, DefaultSynthesisLineThreshold) {
		t.Fatal("findings should run synthesis below threshold")
	}
	if !ShouldRunSynthesis(true, 240, 0, DefaultSynthesisLineThreshold) {
		t.Fatal("line threshold should run synthesis")
	}
	if ShouldRunSynthesis(true, 239, 0, 0) {
		t.Fatal("zero threshold should use default and skip below threshold")
	}
}

func TestBuildSynthesisTaskIncludesFindings(t *testing.T) {
	merged := MergeResult{Issues: []schema.Issue{testIssue("ISSUE-0001", schema.SeverityWarn, schema.CategoryContradiction, "Merged finding", 1)}}
	tail, err := BuildSynthesisTask(SynthesisInput{Merged: merged, SpecShown: true})
	if err != nil {
		t.Fatalf("BuildSynthesisTask: %v", err)
	}
	for _, want := range []string{"specification above was reviewed in chunks", "<chunk_findings>", "Merged finding"} {
		if !strings.Contains(tail, want) {
			t.Fatalf("task missing %q:\n%s", want, tail)
		}
	}
	unshown, err := BuildSynthesisTask(SynthesisInput{Merged: merged})
	if err != nil {
		t.Fatalf("BuildSynthesisTask: %v", err)
	}
	if strings.Contains(unshown, "specification above") || !strings.Contains(unshown, "too large to show in full") {
		t.Fatalf("task for an unshown spec refers to it as shown:\n%s", unshown)
	}
}

func TestBuildSynthesisPromptCapsFindings(t *testing.T) {
	issues := make([]schema.Issue, 0, maxSynthesisFindings+1)
	for i := 0; i < maxSynthesisFindings+1; i++ {
		issues = append(issues, testIssue("ISSUE-0001", schema.SeverityWarn, schema.CategoryAmbiguousBehavior, "Finding", 1))
		issues[i].Title = "Finding kept"
	}
	issues[maxSynthesisFindings].Title = "Finding omitted"
	tail, err := BuildSynthesisTask(SynthesisInput{Merged: MergeResult{Issues: issues}})
	if err != nil {
		t.Fatalf("BuildSynthesisTask: %v", err)
	}
	if strings.Contains(tail, "Finding omitted") {
		t.Fatalf("tail contains issue past cap:\n%s", tail)
	}
	if !strings.Contains(tail, "Finding kept") {
		t.Fatalf("tail missing kept issue:\n%s", tail)
	}
}

func TestRunSynthesisCallsProviderAndValidatesTags(t *testing.T) {
	s, _ := executorFixture(t, 2)
	provider := &captureSynthesisProvider{response: synthesisResponse(`["synthesis"]`)}
	report, model, err := RunSynthesis(context.Background(), provider, s, nil, MergeResult{Issues: []schema.Issue{
		testIssue("ISSUE-0001", schema.SeverityWarn, schema.CategoryAmbiguousBehavior, "Finding", 1),
	}}, SynthesisConfig{Enabled: true, LineThreshold: DefaultSynthesisLineThreshold, MaxTokens: 1000})
	if err != nil {
		t.Fatalf("RunSynthesis: %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("calls = %d, want 1", provider.calls)
	}
	if model != "fake:synthesis" {
		t.Fatalf("model = %q", model)
	}
	if len(report.Report.Issues) != 1 || !hasTag(report.Report.Issues[0].Tags, "synthesis") {
		t.Fatalf("report issues = %#v", report.Report.Issues)
	}
	if !strings.Contains(provider.lastPrompt, "<chunk_findings>") {
		t.Fatalf("prompt missing merged findings: %s", provider.lastPrompt)
	}
}

func TestRunSynthesisRepairsInvalidOutputOnce(t *testing.T) {
	s, _ := executorFixture(t, 2)
	provider := &captureSynthesisProvider{responses: []string{`{"issues":[`, synthesisResponse(`["synthesis"]`)}}
	_, _, err := RunSynthesis(context.Background(), provider, s, nil, MergeResult{Issues: []schema.Issue{
		testIssue("ISSUE-0001", schema.SeverityWarn, schema.CategoryAmbiguousBehavior, "Finding", 1),
	}}, SynthesisConfig{Enabled: true, MaxTokens: 1000})
	if err != nil {
		t.Fatalf("RunSynthesis: %v", err)
	}
	if provider.calls != 2 {
		t.Fatalf("calls = %d, want initial + repair", provider.calls)
	}
	if provider.lastMaxTokens <= 1000 {
		t.Fatalf("repair max tokens = %d, want extra headroom", provider.lastMaxTokens)
	}
	if !strings.Contains(provider.lastPrompt, "failed synthesis validation") {
		t.Fatalf("repair prompt = %s", provider.lastPrompt)
	}
}

func TestRunSynthesisCountsPreflightFindings(t *testing.T) {
	s, _ := executorFixture(t, 2)
	provider := &captureSynthesisProvider{response: synthesisResponse(`["synthesis"]`)}
	_, _, err := RunSynthesis(context.Background(), provider, s, []schema.Issue{
		testIssue("PREFLIGHT-0001", schema.SeverityWarn, schema.CategoryAmbiguousBehavior, "Preflight", 1),
	}, MergeResult{}, SynthesisConfig{Enabled: true, MaxTokens: 1000})
	if err != nil {
		t.Fatalf("RunSynthesis: %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("calls = %d, want synthesis to run for preflight findings", provider.calls)
	}
}

func TestRunSynthesisAddsMissingSynthesisTag(t *testing.T) {
	s, _ := executorFixture(t, 2)
	provider := &captureSynthesisProvider{response: synthesisResponse(`[]`)}
	report, _, err := RunSynthesis(context.Background(), provider, s, nil, MergeResult{Issues: []schema.Issue{
		testIssue("ISSUE-0001", schema.SeverityWarn, schema.CategoryAmbiguousBehavior, "Finding", 1),
	}}, SynthesisConfig{Enabled: true, MaxTokens: 1000})
	if err != nil {
		t.Fatalf("RunSynthesis: %v", err)
	}
	if provider.calls != 1 {
		t.Fatalf("calls = %d, want no repair for missing tag", provider.calls)
	}
	if !hasTag(report.Report.Issues[0].Tags, TagSynthesis) {
		t.Fatalf("tags = %#v, want synthesis tag added", report.Report.Issues[0].Tags)
	}
}

// synthesisSpec is the spec the ApplySynthesis tests refer to.
const synthesisSpec = "# Spec\n## Terms\nA tenant is one customer account.\n## Rules\nEach tenant has one owner.\nOwners may invite members.\n"

func mergedForSynthesis() MergeResult {
	return MergeResult{
		Issues: []schema.Issue{
			testIssue("ISSUE-0001", schema.SeverityCritical, schema.CategoryUndefinedInterface, "Tenant is not defined", 5, "chunked-review"),
			testIssue("ISSUE-0002", schema.SeverityWarn, schema.CategoryAmbiguousBehavior, "Owner role unclear", 5, "chunked-review"),
			testIssue("ISSUE-0003", schema.SeverityWarn, schema.CategoryAmbiguousBehavior, "Owner role is ambiguous", 6, "chunked-review"),
			testIssue("PREFLIGHT-X", schema.SeverityWarn, schema.CategoryUnspecifiedConstraint, "Preflight finding", 3, tagPreflight),
		},
		Questions: []schema.Question{
			{ID: "Q-0001", Severity: schema.SeverityWarn, Question: "What is a tenant?", Evidence: []schema.Evidence{{LineStart: 5, LineEnd: 5}}},
		},
		Patches: []schema.Patch{
			{IssueID: "ISSUE-0001", Before: "Each tenant has one owner.", After: "Each tenant (see Terms) has one owner."},
			{IssueID: "ISSUE-0003", Before: "Owners may invite members.", After: "Owners may invite up to 50 members."},
		},
	}
}

func TestApplySynthesis(t *testing.T) {
	answer := Retraction{LineStart: 3, LineEnd: 3, Quote: "A tenant is one customer account.", Reason: "Defined under Terms"}
	retract := func(id string) Retraction { r := answer; r.ID = id; return r }
	synthesis := &SynthesisResult{
		Report: &schema.Report{
			Issues:  []schema.Issue{testIssue("ISSUE-0001", schema.SeverityCritical, schema.CategoryContradiction, "Owner rules contradict", 5)},
			Patches: []schema.Patch{{IssueID: "ISSUE-0001", Before: "Each tenant has one owner", After: "Each tenant has exactly one owner"}},
		},
		Merges: []MergeGroup{{IssueIDs: []string{"ISSUE-0003", "ISSUE-0002"}, Reason: "Same gap"}},
		Retractions: []Retraction{
			retract("ISSUE-0001"),
			retract("Q-0001"),
			retract("PREFLIGHT-X"), // preflight findings are never retracted
			retract("ISSUE-0404"),  // unknown
			{ID: "ISSUE-0002", LineStart: 4, LineEnd: 4, Quote: "Owners hold the admin role.", Reason: "made up"},
		},
	}
	result, meta := ApplySynthesis(mergedForSynthesis(), synthesis, synthesisSpec, "SPEC.md")

	var titles []string
	for i, issue := range result.Issues {
		if want := fmt.Sprintf("ISSUE-%04d", i+1); issue.ID != want {
			t.Errorf("issue %q id = %s, want %s", issue.Title, issue.ID, want)
		}
		titles = append(titles, issue.Title)
	}
	if got := strings.Join(titles, " | "); got != "Owner rules contradict | Preflight finding | Owner role unclear" {
		t.Fatalf("issues = %s", got)
	}
	if !hasTag(result.Issues[0].Tags, TagSynthesis) {
		t.Errorf("new finding is not tagged %s: %v", TagSynthesis, result.Issues[0].Tags)
	}
	kept := result.Issues[2]
	if len(kept.Evidence) != 2 {
		t.Errorf("merged finding evidence = %v, want both findings' lines", kept.Evidence)
	}
	if len(result.Questions) != 0 {
		t.Errorf("questions = %v, want the answered one retracted", result.Questions)
	}

	if meta.MergedFindings != 1 || len(meta.Retracted) != 2 || meta.IgnoredRetractions != 3 {
		t.Fatalf("meta = %+v, want 1 merged, 2 retracted, 3 ignored", meta)
	}
	if r := meta.Retracted[0]; r.Title != "Tenant is not defined" || r.AnsweredBy.LineStart != 3 || r.AnsweredBy.Quote != "A tenant is one customer account." || r.AnsweredBy.Path != "SPEC.md" {
		t.Errorf("retraction record = %+v", r)
	}

	// The retracted issue's patch goes; the folded issue's patch follows it to
	// the kept one; the new finding's patch is kept.
	byIssue := map[string]string{}
	for _, patch := range result.Patches {
		byIssue[patch.IssueID] = patch.Before
	}
	if len(result.Patches) != 2 || byIssue["ISSUE-0001"] != "Each tenant has one owner" || byIssue["ISSUE-0003"] != "Owners may invite members." {
		t.Errorf("patches = %+v", result.Patches)
	}
}

func TestApplySynthesisWithNothingToDo(t *testing.T) {
	merged := mergedForSynthesis()
	if result, meta := ApplySynthesis(merged, nil, synthesisSpec, "SPEC.md"); meta != nil || len(result.Issues) != len(merged.Issues) {
		t.Fatalf("no synthesis changed the result: %+v %+v", result, meta)
	}
	result, meta := ApplySynthesis(merged, &SynthesisResult{}, synthesisSpec, "SPEC.md")
	if len(result.Issues) != len(merged.Issues) || len(result.Patches) != len(merged.Patches) || meta.MergedFindings != 0 || len(meta.Retracted) != 0 {
		t.Fatalf("an empty synthesis changed the result: %+v %+v", result, meta)
	}
}

// Overlapping groups fold into one finding, which keeps the most severe
// severity among them.
func TestApplySynthesisJoinsOverlappingMergeGroups(t *testing.T) {
	merged := MergeResult{Issues: []schema.Issue{
		testIssue("ISSUE-0001", schema.SeverityCritical, schema.CategoryAmbiguousBehavior, "A", 1),
		testIssue("ISSUE-0002", schema.SeverityWarn, schema.CategoryAmbiguousBehavior, "B", 2),
		testIssue("ISSUE-0003", schema.SeverityInfo, schema.CategoryAmbiguousBehavior, "C", 3),
		testIssue("ISSUE-0004", schema.SeverityInfo, schema.CategoryAmbiguousBehavior, "D", 4),
	}}
	result, meta := ApplySynthesis(merged, &SynthesisResult{Merges: []MergeGroup{
		{IssueIDs: []string{"ISSUE-0003", "ISSUE-0002"}},
		{IssueIDs: []string{"ISSUE-0002", "ISSUE-0001"}},
		{IssueIDs: []string{"ISSUE-0004"}},
	}}, synthesisSpec, "SPEC.md")
	if len(result.Issues) != 2 || result.Issues[0].Title != "A" || result.Issues[0].Severity != schema.SeverityCritical || meta.MergedFindings != 2 {
		t.Fatalf("issues = %+v meta = %+v, want A (with B and C folded in) and D", result.Issues, meta)
	}
}

func TestBuildSynthesisTaskListsFindingsOnOneLine(t *testing.T) {
	merged := mergedForSynthesis()
	merged.Issues[0].Description = "The term\ntenant is used\n\nbut never defined. " + strings.Repeat("x", 300)
	task, err := BuildSynthesisTask(SynthesisInput{Merged: merged, SpecShown: true})
	if err != nil {
		t.Fatalf("BuildSynthesisTask: %v", err)
	}
	for _, want := range []string{
		"- ISSUE-0001 CRITICAL UNDEFINED_INTERFACE L5 [chunk]: Tenant is not defined - The term tenant is used but never defined.",
		"- PREFLIGHT-X WARN UNSPECIFIED_CONSTRAINT L3 [preflight]: Preflight finding",
		"- Q-0001 WARN L5: What is a tenant?",
		"never retract a [preflight] finding",
	} {
		if !strings.Contains(task, want) {
			t.Fatalf("task missing %q:\n%s", want, task)
		}
	}
	if strings.Contains(task, strings.Repeat("x", 200)) || strings.Contains(task, "{") {
		t.Fatalf("task carries long descriptions or JSON:\n%s", task)
	}
}

func TestRunSynthesisReturnsMergesAndRetractions(t *testing.T) {
	s, _ := executorFixture(t, 2)
	response := `{"issues":[],"questions":[],"patches":[],"merge":[{"issue_ids":["ISSUE-0001","ISSUE-0002"],"reason":"same"}],"retract":[{"id":"ISSUE-0003","line_start":2,"line_end":2,"quote":"Line 1","reason":"answered"}]}`
	provider := &captureSynthesisProvider{response: response}
	result, _, err := RunSynthesis(context.Background(), provider, s, nil, MergeResult{Issues: []schema.Issue{
		testIssue("ISSUE-0001", schema.SeverityWarn, schema.CategoryAmbiguousBehavior, "Finding", 1),
	}}, SynthesisConfig{Enabled: true, MaxTokens: 1000, EnforceSchema: true})
	if err != nil {
		t.Fatalf("RunSynthesis: %v", err)
	}
	if len(result.Merges) != 1 || len(result.Merges[0].IssueIDs) != 2 || len(result.Retractions) != 1 || result.Retractions[0].ID != "ISSUE-0003" {
		t.Fatalf("result = %+v, want the merge group and the retraction", result)
	}
	if provider.lastSchema == nil || provider.lastSchema.Name != "spec_synthesis" {
		t.Fatalf("schema = %+v, want the synthesis schema", provider.lastSchema)
	}
}

func TestRunSynthesisKeepsMergesFromEveryPartOfAContinuedResponse(t *testing.T) {
	s, _ := executorFixture(t, 2)
	issue := `{"id":"ISSUE-0001","severity":"WARN","category":"CONTRADICTION","title":"Cross","description":"d","evidence":[{"line_start":1,"line_end":1,"quote":"q"}],"impact":"i","recommendation":"r","blocking":false,"tags":[]}`
	provider := &captureSynthesisProvider{responses: []string{
		`{"merge":[{"issue_ids":["ISSUE-0001","ISSUE-0002"],"reason":"a"}],"retract":[],"issues":[` + issue + `,{"id":"ISSUE-0002","sev`,
		`{"issues":[],"questions":[],"patches":[],"merge":[{"issue_ids":["ISSUE-0003","ISSUE-0004"],"reason":"b"}],"retract":[]}`,
	}}
	result, _, err := RunSynthesis(context.Background(), provider, s, nil, MergeResult{Issues: []schema.Issue{
		testIssue("ISSUE-0001", schema.SeverityWarn, schema.CategoryAmbiguousBehavior, "Finding", 1),
	}}, SynthesisConfig{Enabled: true, MaxTokens: 1000})
	if err != nil {
		t.Fatalf("RunSynthesis: %v", err)
	}
	if provider.calls != 2 || len(result.Merges) != 2 || len(result.Report.Issues) != 1 {
		t.Fatalf("calls = %d, result = %+v; want both parts' merge groups and the one complete issue", provider.calls, result)
	}
}

type captureSynthesisProvider struct {
	calls         int
	response      string
	responses     []string
	lastPrompt    string
	lastMaxTokens int
	lastSchema    *llm.OutputSchema
}

func (p *captureSynthesisProvider) Complete(_ context.Context, req *llm.Request) (*llm.Response, error) {
	p.calls++
	p.lastPrompt = req.UserPrompt
	p.lastMaxTokens = req.MaxTokens
	p.lastSchema = req.Schema
	if len(p.responses) > 0 {
		return &llm.Response{Content: p.responses[p.calls-1], Model: "fake:synthesis"}, nil
	}
	return &llm.Response{Content: p.response, Model: "fake:synthesis"}, nil
}

func synthesisResponse(tags string) string {
	return `{"issues":[{"id":"ISSUE-9000","severity":"CRITICAL","category":"CONTRADICTION","title":"Cross Section","description":"desc","evidence":[{"path":"SPEC.md","line_start":1,"line_end":1,"quote":"q"}],"impact":"impact","recommendation":"rec","blocking":true,"tags":` + tags + `}],"questions":[],"patches":[],"meta":{}}`
}

// A patch written for a finding that is folded into another, through a chain
// of overlapping groups, follows it to the finding that is kept.
func TestApplySynthesisKeepsPatchesOfFoldedFindings(t *testing.T) {
	const specText = "alpha line\nbeta line\ngamma line\n"
	merged := MergeResult{
		Issues: []schema.Issue{
			testIssue("ISSUE-0001", schema.SeverityCritical, schema.CategoryAmbiguousBehavior, "A", 1),
			testIssue("ISSUE-0002", schema.SeverityWarn, schema.CategoryAmbiguousBehavior, "B", 2),
			testIssue("ISSUE-0003", schema.SeverityInfo, schema.CategoryAmbiguousBehavior, "C", 3),
		},
		Patches: []schema.Patch{
			{IssueID: "ISSUE-0002", Before: "beta line", After: "beta line, defined"},
			{IssueID: "ISSUE-0003", Before: "gamma line", After: "gamma line, defined"},
		},
	}
	result, _ := ApplySynthesis(merged, &SynthesisResult{Merges: []MergeGroup{
		{IssueIDs: []string{"ISSUE-0003", "ISSUE-0002"}},
		{IssueIDs: []string{"ISSUE-0002", "ISSUE-0001"}},
	}}, specText, "SPEC.md")
	if len(result.Issues) != 1 || len(result.Patches) != 2 {
		t.Fatalf("issues = %d patches = %+v, want one finding keeping both patches", len(result.Issues), result.Patches)
	}
	for _, patch := range result.Patches {
		if patch.IssueID != result.Issues[0].ID {
			t.Errorf("patch %q points at %s, want %s", patch.Before, patch.IssueID, result.Issues[0].ID)
		}
	}
}

func TestParseSynthesisResponseIgnoresAMalformedList(t *testing.T) {
	raw := `{"issues":[],"questions":[],"patches":[],"merge":[{"issue_ids":["ISSUE-0001","ISSUE-0002"],"reason":"a"},{"issue_ids":"ISSUE-0003","reason":"b"}],"retract":[{"id":"ISSUE-0004","line_start":1,"line_end":1,"quote":"x","reason":"r"}]}`
	_, extras, err := parseSynthesisResponse(raw, "SPEC.md", "", 10)
	if err != nil {
		t.Fatalf("parseSynthesisResponse: %v", err)
	}
	if len(extras.merges) != 0 {
		t.Errorf("merges = %+v, want the malformed list dropped whole", extras.merges)
	}
	if len(extras.retractions) != 1 {
		t.Errorf("retractions = %+v, want the well-formed list kept", extras.retractions)
	}
}
