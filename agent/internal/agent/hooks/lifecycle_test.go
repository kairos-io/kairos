package hook

import "testing"

func TestGracePeriodMessage(t *testing.T) {
	tests := []struct {
		name     string
		action   string
		expected string
	}{
		{
			name:     "power off message announces the grace period and how to cancel",
			action:   "Powering off node",
			expected: "Powering off node in 5s, press Ctrl+C to cancel",
		},
		{
			name:     "reboot message announces the grace period and how to cancel",
			action:   "Rebooting node",
			expected: "Rebooting node in 5s, press Ctrl+C to cancel",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := gracePeriodMessage(tt.action); got != tt.expected {
				t.Fatalf("gracePeriodMessage(%q) = %q, want %q", tt.action, got, tt.expected)
			}
		})
	}
}
