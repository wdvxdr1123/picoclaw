package providers

import (
	"strings"

	"github.com/sipeed/picoclaw/pkg/auth"
)

const defaultAnthropicAPIBase = "https://api.anthropic.com/v1"

var getCredential = auth.GetCredential

type WireFormat string

const (
	WireFormatOpenAI    WireFormat = "openai"
	WireFormatAnthropic WireFormat = "anthropic"
)

// ResolveWireFormat collapses provider-specific protocols into the two wire
// formats we actually implement: OpenAI-compatible and Anthropic-compatible.
func ResolveWireFormat(protocol string) WireFormat {
	switch strings.ToLower(strings.TrimSpace(protocol)) {
	case "anthropic", "claude":
		return WireFormatAnthropic
	default:
		return WireFormatOpenAI
	}
}
