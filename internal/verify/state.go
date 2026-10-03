package verify

import (
	"net"
	"strings"
	"time"
	"unicode/utf8"

	"solo-0014-zone-rollout-control/internal/domain"
)

// checkState verifies per-entity value constraints and cross-entity state
// agreements. Every rule mirrors a constraint the services enforce on write,
// so a violation means the snapshot no longer matches what the system could
// have produced.
func (c *checker) checkState() {
	c.checkPlanStates()
	c.checkRevisionStates()
	c.checkNodeStates()
	c.checkLeaseStates()
	c.checkLeaseGroupStates()
	c.checkProbeStates()
	c.checkAssessmentStates()
	c.checkAuditStates()
	c.checkNodeLeaseAgreement()
}

func (c *checker) checkPlanStates() {
	for _, id := range sortedKeys(c.state.Plans) {
		if c.tick() {
			return
		}
		plan := c.state.Plans[id]
		switch plan.State {
		case domain.PlanDraft, domain.PlanActive, domain.PlanCompleted:
		default:
			c.add("plan", id, "state", "unknown_plan_state",
				"plan state is not a known value",
				map[string]any{"state": plan.State})
		}
		activeOrCompleted := plan.State == domain.PlanActive || plan.State == domain.PlanCompleted
		if activeOrCompleted && plan.ActivatedAt == nil {
			c.add("plan", id, "activated_at", "missing_activated_at",
				"plan is active or completed without an activation time", nil)
		}
		if plan.State == domain.PlanDraft && plan.ActivatedAt != nil {
			c.add("plan", id, "activated_at", "unexpected_activated_at",
				"draft plan carries an activation time", nil)
		}
		if plan.State == domain.PlanCompleted && plan.CompletedAt == nil {
			c.add("plan", id, "completed_at", "missing_completed_at",
				"completed plan has no completion time", nil)
		}
		if plan.State != domain.PlanCompleted && plan.CompletedAt != nil {
			c.add("plan", id, "completed_at", "unexpected_completed_at",
				"plan is not completed but carries a completion time", nil)
		}
		if plan.Version < 1 {
			c.add("plan", id, "version", "invalid_version",
				"plan version must be at least 1",
				map[string]any{"version": plan.Version})
		}
		if plan.ExpectedNodes < 1 || plan.ExpectedNodes > 500 {
			c.add("plan", id, "expected_nodes", "invalid_expected_nodes",
				"expected nodes must be between 1 and 500",
				map[string]any{"expected_nodes": plan.ExpectedNodes})
		}
		if plan.MinimumSuccessRatio < 0 || plan.MinimumSuccessRatio > 1 {
			c.add("plan", id, "minimum_success_ratio", "invalid_minimum_success_ratio",
				"minimum success ratio must be between 0 and 1",
				map[string]any{"minimum_success_ratio": plan.MinimumSuccessRatio})
		}
		if plan.MaximumLatencyMS < 1 || plan.MaximumLatencyMS > 600000 {
			c.add("plan", id, "maximum_latency_ms", "invalid_maximum_latency_ms",
				"maximum latency must be between 1 and 600000",
				map[string]any{"maximum_latency_ms": plan.MaximumLatencyMS})
		}
		if !domain.IsCanonicalName(plan.Zone) {
			c.add("plan", id, "zone", "invalid_zone",
				"zone is not a valid canonical DNS name",
				map[string]any{"zone": plan.Zone})
		}
		if plan.CreatedAt.IsZero() {
			c.add("plan", id, "created_at", "missing_created_at",
				"plan has no creation time", nil)
		}
		if activeOrCompleted && plan.LatestRevisionID == "" {
			c.add("plan", id, "latest_revision_id", "missing_latest_revision",
				"plan left draft without a zone revision", nil)
		}
		if plan.ActivatedAt != nil && !plan.CreatedAt.IsZero() && plan.ActivatedAt.Before(plan.CreatedAt) {
			c.add("plan", id, "activated_at", "activated_before_created",
				"activation time is earlier than the creation time", nil)
		}
		if plan.CompletedAt != nil && plan.ActivatedAt != nil && plan.CompletedAt.Before(*plan.ActivatedAt) {
			c.add("plan", id, "completed_at", "completed_before_activated",
				"completion time is earlier than the activation time", nil)
		}
	}
}

func (c *checker) checkRevisionStates() {
	for _, id := range sortedKeys(c.state.Revisions) {
		if c.tick() {
			return
		}
		revision := c.state.Revisions[id]
		if revision.Serial == 0 {
			c.add("revision", id, "serial", "invalid_serial",
				"revision serial must be greater than zero", nil)
		}
		if !domain.IsCanonicalName(revision.Authority) {
			c.add("revision", id, "authority", "invalid_authority",
				"authority is not a valid canonical DNS name",
				map[string]any{"authority": revision.Authority})
		}
		if len(revision.Entries) < 1 || len(revision.Entries) > domain.MaxEntries {
			c.add("revision", id, "entries", "invalid_entries",
				"revision must contain between 1 and 500 entries",
				map[string]any{"entries": len(revision.Entries)})
		}
		if revision.CreatedAt.IsZero() {
			c.add("revision", id, "created_at", "missing_created_at",
				"revision has no creation time", nil)
		}
		sorted := true
		for index, entry := range revision.Entries {
			if !domain.IsCanonicalName(entry.Owner) {
				c.add("revision", id, "entries", "invalid_entry_owner",
					"entry owner is not a valid canonical DNS name",
					map[string]any{"index": index, "owner": entry.Owner})
			}
			switch entry.Type {
			case domain.EntryA:
				parsed := net.ParseIP(entry.Value)
				if parsed == nil || parsed.To4() == nil || entry.Value != parsed.To4().String() {
					c.add("revision", id, "entries", "invalid_entry_value",
						"entry value is not a canonical IPv4 address",
						map[string]any{"index": index, "value": entry.Value})
				}
			case domain.EntryAAAA:
				parsed := net.ParseIP(entry.Value)
				if parsed == nil || parsed.To4() != nil || entry.Value != parsed.String() {
					c.add("revision", id, "entries", "invalid_entry_value",
						"entry value is not a canonical IPv6 address",
						map[string]any{"index": index, "value": entry.Value})
				}
			case domain.EntryCNAME:
				if !domain.IsCanonicalName(entry.Value) {
					c.add("revision", id, "entries", "invalid_entry_value",
						"entry value is not a valid canonical DNS name",
						map[string]any{"index": index, "value": entry.Value})
				}
			default:
				c.add("revision", id, "entries", "invalid_entry_type",
					"entry type must be A, AAAA, or CNAME",
					map[string]any{"index": index, "type": entry.Type})
			}
			if index == 0 {
				continue
			}
			previous := revision.Entries[index-1]
			if entry == previous {
				c.add("revision", id, "entries", "duplicate_entry",
					"revision contains a duplicated entry",
					map[string]any{"index": index, "owner": entry.Owner, "type": entry.Type, "value": entry.Value})
			}
			if sorted && entryLess(entry, previous) {
				sorted = false
				c.add("revision", id, "entries", "entries_not_sorted",
					"revision entries are not in the stored sort order",
					map[string]any{"index": index})
			}
		}
	}
}

func (c *checker) checkNodeStates() {
	for _, id := range sortedKeys(c.state.Nodes) {
		if c.tick() {
			return
		}
		node := c.state.Nodes[id]
		switch node.State {
		case domain.NodeReady, domain.NodeLeased, domain.NodeQuarantined:
		default:
			c.add("node", id, "state", "unknown_node_state",
				"node state is not a known value",
				map[string]any{"state": node.State})
		}
		if !domain.IsCanonicalName(node.NodeName) {
			c.add("node", id, "node_name", "invalid_node_name",
				"node name is not a valid canonical DNS name",
				map[string]any{"node_name": node.NodeName})
		}
		normalizedRegion := strings.TrimSpace(strings.ToLower(node.Region))
		if node.Region != normalizedRegion || utf8.RuneCountInString(node.Region) < 2 ||
			utf8.RuneCountInString(node.Region) > domain.MaxRegionRunes {
			c.add("node", id, "region", "invalid_region",
				"region is not normalized or outside the 2 to 48 character range",
				map[string]any{"region": node.Region})
		}
		if node.Version < 1 {
			c.add("node", id, "version", "invalid_version",
				"node version must be at least 1",
				map[string]any{"version": node.Version})
		}
		if node.CreatedAt.IsZero() {
			c.add("node", id, "created_at", "missing_created_at",
				"node has no creation time", nil)
		}
		if node.UpdatedAt.IsZero() {
			c.add("node", id, "updated_at", "missing_updated_at",
				"node has no update time", nil)
		}
		if !node.CreatedAt.IsZero() && !node.UpdatedAt.IsZero() && node.UpdatedAt.Before(node.CreatedAt) {
			c.add("node", id, "updated_at", "updated_before_created",
				"update time is earlier than the creation time", nil)
		}
	}
	byName := make(map[string]string, len(c.state.Nodes))
	for _, id := range sortedKeys(c.state.Nodes) {
		if c.tick() {
			return
		}
		name := c.state.Nodes[id].NodeName
		if first, ok := byName[name]; ok {
			c.add("node", id, "node_name", "duplicate_node_name",
				"node name is enrolled more than once",
				map[string]any{"node_name": name, "first_node_id": first})
			continue
		}
		byName[name] = id
	}
}

func (c *checker) checkLeaseStates() {
	for _, id := range sortedKeys(c.state.Leases) {
		if c.tick() {
			return
		}
		lease := c.state.Leases[id]
		terminal := false
		switch lease.State {
		case domain.LeaseClaimed:
		case domain.LeaseReported, domain.LeaseReleased, domain.LeaseExpired:
			terminal = true
		default:
			c.add("lease", id, "state", "unknown_lease_state",
				"lease state is not a known value",
				map[string]any{"state": lease.State})
		}
		if lease.ClaimedAt.IsZero() {
			c.add("lease", id, "claimed_at", "missing_claimed_at",
				"lease has no claim time", nil)
		}
		if !lease.ClaimedAt.IsZero() && !lease.ExpiresAt.IsZero() {
			if !lease.ExpiresAt.After(lease.ClaimedAt) {
				c.add("lease", id, "expires_at", "expires_not_after_claimed",
					"lease expiry is not after its claim time", nil)
			} else if duration := lease.ExpiresAt.Sub(lease.ClaimedAt); duration < 30*time.Second || duration > 86400*time.Second {
				c.add("lease", id, "expires_at", "invalid_lease_duration",
					"lease duration must be between 30 and 86400 seconds",
					map[string]any{"seconds": int64(duration / time.Second)})
			}
		}
		if terminal && lease.FinishedAt == nil {
			c.add("lease", id, "finished_at", "missing_finished_at",
				"finished lease has no finish time", nil)
		}
		if !terminal && lease.FinishedAt != nil {
			c.add("lease", id, "finished_at", "unexpected_finished_at",
				"unfinished lease carries a finish time", nil)
		}
		if lease.State == domain.LeaseReported && lease.ProbeID == "" {
			c.add("lease", id, "probe_id", "missing_probe_id",
				"reported lease has no probe reference", nil)
		}
		if lease.State != domain.LeaseReported && lease.ProbeID != "" {
			c.add("lease", id, "probe_id", "unexpected_probe_id",
				"lease is not reported but carries a probe reference", nil)
		}
	}
}

func (c *checker) checkLeaseGroupStates() {
	for _, id := range sortedKeys(c.state.LeaseGroups) {
		if c.tick() {
			return
		}
		group := c.state.LeaseGroups[id]
		if !domain.IsValidRequestKey(strings.TrimSpace(group.RequestKey)) {
			c.add("lease_group", id, "request_key", "invalid_request_key",
				"request key violates the request key constraints",
				map[string]any{"request_key": group.RequestKey})
		}
		if !isFingerprint(group.RequestFingerprint) {
			c.add("lease_group", id, "request_fingerprint", "invalid_request_fingerprint",
				"request fingerprint is not a SHA-256 hex digest",
				map[string]any{"request_fingerprint": group.RequestFingerprint})
		}
		if len(group.LeaseIDs) < 1 || len(group.LeaseIDs) > 100 {
			c.add("lease_group", id, "lease_ids", "invalid_lease_count",
				"lease group must contain between 1 and 100 leases",
				map[string]any{"leases": len(group.LeaseIDs)})
		}
		if group.CreatedAt.IsZero() {
			c.add("lease_group", id, "created_at", "missing_created_at",
				"lease group has no creation time", nil)
		}
	}
}

func (c *checker) checkProbeStates() {
	byLease := make(map[string]string, len(c.state.Probes))
	for _, id := range sortedKeys(c.state.Probes) {
		if c.tick() {
			return
		}
		probe := c.state.Probes[id]
		if !domain.IsValidRequestKey(strings.TrimSpace(probe.RequestKey)) {
			c.add("probe", id, "request_key", "invalid_request_key",
				"request key violates the request key constraints",
				map[string]any{"request_key": probe.RequestKey})
		}
		if probe.LatencyMS < 0 || probe.LatencyMS > 600000 {
			c.add("probe", id, "latency_ms", "invalid_latency_ms",
				"latency must be between 0 and 600000",
				map[string]any{"latency_ms": probe.LatencyMS})
		}
		if probe.ObservedAt.IsZero() {
			c.add("probe", id, "observed_at", "missing_observed_at",
				"probe has no observation time", nil)
		}
		if probe.StoredAt.IsZero() {
			c.add("probe", id, "stored_at", "missing_stored_at",
				"probe has no storage time", nil)
		}
		if !isFingerprint(probe.PayloadFingerprint) {
			c.add("probe", id, "payload_fingerprint", "invalid_payload_fingerprint",
				"payload fingerprint is not a SHA-256 hex digest",
				map[string]any{"payload_fingerprint": probe.PayloadFingerprint})
		}
		if first, ok := byLease[probe.LeaseID]; ok {
			c.add("probe", id, "lease_id", "duplicate_lease_probe",
				"lease already has a stored probe",
				map[string]any{"lease_id": probe.LeaseID, "first_probe_id": first})
			continue
		}
		byLease[probe.LeaseID] = id
	}
}

func (c *checker) checkAssessmentStates() {
	for _, id := range sortedKeys(c.state.Assessments) {
		if c.tick() {
			return
		}
		assessment := c.state.Assessments[id]
		switch assessment.Verdict {
		case domain.VerdictConsistent, domain.VerdictReviewRequired:
		default:
			c.add("assessment", id, "verdict", "unknown_verdict",
				"assessment verdict is not a known value",
				map[string]any{"verdict": assessment.Verdict})
		}
		if !domain.IsValidRequestKey(strings.TrimSpace(assessment.RequestKey)) {
			c.add("assessment", id, "request_key", "invalid_request_key",
				"request key violates the request key constraints",
				map[string]any{"request_key": assessment.RequestKey})
		}
		for index, reason := range assessment.Reasons {
			if !isKnownReason(reason) {
				c.add("assessment", id, "reasons", "unknown_reason",
					"assessment reason is not a known reason code",
					map[string]any{"index": index, "reason": reason})
			}
			if index == 0 {
				continue
			}
			previous := assessment.Reasons[index-1]
			if reason == previous {
				c.add("assessment", id, "reasons", "duplicate_reason",
					"assessment reason is duplicated",
					map[string]any{"index": index, "reason": reason})
			} else if reason < previous {
				c.add("assessment", id, "reasons", "reasons_not_sorted",
					"assessment reasons are not in sorted order",
					map[string]any{"index": index})
			}
		}
		if assessment.CreatedAt.IsZero() {
			c.add("assessment", id, "created_at", "missing_created_at",
				"assessment has no creation time", nil)
		}
	}
}

func (c *checker) checkAuditStates() {
	for index, event := range c.state.AuditEvents {
		if c.tick() {
			return
		}
		if event.ID == "" {
			c.add("audit_event", "", "id", "missing_audit_id",
				"audit event has no id",
				map[string]any{"index": index})
		}
		if _, known := c.auditTargetExists(event.EntityType, ""); !known {
			c.add("audit_event", event.ID, "entity_type", "unknown_entity_type",
				"audit event entity type is not a known value",
				map[string]any{"index": index, "entity_type": event.EntityType})
		}
		if event.Action == "" {
			c.add("audit_event", event.ID, "action", "missing_action",
				"audit event has no action",
				map[string]any{"index": index})
		}
		if event.At.IsZero() {
			c.add("audit_event", event.ID, "at", "missing_timestamp",
				"audit event has no timestamp",
				map[string]any{"index": index})
		}
	}
}

// checkNodeLeaseAgreement verifies that the leased node state and claimed
// leases agree: a node is leased exactly while one claimed lease points at
// it, and no node carries more than one claimed lease.
func (c *checker) checkNodeLeaseAgreement() {
	claimedByNode := make(map[string][]string)
	for _, id := range sortedKeys(c.state.Leases) {
		if c.tick() {
			return
		}
		if c.state.Leases[id].State == domain.LeaseClaimed {
			nodeID := c.state.Leases[id].NodeID
			claimedByNode[nodeID] = append(claimedByNode[nodeID], id)
		}
	}
	for _, id := range sortedKeys(c.state.Nodes) {
		if c.tick() {
			return
		}
		node := c.state.Nodes[id]
		claimed := claimedByNode[id]
		if len(claimed) > 1 {
			c.add("node", id, "state", "multiple_claimed_leases",
				"node has more than one claimed lease",
				map[string]any{"lease_ids": claimed})
		}
		if node.State == domain.NodeLeased && len(claimed) == 0 {
			c.add("node", id, "state", "node_lease_state_mismatch",
				"node is leased but no claimed lease references it", nil)
		}
		if node.State != domain.NodeLeased && len(claimed) > 0 {
			c.add("node", id, "state", "node_lease_state_mismatch",
				"node is not leased but a claimed lease references it",
				map[string]any{"lease_ids": claimed})
		}
	}
}

// entryLess mirrors the stable entry ordering applied on write.
func entryLess(a domain.ZoneEntry, b domain.ZoneEntry) bool {
	if a.Owner != b.Owner {
		return a.Owner < b.Owner
	}
	if a.Type != b.Type {
		return a.Type < b.Type
	}
	return a.Value < b.Value
}

// isKnownReason mirrors the reason codes produced by the assessment builder.
func isKnownReason(reason string) bool {
	switch reason {
	case "no_probes", "node_not_observed", "serial_mismatch",
		"success_ratio_below_minimum", "latency_limit_exceeded":
		return true
	default:
		return false
	}
}

// isFingerprint reports whether value looks like a SHA-256 hex digest, the
// form used for lease request and probe payload fingerprints.
func isFingerprint(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if r >= '0' && r <= '9' || r >= 'a' && r <= 'f' {
			continue
		}
		return false
	}
	return true
}
