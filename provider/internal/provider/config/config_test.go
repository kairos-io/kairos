package config

import "testing"

func TestHardcodedRole(t *testing.T) {
	// The provider accepts "", "master" and "worker" only
	// (internal/role/p2p/k8s.go). "none" reaches us from configs written
	// against the schema that advertised it as p2p.role's default, and has
	// to mean the same thing as leaving the key out.
	for _, tc := range []struct {
		role string
		want string
	}{
		{role: "", want: ""},
		{role: RoleNone, want: ""},
		{role: "master", want: "master"},
		{role: "worker", want: "worker"},
	} {
		if got := (P2P{Role: tc.role}).HardcodedRole(); got != tc.want {
			t.Errorf("P2P{Role: %q}.HardcodedRole() = %q, want %q", tc.role, got, tc.want)
		}
	}
}
