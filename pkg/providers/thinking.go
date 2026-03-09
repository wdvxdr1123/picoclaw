package providers

import "strings"

const (
	ThinkingOff    = "off"
	ThinkingLow    = "low"
	ThinkingMedium = "medium"
	ThinkingHigh   = "high"
	ThinkingXHigh  = "xhigh"
)

func NormalizeThinkingLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case ThinkingLow:
		return ThinkingLow
	case ThinkingMedium:
		return ThinkingMedium
	case ThinkingHigh:
		return ThinkingHigh
	case ThinkingXHigh:
		return ThinkingXHigh
	default:
		return ""
	}
}
