package validation

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidateBootFileSymlink(t *testing.T) {
	t.Run("relative target in same directory", func(t *testing.T) {
		boot := t.TempDir()
		kernel := filepath.Join(boot, "vmlinux-6.12.95+deb13-riscv64")
		if err := os.WriteFile(kernel, []byte("kernel"), 0o644); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(boot, "vmlinuz")
		if err := os.Symlink("vmlinux-6.12.95+deb13-riscv64", link); err != nil {
			t.Fatal(err)
		}

		if err := validateBootFileSymlink(link); err != nil {
			t.Fatalf("expected relative symlink to validate, got: %v", err)
		}
	})

	t.Run("broken relative target", func(t *testing.T) {
		boot := t.TempDir()
		link := filepath.Join(boot, "vmlinuz")
		if err := os.Symlink("missing-kernel", link); err != nil {
			t.Fatal(err)
		}

		if err := validateBootFileSymlink(link); err == nil {
			t.Fatal("expected error for broken symlink")
		}
	})

	t.Run("absolute target", func(t *testing.T) {
		boot := t.TempDir()
		kernel := filepath.Join(boot, "vmlinuz-6.1.0")
		if err := os.WriteFile(kernel, []byte("kernel"), 0o644); err != nil {
			t.Fatal(err)
		}
		link := filepath.Join(boot, "vmlinuz")
		if err := os.Symlink(kernel, link); err != nil {
			t.Fatal(err)
		}

		if err := validateBootFileSymlink(link); err != nil {
			t.Fatalf("expected absolute symlink to validate, got: %v", err)
		}
	})
}
