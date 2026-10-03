package snapshotcheck

import (
	"context"

	"solo-0014-zone-rollout-control/internal/domain"
)

// checkpointInterval is the number of examined items between cancellation
// checks. It keeps cancellation responsive on large snapshots without
// measuring time in the hot loop.
const checkpointInterval = 128

// Check scans state in a stable check order and returns the consolidated
// report. The scan stops early when the context is canceled or the problem
// limit is reached; both conditions are reported, never hidden.
func Check(ctx context.Context, state *domain.State, opts Options) Report {
	if ctx == nil {
		ctx = context.Background()
	}
	s := &scanner{
		ctx:         ctx,
		maxProblems: opts.MaxProblems,
		problems:    []Problem{},
		perCheck:    make(map[string]int, len(checkOrder)),
	}
	if ctx.Err() != nil {
		s.canceled = true
	}
	completed := make(map[string]bool, len(checkOrder))
	for _, name := range checkOrder {
		if s.done() {
			break
		}
		checkFns[name](s, state)
		if !s.done() {
			completed[name] = true
		}
	}
	status := StatusOK
	switch {
	case s.canceled:
		status = StatusCanceled
	case len(s.problems) > 0:
		status = StatusIssues
	}
	return Report{
		Status:        status,
		StatePath:     opts.StatePath,
		SchemaVersion: state.SchemaVersion,
		Revision:      state.Revision,
		Counts: Counts{
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
			EntitiesScanned:       s.scanned,
		},
		Checks:       summarizeChecks(s.perCheck, completed),
		ProblemCount: len(s.problems),
		Problems:     s.problems,
		Truncated:    s.truncated,
		Canceled:     s.canceled,
	}
}

var checkFns = map[string]func(*scanner, *domain.State){
	CheckReferences:  scanReferences,
	CheckConstraints: scanConstraints,
	CheckIndexes:     scanIndexes,
	CheckSummaries:   scanSummaries,
}

type scanner struct {
	ctx         context.Context
	maxProblems int

	problems []Problem
	perCheck map[string]int

	scanned         int
	sinceCheckpoint int
	truncated       bool
	canceled        bool
}

func (s *scanner) done() bool {
	return s.truncated || s.canceled
}

// next reports whether the scan may examine one more item. Every scan
// loop calls it once per item so cancellation and truncation bound the
// total work regardless of snapshot size.
func (s *scanner) next() bool {
	if s.done() {
		return false
	}
	s.scanned++
	s.sinceCheckpoint++
	if s.sinceCheckpoint >= checkpointInterval {
		s.sinceCheckpoint = 0
		if s.ctx.Err() != nil {
			s.canceled = true
			return false
		}
	}
	return true
}

func (s *scanner) add(check string, code string, entity string, entityID string, field string, message string, details map[string]any) {
	if s.done() {
		return
	}
	if s.maxProblems > 0 && len(s.problems) >= s.maxProblems {
		s.truncated = true
		return
	}
	s.problems = append(s.problems, Problem{
		Check:    check,
		Code:     code,
		Entity:   entity,
		EntityID: entityID,
		Field:    field,
		Message:  message,
		Details:  details,
	})
	s.perCheck[check]++
}
