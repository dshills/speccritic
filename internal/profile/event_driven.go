package profile

import "github.com/dshills/speccritic/internal/schema"

func eventDriven() *Profile {
	return &Profile{
		Name: "event-driven",
		RequiredSections: []string{
			"Event Schema", "Delivery Guarantees", "Consumer Failure",
		},
		ForbiddenPhrases: []string{
			"as needed", "eventually consistent", "best effort",
			"usually", "typically",
		},
		DomainInvariants: []string{
			"Every event type must have defined ordering guarantees, or explicitly state that ordering is not guaranteed",
			"At-least-once vs exactly-once delivery semantics must be stated per event type",
			"Consumer failure modes and retry policies must be specified",
			"Schema evolution strategy (backward/forward compatibility) must be present",
		},
		ExtraCategories: []schema.Category{
			schema.CategoryOrderingUndefined,
			schema.CategoryMissingFailureMode,
			schema.CategoryUnspecifiedConstraint,
		},
	}
}
