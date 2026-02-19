package profile

import "github.com/dshills/speccritic/internal/schema"

func regulatedSystem() *Profile {
	return &Profile{
		Name: "regulated-system",
		RequiredSections: []string{
			"Audit Trail", "Data Retention", "Access Control",
		},
		ForbiddenPhrases: []string{
			"as needed", "as appropriate", "TBD", "reasonable period",
			"periodically", "eventually",
		},
		DomainInvariants: []string{
			"Audit trail requirements must be explicit and enumerated",
			"Data retention periods must be stated as concrete durations (e.g. 7 years)",
			"Every state transition must be enumerable and auditable",
			"Rollback behavior must be defined for every mutating operation",
		},
		ExtraCategories: []schema.Category{
			schema.CategoryMissingInvariant,
			schema.CategoryMissingFailureMode,
			schema.CategoryOrderingUndefined,
		},
	}
}
