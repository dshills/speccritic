package profile

import "github.com/dshills/speccritic/internal/schema"

func backendAPI() *Profile {
	return &Profile{
		Name: "backend-api",
		RequiredSections: []string{
			"Authentication", "Error Codes", "Rate Limiting",
		},
		ForbiddenPhrases: []string{
			"as needed", "as appropriate", "TBD", "fast", "quickly",
		},
		DomainInvariants: []string{
			"Every endpoint must define its request and response schemas",
			"All error codes must be enumerated with their conditions",
			"Authentication requirements must be stated per endpoint",
			"Rate limits must be expressed as numeric values with time windows",
		},
		ExtraCategories: []schema.Category{
			schema.CategoryUndefinedInterface,
			schema.CategoryMissingFailureMode,
		},
	}
}
