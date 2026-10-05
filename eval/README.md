# SpecCritic eval

This eval answers one question: when SpecCritic reviews a spec whose defects are known, does it find them, and does it leave a sound spec alone?

Use it before and after any change to prompts, chunking, model or effort. A change that only looks better on one hand-picked spec is not evidence.

## Status

The eval has not been run against a model yet. Until it has, and until someone has read the results, treat two things as unconfirmed:

- **The clean specs may not be clean.** They were written to be complete and they pass preflight with no findings, but a model may still find a real gap in one. The first run is the calibration: read every CRITICAL reported on a clean spec, then either fix the spec or accept the finding as a false positive.
- **The labels have not been checked by a second person.** Each defect was seeded on purpose, so it exists by construction. Whether its category, severity and keywords are the best description is a judgment that deserves a second look.

## Running it

```bash
make eval-plan
```

prints the cases and the model that would be used, and spends nothing.

```bash
make eval
```

reviews every case with the configured model. With the defaults that is 8 cases, 3 runs each, 24 reviews, and it costs real money. The model comes from `SPECCRITIC_LLM_PROVIDER` and `SPECCRITIC_LLM_MODEL`, the same as the CLI.

Pass options through `EVAL_FLAGS`:

```bash
make eval EVAL_FLAGS="--runs 5 --effort high"
make eval EVAL_FLAGS="--cases rate-limiter --runs 1"
make eval EVAL_FLAGS="--chunking off"
make eval EVAL_FLAGS="--isolated"
```

`--isolated` adds one spec per single defect. It shows exactly which defect was missed, at about four times the cost. Run `go run ./cmd/speccritic-eval --help` for every option.

To compare two settings, run the eval once with each and compare the two `summary.md` files. The interval beside each rate says how far that rate could move if the same run were repeated; it is not a test of the difference between two runs. As a working rule, do not act on a difference smaller than those intervals, and confirm a larger one by looking at which defects changed in the "By defect" table.

## What is in the corpus

`corpus/` holds four clean specs. Each has a `.defects.json` file listing mutations: small edits that each seed one defect.

| Spec | Lines | Review path it exercises |
|---|---|---|
| `rate-limiter` | 71 | single call |
| `link-shortener` | 79 | single call |
| `job-queue` | 88 | single call |
| `export-service` | 127 | chunked, with two defects that span sections |

Every spec is reviewed twice: unchanged (the clean case) and with all six of its mutations applied (the seeded case). That is 24 seeded defects across ten categories. None of them is caught by preflight, so the results measure the model.

A mutation names its edits, the categories that correctly describe the defect, the severity a reviewer should assign, keywords, and where a finding should point. The location is given as text or a section title, not a line number, so labels do not drift when a spec changes. A unit test checks that every edit applies and every anchor resolves.

## How a review is graded

A finding matches a seeded defect when both of these hold:

- it cites a line within two lines of the defect's evidence, and
- it is about the same thing: its category is one the defect accepts, or its text contains one of the defect's keywords.

Location alone does not count, because an unrelated remark about the same line would otherwise earn credit. Several findings may match one defect; they are repeats, not errors.

Grading is deterministic. No model judges another model's output.

## What it reports

`summary.md` in the run's directory gives each rate with its 95% interval.

| Measure | Meaning |
|---|---|
| Defects found | Recall over all seeded defects |
| CRITICAL defects found and reported as CRITICAL | A miss here lets a blocking defect through the gate |
| Category and severity agreement | Among found defects, how many were described as labeled |
| Model findings that match a label | A lower bound on precision; see below |
| Clean runs that reported a CRITICAL | The false-alarm rate that fails a sound spec |
| Verdict agreement | How often repeated runs of one case return the same verdict |
| Cost | Calls, tokens by kind and wall-clock time, from `meta.usage` |

It also lists recall and precision per category, the found rate of each individual defect, and every model finding that matched no label.

A run directory holds `specs/` (what was reviewed), `reports/` (every full report), `results.jsonl` (one graded review per line), `errors.jsonl` and the two summaries. `eval/results/` is not committed.

## Reading the numbers

- **Intervals are wide.** Twenty-four defects over three runs gives 72 observations, so overall recall is known to about plus or minus 10 points at best. The true uncertainty is larger, because three runs on the same defect are not independent observations. Per-category figures rest on a handful of defects each and are for spotting a category that is never found, not for comparing two settings.
- **Precision is a lower bound.** A finding that matches no label counts against it, even when it is a real defect the labels do not list. Read the unmatched findings before quoting the figure.
- **A failed review is not a review that found nothing.** A provider error, a timeout or invalid output goes to `errors.jsonl` and stays out of every rate.
- **The eval measures the tool as shipped.** It calls the same entry point as library callers with the CLI's defaults, preflight included.

## Adding a spec

1. Write `corpus/NAME.md`. Run `speccritic check corpus/NAME.md --preflight-mode only` and revise until it reports nothing.
2. Write `corpus/NAME.defects.json` with an `id` equal to `NAME`. Copy the shape of an existing file. Give every mutation at least one evidence anchor that exists after all of the spec's mutations are applied.
3. Run `go test ./internal/eval`. It fails if an edit does not apply exactly once or an anchor does not resolve.
