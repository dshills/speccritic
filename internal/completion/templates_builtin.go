package completion

import "github.com/dshills/speccritic/internal/schema"

func generalTemplate() Template {
	return Template{Name: schema.CompletionTemplateGeneral, Sections: []TemplateSection{
		section(1, "Purpose", []schema.Category{schema.CategoryUnspecifiedConstraint}, []string{"PREFLIGHT-STRUCTURE-001"}, decision("OPEN DECISION: State the concrete purpose this specification must satisfy.", "purpose", "goal")),
		section(2, "Non-Goals", []schema.Category{schema.CategoryScopeLeak}, []string{"PREFLIGHT-STRUCTURE-002"}, decision("OPEN DECISION: State behavior or scope that this specification intentionally excludes.", "scope", "non-goal")),
		section(3, "Functional Requirements", []schema.Category{schema.CategoryUnspecifiedConstraint, schema.CategoryAssumptionRequired}, []string{"PREFLIGHT-STRUCTURE-003"}, decision("OPEN DECISION: State the observable behavior required before implementation can begin.", "behavior", "requirement")),
		section(4, "Acceptance Criteria", []schema.Category{schema.CategoryNonTestableRequirement}, []string{"PREFLIGHT-STRUCTURE-004"}, note("Add observable input, action, and expected outcome checks for each requirement.", "test", "acceptance")),
		section(5, "Failure Modes", []schema.Category{schema.CategoryMissingFailureMode}, nil, decision("OPEN DECISION: State expected behavior for the failure condition named by the finding.", "timeout", "invalid input", "dependency", "permission")),
		section(6, "Open Decisions", []schema.Category{schema.CategoryAmbiguousBehavior, schema.CategoryOrderingUndefined}, nil, decision("OPEN DECISION: Resolve the implementation-blocking choice identified by the finding.", "decision", "ambiguous")),
	}}
}

func backendAPITemplate() Template {
	return Template{Name: schema.CompletionTemplateBackendAPI, Sections: []TemplateSection{
		section(1, "Endpoints", []schema.Category{schema.CategoryUndefinedInterface}, []string{"PREFLIGHT-STRUCTURE-102"}, decision("OPEN DECISION: Define each endpoint path, method, request, response, and owning behavior.", "endpoint", "route")),
		section(2, "Authentication and Authorization", []schema.Category{schema.CategoryMissingInvariant, schema.CategoryUnspecifiedConstraint}, []string{"PREFLIGHT-STRUCTURE-101"}, decision("OPEN DECISION: Define authentication mechanism and per-endpoint authorization rules.", "auth", "permission")),
		section(3, "Request and Response Schemas", []schema.Category{schema.CategoryUndefinedInterface}, []string{"PREFLIGHT-STRUCTURE-103"}, decision("OPEN DECISION: Define exact request and response fields, types, required fields, and validation rules.", "schema", "request", "response")),
		section(4, "Error Responses", []schema.Category{schema.CategoryMissingFailureMode}, []string{"PREFLIGHT-STRUCTURE-104"}, decision("OPEN DECISION: Define exact HTTP status codes, response body shape, and retryability for each error condition.", "error", "status")),
		section(5, "Rate Limits and Abuse Handling", []schema.Category{schema.CategoryUnspecifiedConstraint}, []string{"PREFLIGHT-STRUCTURE-105"}, decision("OPEN DECISION: Define exact request count, time window, abuse signal, and enforcement response.", "rate", "abuse")),
		section(6, "Idempotency and Repeat Submission Behavior", []schema.Category{schema.CategoryOrderingUndefined, schema.CategoryMissingFailureMode}, nil, decision("OPEN DECISION: Define idempotency keys, repeat submission behavior, and retry result semantics.", "idempotency", "retry")),
		section(7, "Observability", []schema.Category{schema.CategoryMissingInvariant}, nil, note("Add required logs, metrics, traces, and alerting signals for API behavior.", "observability", "metrics")),
		section(8, "Acceptance Tests", []schema.Category{schema.CategoryNonTestableRequirement}, nil, note("Add observable API test cases covering success, validation, auth, error, and limit behavior.", "test", "acceptance")),
	}}
}

func regulatedSystemTemplate() Template {
	return Template{Name: schema.CompletionTemplateRegulatedSystem, Sections: []TemplateSection{
		section(1, "Compliance Scope", []schema.Category{schema.CategoryUnspecifiedConstraint}, []string{"PREFLIGHT-STRUCTURE-204"}, decision("OPEN DECISION: State the compliance scope named by the spec or context without inventing obligations.", "compliance")),
		section(2, "Data Classification", []schema.Category{schema.CategoryMissingInvariant}, nil, decision("OPEN DECISION: Define data classes, sensitivity, and handling constraints.", "data")),
		section(3, "Access Control", []schema.Category{schema.CategoryMissingInvariant}, []string{"PREFLIGHT-STRUCTURE-203"}, decision("OPEN DECISION: Define roles, permissions, approval requirements, and denied-access behavior.", "access", "permission")),
		section(4, "Audit Trail", []schema.Category{schema.CategoryMissingInvariant}, []string{"PREFLIGHT-STRUCTURE-201"}, decision("OPEN DECISION: Define audited actors, actions, timestamps, immutable fields, and audit access controls.", "audit")),
		section(5, "Data Lifecycle and Deletion", []schema.Category{schema.CategoryUnspecifiedConstraint}, []string{"PREFLIGHT-STRUCTURE-202"}, decision("OPEN DECISION: Define retention periods, deletion triggers, deletion verification, and legal-hold behavior.", "retention", "deletion")),
		section(6, "Approval and Review Workflow", []schema.Category{schema.CategoryOrderingUndefined}, nil, decision("OPEN DECISION: Define approval steps, reviewers, evidence, and rejection behavior.", "approval", "review")),
		section(7, "Incident and Exception Handling", []schema.Category{schema.CategoryMissingFailureMode}, nil, decision("OPEN DECISION: Define incident detection, escalation, exception approval, and remediation behavior.", "incident", "exception")),
		section(8, "Validation Evidence", []schema.Category{schema.CategoryNonTestableRequirement}, nil, note("Add objective validation evidence required before release.", "validation", "evidence")),
	}}
}

func eventDrivenTemplate() Template {
	return Template{Name: schema.CompletionTemplateEventDriven, Sections: []TemplateSection{
		section(1, "Event Producers and Consumers", []schema.Category{schema.CategoryUndefinedInterface}, nil, decision("OPEN DECISION: Define each producer, consumer, event, and ownership boundary.", "producer", "consumer")),
		section(2, "Event Schema", []schema.Category{schema.CategoryUndefinedInterface}, []string{"PREFLIGHT-STRUCTURE-301"}, decision("OPEN DECISION: Define event fields, types, required fields, versioning, and compatibility rules.", "schema", "event")),
		section(3, "Delivery Guarantees", []schema.Category{schema.CategoryMissingInvariant}, []string{"PREFLIGHT-STRUCTURE-302"}, decision("OPEN DECISION: Define delivery guarantee and duplicate handling behavior.", "delivery", "duplicate")),
		section(4, "Ordering and Idempotency", []schema.Category{schema.CategoryOrderingUndefined}, nil, decision("OPEN DECISION: Define ordering key, partitioning rules, and idempotency behavior.", "order", "partition", "sequence")),
		section(5, "Retry and Failed-Event Queue Behavior", []schema.Category{schema.CategoryMissingFailureMode}, []string{"PREFLIGHT-STRUCTURE-303"}, decision("OPEN DECISION: Define retry policy, failed-event queue retention, alerting, replay, and poison-message behavior.", "retry", "dead-letter", "failed-event")),
		section(6, "Consumer Failure Behavior", []schema.Category{schema.CategoryMissingFailureMode}, []string{"PREFLIGHT-STRUCTURE-304"}, decision("OPEN DECISION: Define consumer failure detection, backoff, blocking, and recovery behavior.", "consumer", "failure")),
		section(7, "Backfill and Replay", []schema.Category{schema.CategoryOrderingUndefined, schema.CategoryMissingFailureMode}, nil, decision("OPEN DECISION: Define backfill source, replay limits, ordering, and duplicate handling.", "backfill", "replay")),
		section(8, "Observability", []schema.Category{schema.CategoryMissingInvariant}, nil, note("Add event metrics, logs, traces, lag indicators, and alerting signals.", "observability", "lag")),
	}}
}
