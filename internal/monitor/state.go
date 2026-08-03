package monitor

// StateRank returns the monotonic severity weight used by _setState fold
// (no_state=0 < ok=1 < unknown=2 < info=3 < warning=4 < critical=5 < error=6).
// Unknown labels rank as unknown (2).
func StateRank(state string) int {
	switch state {
	case "no_state":
		return 0
	case "ok":
		return 1
	case "unknown":
		return 2
	case "info":
		return 3
	case "warning":
		return 4
	case "critical":
		return 5
	case "error":
		return 6
	default:
		return 2 // unknown
	}
}

// NormalizeState maps an empty or unknown label to a canonical enum value.
func NormalizeState(state string) string {
	switch state {
	case "no_state", "ok", "unknown", "info", "warning", "critical", "error":
		return state
	case "":
		return "no_state"
	default:
		return "unknown"
	}
}

// SetState promotes current to candidate only when candidate is more severe
// (port of monitor_tools::_setState). Equal or lower severity leaves current
// unchanged. Empty current is treated as no_state; unknown labels normalize
// to "unknown".
func SetState(current, candidate string) string {
	current = NormalizeState(current)
	if candidate == "" {
		return current
	}
	candidate = NormalizeState(candidate)
	if StateRank(candidate) > StateRank(current) {
		return candidate
	}
	return current
}

// FoldStates reduces a list of states with SetState promote-only semantics.
// Empty input yields "no_state".
func FoldStates(states ...string) string {
	out := "no_state"
	for _, s := range states {
		out = SetState(out, s)
	}
	return out
}
