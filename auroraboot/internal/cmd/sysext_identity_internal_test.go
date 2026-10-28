package cmd

import (
	"strings"
	"testing"
)

func TestCheckPackingIdentity(t *testing.T) {
	tests := []struct {
		name      string
		buildType string
		euid      int
		wantErr   bool
		wantIn    []string
	}{
		{name: "root packs a sysext", buildType: "sysext", euid: 0},
		{name: "root packs a confext", buildType: "confext", euid: 0},
		{
			name:      "a non-root sysext build is refused",
			buildType: "sysext",
			euid:      1001,
			wantErr:   true,
			// The refusal also fires for a host user who never passed --user and
			// for a web UI build under a Helm securityContext.runAsUser, so it has
			// to name every way to get root, not just the container one.
			wantIn: []string{"sysext", "1001", "6.1.11", "6.1.12", "--user", "runAsUser", "sudo"},
		},
		{
			name:      "a non-root confext build is refused",
			buildType: "confext",
			euid:      1000,
			wantErr:   true,
			wantIn:    []string{"confext", "1000"},
		},
		{
			name:    "an unnamed build still names what it is",
			euid:    1001,
			wantErr: true,
			wantIn:  []string{"extension"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkPackingIdentity(tt.buildType, tt.euid)
			if !tt.wantErr {
				if err != nil {
					t.Fatalf("checkPackingIdentity(%q, %d) = %v, want nil", tt.buildType, tt.euid, err)
				}
				return
			}
			if err == nil {
				t.Fatalf("checkPackingIdentity(%q, %d) = nil, want an error", tt.buildType, tt.euid)
			}
			for _, want := range tt.wantIn {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error %q does not mention %q", err, want)
				}
			}
		})
	}
}
