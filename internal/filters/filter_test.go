package filters

import (
	"testing"
)

func TestEvaluateName(t *testing.T) {
	tests := []struct {
		name       string
		avoid      []string
		wantValid  bool
		wantReason string
	}{
		{"Velora", nil, true, ""},
		{"V", nil, false, "Too short"},
		{"SuperLongNameThatIsInvalid", nil, false, "Too long"},
		{"Vel-trix", nil, false, "Contains non-alphabetic characters"},
		{"Veltrix", []string{"vel"}, false, "Matches avoided keyword: vel"},
		{"bzpqrt", nil, false, "Excessive consecutive consonants"},
		{"queeei", nil, false, "Excessive consecutive vowels"},
		{"abbby", nil, false, "Character repeated 3+ times"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := EvaluateName(tt.name, tt.avoid)
			if res.IsValid != tt.wantValid {
				t.Errorf("EvaluateName(%q) isValid = %v, want %v (reason: %q)", tt.name, res.IsValid, tt.wantValid, res.Reason)
			}
		})
	}
}
