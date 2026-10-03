package verify

import "strings"

// requestIndexSeparator joins plan id and request key in the idempotency
// indexes, mirroring the services that write them.
const requestIndexSeparator = "\x00"

// checkIndexes verifies the idempotency request indexes in both directions:
// every index entry must resolve to the record it points at, and every
// record must be reachable through its index entry.
func (c *checker) checkIndexes() {
	for _, key := range sortedKeys(c.state.LeaseRequestKeys) {
		if c.tick() {
			return
		}
		groupID := c.state.LeaseRequestKeys[key]
		planID, requestKey, ok := splitRequestIndexKey(key)
		if !ok {
			c.add("lease_request_index", key, "", "malformed_request_index_key",
				"index key is not a plan id and request key pair", nil)
			continue
		}
		_, planOK := c.state.Plans[planID]
		c.requireRef("lease_request_index", key, "", "plan", planID, planOK)
		group, groupOK := c.state.LeaseGroups[groupID]
		c.requireRef("lease_request_index", key, "", "lease_group", groupID, groupOK)
		if !groupOK {
			continue
		}
		if group.PlanID != planID || group.RequestKey != requestKey {
			c.add("lease_request_index", key, "", "request_index_mismatch",
				"index key does not match the referenced lease group",
				map[string]any{
					"lease_group_id": groupID,
					"group_plan_id":  group.PlanID,
					"group_request":  group.RequestKey,
					"index_plan_id":  planID,
					"index_request":  requestKey,
				})
		}
	}
	for _, id := range sortedKeys(c.state.LeaseGroups) {
		if c.tick() {
			return
		}
		group := c.state.LeaseGroups[id]
		key := group.PlanID + requestIndexSeparator + group.RequestKey
		indexed, ok := c.state.LeaseRequestKeys[key]
		if !ok || indexed != id {
			c.add("lease_group", id, "request_key", "missing_request_index",
				"lease group is not reachable through the lease request index",
				map[string]any{"index_key_present": ok, "indexed_group_id": indexed})
		}
	}
	for _, key := range sortedKeys(c.state.AssessmentRequestKeys) {
		if c.tick() {
			return
		}
		assessmentID := c.state.AssessmentRequestKeys[key]
		planID, requestKey, ok := splitRequestIndexKey(key)
		if !ok {
			c.add("assessment_request_index", key, "", "malformed_request_index_key",
				"index key is not a plan id and request key pair", nil)
			continue
		}
		_, planOK := c.state.Plans[planID]
		c.requireRef("assessment_request_index", key, "", "plan", planID, planOK)
		assessment, assessmentOK := c.state.Assessments[assessmentID]
		c.requireRef("assessment_request_index", key, "", "assessment", assessmentID, assessmentOK)
		if !assessmentOK {
			continue
		}
		if assessment.PlanID != planID || assessment.RequestKey != requestKey {
			c.add("assessment_request_index", key, "", "request_index_mismatch",
				"index key does not match the referenced assessment",
				map[string]any{
					"assessment_id":      assessmentID,
					"assessment_plan_id": assessment.PlanID,
					"assessment_request": assessment.RequestKey,
					"index_plan_id":      planID,
					"index_request":      requestKey,
				})
		}
	}
	for _, id := range sortedKeys(c.state.Assessments) {
		if c.tick() {
			return
		}
		assessment := c.state.Assessments[id]
		key := assessment.PlanID + requestIndexSeparator + assessment.RequestKey
		indexed, ok := c.state.AssessmentRequestKeys[key]
		if !ok || indexed != id {
			c.add("assessment", id, "request_key", "missing_request_index",
				"assessment is not reachable through the assessment request index",
				map[string]any{"index_key_present": ok, "indexed_assessment_id": indexed})
		}
	}
}

// splitRequestIndexKey splits an index key into plan id and request key.
// Request keys never contain the separator, so a key with zero or multiple
// separators is malformed.
func splitRequestIndexKey(key string) (string, string, bool) {
	planID, requestKey, found := strings.Cut(key, requestIndexSeparator)
	if !found || planID == "" || requestKey == "" {
		return "", "", false
	}
	if strings.Contains(requestKey, requestIndexSeparator) {
		return "", "", false
	}
	return planID, requestKey, true
}
