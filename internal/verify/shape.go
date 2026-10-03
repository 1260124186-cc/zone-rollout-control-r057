package verify

// checkShape verifies snapshot-level invariants that every later phase
// relies on: the schema version and the identity between map keys and the
// entity ids stored under them.
func (c *checker) checkShape() {
	if c.state.SchemaVersion != supportedSchemaVersion {
		c.add("snapshot", "", "schema_version", "unsupported_schema_version",
			"schema version does not match the supported version",
			map[string]any{
				"schema_version": c.state.SchemaVersion,
				"supported":      supportedSchemaVersion,
			})
	}
	for _, id := range sortedKeys(c.state.Plans) {
		if c.tick() {
			return
		}
		if entityID := c.state.Plans[id].ID; entityID != id {
			c.add("plan", id, "id", "id_key_mismatch",
				"map key does not match the stored plan id",
				map[string]any{"entity_id": entityID})
		}
	}
	for _, id := range sortedKeys(c.state.Revisions) {
		if c.tick() {
			return
		}
		if entityID := c.state.Revisions[id].ID; entityID != id {
			c.add("revision", id, "id", "id_key_mismatch",
				"map key does not match the stored revision id",
				map[string]any{"entity_id": entityID})
		}
	}
	for _, id := range sortedKeys(c.state.Nodes) {
		if c.tick() {
			return
		}
		if entityID := c.state.Nodes[id].ID; entityID != id {
			c.add("node", id, "id", "id_key_mismatch",
				"map key does not match the stored node id",
				map[string]any{"entity_id": entityID})
		}
	}
	for _, id := range sortedKeys(c.state.Leases) {
		if c.tick() {
			return
		}
		if entityID := c.state.Leases[id].ID; entityID != id {
			c.add("lease", id, "id", "id_key_mismatch",
				"map key does not match the stored lease id",
				map[string]any{"entity_id": entityID})
		}
	}
	for _, id := range sortedKeys(c.state.LeaseGroups) {
		if c.tick() {
			return
		}
		if entityID := c.state.LeaseGroups[id].ID; entityID != id {
			c.add("lease_group", id, "id", "id_key_mismatch",
				"map key does not match the stored lease group id",
				map[string]any{"entity_id": entityID})
		}
	}
	for _, id := range sortedKeys(c.state.Probes) {
		if c.tick() {
			return
		}
		if entityID := c.state.Probes[id].ID; entityID != id {
			c.add("probe", id, "id", "id_key_mismatch",
				"map key does not match the stored probe id",
				map[string]any{"entity_id": entityID})
		}
	}
	for _, id := range sortedKeys(c.state.Assessments) {
		if c.tick() {
			return
		}
		if entityID := c.state.Assessments[id].ID; entityID != id {
			c.add("assessment", id, "id", "id_key_mismatch",
				"map key does not match the stored assessment id",
				map[string]any{"entity_id": entityID})
		}
	}
	seen := make(map[string]struct{}, len(c.state.AuditEvents))
	for index, event := range c.state.AuditEvents {
		if c.tick() {
			return
		}
		if event.ID == "" {
			continue
		}
		if _, ok := seen[event.ID]; ok {
			c.add("audit_event", event.ID, "id", "duplicate_audit_id",
				"audit event id is duplicated",
				map[string]any{"index": index})
		}
		seen[event.ID] = struct{}{}
	}
}
