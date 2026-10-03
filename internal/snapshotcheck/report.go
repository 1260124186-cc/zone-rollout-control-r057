// Package snapshotcheck verifies the internal consistency of a state
// snapshot offline, without starting the service. The scan is read-only:
// it never creates, locks, modifies, or replaces the snapshot file.
//
// Checks run in a stable order (entity references, state constraints,
// request indexes, counts and summaries) and every collection is visited
// in sorted key order, so repeated runs over the same file produce the
// same report and the same problem ordering.
package snapshotcheck

// Overall report statuses.
const (
	StatusOK       = "ok"
	StatusIssues   = "issues_found"
	StatusCanceled = "canceled"
	StatusError    = "error"
)

// Check names in stable execution order.
const (
	CheckReferences  = "entity_references"
	CheckConstraints = "state_constraints"
	CheckIndexes     = "request_indexes"
	CheckSummaries   = "counts_and_summaries"
)

var checkOrder = []string{CheckReferences, CheckConstraints, CheckIndexes, CheckSummaries}

// Options tunes a single scan. MaxProblems bounds the collected problem
// list; zero means no limit. StatePath is reported back for correlation.
type Options struct {
	StatePath   string
	MaxProblems int
}

// Report is the machine-readable result of one scan. It contains no
// timestamps, durations, or random values, so it is byte-stable across
// repeated runs over the same snapshot.
type Report struct {
	Status        string         `json:"status"`
	Error         string         `json:"error,omitempty"`
	StatePath     string         `json:"state_path"`
	SchemaVersion int            `json:"schema_version"`
	Revision      uint64         `json:"revision"`
	Counts        Counts         `json:"counts"`
	Checks        []CheckSummary `json:"checks"`
	ProblemCount  int            `json:"problem_count"`
	Problems      []Problem      `json:"problems"`
	Truncated     bool           `json:"truncated"`
	Canceled      bool           `json:"canceled"`
}

// Counts always reflects the full collection sizes, even when the scan
// was canceled or truncated. EntitiesScanned counts examined items
// (entities, entries, and index records) and shows scan progress.
type Counts struct {
	Plans                 int `json:"plans"`
	Revisions             int `json:"revisions"`
	Nodes                 int `json:"nodes"`
	Leases                int `json:"leases"`
	LeaseGroups           int `json:"lease_groups"`
	Probes                int `json:"probes"`
	Assessments           int `json:"assessments"`
	LeaseRequestKeys      int `json:"lease_request_keys"`
	AssessmentRequestKeys int `json:"assessment_request_keys"`
	AuditEvents           int `json:"audit_events"`
	EntitiesScanned       int `json:"entities_scanned"`
}

// CheckSummary describes the outcome of one check group.
type CheckSummary struct {
	Name      string `json:"name"`
	Completed bool   `json:"completed"`
	Problems  int    `json:"problems"`
}

// Problem is a single integrity violation. Entity and Field locate the
// offending value; Details carries stable identifiers for automation.
type Problem struct {
	Check    string         `json:"check"`
	Code     string         `json:"code"`
	Entity   string         `json:"entity"`
	EntityID string         `json:"entity_id,omitempty"`
	Field    string         `json:"field,omitempty"`
	Message  string         `json:"message"`
	Details  map[string]any `json:"details,omitempty"`
}

// FailureReport builds the report emitted when the snapshot cannot be
// read or decoded at all. No check is marked completed.
func FailureReport(path string, cause error) Report {
	return Report{
		Status:    StatusError,
		Error:     cause.Error(),
		StatePath: path,
		Checks:    summarizeChecks(map[string]int{}, map[string]bool{}),
		Problems:  []Problem{},
	}
}

func summarizeChecks(perCheck map[string]int, completed map[string]bool) []CheckSummary {
	summaries := make([]CheckSummary, 0, len(checkOrder))
	for _, name := range checkOrder {
		summaries = append(summaries, CheckSummary{
			Name:      name,
			Completed: completed[name],
			Problems:  perCheck[name],
		})
	}
	return summaries
}
