# Deterministic Preflight Limitations

Preflight is a fast local screen, not a replacement for model review.

- Pattern rules intentionally prefer obvious defects over semantic nuance.
- Missing-section checks depend on recognizable Markdown headings and profile-specific synonyms.
- Acronym checks use local parenthetical and glossary definitions plus a built-in allow-list; domain-specific acronyms may need normal prose definitions to avoid warnings.
- Measurable-criteria checks look for nearby numeric values, durations, percentages, sizes, or explicit limits. Multi-line requirements can still require LLM review.
- Preflight/LLM duplicate merging is conservative; semantically similar findings may both appear when their evidence or tags do not line up exactly.
