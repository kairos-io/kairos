package ops

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStageCurtinLanding(t *testing.T) {
	dir := t.TempDir()
	// Use a fake busybox payload.
	fakeBusybox := []byte("fake-busybox-binary")
	if err := stageCurtinLanding(dir, fakeBusybox); err != nil {
		t.Fatalf("stageCurtinLanding: %v", err)
	}

	// bin/busybox must exist with the right content
	bbPath := filepath.Join(dir, "bin", "busybox")
	got, err := os.ReadFile(bbPath)
	if err != nil {
		t.Fatalf("bin/busybox not written: %v", err)
	}
	if string(got) != string(fakeBusybox) {
		t.Fatalf("bin/busybox content mismatch")
	}

	// sh/ash are real busybox applets -> symlinks. bash is not an applet, so it
	// must be a wrapper script forwarding to busybox sh (curtin runs bash -c).
	for _, link := range []string{"sh", "ash"} {
		target, err := os.Readlink(filepath.Join(dir, "bin", link))
		if err != nil {
			t.Fatalf("bin/%s symlink missing: %v", link, err)
		}
		if target != "busybox" {
			t.Fatalf("bin/%s -> %q, want busybox", link, target)
		}
	}
	// bash must exist in /usr/bin: curtin runs `chroot target bash` resolving via
	// PATH, and the merged-usr Ubuntu ephemeral env's PATH is
	// /usr/sbin:/usr/bin:/sbin (it omits /bin), so the wrapper goes in /usr/bin.
	bashWrapper, err := os.ReadFile(filepath.Join(dir, "usr/bin", "bash"))
	if err != nil {
		t.Fatalf("usr/bin/bash wrapper missing: %v", err)
	}
	if string(bashWrapper) != "#!/bin/sh\nexec /bin/busybox sh \"$@\"\n" {
		t.Fatalf("usr/bin/bash wrapper content wrong: %q", string(bashWrapper))
	}
	if fi, _ := os.Lstat(filepath.Join(dir, "usr/bin", "bash")); fi != nil && fi.Mode()&0o111 == 0 {
		t.Fatalf("usr/bin/bash not executable")
	}
	// bash is intentionally NOT in /bin (nothing resolves it there).
	if _, err := os.Lstat(filepath.Join(dir, "bin", "bash")); err == nil {
		t.Fatalf("/bin/bash should not exist (only /usr/bin/bash is needed)")
	}

	// stub executables
	for _, s := range []string{"cloud-init", "netplan"} {
		p := filepath.Join(dir, "usr/bin", s)
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("usr/bin/%s not written: %v", s, err)
		}
		if string(b) != "#!/bin/sh\nexit 0\n" {
			t.Fatalf("usr/bin/%s content wrong: %q", s, b)
		}
	}

	// curtin/curtin-hooks must match the embedded asset
	hookPath := filepath.Join(dir, "curtin", "curtin-hooks")
	hookBytes, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("curtin/curtin-hooks not written: %v", err)
	}
	if len(hookBytes) == 0 || string(hookBytes) != string(maasCurtinHook) {
		t.Fatalf("curtin/curtin-hooks does not match embedded asset (len=%d)", len(hookBytes))
	}
	// Mountpoints (/proc /sys /dev /run /tmp) are created at image-build time by
	// CreateDirStructure (DeployImage), not by stageCurtinLanding, so they are
	// validated by the live deploy / in-chroot bind-mount check, not here.
}

// stageCurtinLanding must surface filesystem errors rather than swallowing them,
// so a partially-staged partition never silently ships. Each case seeds an
// obstacle that makes exactly one staging step fail.
func TestStageCurtinLandingErrors(t *testing.T) {
	busybox := []byte("fake-busybox-binary")

	t.Run("cannot create payload dir", func(t *testing.T) {
		dir := t.TempDir()
		// A regular file where the "bin" directory needs to be makes MkdirAll fail.
		if err := os.WriteFile(filepath.Join(dir, "bin"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := stageCurtinLanding(dir, busybox); err == nil {
			t.Fatal("expected error when payload dir cannot be created, got nil")
		}
	})

	t.Run("cannot write busybox", func(t *testing.T) {
		dir := t.TempDir()
		// A directory where the busybox file needs to be makes WriteFile fail.
		if err := os.MkdirAll(filepath.Join(dir, "bin", "busybox"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := stageCurtinLanding(dir, busybox); err == nil {
			t.Fatal("expected error when busybox cannot be written, got nil")
		}
	})

	t.Run("cannot create symlink", func(t *testing.T) {
		dir := t.TempDir()
		// A pre-existing bin/sh makes the busybox symlink fail with EEXIST.
		if err := os.MkdirAll(filepath.Join(dir, "bin"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "bin", "sh"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := stageCurtinLanding(dir, busybox); err == nil {
			t.Fatal("expected error when symlink cannot be created, got nil")
		}
	})

	t.Run("cannot write bash wrapper", func(t *testing.T) {
		dir := t.TempDir()
		// A directory where usr/bin/bash needs to be makes WriteFile fail, after
		// the busybox write and symlinks have succeeded.
		if err := os.MkdirAll(filepath.Join(dir, "usr", "bin", "bash"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := stageCurtinLanding(dir, busybox); err == nil {
			t.Fatal("expected error when bash wrapper cannot be written, got nil")
		}
	})

	t.Run("cannot write stub", func(t *testing.T) {
		dir := t.TempDir()
		// A directory where usr/bin/cloud-init needs to be makes the stub write fail.
		if err := os.MkdirAll(filepath.Join(dir, "usr", "bin", "cloud-init"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := stageCurtinLanding(dir, busybox); err == nil {
			t.Fatal("expected error when stub cannot be written, got nil")
		}
	})
}
