package kernel

import (
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/values"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
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
			name:       "non-semver kernel name → returned as-is",
			model:      values.Generic.String(),
			dirs:       []string{"5.4.0-101-generic.fc32.x86_64"},
			wantKernel: "5.4.0-101-generic.fc32.x86_64",
		},
		{
			// This case used to expect "alpha-kernel", the first directory
			// entry. That was the defect in kairos-io/kairos#5213: the
			// fallback returned whatever the directory listing happened to
			// put first.
			name:       "multiple non-semver kernels → greatest in natural order",
			model:      values.Generic.String(),
			dirs:       []string{"alpha-kernel", "beta-kernel"},
			wantKernel: "beta-kernel",
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
			name:       "rpi4: raspi non-semver name → greatest in natural order",
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

// These three cases describe what the fallback is supposed to do when no
// directory name parses as a version. Every Red Hat family x86_64 kernel is in
// that situation, because go-version rejects a name containing an underscore,
// so this is the only path there. See kairos-io/kairos#5213.
func TestGetLatestFromPathFallbackOrder(t *testing.T) {
	log := newTestLogger()

	t.Run("picks the newest of two RHEL kernels", func(t *testing.T) {
		base := t.TempDir()
		mkdirs(t, base, "5.14.0-427.el9.x86_64", "5.14.0-503.el9.x86_64")

		got, err := GetLatestFromPath(base, values.Generic.String(), log)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "5.14.0-503.el9.x86_64" {
			t.Errorf("got kernel %q, want %q", got, "5.14.0-503.el9.x86_64")
		}
	})

	// Plain string order fails this one in the other direction: it puts
	// "5.14.0-70" after "5.14.0-427", so the digit runs have to be compared as
	// numbers.
	t.Run("compares the release as a number, not as text", func(t *testing.T) {
		base := t.TempDir()
		mkdirs(t, base, "5.14.0-70.el9.x86_64", "5.14.0-427.el9.x86_64")

		got, err := GetLatestFromPath(base, values.Generic.String(), log)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "5.14.0-427.el9.x86_64" {
			t.Errorf("got kernel %q, want %q", got, "5.14.0-427.el9.x86_64")
		}
	})

	// The doc comment on GetLatestFromPath says an error is returned when no
	// directory exists. A plain file is not a kernel.
	t.Run("a file is not a kernel directory", func(t *testing.T) {
		base := t.TempDir()
		if err := os.WriteFile(filepath.Join(base, "modules.dep"), []byte("x"), 0644); err != nil {
			t.Fatalf("write: %v", err)
		}

		got, err := GetLatestFromPath(base, values.Generic.String(), log)
		if err == nil {
			t.Fatalf("expected an error, got kernel %q", got)
		}
		if !strings.Contains(err.Error(), "no kernel versions found") {
			t.Errorf("error %q does not contain %q", err.Error(), "no kernel versions found")
		}
	})
}

// rhelReleaseOrder is a set of real Red Hat family kernel directory names in
// release order. None of them parses as a version, so GetLatestFromPath has
// only the fallback to order them with.
var rhelReleaseOrder = []string{
	"5.14.0-70.el9.x86_64",
	"5.14.0-284.11.1.el9_2.x86_64",
	"5.14.0-427.el9.x86_64",
	"5.14.0-503.el9.x86_64",
	"5.14.0-503.35.1.el9_5.x86_64",
	"6.12.0-55.9.1.el10_0.x86_64",
}

func TestGetLatestFromPathPicksTheNewestRHELKernel(t *testing.T) {
	log := newTestLogger()

	// Every adjacent pair, so a regression names the two kernels it confused
	// rather than just the end of the list.
	for i := 0; i < len(rhelReleaseOrder)-1; i++ {
		older, newer := rhelReleaseOrder[i], rhelReleaseOrder[i+1]
		t.Run(older+" < "+newer, func(t *testing.T) {
			base := t.TempDir()
			mkdirs(t, base, older, newer)

			got, err := GetLatestFromPath(base, values.Generic.String(), log)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != newer {
				t.Errorf("got kernel %q, want %q", got, newer)
			}
		})
	}

	t.Run("all of them at once", func(t *testing.T) {
		base := t.TempDir()
		mkdirs(t, base, rhelReleaseOrder...)

		got, err := GetLatestFromPath(base, values.Generic.String(), log)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := rhelReleaseOrder[len(rhelReleaseOrder)-1]
		if got != want {
			t.Errorf("got kernel %q, want %q", got, want)
		}
	})
}

// The answer must not depend on the order the names arrive in, which is what
// the old fallback got wrong: it returned whatever os.ReadDir listed first.
func TestGreatestNaturalIsIndependentOfInputOrder(t *testing.T) {
	want := rhelReleaseOrder[len(rhelReleaseOrder)-1]
	shuffled := append([]string(nil), rhelReleaseOrder...)
	r := rand.New(rand.NewSource(1))

	for n := 0; n < 200; n++ {
		r.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })
		if got := greatestNatural(shuffled); got != want {
			t.Fatalf("greatestNatural(%v) = %q, want %q", shuffled, got, want)
		}
	}
}

// greatestNatural only gives a stable answer if naturalLess is a strict weak
// ordering. Names that are all separator and padding are the ones that break
// a hand-written comparator.
func TestNaturalLessIsAStrictWeakOrdering(t *testing.T) {
	names := append([]string(nil), rhelReleaseOrder...)
	names = append(names, "", "a", "a0", "a00", "a1", "a01", "a1b", "a1b2",
		"1", "01", "9", "10", "6.8.0-51-generic", "6.8.0-1018-raspi")

	for _, a := range names {
		if naturalLess(a, a) {
			t.Errorf("%q sorts before itself", a)
		}
		for _, b := range names {
			if naturalLess(a, b) && naturalLess(b, a) {
				t.Errorf("%q and %q each sort before the other", a, b)
			}
		}
	}
}
