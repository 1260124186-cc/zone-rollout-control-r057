package snapshotcheck

import (
	"unicode/utf8"

	"solo-0014-zone-rollout-control/internal/domain"
)

// scanConstraints verifies enum membership, required and forbidden
// fields, value ranges, and cross-entity state agreements. Every rule is
// a pure function of the snapshot so results stay stable across runs.
func scanConstraints(s *scanner, state *domain.State) {
	scanPlanConstraints(s, state)
	scanRevisionConstraints(s, state)
	scanNodeConstraints(s, state)
	scanLeaseConstraints(s, state)
	scanNodeLeaseAgreement(s, state)
	scanLeaseGroupConstraints(s, state)
	scanProbeConstraints(s, state)
	scanProbeLeaseAgreement(s, state)
	scanAssessmentConstraints(s, state)
	scanAuditConstraints(s, state)
}

func scanPlanConstraints(s *scanner, state *domain.State) {
	for _, key := range sortedKeys(state.Plans) {
		if !s.next() {
			return
		}
		plan := state.Plans[key]
		switch plan.State {
		case domain.PlanDraft, domain.PlanActive, domain.PlanCompleted:
		default:
			s.add(CheckConstraints, "unknown_state", "plan", plan.ID, "state",
				"plan state is not a known value",
				map[string]any{"state": string(plan.State)})
		}
		if !isCanonicalName(plan.Zone) {
			s.add(CheckConstraints, "invalid_value", "plan", plan.ID, "zone",
				"plan zone is not a canonical lowercase DNS name",
				map[string]any{"zone": plan.Zone})
		}
		if plan.MinimumSuccessRatio < 0 || plan.MinimumSuccessRatio > 1 {
			s.add(CheckConstraints, "invalid_value", "plan", plan.ID, "minimum_success_ratio",
				"minimum success ratio must be between 0 and 1",
				map[string]any{"minimum_success_ratio": plan.MinimumSuccessRatio})
		}
		if plan.MaximumLatencyMS < 1 || plan.MaximumLatencyMS > 600000 {
			s.add(CheckConstraints, "invalid_value", "plan", plan.ID, "maximum_latency_ms",
				"maximum latency must be between 1 and 600000",
				map[string]any{"maximum_latency_ms": plan.MaximumLatencyMS})
		}
		if plan.ExpectedNodes < 1 || plan.ExpectedNodes > 500 {
			s.add(CheckConstraints, "invalid_value", "plan", plan.ID, "expected_nodes",
				"expected nodes must be between 1 and 500",
				map[string]any{"expected_nodes": plan.ExpectedNodes})
		}
		if plan.Version < 1 {
			s.add(CheckConstraints, "invalid_value", "plan", plan.ID, "version",
				"plan version must be at least 1",
				map[string]any{"version": plan.Version})
		}
		if plan.CreatedAt.IsZero() {
			s.add(CheckConstraints, "missing_value", "plan", plan.ID, "created_at",
				"plan created_at is not set", nil)
		}
		switch plan.State {
		case domain.PlanDraft:
			if plan.ActivatedAt != nil {
				s.add(CheckConstraints, "unexpected_value", "plan", plan.ID, "activated_at",
					"draft plan must not have activated_at set", nil)
			}
			if plan.CompletedAt != nil {
				s.add(CheckConstraints, "unexpected_value", "plan", plan.ID, "completed_at",
					"draft plan must not have completed_at set", nil)
			}
		case domain.PlanActive:
			if plan.ActivatedAt == nil {
				s.add(CheckConstraints, "missing_value", "plan", plan.ID, "activated_at",
					"active plan must have activated_at set", nil)
			}
			if plan.CompletedAt != nil {
				s.add(CheckConstraints, "unexpected_value", "plan", plan.ID, "completed_at",
					"active plan must not have completed_at set", nil)
			}
		case domain.PlanCompleted:
			if plan.ActivatedAt == nil {
				s.add(CheckConstraints, "missing_value", "plan", plan.ID, "activated_at",
					"completed plan must have activated_at set", nil)
			}
			if plan.CompletedAt == nil {
				s.add(CheckConstraints, "missing_value", "plan", plan.ID, "completed_at",
					"completed plan must have completed_at set", nil)
			}
		}
		if plan.ActivatedAt != nil && !plan.CreatedAt.IsZero() && plan.ActivatedAt.Before(plan.CreatedAt) {
			s.add(CheckConstraints, "invalid_value", "plan", plan.ID, "activated_at",
				"plan activated_at is before created_at", nil)
		}
		if plan.ActivatedAt != nil && plan.CompletedAt != nil && plan.CompletedAt.Before(*plan.ActivatedAt) {
			s.add(CheckConstraints, "invalid_value", "plan", plan.ID, "completed_at",
				"plan completed_at is before activated_at", nil)
		}
	}
}

func scanRevisionConstraints(s *scanner, state *domain.State) {
	for _, key := range sortedKeys(state.Revisions) {
		if !s.next() {
			return
		}
		revision := state.Revisions[key]
		if revision.PlanID == "" {
			s.add(CheckConstraints, "missing_value", "revision", revision.ID, "plan_id",
				"revision plan_id is not set", nil)
		}
		if !isCanonicalName(revision.Authority) {
			s.add(CheckConstraints, "invalid_value", "revision", revision.ID, "authority",
				"revision authority is not a canonical lowercase DNS name",
				map[string]any{"authority": revision.Authority})
		}
		if len(revision.Entries) < 1 || len(revision.Entries) > domain.MaxEntries {
			s.add(CheckConstraints, "invalid_value", "revision", revision.ID, "entries",
				"revision entries must contain between 1 and 500 items",
				map[string]any{"entries": len(revision.Entries)})
		}
		for index, entry := range revision.Entries {
			if !s.next() {
				return
			}
			if !isCanonicalName(entry.Owner) {
				s.add(CheckConstraints, "invalid_value", "revision", revision.ID, "entries",
					"revision entry owner is not a canonical lowercase DNS name",
					map[string]any{"index": index, "owner": entry.Owner})
			}
			switch entry.Type {
			case domain.EntryA, domain.EntryAAAA, domain.EntryCNAME:
				if !isEntryValue(entry.Type, entry.Value) {
					s.add(CheckConstraints, "invalid_value", "revision", revision.ID, "entries",
						"revision entry value does not match its type",
						map[string]any{"index": index, "type": string(entry.Type), "value": entry.Value})
				}
			default:
				s.add(CheckConstraints, "invalid_value", "revision", revision.ID, "entries",
					"revision entry type is not A, AAAA, or CNAME",
					map[string]any{"index": index, "type": string(entry.Type)})
			}
			if index > 0 {
				previous := revision.Entries[index-1]
				if previous == entry {
					s.add(CheckConstraints, "duplicate_entry", "revision", revision.ID, "entries",
						"revision contains the same entry more than once",
						map[string]any{"index": index, "owner": entry.Owner, "type": string(entry.Type), "value": entry.Value})
				} else if entryLess(entry, previous) {
					s.add(CheckConstraints, "unsorted_entries", "revision", revision.ID, "entries",
						"revision entries are not in stable sorted order",
						map[string]any{"index": index})
				}
			}
		}
		if revision.CreatedAt.IsZero() {
			s.add(CheckConstraints, "missing_value", "revision", revision.ID, "created_at",
				"revision created_at is not set", nil)
		}
	}

	// Serials must be unique within a plan; flag every revision that
	// shares its serial with a lower-id revision of the same plan.
	serialsByPlan := make(map[string]map[uint32]string, len(state.Plans))
	for _, key := range sortedKeys(state.Revisions) {
		if !s.next() {
			return
		}
		revision := state.Revisions[key]
		serials, ok := serialsByPlan[revision.PlanID]
		if !ok {
			serials = make(map[uint32]string)
			serialsByPlan[revision.PlanID] = serials
		}
		if firstID, exists := serials[revision.Serial]; exists {
			s.add(CheckConstraints, "duplicate_serial", "revision", revision.ID, "serial",
				"plan already contains a revision with this serial",
				map[string]any{"serial": revision.Serial, "first_revision_id": firstID})
			continue
		}
		serials[revision.Serial] = revision.ID
	}
}

func scanNodeConstraints(s *scanner, state *domain.State) {
	nameOwners := make(map[string]string, len(state.Nodes))
	for _, key := range sortedKeys(state.Nodes) {
		if !s.next() {
			return
		}
		node := state.Nodes[key]
		switch node.State {
		case domain.NodeReady, domain.NodeLeased, domain.NodeQuarantined:
		default:
			s.add(CheckConstraints, "unknown_state", "node", node.ID, "state",
				"node state is not a known value",
				map[string]any{"state": string(node.State)})
		}
		if !isCanonicalName(node.NodeName) {
			s.add(CheckConstraints, "invalid_value", "node", node.ID, "node_name",
				"node name is not a canonical lowercase DNS name",
				map[string]any{"node_name": node.NodeName})
		}
		if count := utf8.RuneCountInString(node.Region); count < 2 || count > domain.MaxRegionRunes {
			s.add(CheckConstraints, "invalid_value", "node", node.ID, "region",
				"node region must contain between 2 and 48 characters",
				map[string]any{"region": node.Region})
		}
		if node.Version < 1 {
			s.add(CheckConstraints, "invalid_value", "node", node.ID, "version",
				"node version must be at least 1",
				map[string]any{"version": node.Version})
		}
		if node.CreatedAt.IsZero() {
			s.add(CheckConstraints, "missing_value", "node", node.ID, "created_at",
				"node created_at is not set", nil)
		}
		if node.UpdatedAt.IsZero() {
			s.add(CheckConstraints, "missing_value", "node", node.ID, "updated_at",
				"node updated_at is not set", nil)
		}
		if !node.CreatedAt.IsZero() && !node.UpdatedAt.IsZero() && node.UpdatedAt.Before(node.CreatedAt) {
			s.add(CheckConstraints, "invalid_value", "node", node.ID, "updated_at",
				"node updated_at is before created_at", nil)
		}
		if firstID, exists := nameOwners[node.NodeName]; exists {
			s.add(CheckConstraints, "uniqueness_violation", "node", node.ID, "node_name",
				"node name is already used by another node",
				map[string]any{"node_name": node.NodeName, "first_node_id": firstID})
			continue
		}
		nameOwners[node.NodeName] = node.ID
	}
}

func scanLeaseConstraints(s *scanner, state *domain.State) {
	for _, key := range sortedKeys(state.Leases) {
		if !s.next() {
			return
		}
		lease := state.Leases[key]
		switch lease.State {
		case domain.LeaseClaimed, domain.LeaseReported, domain.LeaseReleased, domain.LeaseExpired:
		default:
			s.add(CheckConstraints, "unknown_state", "lease", lease.ID, "state",
				"lease state is not a known value",
				map[string]any{"state": string(lease.State)})
		}
		if lease.PlanID == "" {
			s.add(CheckConstraints, "missing_value", "lease", lease.ID, "plan_id",
				"lease plan_id is not set", nil)
		}
		if lease.NodeID == "" {
			s.add(CheckConstraints, "missing_value", "lease", lease.ID, "node_id",
				"lease node_id is not set", nil)
		}
		if lease.AllocationID == "" {
			s.add(CheckConstraints, "missing_value", "lease", lease.ID, "allocation_id",
				"lease allocation_id is not set", nil)
		}
		if lease.ClaimedAt.IsZero() {
			s.add(CheckConstraints, "missing_value", "lease", lease.ID, "claimed_at",
				"lease claimed_at is not set", nil)
		}
		if lease.ExpiresAt.IsZero() {
			s.add(CheckConstraints, "missing_value", "lease", lease.ID, "expires_at",
				"lease expires_at is not set", nil)
		}
		if !lease.ClaimedAt.IsZero() && !lease.ExpiresAt.IsZero() && !lease.ExpiresAt.After(lease.ClaimedAt) {
			s.add(CheckConstraints, "invalid_value", "lease", lease.ID, "expires_at",
				"lease expires_at must be after claimed_at", nil)
		}
		if lease.FinishedAt != nil && !lease.ClaimedAt.IsZero() && lease.FinishedAt.Before(lease.ClaimedAt) {
			s.add(CheckConstraints, "invalid_value", "lease", lease.ID, "finished_at",
				"lease finished_at is before claimed_at", nil)
		}
		switch lease.State {
		case domain.LeaseClaimed:
			if lease.FinishedAt != nil {
				s.add(CheckConstraints, "unexpected_value", "lease", lease.ID, "finished_at",
					"claimed lease must not have finished_at set", nil)
			}
			if lease.ProbeID != "" {
				s.add(CheckConstraints, "unexpected_value", "lease", lease.ID, "probe_id",
					"claimed lease must not have probe_id set", nil)
			}
		case domain.LeaseReported:
			if lease.FinishedAt == nil {
				s.add(CheckConstraints, "missing_value", "lease", lease.ID, "finished_at",
					"reported lease must have finished_at set", nil)
			}
			if lease.ProbeID == "" {
				s.add(CheckConstraints, "missing_value", "lease", lease.ID, "probe_id",
					"reported lease must have probe_id set", nil)
			}
		case domain.LeaseReleased, domain.LeaseExpired:
			if lease.FinishedAt == nil {
				s.add(CheckConstraints, "missing_value", "lease", lease.ID, "finished_at",
					"finished lease must have finished_at set", nil)
			}
			if lease.ProbeID != "" {
				s.add(CheckConstraints, "unexpected_value", "lease", lease.ID, "probe_id",
					"released or expired lease must not have probe_id set", nil)
			}
		}
	}
}

// scanNodeLeaseAgreement verifies the node/lease state contract: a claimed
// lease requires a leased node, a leased node requires exactly one claimed
// lease, and no node ever holds more than one claimed lease.
func scanNodeLeaseAgreement(s *scanner, state *domain.State) {
	claimedByNode := make(map[string][]string)
	for _, key := range sortedKeys(state.Leases) {
		if !s.next() {
			return
		}
		lease := state.Leases[key]
		if lease.State != domain.LeaseClaimed {
			continue
		}
		claimedByNode[lease.NodeID] = append(claimedByNode[lease.NodeID], lease.ID)
		if node, ok := state.Nodes[lease.NodeID]; ok && node.State != domain.NodeLeased {
			s.add(CheckConstraints, "state_mismatch", "lease", lease.ID, "node_id",
				"claimed lease belongs to a node that is not leased",
				map[string]any{"node_id": lease.NodeID, "node_state": string(node.State)})
		}
	}
	for _, key := range sortedKeys(state.Nodes) {
		if !s.next() {
			return
		}
		node := state.Nodes[key]
		claimed := claimedByNode[node.ID]
		if len(claimed) > 1 {
			s.add(CheckConstraints, "uniqueness_violation", "node", node.ID, "state",
				"node has more than one claimed lease",
				map[string]any{"lease_ids": claimed})
		}
		if node.State == domain.NodeLeased && len(claimed) == 0 {
			s.add(CheckConstraints, "state_mismatch", "node", node.ID, "state",
				"leased node has no claimed lease", nil)
		}
	}
}

func scanLeaseGroupConstraints(s *scanner, state *domain.State) {
	for _, key := range sortedKeys(state.LeaseGroups) {
		if !s.next() {
			return
		}
		group := state.LeaseGroups[key]
		if group.PlanID == "" {
			s.add(CheckConstraints, "missing_value", "lease_group", group.ID, "plan_id",
				"lease group plan_id is not set", nil)
		}
		if !isRequestKey(group.RequestKey) {
			s.add(CheckConstraints, "invalid_value", "lease_group", group.ID, "request_key",
				"lease group request key is malformed",
				map[string]any{"request_key": group.RequestKey})
		}
		if !isFingerprint(group.RequestFingerprint) {
			s.add(CheckConstraints, "invalid_value", "lease_group", group.ID, "request_fingerprint",
				"lease group request fingerprint is not a SHA-256 hex digest", nil)
		}
		if len(group.LeaseIDs) == 0 {
			s.add(CheckConstraints, "invalid_value", "lease_group", group.ID, "lease_ids",
				"lease group must contain at least one lease", nil)
		}
		if group.CreatedAt.IsZero() {
			s.add(CheckConstraints, "missing_value", "lease_group", group.ID, "created_at",
				"lease group created_at is not set", nil)
		}
	}
}

func scanProbeConstraints(s *scanner, state *domain.State) {
	for _, key := range sortedKeys(state.Probes) {
		if !s.next() {
			return
		}
		probe := state.Probes[key]
		if probe.LeaseID == "" {
			s.add(CheckConstraints, "missing_value", "probe", probe.ID, "lease_id",
				"probe lease_id is not set", nil)
		}
		if probe.PlanID == "" {
			s.add(CheckConstraints, "missing_value", "probe", probe.ID, "plan_id",
				"probe plan_id is not set", nil)
		}
		if probe.NodeID == "" {
			s.add(CheckConstraints, "missing_value", "probe", probe.ID, "node_id",
				"probe node_id is not set", nil)
		}
		if !isRequestKey(probe.RequestKey) {
			s.add(CheckConstraints, "invalid_value", "probe", probe.ID, "request_key",
				"probe request key is malformed",
				map[string]any{"request_key": probe.RequestKey})
		}
		if probe.LatencyMS < 0 || probe.LatencyMS > 600000 {
			s.add(CheckConstraints, "invalid_value", "probe", probe.ID, "latency_ms",
				"probe latency must be between 0 and 600000",
				map[string]any{"latency_ms": probe.LatencyMS})
		}
		if probe.ObservedAt.IsZero() {
			s.add(CheckConstraints, "missing_value", "probe", probe.ID, "observed_at",
				"probe observed_at is not set", nil)
		}
		if probe.StoredAt.IsZero() {
			s.add(CheckConstraints, "missing_value", "probe", probe.ID, "stored_at",
				"probe stored_at is not set", nil)
		}
		if !isFingerprint(probe.PayloadFingerprint) {
			s.add(CheckConstraints, "invalid_value", "probe", probe.ID, "payload_fingerprint",
				"probe payload fingerprint is not a SHA-256 hex digest", nil)
		}
	}
}

// scanProbeLeaseAgreement verifies that each lease carries at most one
// probe and that a probe always belongs to a reported lease.
func scanProbeLeaseAgreement(s *scanner, state *domain.State) {
	probesByLease := make(map[string][]string)
	for _, key := range sortedKeys(state.Probes) {
		if !s.next() {
			return
		}
		probe := state.Probes[key]
		probesByLease[probe.LeaseID] = append(probesByLease[probe.LeaseID], probe.ID)
		if lease, ok := state.Leases[probe.LeaseID]; ok && lease.State != domain.LeaseReported {
			s.add(CheckConstraints, "state_mismatch", "probe", probe.ID, "lease_id",
				"probe belongs to a lease that is not reported",
				map[string]any{"lease_id": probe.LeaseID, "lease_state": string(lease.State)})
		}
	}
	for _, leaseID := range sortedKeys(probesByLease) {
		if !s.next() {
			return
		}
		probeIDs := probesByLease[leaseID]
		if len(probeIDs) > 1 {
			s.add(CheckConstraints, "uniqueness_violation", "lease", leaseID, "probe_id",
				"lease has more than one probe",
				map[string]any{"probe_ids": probeIDs})
		}
	}
}

func scanAssessmentConstraints(s *scanner, state *domain.State) {
	for _, key := range sortedKeys(state.Assessments) {
		if !s.next() {
			return
		}
		assessment := state.Assessments[key]
		switch assessment.Verdict {
		case domain.VerdictConsistent, domain.VerdictReviewRequired:
		default:
			s.add(CheckConstraints, "unknown_verdict", "assessment", assessment.ID, "verdict",
				"assessment verdict is not a known value",
				map[string]any{"verdict": string(assessment.Verdict)})
		}
		if assessment.PlanID == "" {
			s.add(CheckConstraints, "missing_value", "assessment", assessment.ID, "plan_id",
				"assessment plan_id is not set", nil)
		}
		if assessment.RevisionID == "" {
			s.add(CheckConstraints, "missing_value", "assessment", assessment.ID, "revision_id",
				"assessment revision_id is not set", nil)
		}
		if !isRequestKey(assessment.RequestKey) {
			s.add(CheckConstraints, "invalid_value", "assessment", assessment.ID, "request_key",
				"assessment request key is malformed",
				map[string]any{"request_key": assessment.RequestKey})
		}
		if assessment.ProbeCount < 0 {
			s.add(CheckConstraints, "invalid_value", "assessment", assessment.ID, "probe_count",
				"assessment probe count must not be negative",
				map[string]any{"probe_count": assessment.ProbeCount})
		}
		if assessment.SuccessCount < 0 {
			s.add(CheckConstraints, "invalid_value", "assessment", assessment.ID, "success_count",
				"assessment success count must not be negative",
				map[string]any{"success_count": assessment.SuccessCount})
		}
		if assessment.MaximumLatencyMS < 0 {
			s.add(CheckConstraints, "invalid_value", "assessment", assessment.ID, "maximum_latency_ms",
				"assessment maximum latency must not be negative",
				map[string]any{"maximum_latency_ms": assessment.MaximumLatencyMS})
		}
		seen := make(map[string]bool, len(assessment.Reasons))
		for index, reason := range assessment.Reasons {
			if !s.next() {
				return
			}
			if !knownReasons[reason] {
				s.add(CheckConstraints, "unknown_reason", "assessment", assessment.ID, "reasons",
					"assessment reason is not a known code",
					map[string]any{"reason": reason, "index": index})
			}
			if seen[reason] {
				s.add(CheckConstraints, "duplicate_reason", "assessment", assessment.ID, "reasons",
					"assessment lists the same reason more than once",
					map[string]any{"reason": reason, "index": index})
			}
			seen[reason] = true
			if index > 0 && assessment.Reasons[index-1] > reason {
				s.add(CheckConstraints, "unsorted_reasons", "assessment", assessment.ID, "reasons",
					"assessment reasons are not in stable sorted order",
					map[string]any{"index": index})
			}
		}
		switch assessment.Verdict {
		case domain.VerdictConsistent:
			if len(assessment.Reasons) > 0 {
				s.add(CheckConstraints, "verdict_mismatch", "assessment", assessment.ID, "verdict",
					"consistent assessment must not carry reasons",
					map[string]any{"reasons": assessment.Reasons})
			}
		case domain.VerdictReviewRequired:
			if len(assessment.Reasons) == 0 {
				s.add(CheckConstraints, "verdict_mismatch", "assessment", assessment.ID, "verdict",
					"review_required assessment must carry at least one reason", nil)
			}
		}
		if assessment.CreatedAt.IsZero() {
			s.add(CheckConstraints, "missing_value", "assessment", assessment.ID, "created_at",
				"assessment created_at is not set", nil)
		}
	}
}

func scanAuditConstraints(s *scanner, state *domain.State) {
	seenIDs := make(map[string]int, len(state.AuditEvents))
	for index, event := range state.AuditEvents {
		if !s.next() {
			return
		}
		eventID := event.ID
		if eventID == "" {
			eventID = indexLabel(index)
		}
		if event.ID == "" {
			s.add(CheckConstraints, "missing_value", "audit_event", eventID, "id",
				"audit event id is not set",
				map[string]any{"index": index})
		} else if firstIndex, exists := seenIDs[event.ID]; exists {
			s.add(CheckConstraints, "uniqueness_violation", "audit_event", eventID, "id",
				"audit event id is used more than once",
				map[string]any{"index": index, "first_index": firstIndex})
		}
		seenIDs[event.ID] = index
		if event.EntityType == "" {
			s.add(CheckConstraints, "missing_value", "audit_event", eventID, "entity_type",
				"audit event entity_type is not set",
				map[string]any{"index": index})
		}
		if event.EntityID == "" {
			s.add(CheckConstraints, "missing_value", "audit_event", eventID, "entity_id",
				"audit event entity_id is not set",
				map[string]any{"index": index})
		}
		if event.Action == "" {
			s.add(CheckConstraints, "missing_value", "audit_event", eventID, "action",
				"audit event action is not set",
				map[string]any{"index": index})
		}
		if event.At.IsZero() {
			s.add(CheckConstraints, "missing_value", "audit_event", eventID, "at",
				"audit event timestamp is not set",
				map[string]any{"index": index})
		}
	}
}
