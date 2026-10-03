package verify

import (
	"math"
	"sort"

	"solo-0014-zone-rollout-control/internal/domain"
)

// checkSummaries verifies derived counters and summary fields: stored counts
// must match the records they summarize, and plan summary pointers must
// reference the newest revision and assessment of the plan.
func (c *checker) checkSummaries() {
	for _, id := range sortedKeys(c.state.Revisions) {
		if c.tick() {
			return
		}
		revision := c.state.Revisions[id]
		if revision.EntryCount != len(revision.Entries) {
			c.add("revision", id, "entry_count", "entry_count_mismatch",
				"stored entry count does not match the number of entries",
				map[string]any{"entry_count": revision.EntryCount, "entries": len(revision.Entries)})
		}
	}
	revisionsByPlan := make(map[string][]domain.ZoneRevision)
	for _, id := range sortedKeys(c.state.Revisions) {
		revision := c.state.Revisions[id]
		revisionsByPlan[revision.PlanID] = append(revisionsByPlan[revision.PlanID], revision)
	}
	assessmentsByPlan := make(map[string][]domain.RolloutAssessment)
	for _, id := range sortedKeys(c.state.Assessments) {
		assessment := c.state.Assessments[id]
		assessmentsByPlan[assessment.PlanID] = append(assessmentsByPlan[assessment.PlanID], assessment)
	}
	for _, id := range sortedKeys(c.state.Plans) {
		if c.tick() {
			return
		}
		plan := c.state.Plans[id]
		revisions := revisionsByPlan[id]
		c.checkPlanSerials(id, revisions)
		if len(revisions) == 0 {
			if plan.LatestRevisionID != "" {
				c.add("plan", id, "latest_revision_id", "unexpected_latest_revision",
					"plan has no revisions but references a latest revision",
					map[string]any{"latest_revision_id": plan.LatestRevisionID})
			}
		} else if plan.LatestRevisionID == "" {
			c.add("plan", id, "latest_revision_id", "missing_latest_revision",
				"plan has revisions but no latest revision reference", nil)
		} else {
			latest := latestRevision(revisions)
			if plan.LatestRevisionID != latest.ID {
				c.add("plan", id, "latest_revision_id", "stale_latest_revision",
					"latest revision reference does not match the highest serial",
					map[string]any{
						"latest_revision_id": plan.LatestRevisionID,
						"expected":           latest.ID,
						"highest_serial":     latest.Serial,
					})
			}
		}
		assessments := assessmentsByPlan[id]
		if len(assessments) == 0 {
			if plan.LatestAssessmentID != "" {
				c.add("plan", id, "latest_assessment_id", "unexpected_latest_assessment",
					"plan has no assessments but references a latest assessment",
					map[string]any{"latest_assessment_id": plan.LatestAssessmentID})
			}
		} else if plan.LatestAssessmentID == "" {
			c.add("plan", id, "latest_assessment_id", "missing_latest_assessment",
				"plan has assessments but no latest assessment reference", nil)
		} else if latest, ok := c.state.Assessments[plan.LatestAssessmentID]; ok {
			newest := newestAssessment(assessments)
			if latest.CreatedAt != newest.CreatedAt {
				c.add("plan", id, "latest_assessment_id", "stale_latest_assessment",
					"latest assessment reference is not the newest assessment",
					map[string]any{
						"latest_assessment_id": plan.LatestAssessmentID,
						"expected":             newest.ID,
					})
			}
		}
	}
	for _, id := range sortedKeys(c.state.Assessments) {
		if c.tick() {
			return
		}
		c.checkAssessmentSummary(c.state.Assessments[id])
	}
	leasesByAllocation := make(map[string]int)
	for _, id := range sortedKeys(c.state.Leases) {
		leasesByAllocation[c.state.Leases[id].AllocationID]++
	}
	for _, id := range sortedKeys(c.state.LeaseGroups) {
		if c.tick() {
			return
		}
		group := c.state.LeaseGroups[id]
		if actual := leasesByAllocation[id]; len(group.LeaseIDs) != actual {
			c.add("lease_group", id, "lease_ids", "lease_count_mismatch",
				"lease group size does not match the leases pointing at it",
				map[string]any{"listed": len(group.LeaseIDs), "referencing": actual})
		}
	}
	// Every successful store transaction bumps the revision counter and
	// appends an audit event, so a zero revision only pairs with an empty
	// snapshot and a non-zero revision always leaves records behind.
	if c.state.Revision == 0 && c.report.Counts.total() > 0 {
		c.add("snapshot", "", "revision", "revision_counter_invalid",
			"snapshot contains records but the revision counter is zero",
			map[string]any{"revision": c.state.Revision})
	}
	if c.state.Revision > 0 && c.report.Counts.total() == 0 {
		c.add("snapshot", "", "revision", "revision_counter_invalid",
			"revision counter is non-zero but the snapshot contains no records",
			map[string]any{"revision": c.state.Revision})
	}
}

// checkPlanSerials flags duplicated serials inside one plan. Serials must be
// strictly increasing, so duplicates can never be written by the service.
func (c *checker) checkPlanSerials(planID string, revisions []domain.ZoneRevision) {
	ordered := make([]domain.ZoneRevision, len(revisions))
	copy(ordered, revisions)
	sort.Slice(ordered, func(i, j int) bool {
		if ordered[i].Serial != ordered[j].Serial {
			return ordered[i].Serial < ordered[j].Serial
		}
		return ordered[i].ID < ordered[j].ID
	})
	for index := 1; index < len(ordered); index++ {
		if ordered[index].Serial == ordered[index-1].Serial {
			c.add("revision", ordered[index].ID, "serial", "duplicate_serial",
				"plan already contains this serial",
				map[string]any{
					"plan_id":           planID,
					"serial":            ordered[index].Serial,
					"first_revision_id": ordered[index-1].ID,
				})
		}
	}
}

// checkAssessmentSummary verifies the counters and verdict stored on one
// assessment against each other. These relations are computed from values on
// the record itself, so they hold no matter how the snapshot evolved after
// the assessment was written.
func (c *checker) checkAssessmentSummary(assessment domain.RolloutAssessment) {
	id := assessment.ID
	if assessment.ProbeCount < 0 || assessment.SuccessCount < 0 {
		c.add("assessment", id, "probe_count", "invalid_counts",
			"assessment counts must not be negative",
			map[string]any{
				"probe_count":   assessment.ProbeCount,
				"success_count": assessment.SuccessCount,
			})
	}
	if assessment.ProbeCount >= 0 && assessment.SuccessCount > assessment.ProbeCount {
		c.add("assessment", id, "success_count", "success_exceeds_probes",
			"success count exceeds the probe count",
			map[string]any{
				"probe_count":   assessment.ProbeCount,
				"success_count": assessment.SuccessCount,
			})
	}
	expectedRatio := 0.0
	if assessment.ProbeCount > 0 {
		expectedRatio = round6(float64(assessment.SuccessCount) / float64(assessment.ProbeCount))
	}
	if assessment.SuccessRatio != expectedRatio {
		c.add("assessment", id, "success_ratio", "success_ratio_mismatch",
			"success ratio does not match the stored counts",
			map[string]any{"success_ratio": assessment.SuccessRatio, "expected": expectedRatio})
	}
	hasNoProbes := false
	for _, reason := range assessment.Reasons {
		if reason == "no_probes" {
			hasNoProbes = true
		}
	}
	if hasNoProbes != (assessment.ProbeCount == 0) {
		c.add("assessment", id, "reasons", "no_probes_reason_mismatch",
			"no_probes reason does not match the probe count",
			map[string]any{"probe_count": assessment.ProbeCount})
	}
	if assessment.Verdict == domain.VerdictConsistent || assessment.Verdict == domain.VerdictReviewRequired {
		if consistent := len(assessment.Reasons) == 0; consistent != (assessment.Verdict == domain.VerdictConsistent) {
			c.add("assessment", id, "verdict", "verdict_reason_mismatch",
				"verdict does not match the presence of reasons",
				map[string]any{"verdict": assessment.Verdict, "reasons": len(assessment.Reasons)})
		}
	}
	if assessment.MaximumLatencyMS < 0 {
		c.add("assessment", id, "maximum_latency_ms", "invalid_maximum_latency",
			"assessment maximum latency must not be negative",
			map[string]any{"maximum_latency_ms": assessment.MaximumLatencyMS})
	}
}

// latestRevision returns the revision with the highest serial, breaking ties
// by id so the result is deterministic even for corrupted snapshots.
func latestRevision(revisions []domain.ZoneRevision) domain.ZoneRevision {
	latest := revisions[0]
	for _, revision := range revisions[1:] {
		if revision.Serial > latest.Serial ||
			revision.Serial == latest.Serial && revision.ID > latest.ID {
			latest = revision
		}
	}
	return latest
}

// newestAssessment returns the assessment with the newest creation time,
// breaking ties by id so the result is deterministic.
func newestAssessment(assessments []domain.RolloutAssessment) domain.RolloutAssessment {
	newest := assessments[0]
	for _, assessment := range assessments[1:] {
		if assessment.CreatedAt.After(newest.CreatedAt) ||
			assessment.CreatedAt.Equal(newest.CreatedAt) && assessment.ID > newest.ID {
			newest = assessment
		}
	}
	return newest
}

// round6 mirrors the ratio rounding applied by the assessment builder.
func round6(value float64) float64 {
	return math.Round(value*1e6) / 1e6
}
