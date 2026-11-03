package cmd

import "testing"

// The names on the right are systemd's, from the architecture table in
// src/basic/architecture.c. They are not Go's GOARCH names: systemd spells
// amd64 "x86-64" and agrees with Go on the other two.
func TestSystemdArchitecture(t *testing.T) {
	for _, tt := range []struct {
		arch string
		want string
	}{
		{"amd64", "x86-64"},
		{"arm64", "arm64"},
		{"riscv64", "riscv64"},
	} {
		got, err := systemdArchitecture(tt.arch)
		if err != nil {
			t.Fatalf("systemdArchitecture(%q) returned %v, want %q", tt.arch, err, tt.want)
		}
		if got != tt.want {
			t.Errorf("systemdArchitecture(%q) = %q, want %q", tt.arch, got, tt.want)
		}
	}
}

// An unknown value must be an error, not a silent fall back to x86-64: that
// fall back is what let riscv64 be accepted by the flag validation and written
// as x86-64 into extension-release, and systemd skips such an extension
// without failing.
func TestSystemdArchitectureRejectsUnknown(t *testing.T) {
	for _, arch := range []string{"", "x86_64", "aarch64", "386", "riscv"} {
		if got, err := systemdArchitecture(arch); err == nil {
			t.Errorf("systemdArchitecture(%q) = %q, want an error", arch, got)
		}
	}
}

// The flag validation and the extension-release writer must agree on the set
// of accepted architectures, because they used to be two separate literals and
// only one of them grew riscv64.
func TestValidatedArchIsWritable(t *testing.T) {
	for _, arch := range supportedArches {
		if _, err := systemdArchitecture(arch); err != nil {
			t.Errorf("flag validation accepts %q but systemdArchitecture rejects it: %v", arch, err)
		}
	}
}
