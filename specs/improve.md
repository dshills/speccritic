# SpecCritic Performance and Iteration Improvements

SpecCritic has three separate performance problems: single-run latency, number of review loops, and token volume. They overlap, but they need different fixes.

**Speed**
The biggest win is to stop making one large “review everything” model call for every check.

Practical options:

1. **Two-pass review**
   - Fast deterministic preflight first: missing sections, TODO/TBD, vague terms, undefined acronyms, duplicate headings, weak acceptance criteria, missing error cases.
   - Only call the LLM after those cheap checks pass or when the user asks for deeper review.

2. **Section-level parallel review**
   - Split the spec by headings.
   - Run smaller model calls in parallel per section or section group.
   - Give each chunk a table of contents and bounded global context.
   - Run one final synthesis/merge/dedup pass that checks cross-section consistency.
   - This usually improves wall-clock time even if total token usage is similar.

3. **Use a faster model for first-pass critique**
   - Fast model: find likely issues/questions.
   - Stronger model: only validate high-impact findings or final release review.
   - This fits SpecCritic well because not every pass needs maximal reasoning.

4. **Cache by spec hash**
   - Cache full results by `spec_hash + context_hash + profile + strict + model + prompt_version`.
   - Include all external context files/documents in `context_hash`.
   - For unchanged sections, reuse previous section findings only when their dependency inputs are unchanged.
   - Track simple section dependencies so global contract changes can invalidate affected section results.
   - For edited specs, review changed sections plus nearby and dependent sections.

5. **Incremental/diff mode**
   - Instead of resubmitting the whole spec every time, send:
     - changed lines,
     - prior findings,
     - unresolved questions,
     - relevant surrounding sections.
   - Before selecting sections, build a lightweight impact graph from headings, referenced terms, requirement IDs, interface names, glossary entries, and prior finding evidence.
   - Include changed sections, directly referenced sections, global definitions, and any sections invalidated by changed constraints.
   - If the impact graph cannot explain the dependency boundary, fall back to chunked whole-spec review.
   - Ask: “Are previous blockers resolved? Did this edit introduce new blockers?”

**Reducing 10-20 Iterations**
This is probably the larger product opportunity. SpecCritic should help the user converge, not just keep finding the next layer of ambiguity.

Ideas:

1. **Batch questions by decision**
   Instead of many individual questions, group them into decision sets:

   - API error behavior
   - authorization/authentication
   - persistence/retention
   - retry/idempotency
   - UX state transitions
   - deployment/configuration

   Then ask the user to answer one compact decision block.

2. **Generate a “spec completion patch”**
   After review, produce a proposed patch that fills common missing structure:
   - error codes
   - edge cases
   - acceptance criteria
   - invariants
   - state transitions
   - non-goals
   - open decisions

   Completion patches must be treated as draft alternatives, not inferred truth. Every hunk must cite the issue or question it addresses, explain whether it is filling an explicitly stated requirement or proposing a user decision, and require explicit user confirmation before being applied. When the missing behavior is business-specific, the patch should insert an `OPEN DECISION` placeholder instead of inventing requirements.

   Keep findings hostile, but make the patch useful and auditable.

3. **Add a “convergence mode”**
   Track unresolved findings across runs:
   - resolved
   - still present
   - replaced by new ambiguity
   - newly introduced

   This avoids the feeling that every pass starts from scratch.

4. **Add templates by profile**
   Before critique, validate against a profile-specific spec skeleton. For example, `backend-api` could require:
   - endpoints
   - schemas
   - auth
   - errors
   - rate limits
   - idempotency
   - observability
   - rollout/migration
   - tests

   A better starting structure reduces rounds.

5. **Add “readiness checklist” output**
   Instead of only issues, output a short checklist:
   - “Answer these 5 questions before rerunning.”
   - “Add these 3 sections.”
   - “These 2 findings are likely duplicates.”
   - “Do not rerun until X/Y/Z are addressed.”

**Token-Lowering Strategies**
Good candidates:

1. **Compact prompt format**
   Use line-numbered spec, but avoid verbose instruction blocks every time. Move stable rules into compact enumerated criteria.

2. **Schema minimization**
   For model output, reduce repeated text:
   - Use short enum keys.
   - Avoid long quotes when line references are enough.
   - Include only one quote per finding, not every supporting excerpt.
   - Generate Markdown rendering from compact JSON locally.

3. **Section chunking**
   Send only relevant sections for targeted checks. For whole-spec checks, summarize stable sections and fully include risky/changed sections.

4. **Prior-result compression**
   Store prior findings locally. On rerun, send compact prior state:
   ```text
   ISSUE-0003 unresolved, lines 42-55, ambiguity: timeout behavior
   ISSUE-0004 resolved by lines 88-94
   ```
   Not the entire previous report.

5. **Context ranking**
   If context files exist, don’t include all of them. Retrieve only sections matching terms from the spec section under review.

6. **Line-window evidence**
   For changed-line review, send ±20 lines around edits plus headings/TOC. Only escalate to whole-spec review if cross-section dependencies are detected.
   First run a cheap high-level impact scan over headings, changed sections, and global definitions so non-local effects are not missed.

7. **Progressive strictness**
   Run non-strict first. Strict mode only after the spec is structurally complete. Strict mode tends to generate more assumptions/questions and therefore more tokens.

**What I’d Build First**
I’d prioritize this order:

1. Add deterministic preflight checks. (DONE)
2. Add section-level chunking with parallel LLM calls. (DONE)
3. Add incremental rerun mode using previous result + changed sections. (DONE)
4. Add convergence tracking: resolved/still-open/new. (DONE)
5. Add profile-specific completion templates and patch suggestions. (DONE)

That should improve perceived speed and reduce the number of full-spec model calls. The biggest UX improvement will come from making each run tell the user exactly what to fix before rerunning, instead of just producing another large critique.
