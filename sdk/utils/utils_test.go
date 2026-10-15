package utils

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetEfiGrubFiles(t *testing.T) {
	tests := []struct {
		arch     string
		expected []string
	}{
		{
			arch:     "amd64",
			expected: []string{"/usr/share/efi/x86_64/grub.efi"},
		},
		{
			arch:     "arm64",
			expected: []string{"/usr/share/efi/aarch64/grub.efi"},
		},
		{
			arch: "riscv64",
			expected: []string{
				"/usr/share/efi/riscv64/grub.efi",
				"/usr/lib/grub/riscv64-efi/grubriscv64.efi",
				"/usr/lib/grub/riscv64-efi/monolithic/grubriscv64.efi",
				"/boot/efi/EFI/debian/grubriscv64.efi",
				"/boot/efi/EFI/ubuntu/grubriscv64.efi",
				"/boot/efi/EFI/fedora/grubriscv64.efi",
				"/boot/efi/EFI/BOOT/BOOTRISCV64.EFI",
			},
		},
	}

	for _, tt := range tests {
		files := GetEfiGrubFiles(tt.arch)
		if len(files) == 0 {
			t.Fatalf("GetEfiGrubFiles(%q) returned no files", tt.arch)
		}

		for _, expected := range tt.expected {
			found := false
			for _, file := range files {
				if file == expected {
					found = true
					break
				}
			}
			if !found {
				t.Fatalf("GetEfiGrubFiles(%q) did not include %q; got %v", tt.arch, expected, files)
			}
		}
	}
}

func TestGetEfiLiveGrubFiles(t *testing.T) {
	tests := []struct {
		arch         string
		fallbackArch string
		cdGrub       string
		diskGrub     string
	}{
		{
			arch:     "amd64",
			cdGrub:   "/usr/lib/grub/x86_64-efi-signed/gcdx64.efi.signed",
			diskGrub: "/usr/lib/grub/x86_64-efi-signed/grubx64.efi.signed",
		},
		{
			arch:     "x86_64",
			cdGrub:   "/usr/lib/grub/x86_64-efi-signed/gcdx64.efi.signed",
			diskGrub: "/usr/lib/grub/x86_64-efi-signed/grubx64.efi.signed",
		},
		{
			arch:     "arm64",
			cdGrub:   "/usr/lib/grub/arm64-efi-signed/gcdaa64.efi.signed",
			diskGrub: "/usr/lib/grub/arm64-efi-signed/grubaa64.efi.signed",
		},
		{
			arch:         "aarch64",
			fallbackArch: "arm64",
			cdGrub:       "/usr/lib/grub/arm64-efi-signed/gcdaa64.efi.signed",
			diskGrub:     "/usr/lib/grub/arm64-efi-signed/grubaa64.efi.signed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.arch, func(t *testing.T) {
			files := GetEfiLiveGrubFiles(tt.arch)
			if files[0] != tt.cdGrub {
				t.Fatalf("GetEfiLiveGrubFiles(%q)[0] = %q, want %q", tt.arch, files[0], tt.cdGrub)
			}
			if indexOfString(files, tt.diskGrub) <= indexOfString(files, tt.cdGrub) {
				t.Fatalf("GetEfiLiveGrubFiles(%q) = %v, want CD grub before disk grub", tt.arch, files)
			}
			fallbackArch := tt.fallbackArch
			if fallbackArch == "" {
				fallbackArch = tt.arch
			}
			for _, path := range GetEfiGrubFiles(fallbackArch) {
				if indexOfString(files, path) == -1 {
					t.Fatalf("GetEfiLiveGrubFiles(%q) omitted fallback %q; got %v", tt.arch, path, files)
				}
			}
		})
	}

	riscvFiles := GetEfiLiveGrubFiles("riscv64")
	wantRiscvFiles := GetEfiGrubFiles("riscv64")
	if len(riscvFiles) != len(wantRiscvFiles) {
		t.Fatalf("GetEfiLiveGrubFiles(\"riscv64\") = %v, want %v", riscvFiles, wantRiscvFiles)
	}
	for i := range wantRiscvFiles {
		if riscvFiles[i] != wantRiscvFiles[i] {
			t.Fatalf("GetEfiLiveGrubFiles(\"riscv64\") = %v, want %v", riscvFiles, wantRiscvFiles)
		}
	}
}

func indexOfString(items []string, target string) int {
	for i, item := range items {
		if item == target {
			return i
		}
	}
	return -1
}

func TestGetEfiShimFiles(t *testing.T) {
	arm64Files := GetEfiShimFiles("arm64")
	if len(arm64Files) == 0 {
		t.Fatal("GetEfiShimFiles(\"arm64\") returned no files")
	}

	riscv64Files := GetEfiShimFiles("riscv64")
	if len(riscv64Files) != 0 {
		t.Fatalf("GetEfiShimFiles(\"riscv64\") = %v, want no shim paths", riscv64Files)
	}
}

func TestPoweroffCommand(t *testing.T) {
	tests := []struct {
		name     string
		openRC   bool
		expected string
	}{
		{
			name:     "systemd based uses shutdown now to avoid the default one minute delay",
			openRC:   false,
			expected: "shutdown now",
		},
		{
			name:     "openRC based uses poweroff",
			openRC:   true,
			expected: "poweroff",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := poweroffCommand(tt.openRC); got != tt.expected {
				t.Fatalf("poweroffCommand(%v) = %q, want %q", tt.openRC, got, tt.expected)
			}
		})
	}
}

func TestShellSTDINRunsTheCommand(t *testing.T) {
	dir := t.TempDir()
	marker := filepath.Join(dir, "marker")

	out, err := ShellSTDIN("hello from stdin\n", "cat > "+marker+"; echo done")
	if err != nil {
		t.Fatalf("ShellSTDIN returned %v, want nil", err)
	}
	if out != "done\n" {
		t.Errorf("combined output is %q, want %q", out, "done\n")
	}

	// The command has to have run, and it has to have read the string it was
	// given. Setting cmd.Stdout made CombinedOutput refuse to start, so both
	// of these used to be false while the error said "Stdout already set".
	b, err := os.ReadFile(marker)
	if err != nil {
		t.Fatalf("the command did not run: %v", err)
	}
	if string(b) != "hello from stdin\n" {
		t.Errorf("the command read %q from stdin, want %q", string(b), "hello from stdin\n")
	}
}

func TestShellSTDINReportsTheCommandsOwnFailure(t *testing.T) {
	out, err := ShellSTDIN("", "echo on stdout; echo on stderr >&2; exit 7")
	if err == nil {
		t.Fatal("ShellSTDIN returned nil, want the command's exit status")
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("ShellSTDIN returned %v, want an *exec.ExitError", err)
	}
	if exitErr.ExitCode() != 7 {
		t.Errorf("exit code is %d, want 7", exitErr.ExitCode())
	}
	// Both streams come back, which is what CombinedOutput is for.
	if !strings.Contains(out, "on stdout") || !strings.Contains(out, "on stderr") {
		t.Errorf("combined output is %q, want both streams", out)
	}
}
