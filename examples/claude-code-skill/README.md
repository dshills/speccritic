# SpecCritic as a Claude Code Skill

This directory contains an example [Claude Code Skill](https://docs.claude.com/en/docs/claude-code/skills) that teaches Claude Code when and how to run `speccritic` as the first gate in a SPEC → PLAN → CODE pipeline.

The skill in [`SKILL.md`](./SKILL.md) is what the author uses day-to-day. Treat it as a starting point — tune the trigger phrases, profile defaults, and pipeline handoff language to match your team's workflow.

## What this skill does

When active, Claude Code will:

- Invoke `speccritic check` on a spec file when you ask it to (`run speccritic`, `gate the spec`, `check SPEC.md`, etc.).
- Parse `.speccritic-review.json` and decide on `summary.verdict` — not on issue counts.
- Apply CRITICAL `issue.recommendation` edits to the cited line ranges in `SPEC.md`.
- Surface CRITICAL `question` entries to you verbatim rather than guessing answers.
- Loop until the verdict is `VALID` (or `VALID_WITH_GAPS` with documented WARNs) before handing off to the next gate.

## Prerequisites

1. **Claude Code** installed — see [claude.com/claude-code](https://claude.com/claude-code).
2. **`speccritic` on your `$PATH`**:
   ```sh
   go install github.com/dshills/speccritic/cmd/speccritic@latest
   ```
3. **Model + API key** exported in the shell that runs Claude Code:
   ```sh
   export SPECCRITIC_MODEL=anthropic:claude-sonnet-4-6   # or openai:gpt-4o, gemini:gemini-2.0-flash
   export ANTHROPIC_API_KEY=...                          # or OPENAI_API_KEY / GEMINI_API_KEY
   ```

## Install

Claude Code looks for skills in `~/.claude/skills/<skill-name>/SKILL.md` (user-level) or `.claude/skills/<skill-name>/SKILL.md` (project-level).

### User-level install (recommended)

Makes the skill available to Claude Code in every project.

```sh
mkdir -p ~/.claude/skills/speccritic
cp examples/claude-code-skill/SKILL.md ~/.claude/skills/speccritic/SKILL.md
```

### Project-level install

Scoped to a single repository. Commit the file so the rest of the team picks it up.

```sh
mkdir -p .claude/skills/speccritic
cp examples/claude-code-skill/SKILL.md .claude/skills/speccritic/SKILL.md
```

## Verify

Start a new Claude Code session and ask:

> run speccritic on specs/SPEC.md

Claude Code should invoke `speccritic check`, write `.speccritic-review.json`, read the verdict, and either apply CRITICAL recommendations directly or surface CRITICAL questions back to you. If it instead tries to review the spec itself without calling the CLI, the skill frontmatter did not match — check that the file is at `~/.claude/skills/speccritic/SKILL.md` and restart the session.

## Customizing

Common edits:

- **Trigger phrases** — the `description` field in the frontmatter is what Claude Code matches against. Add phrases your team actually uses.
- **Default profile** — the shipped skill defaults to `--profile regulated-system` for clinical-trial work. Change this to `general`, `backend-api`, or `event-driven` to match your domain.
- **Pipeline handoff** — the final section names the next gate (`plancritic`). Remove or rename if your pipeline differs.

## Related

- Main project: [`../../README.md`](../../README.md)
- Spec format and invariants: [`../../specs/SPEC.md`](../../specs/SPEC.md)
- Workflow that chains speccritic with plancritic, realitycheck, prism, and clarion: [`../../WORKFLOW.md`](../../WORKFLOW.md)
