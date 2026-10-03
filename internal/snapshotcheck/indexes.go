package snapshotcheck

import (
	"strings"

	"solo-0014-zone-rollout-control/internal/domain"
)

// indexSeparator joins plan id and request key in the idempotency indexes,
// matching the services that maintain them.
const indexSeparator = "\x00"

// scanIndexes verifies both directions of the idempotency indexes: every
// index entry must resolve to a matching record, and every lease group and
// assessment must be reachable through exactly its own request key.
func scanIndexes(s *scanner, state *domain.State) {
	for _, key := range sortedKeys(state.LeaseRequestKeys) {
		if !s.next() {
			return
		}
		groupID := state.LeaseRequestKeys[key]
		planID, requestKey, ok := splitIndexKey(key)
		if !ok {
			s.add(CheckIndexes, "malformed_index_key", "lease_request_key", key, "key",
				"lease request index key is not a plan id and request key pair", nil)
			continue
		}
		if _, exists := state.Plans[planID]; !exists {
			s.add(CheckIndexes, "missing_reference", "lease_request_key", key, "key",
				"lease request index key references a missing plan",
				map[string]any{"referenced_id": planID})
		}
		group, exists := state.LeaseGroups[groupID]
		if !exists {
			s.add(CheckIndexes, "missing_reference", "lease_request_key", key, "value",
				"lease request index references a missing lease group",
				map[string]any{"referenced_id": groupID})
			continue
		}
		if group.PlanID != planID {
			s.add(CheckIndexes, "inconsistent_index", "lease_request_key", key, "value",
				"indexed lease group belongs to a different plan",
				map[string]any{"lease_group_id": groupID, "lease_group_plan_id": group.PlanID})
		}
		if group.RequestKey != requestKey {
			s.add(CheckIndexes, "inconsistent_index", "lease_request_key", key, "value",
				"indexed lease group carries a different request key",
				map[string]any{"lease_group_id": groupID, "lease_group_request_key": group.RequestKey})
		}
	}

	for _, key := range sortedKeys(state.LeaseGroups) {
		if !s.next() {
			return
		}
		group := state.LeaseGroups[key]
		if group.PlanID == "" || group.RequestKey == "" {
			continue
		}
		expectedKey := group.PlanID + indexSeparator + group.RequestKey
		indexed, exists := state.LeaseRequestKeys[expectedKey]
		switch {
		case !exists:
			s.add(CheckIndexes, "missing_index_entry", "lease_group", group.ID, "request_key",
				"lease group is not reachable through the lease request index",
				map[string]any{"expected_key": expectedKey})
		case indexed != group.ID:
			s.add(CheckIndexes, "inconsistent_index", "lease_group", group.ID, "request_key",
				"lease request index points to a different lease group",
				map[string]any{"expected_key": expectedKey, "indexed_lease_group_id": indexed})
		}
	}

	for _, key := range sortedKeys(state.AssessmentRequestKeys) {
		if !s.next() {
			return
		}
		assessmentID := state.AssessmentRequestKeys[key]
		planID, requestKey, ok := splitIndexKey(key)
		if !ok {
			s.add(CheckIndexes, "malformed_index_key", "assessment_request_key", key, "key",
				"assessment request index key is not a plan id and request key pair", nil)
			continue
		}
		if _, exists := state.Plans[planID]; !exists {
			s.add(CheckIndexes, "missing_reference", "assessment_request_key", key, "key",
				"assessment request index key references a missing plan",
				map[string]any{"referenced_id": planID})
		}
		assessment, exists := state.Assessments[assessmentID]
		if !exists {
			s.add(CheckIndexes, "missing_reference", "assessment_request_key", key, "value",
				"assessment request index references a missing assessment",
				map[string]any{"referenced_id": assessmentID})
			continue
		}
		if assessment.PlanID != planID {
			s.add(CheckIndexes, "inconsistent_index", "assessment_request_key", key, "value",
				"indexed assessment belongs to a different plan",
				map[string]any{"assessment_id": assessmentID, "assessment_plan_id": assessment.PlanID})
		}
		if assessment.RequestKey != requestKey {
			s.add(CheckIndexes, "inconsistent_index", "assessment_request_key", key, "value",
				"indexed assessment carries a different request key",
				map[string]any{"assessment_id": assessmentID, "assessment_request_key": assessment.RequestKey})
		}
	}

	for _, key := range sortedKeys(state.Assessments) {
		if !s.next() {
			return
		}
		assessment := state.Assessments[key]
		if assessment.PlanID == "" || assessment.RequestKey == "" {
			continue
		}
		expectedKey := assessment.PlanID + indexSeparator + assessment.RequestKey
		indexed, exists := state.AssessmentRequestKeys[expectedKey]
		switch {
		case !exists:
			s.add(CheckIndexes, "missing_index_entry", "assessment", assessment.ID, "request_key",
				"assessment is not reachable through the assessment request index",
				map[string]any{"expected_key": expectedKey})
		case indexed != assessment.ID:
			s.add(CheckIndexes, "inconsistent_index", "assessment", assessment.ID, "request_key",
				"assessment request index points to a different assessment",
				map[string]any{"expected_key": expectedKey, "indexed_assessment_id": indexed})
		}
	}
}

func splitIndexKey(key string) (string, string, bool) {
	parts := strings.Split(key, indexSeparator)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", false
	}
	return parts[0], parts[1], true
}
