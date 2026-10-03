package verify

// checkReferences verifies that every cross-entity reference resolves and
// that paired references point back at each other.
func (c *checker) checkReferences() {
	for _, id := range sortedKeys(c.state.Plans) {
		if c.tick() {
			return
		}
		plan := c.state.Plans[id]
		if plan.LatestRevisionID != "" {
			revision, ok := c.state.Revisions[plan.LatestRevisionID]
			c.requireRef("plan", id, "latest_revision_id", "revision", plan.LatestRevisionID, ok)
			if ok && revision.PlanID != id {
				c.add("plan", id, "latest_revision_id", "revision_plan_mismatch",
					"latest revision belongs to another plan",
					map[string]any{"revision_id": revision.ID, "revision_plan_id": revision.PlanID})
			}
		}
		if plan.LatestAssessmentID != "" {
			assessment, ok := c.state.Assessments[plan.LatestAssessmentID]
			c.requireRef("plan", id, "latest_assessment_id", "assessment", plan.LatestAssessmentID, ok)
			if ok && assessment.PlanID != id {
				c.add("plan", id, "latest_assessment_id", "assessment_plan_mismatch",
					"latest assessment belongs to another plan",
					map[string]any{"assessment_id": assessment.ID, "assessment_plan_id": assessment.PlanID})
			}
		}
	}
	for _, id := range sortedKeys(c.state.Revisions) {
		if c.tick() {
			return
		}
		revision := c.state.Revisions[id]
		_, ok := c.state.Plans[revision.PlanID]
		c.requireRef("revision", id, "plan_id", "plan", revision.PlanID, ok)
	}
	for _, id := range sortedKeys(c.state.Leases) {
		if c.tick() {
			return
		}
		lease := c.state.Leases[id]
		_, planOK := c.state.Plans[lease.PlanID]
		c.requireRef("lease", id, "plan_id", "plan", lease.PlanID, planOK)
		_, nodeOK := c.state.Nodes[lease.NodeID]
		c.requireRef("lease", id, "node_id", "node", lease.NodeID, nodeOK)
		group, groupOK := c.state.LeaseGroups[lease.AllocationID]
		c.requireRef("lease", id, "allocation_id", "lease_group", lease.AllocationID, groupOK)
		if groupOK && !containsString(group.LeaseIDs, id) {
			c.add("lease", id, "allocation_id", "lease_not_in_group",
				"lease is not listed in its allocation group",
				map[string]any{"lease_group_id": group.ID})
		}
		if lease.ProbeID != "" {
			probe, ok := c.state.Probes[lease.ProbeID]
			c.requireRef("lease", id, "probe_id", "probe", lease.ProbeID, ok)
			if ok && probe.LeaseID != id {
				c.add("lease", id, "probe_id", "probe_back_reference_mismatch",
					"referenced probe points at a different lease",
					map[string]any{"probe_id": probe.ID, "probe_lease_id": probe.LeaseID})
			}
		}
	}
	for _, id := range sortedKeys(c.state.LeaseGroups) {
		if c.tick() {
			return
		}
		group := c.state.LeaseGroups[id]
		_, planOK := c.state.Plans[group.PlanID]
		c.requireRef("lease_group", id, "plan_id", "plan", group.PlanID, planOK)
		for _, leaseID := range group.LeaseIDs {
			if c.tick() {
				return
			}
			lease, ok := c.state.Leases[leaseID]
			c.requireRef("lease_group", id, "lease_ids", "lease", leaseID, ok)
			if !ok {
				continue
			}
			if lease.AllocationID != id {
				c.add("lease_group", id, "lease_ids", "lease_back_reference_mismatch",
					"listed lease points at a different allocation group",
					map[string]any{"lease_id": leaseID, "lease_allocation_id": lease.AllocationID})
			}
			if lease.PlanID != group.PlanID {
				c.add("lease_group", id, "lease_ids", "lease_plan_mismatch",
					"listed lease belongs to another plan",
					map[string]any{"lease_id": leaseID, "lease_plan_id": lease.PlanID})
			}
		}
	}
	for _, id := range sortedKeys(c.state.Probes) {
		if c.tick() {
			return
		}
		probe := c.state.Probes[id]
		lease, leaseOK := c.state.Leases[probe.LeaseID]
		c.requireRef("probe", id, "lease_id", "lease", probe.LeaseID, leaseOK)
		if leaseOK {
			if lease.ProbeID != id {
				c.add("probe", id, "lease_id", "lease_back_reference_mismatch",
					"referenced lease does not point back at this probe",
					map[string]any{"lease_id": lease.ID, "lease_probe_id": lease.ProbeID})
			}
			if probe.PlanID != lease.PlanID {
				c.add("probe", id, "plan_id", "plan_mismatch",
					"probe plan does not match the lease plan",
					map[string]any{"lease_id": lease.ID, "lease_plan_id": lease.PlanID})
			}
			if probe.NodeID != lease.NodeID {
				c.add("probe", id, "node_id", "node_mismatch",
					"probe node does not match the lease node",
					map[string]any{"lease_id": lease.ID, "lease_node_id": lease.NodeID})
			}
		}
		_, planOK := c.state.Plans[probe.PlanID]
		c.requireRef("probe", id, "plan_id", "plan", probe.PlanID, planOK)
		_, nodeOK := c.state.Nodes[probe.NodeID]
		c.requireRef("probe", id, "node_id", "node", probe.NodeID, nodeOK)
	}
	for _, id := range sortedKeys(c.state.Assessments) {
		if c.tick() {
			return
		}
		assessment := c.state.Assessments[id]
		_, planOK := c.state.Plans[assessment.PlanID]
		c.requireRef("assessment", id, "plan_id", "plan", assessment.PlanID, planOK)
		revision, revisionOK := c.state.Revisions[assessment.RevisionID]
		c.requireRef("assessment", id, "revision_id", "revision", assessment.RevisionID, revisionOK)
		if revisionOK && revision.PlanID != assessment.PlanID {
			c.add("assessment", id, "revision_id", "revision_plan_mismatch",
				"referenced revision belongs to another plan",
				map[string]any{"revision_id": revision.ID, "revision_plan_id": revision.PlanID})
		}
	}
	for index, event := range c.state.AuditEvents {
		if c.tick() {
			return
		}
		if event.EntityID == "" {
			continue
		}
		exists, known := c.auditTargetExists(event.EntityType, event.EntityID)
		if known && !exists {
			c.add("audit_event", event.ID, "entity_id", "unknown_entity_reference",
				"audit event references a missing entity",
				map[string]any{
					"index":       index,
					"entity_type": event.EntityType,
					"target_id":   event.EntityID,
				})
		}
	}
}

// auditTargetExists resolves the entity referenced by an audit event. The
// second result reports whether the entity type is one the system writes.
func (c *checker) auditTargetExists(entityType string, entityID string) (bool, bool) {
	switch entityType {
	case "plan":
		_, ok := c.state.Plans[entityID]
		return ok, true
	case "revision":
		_, ok := c.state.Revisions[entityID]
		return ok, true
	case "node":
		_, ok := c.state.Nodes[entityID]
		return ok, true
	case "lease_group":
		_, ok := c.state.LeaseGroups[entityID]
		return ok, true
	case "lease":
		_, ok := c.state.Leases[entityID]
		return ok, true
	case "probe":
		_, ok := c.state.Probes[entityID]
		return ok, true
	case "assessment":
		_, ok := c.state.Assessments[entityID]
		return ok, true
	default:
		return false, false
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
