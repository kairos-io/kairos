package ops

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kairos-io/AuroraBoot/internal"
	"github.com/kairos-io/AuroraBoot/pkg/extensions"
	"github.com/kairos-io/kairos/v4/agent/pkg/config"
	sdkConstants "github.com/kairos-io/kairos/v4/sdk/constants"
	"github.com/mudler/yip/pkg/schema"
	"gopkg.in/yaml.v3"
)

func newTestRawImage(extensionFiles []string) *RawImage {
	return &RawImage{
		config:         config.NewConfig(config.WithLogger(internal.Log)),
		ExtensionFiles: extensionFiles,
	}
}

func writeExtension(t *testing.T, dir, name string, size int) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, make([]byte, size), 0o644); err != nil {
		t.Fatalf("writing %s: %v", path, err)
	}
	return path
}

func TestStageBundledExtensionsCopiesImagesAndWritesTheCloudConfig(t *testing.T) {
	source := t.TempDir()
	oem := t.TempDir()
	fwupd := writeExtension(t, source, "fwupd.sysext.raw", 3*1024*1024)
	k3s := writeExtension(t, source, "k3s.sysext.raw", 5*1024*1024)

	staged, err := newTestRawImage([]string{fwupd, k3s}).stageBundledExtensions(oem)
	if err != nil {
		t.Fatalf("stageBundledExtensions: %v", err)
	}
	if want := int64(8 * 1024 * 1024); staged != want {
		t.Fatalf("staged = %d bytes, want %d", staged, want)
	}

	for _, name := range []string{"fwupd.sysext.raw", "k3s.sysext.raw"} {
		if _, err := os.Stat(filepath.Join(oem, BundledExtensionsDir, name)); err != nil {
			t.Fatalf("%s was not staged into the OEM partition: %v", name, err)
		}
	}
	if _, err := os.Stat(filepath.Join(oem, BundledExtensionsCloudConfig)); err != nil {
		t.Fatalf("%s was not written: %v", BundledExtensionsCloudConfig, err)
	}
}

// A build with no extensions must leave the OEM partition exactly as it was:
// an empty extensions directory would make the installer stage run for
// nothing, and an unconditional cloud config would mount the persistent
// partition on every reset of every disk image we ship.
func TestStageBundledExtensionsWritesNothingWithoutExtensions(t *testing.T) {
	oem := t.TempDir()

	staged, err := newTestRawImage(nil).stageBundledExtensions(oem)
	if err != nil {
		t.Fatalf("stageBundledExtensions: %v", err)
	}
	if staged != 0 {
		t.Fatalf("staged = %d bytes, want 0", staged)
	}

	entries, err := os.ReadDir(oem)
	if err != nil {
		t.Fatalf("reading the OEM staging dir: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("the OEM staging dir has %d entries, want none", len(entries))
	}
}

func TestStageBundledExtensionsRejectsSomethingThatIsNotARawImage(t *testing.T) {
	source := t.TempDir()
	notAnImage := writeExtension(t, source, "fwupd.sysext.tar", 1024)

	if _, err := newTestRawImage([]string{notAnImage}).stageBundledExtensions(t.TempDir()); err == nil {
		t.Fatal("stageBundledExtensions accepted a file that is not a .raw image")
	}
}

func TestOEMPartitionSizeGrowsWithTheStagedExtensions(t *testing.T) {
	const mib = 1024 * 1024
	tests := []struct {
		name  string
		bytes int64
		want  uint
	}{
		{name: "no extensions keeps the default", bytes: 0, want: sdkConstants.OEMSize},
		{name: "exact megabytes", bytes: 8 * mib, want: sdkConstants.OEMSize + 8},
		{name: "a partial megabyte rounds up", bytes: 8*mib + 1, want: sdkConstants.OEMSize + 9},
		{name: "a single byte still needs a megabyte", bytes: 1, want: sdkConstants.OEMSize + 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := oemPartitionSize(tt.bytes); got != tt.want {
				t.Fatalf("oemPartitionSize(%d) = %d, want %d", tt.bytes, got, tt.want)
			}
		})
	}
}

// The cloud config has to be a yip config carrying an after-reset stage, or the
// agent reads it, finds nothing to run and installs no extension at all.
func TestBundledExtensionsStageIsAnAfterResetYipStage(t *testing.T) {
	var config schema.YipConfig
	if err := yaml.Unmarshal([]byte(BundledExtensionsStage()), &config); err != nil {
		t.Fatalf("the bundled extensions cloud config is not a yip config: %v", err)
	}

	stages, ok := config.Stages["after-reset"]
	if !ok || len(stages) == 0 {
		t.Fatalf("no after-reset stage in %v", config.Stages)
	}
	if len(stages[0].Commands) == 0 {
		t.Fatal("the after-reset stage runs no commands")
	}
	// after-reset-chroot binds the persistent partition at /usr/local, but a
	// reset that formatted it leaves it unmounted, so the writes would land on
	// the recovery rootfs and be discarded.
	if _, found := config.Stages["after-reset-chroot"]; found {
		t.Fatal("the extensions are installed from after-reset-chroot, where the persistent partition may not be mounted")
	}
}

// Executing the stage is the only way to know the shell it carries produces the
// layout immucore reads. mount and umount are replaced with shims so the body
// runs unprivileged, and the shim reports which label was asked for.
func TestBundledExtensionsStageShellProducesTheLayoutImmucoreReads(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no shell available")
	}

	root := t.TempDir()
	persistent := filepath.Join(root, "persistent")
	oemExtensions := filepath.Join(root, "oem", BundledExtensionsDir)
	if err := os.MkdirAll(oemExtensions, 0o755); err != nil {
		t.Fatalf("creating the fake OEM extensions dir: %v", err)
	}
	writeExtension(t, oemExtensions, "fwupd.sysext.raw", 16)

	// `mount -L <label> <dir>` becomes a bind of the fake persistent partition:
	// the shim records the label and points the mountpoint at it.
	shims := filepath.Join(root, "bin")
	if err := os.MkdirAll(shims, 0o755); err != nil {
		t.Fatalf("creating the shim dir: %v", err)
	}
	mountShim := "#!/bin/sh\necho \"$2\" >> " + filepath.Join(root, "labels") +
		"\nrm -rf \"$3\"\nln -s " + persistent + " \"$3\"\n"
	if err := os.WriteFile(filepath.Join(shims, "mount"), []byte(mountShim), 0o755); err != nil {
		t.Fatalf("writing the mount shim: %v", err)
	}
	for _, name := range []string{"umount", "sync"} {
		if err := os.WriteFile(filepath.Join(shims, name), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
			t.Fatalf("writing the %s shim: %v", name, err)
		}
	}
	// rmdir must not delete the symlink the mount shim left behind.
	if err := os.WriteFile(filepath.Join(shims, "rmdir"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatalf("writing the rmdir shim: %v", err)
	}
	if err := os.MkdirAll(persistent, 0o755); err != nil {
		t.Fatalf("creating the fake persistent partition: %v", err)
	}

	var parsed schema.YipConfig
	if err := yaml.Unmarshal([]byte(BundledExtensionsStage()), &parsed); err != nil {
		t.Fatalf("parsing the cloud config: %v", err)
	}
	stages := parsed.Stages["after-reset"]
	if len(stages) == 0 || len(stages[0].Commands) == 0 {
		t.Fatalf("no after-reset commands to run in %v", parsed.Stages)
	}
	// The stage addresses /oem by absolute path, which a test cannot write to.
	script := strings.ReplaceAll(stages[0].Commands[0], "/oem/", filepath.Join(root, "oem")+"/")

	command := exec.Command("sh", "-c", script)
	command.Env = append(os.Environ(), "PATH="+shims+string(os.PathListSeparator)+os.Getenv("PATH"))
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("running the stage: %v\n%s", err, output)
	}

	labels, err := os.ReadFile(filepath.Join(root, "labels"))
	if err != nil {
		t.Fatalf("the stage never mounted anything: %v", err)
	}
	if got := strings.TrimSpace(string(labels)); got != sdkConstants.PersistentLabel {
		t.Fatalf("the stage mounted %q, want %q", got, sdkConstants.PersistentLabel)
	}

	extensions := filepath.Join(persistent, ".state", "var-lib-kairos.bind", "extensions")
	if _, err := os.Stat(filepath.Join(extensions, "fwupd.sysext.raw")); err != nil {
		t.Fatalf("the extension image is not in the persistent extension store: %v", err)
	}
	// immucore populates /run/extensions from <dir>/<boot state> only, and the
	// link has to be relative because the directory is written through
	// /usr/local and read back at /var/lib/kairos/extensions.
	for _, bootState := range []string{"active", "passive"} {
		link := filepath.Join(extensions, bootState, "fwupd.sysext.raw")
		target, err := os.Readlink(link)
		if err != nil {
			t.Fatalf("%s is not enabled for %s: %v", "fwupd.sysext.raw", bootState, err)
		}
		if target != filepath.Join("..", "fwupd.sysext.raw") {
			t.Fatalf("%s links to %q, want a relative link to the image", link, target)
		}
		if _, err := os.Stat(link); err != nil {
			t.Fatalf("%s does not resolve: %v", link, err)
		}
	}
	// recovery boots the recovery image, which carries no extension store, so
	// linking it there would be a link nothing ever follows.
	if _, err := os.Stat(filepath.Join(extensions, "recovery")); !os.IsNotExist(err) {
		t.Fatalf("the stage enabled the extension for recovery: %v", err)
	}
}

func TestWithArchFromLeavesAConfiguredArchitectureAlone(t *testing.T) {
	spec := DiskExtensions{
		Requests:     mustParseRequests(t, "fwupd"),
		Architecture: "arm64",
	}
	if got := spec.withArchFrom(t.TempDir()).Architecture; got != "arm64" {
		t.Fatalf("withArchFrom overrode the configured architecture with %q", got)
	}
}

// An unreadable rootfs must not leave the architecture empty: Materialize
// rejects that, so the build would fail with a message about the architecture
// rather than resolving anything.
func TestWithArchFromFallsBackToTheHostArchitecture(t *testing.T) {
	spec := DiskExtensions{Requests: mustParseRequests(t, "fwupd")}
	if got := spec.withArchFrom(filepath.Join(t.TempDir(), "missing")).Architecture; got == "" {
		t.Fatal("withArchFrom left the architecture empty")
	}
}

func mustParseRequests(t *testing.T, names ...string) []extensions.Request {
	t.Helper()
	requests, err := extensions.ParseRequests(names)
	if err != nil {
		t.Fatalf("parsing %v: %v", names, err)
	}
	return requests
}

// The OEM partition is the only place a raw-disk build can put an extension,
// so the staging has to be wired into the partition the build actually writes,
// not just be callable. This exercises createOemPartitionImage's staging half,
// which is everything up to the privileged deploy.
func TestStageOemContentsBundlesTheExtensionsNextToTheResetConfig(t *testing.T) {
	work := t.TempDir()
	oem := filepath.Join(work, "oem")
	if err := os.MkdirAll(oem, 0o755); err != nil {
		t.Fatalf("creating the OEM staging dir: %v", err)
	}
	cloudConfig := filepath.Join(work, "config.yaml")
	if err := os.WriteFile(cloudConfig, []byte("#cloud-config\n"), 0o644); err != nil {
		t.Fatalf("writing the cloud config: %v", err)
	}
	recovery := filepath.Join(work, "recovery.img")
	if err := os.WriteFile(recovery, make([]byte, 1024), 0o644); err != nil {
		t.Fatalf("writing the fake recovery image: %v", err)
	}
	extension := writeExtension(t, work, "fwupd.sysext.raw", 2*1024*1024)

	image := newTestRawImage([]string{extension})
	image.CloudConfig = cloudConfig

	staged, err := image.stageOemContents(oem, recovery)
	if err != nil {
		t.Fatalf("stageOemContents: %v", err)
	}
	if want := int64(2 * 1024 * 1024); staged != want {
		t.Fatalf("staged = %d bytes, want %d", staged, want)
	}
	if got := oemPartitionSize(staged); got <= sdkConstants.OEMSize {
		t.Fatalf("the OEM partition was sized at %d MiB, which does not hold the staged extension", got)
	}

	// The reset config has to still be there: it is what creates the
	// persistent partition the extensions are installed onto.
	if _, err := os.Stat(filepath.Join(oem, "01_reset.yaml")); err != nil {
		t.Fatalf("the reset cloud config is missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(oem, BundledExtensionsCloudConfig)); err != nil {
		t.Fatalf("the extensions cloud config is missing: %v", err)
	}
	// 01_reset.yaml removes itself after the reset, so the extensions config
	// must sort after it or it would run before the persistent partition is
	// there to write to.
	if BundledExtensionsCloudConfig <= "01_reset.yaml" {
		t.Fatalf("%s does not sort after 01_reset.yaml", BundledExtensionsCloudConfig)
	}
	if _, err := os.Stat(filepath.Join(oem, BundledExtensionsDir, "fwupd.sysext.raw")); err != nil {
		t.Fatalf("the extension image is not in the OEM partition: %v", err)
	}
}

// A build that asked for no extensions must reach neither the registry nor the
// filesystem. materializeDiskExtensions is the only caller of the pull, so if
// its guard were dropped every existing raw-disk build would start creating a
// staging directory and contacting a registry for an empty request list. The
// returned cleanup has to be callable either way, because the caller defers it
// before it knows whether anything was staged.
func TestMaterializeDiskExtensionsWithoutRequestsTouchesNothing(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)

	images, cleanup, err := materializeDiskExtensions(context.Background(), DiskExtensions{})
	if err != nil {
		t.Fatalf("materializeDiskExtensions: %v", err)
	}
	if images != nil {
		t.Fatalf("images = %v, want nil", images)
	}
	if cleanup == nil {
		t.Fatal("cleanup is nil, so the caller's deferred call would panic")
	}

	// Read the directory before running cleanup, otherwise cleanup removes
	// the very staging directory whose absence is the point of the check.
	entries, err := os.ReadDir(tmp)
	if err != nil {
		t.Fatalf("reading the temporary directory: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("a staging directory was created for an empty request list: %v", entries)
	}

	cleanup()
}

// A raw-disk build that names a local image must stage it without reading a
// catalog. That is what lets the e2e suite bake tests/assets/sysext into a raw
// artifact: the fixture lives in the repo, and there is no published catalog
// entry for it.
func TestMaterializeDiskExtensionsStagesALocalImage(t *testing.T) {
	source := writeExtension(t, t.TempDir(), "work.sysext.raw", 2048)

	images, cleanup, err := materializeDiskExtensions(context.Background(), DiskExtensions{
		Requests: mustParseRequests(t, extensions.FileScheme+source),
	})
	if err != nil {
		t.Fatalf("materializeDiskExtensions: %v", err)
	}
	defer cleanup()

	if len(images) != 1 || filepath.Base(images[0]) != "work.sysext.raw" {
		t.Fatalf("images = %v, want one work.sysext.raw", images)
	}
	info, err := os.Stat(images[0])
	if err != nil {
		t.Fatalf("the staged image is not on disk: %v", err)
	}
	if info.Size() != 2048 {
		t.Fatalf("staged image is %d bytes, want 2048", info.Size())
	}
	// The staged copy is what the OEM partition takes, so the name it carries
	// is the name the installed system merges.
	if _, err := newTestRawImage(images).stageBundledExtensions(t.TempDir()); err != nil {
		t.Fatalf("stageBundledExtensions rejected the staged local image: %v", err)
	}

	cleanup()
	if _, err := os.Stat(images[0]); !os.IsNotExist(err) {
		t.Fatalf("cleanup left the staging directory behind: %v", err)
	}
}
