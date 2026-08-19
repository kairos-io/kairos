package kernel

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kairos-io/kairos-init/pkg/values"
	"github.com/kairos-io/kairos-sdk/types/logger"
)

func newTestLogger() logger.KairosLogger {
	return logger.NewKairosLogger("test", "info", false)
}

func mkdirs(t *testing.T, base string, names ...string) {
	t.Helper()
	for _, n := range names {
		if err := os.Mkdir(filepath.Join(base, n), 0755); err != nil {
			t.Fatalf("mkdir %s: %v", n, err)
		}
	}
}

func TestGetLatestFromPath(t *testing.T) {
	log := newTestLogger()

	tests := []struct {
		name        string
		model       string
		dirs        []string
		wantKernel  string
		wantErr     bool
		errContains string
	}{
		{
			name:        "no kernel directories → error",
			model:       values.Generic.String(),
			dirs:        []string{},
			wantErr:     true,
			errContains: "no kernel versions found",
		},
		{
			name:       "single semver kernel",
			model:      values.Generic.String(),
			dirs:       []string{"5.15.0-101-generic"},
			wantKernel: "5.15.0-101-generic",
		},
		{
			name:       "multiple semver kernels → highest selected",
			model:      values.Generic.String(),
			dirs:       []string{"5.15.0-100-generic", "5.15.0-102-generic", "5.15.0-101-generic"},
			wantKernel: "5.15.0-102-generic",
		},
		{
			name:       "non-semver kernel name → returned as-is (first entry fallback)",
			model:      values.Generic.String(),
			dirs:       []string{"5.4.0-101-generic.fc32.x86_64"},
			wantKernel: "5.4.0-101-generic.fc32.x86_64",
		},
		{
			name:       "multiple non-semver kernels → first directory entry returned",
			model:      values.Generic.String(),
			dirs:       []string{"alpha-kernel", "beta-kernel"},
			wantKernel: "alpha-kernel",
		},
		{
			name:       "rpi4: single raspi semver kernel",
			model:      values.Rpi4.String(),
			dirs:       []string{"5.15.0-1025-raspi"},
			wantKernel: "5.15.0-1025-raspi",
		},
		{
			name:       "rpi4: multiple raspi semver kernels → highest selected",
			model:      values.Rpi4.String(),
			dirs:       []string{"5.15.0-1023-raspi", "5.15.0-1025-raspi", "5.15.0-1024-raspi"},
			wantKernel: "5.15.0-1025-raspi",
		},
		{
			name:       "rpi4: raspi preferred over generic even when generic is higher",
			model:      values.Rpi4.String(),
			dirs:       []string{"6.8.0-51-generic", "5.15.0-1025-raspi"},
			wantKernel: "5.15.0-1025-raspi",
		},
		{
			name:       "rpi4: raspi non-semver name → lexicographic last returned",
			model:      values.Rpi4.String(),
			dirs:       []string{"custom-b-raspi", "custom-a-raspi"},
			wantKernel: "custom-b-raspi",
		},
		{
			name:       "rpi4: no raspi dir → falls through to generic semver selection",
			model:      values.Rpi4.String(),
			dirs:       []string{"5.15.0-101-generic", "5.15.0-102-generic"},
			wantKernel: "5.15.0-102-generic",
		},
		{
			name:       "rpi3: raspi kernel preferred",
			model:      values.Rpi3.String(),
			dirs:       []string{"5.15.0-1025-raspi", "6.8.0-50-generic"},
			wantKernel: "5.15.0-1025-raspi",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := t.TempDir()
			mkdirs(t, base, tc.dirs...)

			got, err := GetLatestFromPath(base, tc.model, log)

			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil (kernel=%q)", tc.errContains, got)
				}
				if tc.errContains != "" && !strings.Contains(err.Error(), tc.errContains) {
					t.Errorf("error %q does not contain %q", err.Error(), tc.errContains)
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantKernel != "" && got != tc.wantKernel {
				t.Errorf("got kernel %q, want %q", got, tc.wantKernel)
			}
		})
	}
}
