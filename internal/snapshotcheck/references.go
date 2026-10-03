package snapshotcheck

import (
	"strconv"

	"solo-0014-zone-rollout-control/internal/domain"
)

// scanReferences verifies that every non-empty reference resolves and
// that both sides of a relationship agree with each other. Empty required
// fields are reported by the state constraints check instead, so each
// violation surfaces exactly once.
func scanReferences(s *scanner, state *domain.State) {
	for _, key := range sortedKeys(state.Plans) {
		if !s.next() {
			return
		}
		plan := state.Plans[key]
		if plan.ID != key {
			s.add(CheckReferences, "id_mismatch", "plan", key, "id",
				"plan is stored under a key that does not match its id",
				map[string]any{"entity_id": plan.ID})
		}
		if plan.LatestRevisionID != "" {
			if revision, ok := state.Revisions[plan.LatestRevisionID]; !ok {
				s.add(CheckReferences, "missing_reference", "plan", plan.ID, "latest_revision_id",
					"plan references a missing revision",
					map[string]any{"referenced_id": plan.LatestRevisionID})
			} else if revision.PlanID != plan.ID {
				s.add(CheckReferences, "inconsistent_reference", "plan", plan.ID, "latest_revision_id",
					"plan latest revision belongs to a different plan",
					map[string]any{"referenced_id": plan.LatestRevisionID, "referenced_plan_id": revision.PlanID})
			}
		}
		if plan.LatestAssessmentID != "" {
			if assessment, ok := state.Assessments[plan.LatestAssessmentID]; !ok {
				s.add(CheckReferences, "missing_reference", "plan", plan.ID, "latest_assessment_id",
					"plan references a missing assessment",
					map[string]any{"referenced_id": plan.LatestAssessmentID})
			} else if assessment.PlanID != plan.ID {
				s.add(CheckReferences, "inconsistent_reference", "plan", plan.ID, "latest_assessment_id",
					"plan latest assessment belongs to a different plan",
					map[string]any{"referenced_id": plan.LatestAssessmentID, "referenced_plan_id": assessment.PlanID})
			}
		}
	}

	for _, key := range sortedKeys(state.Revisions) {
		if !s.next() {
			return
		}
		revision := state.Revisions[key]
		if revision.ID != key {
			s.add(CheckReferences, "id_mismatch", "revision", key, "id",
				"revision is stored under a key that does not match its id",
				map[string]any{"entity_id": revision.ID})
		}
		if revision.PlanID != "" {
			if _, ok := state.Plans[revision.PlanID]; !ok {
				s.add(CheckReferences, "missing_reference", "revision", revision.ID, "plan_id",
					"revision references a missing plan",
					map[string]any{"referenced_id": revision.PlanID})
			}
		}
	}

	for _, key := range sortedKeys(state.Leases) {
		if !s.next() {
			return
		}
		lease := state.Leases[key]
		if lease.ID != key {
			s.add(CheckReferences, "id_mismatch", "lease", key, "id",
				"lease is stored under a key that does not match its id",
				map[string]any{"entity_id": lease.ID})
		}
		if lease.PlanID != "" {
			if _, ok := state.Plans[lease.PlanID]; !ok {
				s.add(CheckReferences, "missing_reference", "lease", lease.ID, "plan_id",
					"lease references a missing plan",
					map[string]any{"referenced_id": lease.PlanID})
			}
		}
		if lease.NodeID != "" {
			if _, ok := state.Nodes[lease.NodeID]; !ok {
				s.add(CheckReferences, "missing_reference", "lease", lease.ID, "node_id",
					"lease references a missing node",
					map[string]any{"referenced_id": lease.NodeID})
			}
		}
		if lease.AllocationID != "" {
			if group, ok := state.LeaseGroups[lease.AllocationID]; !ok {
				s.add(CheckReferences, "missing_reference", "lease", lease.ID, "allocation_id",
					"lease references a missing lease group",
					map[string]any{"referenced_id": lease.AllocationID})
			} else {
				if group.PlanID != lease.PlanID {
					s.add(CheckReferences, "inconsistent_reference", "lease", lease.ID, "allocation_id",
						"lease group belongs to a different plan",
						map[string]any{"referenced_id": lease.AllocationID, "referenced_plan_id": group.PlanID})
				}
				if !containsString(group.LeaseIDs, lease.ID) {
					s.add(CheckReferences, "inconsistent_reference", "lease", lease.ID, "allocation_id",
						"lease group does not list this lease",
						map[string]any{"referenced_id": lease.AllocationID})
				}
			}
		}
		if lease.ProbeID != "" {
			if probe, ok := state.Probes[lease.ProbeID]; !ok {
				s.add(CheckReferences, "missing_reference", "lease", lease.ID, "probe_id",
					"lease references a missing probe",
					map[string]any{"referenced_id": lease.ProbeID})
			} else if probe.LeaseID != lease.ID {
				s.add(CheckReferences, "inconsistent_reference", "lease", lease.ID, "probe_id",
					"referenced probe belongs to a different lease",
					map[string]any{"referenced_id": lease.ProbeID, "referenced_lease_id": probe.LeaseID})
			}
		}
	}

	for _, key := range sortedKeys(state.LeaseGroups) {
		if !s.next() {
			return
		}
		group := state.LeaseGroups[key]
		if group.ID != key {
			s.add(CheckReferences, "id_mismatch", "lease_group", key, "id",
				"lease group is stored under a key that does not match its id",
				map[string]any{"entity_id": group.ID})
		}
		if group.PlanID != "" {
			if _, ok := state.Plans[group.PlanID]; !ok {
				s.add(CheckReferences, "missing_reference", "lease_group", group.ID, "plan_id",
					"lease group references a missing plan",
					map[string]any{"referenced_id": group.PlanID})
			}
		}
		seen := make(map[string]bool, len(group.LeaseIDs))
		for _, leaseID := range group.LeaseIDs {
			if !s.next() {
				return
			}
			if seen[leaseID] {
				s.add(CheckReferences, "duplicate_reference", "lease_group", group.ID, "lease_ids",
					"lease group lists the same lease more than once",
					map[string]any{"referenced_id": leaseID})
				continue
			}
			seen[leaseID] = true
			if lease, ok := state.Leases[leaseID]; !ok {
				s.add(CheckReferences, "missing_reference", "lease_group", group.ID, "lease_ids",
					"lease group references a missing lease",
					map[string]any{"referenced_id": leaseID})
			} else if lease.AllocationID != group.ID {
				s.add(CheckReferences, "inconsistent_reference", "lease_group", group.ID, "lease_ids",
					"referenced lease belongs to a different lease group",
					map[string]any{"referenced_id": leaseID, "referenced_allocation_id": lease.AllocationID})
			}
		}
	}

	for _, key := range sortedKeys(state.Probes) {
		if !s.next() {
			return
		}
		probe := state.Probes[key]
		if probe.ID != key {
			s.add(CheckReferences, "id_mismatch", "probe", key, "id",
				"probe is stored under a key that does not match its id",
				map[string]any{"entity_id": probe.ID})
		}
		if probe.LeaseID != "" {
			if lease, ok := state.Leases[probe.LeaseID]; !ok {
				s.add(CheckReferences, "missing_reference", "probe", probe.ID, "lease_id",
					"probe references a missing lease",
					map[string]any{"referenced_id": probe.LeaseID})
			} else {
				if lease.ProbeID != probe.ID {
					s.add(CheckReferences, "inconsistent_reference", "probe", probe.ID, "lease_id",
						"referenced lease does not point back to this probe",
						map[string]any{"referenced_id": probe.LeaseID, "referenced_probe_id": lease.ProbeID})
				}
				if lease.PlanID != probe.PlanID {
					s.add(CheckReferences, "inconsistent_reference", "probe", probe.ID, "plan_id",
						"probe plan does not match its lease plan",
						map[string]any{"referenced_id": probe.LeaseID, "lease_plan_id": lease.PlanID})
				}
				if lease.NodeID != probe.NodeID {
					s.add(CheckReferences, "inconsistent_reference", "probe", probe.ID, "node_id",
						"probe node does not match its lease node",
						map[string]any{"referenced_id": probe.LeaseID, "lease_node_id": lease.NodeID})
				}
			}
		}
		if probe.PlanID != "" {
			if _, ok := state.Plans[probe.PlanID]; !ok {
				s.add(CheckReferences, "missing_reference", "probe", probe.ID, "plan_id",
					"probe references a missing plan",
					map[string]any{"referenced_id": probe.PlanID})
			}
		}
		if probe.NodeID != "" {
			if _, ok := state.Nodes[probe.NodeID]; !ok {
				s.add(CheckReferences, "missing_reference", "probe", probe.ID, "node_id",
					"probe references a missing node",
					map[string]any{"referenced_id": probe.NodeID})
			}
		}
	}

	for _, key := range sortedKeys(state.Assessments) {
		if !s.next() {
			return
		}
		assessment := state.Assessments[key]
		if assessment.ID != key {
			s.add(CheckReferences, "id_mismatch", "assessment", key, "id",
				"assessment is stored under a key that does not match its id",
				map[string]any{"entity_id": assessment.ID})
		}
		if assessment.PlanID != "" {
			if _, ok := state.Plans[assessment.PlanID]; !ok {
				s.add(CheckReferences, "missing_reference", "assessment", assessment.ID, "plan_id",
					"assessment references a missing plan",
					map[string]any{"referenced_id": assessment.PlanID})
			}
		}
		if assessment.RevisionID != "" {
			if revision, ok := state.Revisions[assessment.RevisionID]; !ok {
				s.add(CheckReferences, "missing_reference", "assessment", assessment.ID, "revision_id",
					"assessment references a missing revision",
					map[string]any{"referenced_id": assessment.RevisionID})
			} else if revision.PlanID != assessment.PlanID {
				s.add(CheckReferences, "inconsistent_reference", "assessment", assessment.ID, "revision_id",
					"referenced revision belongs to a different plan",
					map[string]any{"referenced_id": assessment.RevisionID, "referenced_plan_id": revision.PlanID})
			}
		}
	}

	for index, event := range state.AuditEvents {
		if !s.next() {
			return
		}
		eventID := event.ID
		if eventID == "" {
			eventID = indexLabel(index)
		}
		collection, known := auditTargets[event.EntityType]
		if !known {
			s.add(CheckReferences, "unknown_entity_type", "audit_event", eventID, "entity_type",
				"audit event references an unknown entity type",
				map[string]any{"entity_type": event.EntityType, "index": index})
			continue
		}
		if event.EntityID == "" {
			continue
		}
		if !auditTargetExists(state, collection, event.EntityID) {
			s.add(CheckReferences, "missing_reference", "audit_event", eventID, "entity_id",
				"audit event references a missing "+collection[:len(collection)-1],
				map[string]any{"referenced_id": event.EntityID, "index": index})
		}
	}
}

func auditTargetExists(state *domain.State, collection string, id string) bool {
	switch collection {
	case "plans":
		_, ok := state.Plans[id]
		return ok
	case "revisions":
		_, ok := state.Revisions[id]
		return ok
	case "nodes":
		_, ok := state.Nodes[id]
		return ok
	case "leases":
		_, ok := state.Leases[id]
		return ok
	case "lease_groups":
		_, ok := state.LeaseGroups[id]
		return ok
	case "probes":
		_, ok := state.Probes[id]
		return ok
	case "assessments":
		_, ok := state.Assessments[id]
		return ok
	}
	return false
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// indexLabel identifies an audit event without a usable id in problem
// details, keeping the output deterministic for repeated runs.
func indexLabel(index int) string {
	return "audit_events[" + strconv.Itoa(index) + "]"
}
