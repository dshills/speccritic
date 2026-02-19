package profile

func general() *Profile {
	return &Profile{
		Name: "general",
		ForbiddenPhrases: []string{
			"as needed", "as appropriate", "TBD", "to be determined",
			"fast", "quickly", "efficiently", "reasonable", "acceptable",
			"etc.", "and so on", "handle errors appropriately",
		},
		DomainInvariants: []string{
			"Every requirement must be verifiable by a test or inspection",
			"All failure modes must be explicitly stated",
			"Interfaces between components must be fully defined",
		},
	}
}
