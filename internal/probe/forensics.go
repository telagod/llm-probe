package probe

import "strings"

func normalizeForensicsLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "fast":
		return "fast"
	case "forensic":
		return "forensic"
	default:
		return "balanced"
	}
}

func forensicsDepth(cfg RunConfig) int {
	switch normalizeForensicsLevel(cfg.ForensicsLevel) {
	case "fast":
		return 1
	case "forensic":
		return 3
	default:
		return 2
	}
}

func resolveForensicsRounds(cfg RunConfig, fast, balanced, forensic int) int {
	switch normalizeForensicsLevel(cfg.ForensicsLevel) {
	case "fast":
		return clampInt(fast, 1, 16)
	case "forensic":
		return clampInt(forensic, 1, 16)
	default:
		return clampInt(balanced, 1, 16)
	}
}

func resolveConsistencyRuns(cfg RunConfig) int {
	if cfg.ConsistencyRuns > 0 {
		return clampInt(cfg.ConsistencyRuns, 1, 16)
	}
	switch normalizeForensicsLevel(cfg.ForensicsLevel) {
	case "fast":
		return 1
	case "forensic":
		return 4
	default:
		return 2
	}
}

func resolveConsistencyDriftThresholds(cfg RunConfig) (warn float64, fail float64) {
	level := normalizeForensicsLevel(cfg.ForensicsLevel)
	switch level {
	case "fast":
		warn = 28
		fail = 45
	case "forensic":
		warn = 10
		fail = 22
	default:
		warn = 18
		fail = 32
	}
	if cfg.ConsistencyDriftWarn > 0 && cfg.ConsistencyDriftWarn <= 100 {
		warn = cfg.ConsistencyDriftWarn
	}
	if cfg.ConsistencyDriftFail > 0 && cfg.ConsistencyDriftFail <= 100 {
		fail = cfg.ConsistencyDriftFail
	}
	if fail <= warn {
		fail = warn + 5
		if fail > 100 {
			fail = 100
		}
	}
	return warn, fail
}

func clampInt(value, minValue, maxValue int) int {
	if value < minValue {
		return minValue
	}
	if value > maxValue {
		return maxValue
	}
	return value
}
