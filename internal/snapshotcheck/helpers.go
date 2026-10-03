package snapshotcheck

import (
	"math"
	"net"
	"sort"
	"strings"
	"unicode/utf8"

	"solo-0014-zone-rollout-control/internal/domain"
)

// The validators below deliberately mirror the write-side rules in
// internal/domain instead of calling them, so the checker stays an
// independent verifier of what the services persist.

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// isCanonicalName mirrors domain canonical name validation: the value is
// already lowercased and trimmed, each label is 1-63 bytes of lowercase
// letters, digits, hyphens, or underscores, and labels cannot start or
// end with a hyphen.
func isCanonicalName(value string) bool {
	if value == "" || len(value) > domain.MaxZoneRunes {
		return false
	}
	for _, label := range strings.Split(value, ".") {
		if len(label) < 1 || len(label) > 63 {
			return false
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
				continue
			}
			return false
		}
	}
	return true
}

// isRequestKey mirrors domain request key validation.
func isRequestKey(value string) bool {
	count := utf8.RuneCountInString(value)
	if count < domain.MinRequestKeyRunes || count > domain.MaxRequestKeyRunes {
		return false
	}
	for _, r := range value {
		if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			continue
		}
		if r == '-' || r == '_' {
			continue
		}
		return false
	}
	return true
}

// isFingerprint reports whether value looks like a SHA-256 hex digest,
// which is what the services store for request and payload fingerprints.
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

// isEntryValue mirrors the per-type zone entry value rules.
func isEntryValue(entryType domain.EntryType, value string) bool {
	switch entryType {
	case domain.EntryA:
		parsed := net.ParseIP(value)
		return parsed != nil && parsed.To4() != nil
	case domain.EntryAAAA:
		parsed := net.ParseIP(value)
		return parsed != nil && parsed.To4() == nil
	case domain.EntryCNAME:
		return isCanonicalName(value)
	}
	return false
}

// entryLess mirrors the stable entry ordering applied at write time:
// owner, then type, then value, all ascending.
func entryLess(left domain.ZoneEntry, right domain.ZoneEntry) bool {
	if left.Owner != right.Owner {
		return left.Owner < right.Owner
	}
	if left.Type != right.Type {
		return left.Type < right.Type
	}
	return left.Value < right.Value
}

// round6 mirrors the assessment success ratio rounding.
func round6(value float64) float64 {
	return math.Round(value*1e6) / 1e6
}

var knownReasons = map[string]bool{
	"no_probes":                   true,
	"node_not_observed":           true,
	"serial_mismatch":             true,
	"success_ratio_below_minimum": true,
	"latency_limit_exceeded":      true,
}

// auditTargets maps audit entity types to the collection they reference.
var auditTargets = map[string]string{
	"plan":        "plans",
	"revision":    "revisions",
	"node":        "nodes",
	"lease":       "leases",
	"lease_group": "lease_groups",
	"probe":       "probes",
	"assessment":  "assessments",
}
