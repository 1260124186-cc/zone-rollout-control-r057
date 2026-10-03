// Package verify implements the offline snapshot integrity check. It reads a
// state snapshot without locking, creating, modifying, or replacing it, and
// reports every consistency violation it can find in a stable order.
package verify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"

	"solo-0014-zone-rollout-control/internal/domain"
)

// Report statuses.
const (
	StatusOK         = "ok"
	StatusProblems   = "problems"
	StatusIncomplete = "incomplete"
)

// Incomplete reasons reported when the scan could not run to completion.
const (
	ReasonSnapshotUnreadable = "snapshot_unreadable"
	ReasonSnapshotTooLarge   = "snapshot_too_large"
	ReasonDecodeFailed       = "decode_failed"
	ReasonProblemLimit       = "problem_limit_reached"
	ReasonCancelled          = "cancelled"
	ReasonTimeout            = "timeout_exceeded"
)

// supportedSchemaVersion mirrors the schema version written by the store.
const supportedSchemaVersion = 1

const (
	// DefaultMaxBytes bounds the snapshot size the checker will read.
	DefaultMaxBytes = 64 << 20
	// DefaultMaxProblems bounds the number of problems collected per scan.
	DefaultMaxProblems = 1024
	// tickInterval controls how often cancellation is observed while scanning.
	tickInterval = 128
)

// Options configures a single scan.
type Options struct {
	// MaxBytes is the largest snapshot the checker will read.
	MaxBytes int64
	// MaxProblems is the largest number of problems collected before the
	// scan stops and reports a truncated result.
	MaxProblems int
}

func (opts Options) withDefaults() Options {
	if opts.MaxBytes <= 0 {
		opts.MaxBytes = DefaultMaxBytes
	}
	if opts.MaxProblems <= 0 {
		opts.MaxProblems = DefaultMaxProblems
	}
	return opts
}

// Problem is a single machine-readable integrity violation.
type Problem struct {
	Code     string         `json:"code"`
	Entity   string         `json:"entity"`
	EntityID string         `json:"entity_id,omitempty"`
	Field    string         `json:"field,omitempty"`
	Message  string         `json:"message"`
	Details  map[string]any `json:"details,omitempty"`
}

// Counts summarizes the collections found in the snapshot.
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
}

func (c Counts) total() int {
	return c.Plans + c.Revisions + c.Nodes + c.Leases + c.LeaseGroups +
		c.Probes + c.Assessments + c.AuditEvents
}

// Report is the deterministic outcome of one scan. It contains no timestamps
// or environment data, so repeated scans of the same file produce identical
// reports.
type Report struct {
	SnapshotPath     string    `json:"snapshot_path"`
	Status           string    `json:"status"`
	SchemaVersion    int       `json:"schema_version"`
	Revision         uint64    `json:"revision"`
	Counts           Counts    `json:"counts"`
	ProblemCount     int       `json:"problem_count"`
	Problems         []Problem `json:"problems"`
	Truncated        bool      `json:"truncated"`
	IncompleteReason string    `json:"incomplete_reason,omitempty"`
	Message          string    `json:"message,omitempty"`
}

// CheckFile scans the snapshot at path without modifying it and returns the
// integrity report. The scan stops early when ctx is cancelled, when the
// snapshot exceeds the configured size bound, or when the problem bound is
// reached; in those cases the report status is StatusIncomplete.
func CheckFile(ctx context.Context, path string, opts Options) Report {
	opts = opts.withDefaults()
	report := Report{
		SnapshotPath: path,
		Status:       StatusIncomplete,
		Problems:     []Problem{},
	}
	fail := func(reason string, err error) Report {
		report.IncompleteReason = reason
		if err != nil {
			report.Message = err.Error()
		}
		return report
	}
	if err := ctx.Err(); err != nil {
		return fail(cancelReason(err), nil)
	}
	info, err := os.Stat(path)
	if err != nil {
		return fail(ReasonSnapshotUnreadable, fmt.Errorf("stat snapshot: %w", err))
	}
	if !info.Mode().IsRegular() {
		return fail(ReasonSnapshotUnreadable, fmt.Errorf("snapshot path %q is not a regular file", path))
	}
	if info.Size() > opts.MaxBytes {
		return fail(ReasonSnapshotTooLarge, fmt.Errorf(
			"snapshot is %d bytes, limit is %d bytes", info.Size(), opts.MaxBytes))
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		return fail(ReasonSnapshotUnreadable, fmt.Errorf("read snapshot: %w", err))
	}
	if int64(len(payload)) > opts.MaxBytes {
		return fail(ReasonSnapshotTooLarge, fmt.Errorf(
			"snapshot is %d bytes, limit is %d bytes", len(payload), opts.MaxBytes))
	}
	var state domain.State
	if err := json.Unmarshal(payload, &state); err != nil {
		return fail(ReasonDecodeFailed, fmt.Errorf("decode snapshot: %w", err))
	}
	state = normalizeState(state)
	report.SchemaVersion = state.SchemaVersion
	report.Revision = state.Revision
	report.Counts = Counts{
		Plans:                 len(state.Plans),
		Revisions:             len(state.Revisions),
		Nodes:                 len(state.Nodes),
		Leases:                len(state.Leases),
		LeaseGroups:           len(state.LeaseGroups),
		Probes:                len(state.Probes),
		Assessments:           len(state.Assessments),
		LeaseRequestKeys:      len(state.LeaseRequestKeys),
		AssessmentRequestKeys: len(state.AssessmentRequestKeys),
		AuditEvents:           len(state.AuditEvents),
	}

	checker := &checker{
		ctx:         ctx,
		state:       &state,
		report:      &report,
		maxProblems: opts.MaxProblems,
	}
	checker.run()
	report.ProblemCount = len(report.Problems)
	if checker.halted {
		return report
	}
	if report.ProblemCount > 0 {
		report.Status = StatusProblems
	} else {
		report.Status = StatusOK
	}
	return report
}

// checker walks one decoded snapshot and accumulates problems. Every loop
// calls tick so cancellation and the problem bound halt the scan promptly.
type checker struct {
	ctx         context.Context
	state       *domain.State
	report      *Report
	maxProblems int
	ops         uint64
	halted      bool
}

func (c *checker) run() {
	// Phases run in a fixed order so problem ordering is stable.
	phases := []func(){
		c.checkShape,
		c.checkReferences,
		c.checkState,
		c.checkIndexes,
		c.checkSummaries,
	}
	for _, phase := range phases {
		if c.halted {
			return
		}
		phase()
	}
}

// tick reports whether the scan must stop. Cancellation is observed every
// tickInterval operations to keep large snapshots responsive without paying
// a context check per entity.
func (c *checker) tick() bool {
	if c.halted {
		return true
	}
	c.ops++
	if c.ops%tickInterval == 0 {
		if err := c.ctx.Err(); err != nil {
			c.incomplete(cancelReason(err), nil)
		}
	}
	return c.halted
}

func (c *checker) incomplete(reason string, err error) {
	c.report.Status = StatusIncomplete
	c.report.IncompleteReason = reason
	if err != nil {
		c.report.Message = err.Error()
	}
	c.halted = true
}

func (c *checker) add(entity string, id string, field string, code string, message string, details map[string]any) {
	if c.halted {
		return
	}
	c.report.Problems = append(c.report.Problems, Problem{
		Code:     code,
		Entity:   entity,
		EntityID: id,
		Field:    field,
		Message:  message,
		Details:  details,
	})
	if len(c.report.Problems) >= c.maxProblems {
		c.report.Truncated = true
		c.incomplete(ReasonProblemLimit, fmt.Errorf("stopped after %d problems", c.maxProblems))
	}
}

// requireRef records a missing cross-entity reference.
func (c *checker) requireRef(entity string, id string, field string, kind string, target string, exists bool) {
	if exists {
		return
	}
	c.add(entity, id, field, "unknown_"+kind,
		entity+" references a missing "+kind,
		map[string]any{kind + "_id": target})
}

func cancelReason(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return ReasonTimeout
	}
	return ReasonCancelled
}

// normalizeState mirrors the nil-map normalization the store applies after
// decoding so the checker can iterate every collection safely. It never
// rewrites the snapshot file.
func normalizeState(state domain.State) domain.State {
	if state.Plans == nil {
		state.Plans = make(map[string]domain.RolloutPlan)
	}
	if state.Revisions == nil {
		state.Revisions = make(map[string]domain.ZoneRevision)
	}
	if state.Nodes == nil {
		state.Nodes = make(map[string]domain.ResolverNode)
	}
	if state.Leases == nil {
		state.Leases = make(map[string]domain.NodeLease)
	}
	if state.LeaseGroups == nil {
		state.LeaseGroups = make(map[string]domain.LeaseGroup)
	}
	if state.Probes == nil {
		state.Probes = make(map[string]domain.Probe)
	}
	if state.Assessments == nil {
		state.Assessments = make(map[string]domain.RolloutAssessment)
	}
	if state.LeaseRequestKeys == nil {
		state.LeaseRequestKeys = make(map[string]string)
	}
	if state.AssessmentRequestKeys == nil {
		state.AssessmentRequestKeys = make(map[string]string)
	}
	if state.AuditEvents == nil {
		state.AuditEvents = []domain.AuditEvent{}
	}
	return state
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
