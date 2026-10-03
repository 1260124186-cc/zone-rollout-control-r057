package snapshotcheck

import (
	"solo-0014-zone-rollout-control/internal/domain"
)

// supportedSchemaVersion is the only snapshot layout this checker
// understands; anything else is reported, not silently accepted.
const supportedSchemaVersion = 1

// scanSummaries verifies derived counts and plan summary pointers against
// the entities they describe. Probes are append-only and plans freeze
// their revision pointer before assessments run, so every comparison here
// is a stable lower- or upper-bound check with no false positives from
// later writes.
func scanSummaries(s *scanner, state *domain.State) {
	if !s.next() {
		return
	}
	if state.SchemaVersion != supportedSchemaVersion {
		s.add(CheckSummaries, "unsupported_schema_version", "state", "", "schema_version",
			"snapshot schema version is not supported by this checker",
			map[string]any{"schema_version": state.SchemaVersion})
	}
	totalEntities := len(state.Plans) + len(state.Revisions) + len(state.Nodes) +
		len(state.Leases) + len(state.LeaseGroups) + len(state.Probes) + len(state.Assessments)
	if state.Revision == 0 && totalEntities > 0 {
		s.add(CheckSummaries, "invalid_value", "state", "", "revision",
			"snapshot revision is zero but entities exist",
			map[string]any{"revision": state.Revision})
	}

	for _, key := range sortedKeys(state.Revisions) {
		if !s.next() {
			return
		}
		revision := state.Revisions[key]
		if revision.EntryCount != len(revision.Entries) {
			s.add(CheckSummaries, "count_mismatch", "revision", revision.ID, "entry_count",
				"revision entry count does not match its entries",
				map[string]any{"entry_count": revision.EntryCount, "entries": len(revision.Entries)})
		}
	}

	revisionsByPlan := make(map[string][]domain.ZoneRevision)
	for _, key := range sortedKeys(state.Revisions) {
		if !s.next() {
			return
		}
		revision := state.Revisions[key]
		revisionsByPlan[revision.PlanID] = append(revisionsByPlan[revision.PlanID], revision)
	}
	assessmentsByPlan := make(map[string][]domain.RolloutAssessment)
	for _, key := range sortedKeys(state.Assessments) {
		if !s.next() {
			return
		}
		assessment := state.Assessments[key]
		assessmentsByPlan[assessment.PlanID] = append(assessmentsByPlan[assessment.PlanID], assessment)
	}

	for _, key := range sortedKeys(state.Plans) {
		if !s.next() {
			return
		}
		plan := state.Plans[key]
		revisions := revisionsByPlan[plan.ID]
		if len(revisions) > 0 && plan.LatestRevisionID == "" {
			s.add(CheckSummaries, "missing_summary", "plan", plan.ID, "latest_revision_id",
				"plan has revisions but no latest revision pointer", nil)
		}
		if plan.LatestRevisionID != "" {
			if latest, ok := state.Revisions[plan.LatestRevisionID]; ok && latest.PlanID == plan.ID {
				if newest, unique := newestRevision(revisions); unique && latest.Serial < newest.Serial {
					s.add(CheckSummaries, "stale_summary", "plan", plan.ID, "latest_revision_id",
						"plan latest revision is not the highest serial",
						map[string]any{"expected_revision_id": newest.ID, "latest_serial": latest.Serial, "highest_serial": newest.Serial})
				}
			}
		}
		assessments := assessmentsByPlan[plan.ID]
		if len(assessments) > 0 && plan.LatestAssessmentID == "" {
			s.add(CheckSummaries, "missing_summary", "plan", plan.ID, "latest_assessment_id",
				"plan has assessments but no latest assessment pointer", nil)
		}
		if plan.LatestAssessmentID != "" {
			if latest, ok := state.Assessments[plan.LatestAssessmentID]; ok && latest.PlanID == plan.ID {
				if newest, unique := newestAssessment(assessments); unique && latest.CreatedAt.Before(newest.CreatedAt) {
					s.add(CheckSummaries, "stale_summary", "plan", plan.ID, "latest_assessment_id",
						"plan latest assessment is not the most recently created",
						map[string]any{"expected_assessment_id": newest.ID})
				}
			}
		}
	}

	probeStats := make(map[string]probeAggregate)
	for _, key := range sortedKeys(state.Probes) {
		if !s.next() {
			return
		}
		probe := state.Probes[key]
		stats := probeStats[probe.PlanID]
		stats.count++
		if probe.Success {
			stats.success++
		}
		if probe.LatencyMS > stats.maxLatency {
			stats.maxLatency = probe.LatencyMS
		}
		probeStats[probe.PlanID] = stats
	}

	for _, key := range sortedKeys(state.Assessments) {
		if !s.next() {
			return
		}
		assessment := state.Assessments[key]
		if assessment.SuccessCount > assessment.ProbeCount {
			s.add(CheckSummaries, "count_mismatch", "assessment", assessment.ID, "success_count",
				"assessment success count exceeds its probe count",
				map[string]any{"success_count": assessment.SuccessCount, "probe_count": assessment.ProbeCount})
		}
		expectedRatio := 0.0
		if assessment.ProbeCount > 0 {
			expectedRatio = round6(float64(assessment.SuccessCount) / float64(assessment.ProbeCount))
		}
		if assessment.SuccessRatio != expectedRatio {
			s.add(CheckSummaries, "ratio_mismatch", "assessment", assessment.ID, "success_ratio",
				"assessment success ratio does not match its counts",
				map[string]any{"success_ratio": assessment.SuccessRatio, "expected_ratio": expectedRatio})
		}
		if containsString(assessment.Reasons, "no_probes") && assessment.ProbeCount != 0 {
			s.add(CheckSummaries, "count_mismatch", "assessment", assessment.ID, "probe_count",
				"assessment carries the no_probes reason but its probe count is not zero",
				map[string]any{"probe_count": assessment.ProbeCount})
		}
		stats := probeStats[assessment.PlanID]
		if assessment.ProbeCount > stats.count {
			s.add(CheckSummaries, "count_exceeds_current", "assessment", assessment.ID, "probe_count",
				"assessment probe count exceeds the probes stored for its plan",
				map[string]any{"probe_count": assessment.ProbeCount, "current_probe_count": stats.count})
		}
		if assessment.SuccessCount > stats.success {
			s.add(CheckSummaries, "count_exceeds_current", "assessment", assessment.ID, "success_count",
				"assessment success count exceeds the successful probes stored for its plan",
				map[string]any{"success_count": assessment.SuccessCount, "current_success_count": stats.success})
		}
		if assessment.MaximumLatencyMS > stats.maxLatency {
			s.add(CheckSummaries, "count_exceeds_current", "assessment", assessment.ID, "maximum_latency_ms",
				"assessment maximum latency exceeds the probes stored for its plan",
				map[string]any{"maximum_latency_ms": assessment.MaximumLatencyMS, "current_maximum_latency_ms": stats.maxLatency})
		}
		if plan, ok := state.Plans[assessment.PlanID]; ok &&
			plan.LatestRevisionID != "" && assessment.RevisionID != "" &&
			assessment.RevisionID != plan.LatestRevisionID {
			s.add(CheckSummaries, "stale_summary", "assessment", assessment.ID, "revision_id",
				"assessment revision is not the plan latest revision",
				map[string]any{"revision_id": assessment.RevisionID, "plan_latest_revision_id": plan.LatestRevisionID})
		}
	}
}

type probeAggregate struct {
	count      int
	success    int
	maxLatency int
}

// newestRevision returns the revision with the highest serial. When the
// top serial is shared by several revisions the winner is ambiguous, so
// the result is dropped and no stale-summary problem is raised.
func newestRevision(revisions []domain.ZoneRevision) (domain.ZoneRevision, bool) {
	var newest domain.ZoneRevision
	unique := false
	for _, revision := range revisions {
		if !unique || revision.Serial > newest.Serial {
			newest = revision
			unique = true
		} else if revision.Serial == newest.Serial {
			unique = false
		}
	}
	return newest, unique
}

// newestAssessment returns the most recently created assessment. Equal
// creation timestamps make the winner ambiguous, so the result is dropped.
func newestAssessment(assessments []domain.RolloutAssessment) (domain.RolloutAssessment, bool) {
	var newest domain.RolloutAssessment
	unique := false
	for _, assessment := range assessments {
		if !unique || assessment.CreatedAt.After(newest.CreatedAt) {
			newest = assessment
			unique = true
		} else if assessment.CreatedAt.Equal(newest.CreatedAt) {
			unique = false
		}
	}
	return newest, unique
}
