package providers

import "testing"

func TestNormalizeThinkingLevel(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"off", "off", ""},
		{"empty", "", ""},
		{"low", "low", ThinkingLow},
		{"medium", "medium", ThinkingMedium},
		{"high", "high", ThinkingHigh},
		{"xhigh", "xhigh", ThinkingXHigh},
		{"unknown", "unknown", ""},
		{"upper_Medium", "Medium", ThinkingMedium},
		{"upper_HIGH", "HIGH", ThinkingHigh},
		{"leading_space", " high", ThinkingHigh},
		{"trailing_space", "low ", ThinkingLow},
		{"both_spaces", " medium ", ThinkingMedium},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NormalizeThinkingLevel(tt.input); got != tt.want {
				t.Errorf("NormalizeThinkingLevel(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
