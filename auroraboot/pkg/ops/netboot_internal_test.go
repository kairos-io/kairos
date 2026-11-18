package ops

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/disk"
	"github.com/diskfs/go-diskfs/filesystem"
	"github.com/diskfs/go-diskfs/filesystem/iso9660"
	"github.com/kairos-io/AuroraBoot/internal"
	"github.com/kairos-io/AuroraBoot/pkg/schema"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
)

const testLivecdGrubCfg = `menuentry "Kairos" --class os --unrestricted {
    linux ($root)/boot/kernel cdroot root=live:CDLABEL=COS_LIVE rd.live.dir=/ net.ifnames=1 install-mode selinux=0 rd.live.overlay.overlayfs
    initrd ($root)/boot/initrd
}`

// ExtractNetboot must pull the livecd grub config out of the ISO next to the
// three boot artifacts, so StartPixiecore can netboot with the cmdline the ISO
// itself boots with (kairos-io/kairos#2573).
func TestExtractNetbootTakesTheGrubConfig(t *testing.T) {
	internal.Log = logger.NewKairosLogger("test", "fatal", false)

	dst := t.TempDir()
	isoFile := buildISO(t, map[string]string{
		"/rootfs.squashfs":     "squashfs",
		"/boot/kernel":         "kernel",
		"/boot/initrd":         "initrd",
		"/boot/grub2/grub.cfg": testLivecdGrubCfg,
	})

	if err := ExtractNetboot(func() string { return isoFile }, func() string { return dst }, "kairos")(context.Background()); err != nil {
		t.Fatalf("ExtractNetboot: %v", err)
	}

	got, err := os.ReadFile(filepath.Join(dst, "kairos-grub.cfg"))
	if err != nil {
		t.Fatalf("reading the extracted grub config: %v", err)
	}
	if !strings.Contains(string(got), "selinux=0") {
		t.Errorf("extracted grub config = %q, want the livecd one", got)
	}
}

// An ISO without a livecd grub config still netboots, it just does so with the
// default cmdline, so the extraction must not fail and must leave no stub
// behind for StartPixiecore to read.
func TestExtractNetbootWithoutAGrubConfig(t *testing.T) {
	internal.Log = logger.NewKairosLogger("test", "fatal", false)

	dst := t.TempDir()
	isoFile := buildISO(t, map[string]string{
		"/rootfs.squashfs": "squashfs",
		"/boot/kernel":     "kernel",
		"/boot/initrd":     "initrd",
	})

	if err := ExtractNetboot(func() string { return isoFile }, func() string { return dst }, "kairos")(context.Background()); err != nil {
		t.Fatalf("ExtractNetboot: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dst, "kairos-grub.cfg")); !os.IsNotExist(err) {
		t.Errorf("stat of the grub config = %v, want it absent", err)
	}
}

// withLiveCmdline is what StartPixiecore calls once the extraction is done.
func TestWithLiveCmdline(t *testing.T) {
	internal.Log = logger.NewKairosLogger("test", "fatal", false)

	const base = `rd.cos.disable root=live:{{ ID "%s" }} config_url={{ ID "%s" }}`

	grubCfgFile := filepath.Join(t.TempDir(), "kairos-grub.cfg")
	if err := os.WriteFile(grubCfgFile, []byte(testLivecdGrubCfg), 0o644); err != nil {
		t.Fatal(err)
	}

	got := withLiveCmdline(base, func() string { return grubCfgFile })
	if !strings.Contains(got, "selinux=0") || !strings.Contains(got, "net.ifnames=1") {
		t.Errorf("cmdline = %q, want the livecd options merged in", got)
	}
	if strings.Contains(got, "CDLABEL") || strings.Contains(got, "install-mode") {
		t.Errorf("cmdline = %q, want the medium and mode options dropped", got)
	}

	// A missing or unnamed grub config leaves the cmdline alone rather than
	// failing the netboot.
	for name, get := range map[string]valueGetOnCall{
		"nil":     nil,
		"unset":   func() string { return "" },
		"missing": func() string { return filepath.Join(t.TempDir(), "absent.cfg") },
	} {
		if got := withLiveCmdline(base, get); got != base {
			t.Errorf("%s grub config: cmdline = %q, want it untouched", name, got)
		}
	}
}

// buildISO writes an iso9660 image holding the given paths, so the extraction
// runs against a real ISO instead of a mock. xorriso is not needed: go-diskfs
// is already the library the extraction itself reads with.
func buildISO(t *testing.T, files map[string]string) string {
	t.Helper()

	isoFile := filepath.Join(t.TempDir(), "test.iso")
	d, err := diskfs.Create(isoFile, 4*1024*1024, 2048)
	if err != nil {
		t.Fatalf("creating the iso: %v", err)
	}
	defer d.Close()

	fs, err := d.CreateFilesystem(disk.FilesystemSpec{Partition: 0, FSType: filesystem.TypeISO9660, WorkDir: t.TempDir()})
	if err != nil {
		t.Fatalf("creating the iso filesystem: %v", err)
	}

	for path, content := range files {
		if dir := filepath.Dir(path); dir != "/" {
			if err := fs.Mkdir(dir); err != nil {
				t.Fatalf("mkdir %s: %v", dir, err)
			}
		}
		f, err := fs.OpenFile(path, os.O_CREATE|os.O_RDWR)
		if err != nil {
			t.Fatalf("creating %s: %v", path, err)
		}
		if _, err := f.Write([]byte(content)); err != nil {
			t.Fatalf("writing %s: %v", path, err)
		}
	}

	iso, ok := fs.(*iso9660.FileSystem)
	if !ok {
		t.Fatalf("filesystem is %T, want an iso9660 one", fs)
	}
	if err := iso.Finalize(iso9660.FinalizeOptions{RockRidge: true, VolumeIdentifier: "COS_LIVE"}); err != nil {
		t.Fatalf("finalizing the iso: %v", err)
	}

	return isoFile
}

// A kernel option carrying a % must reach the netbooted node as it was
// written. The cmdline is assembled with fmt.Sprintf, so merging text into it
// before formatting turns every % in that text into a format verb and Go
// replaces it with %!(NOVERB).
func TestNetbootCmdlineKeepsAPercentSign(t *testing.T) {
	internal.Log = logger.NewKairosLogger("test", "fatal", false)

	grubCfg := `menuentry "Kairos" --class os --unrestricted {
    linux ($root)/boot/kernel selinux=0 foo=50% bar=%s
    initrd ($root)/boot/initrd
}`
	grubCfgFile := filepath.Join(t.TempDir(), "kairos-grub.cfg")
	if err := os.WriteFile(grubCfgFile, []byte(grubCfg), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := netbootCmdline("/tmp/kairos.squashfs", "/tmp/config.yaml", func() string { return grubCfgFile }, schema.NetBoot{})
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(got, "%!") {
		t.Errorf("cmdline = %q, want no format-verb errors in it", got)
	}
	for _, want := range []string{"foo=50%", "bar=%s", `root=live:{{ ID "/tmp/kairos.squashfs" }}`, `config_url={{ ID "/tmp/config.yaml" }}`} {
		if !strings.Contains(got, want) {
			t.Errorf("cmdline = %q, want it to contain %q", got, want)
		}
	}
}

// Same for an explicit --netboot-cmdline override, which is appended to the
// same template.
func TestNetbootCmdlineOverrideKeepsAPercentSign(t *testing.T) {
	internal.Log = logger.NewKairosLogger("test", "fatal", false)

	got, err := netbootCmdline("/tmp/kairos.squashfs", "/tmp/config.yaml", nil, schema.NetBoot{Cmdline: "foo=50% bar=%s"})
	if err != nil {
		t.Fatal(err)
	}

	if strings.Contains(got, "%!") {
		t.Errorf("cmdline = %q, want no format-verb errors in it", got)
	}
	for _, want := range []string{"foo=50%", "bar=%s", `root=live:{{ ID "/tmp/kairos.squashfs" }}`, `config_url={{ ID "/tmp/config.yaml" }}`} {
		if !strings.Contains(got, want) {
			t.Errorf("cmdline = %q, want it to contain %q", got, want)
		}
	}
}
