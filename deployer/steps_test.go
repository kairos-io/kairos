package deployer

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/kairos-io/AuroraBoot/pkg/constants"
	"github.com/kairos-io/AuroraBoot/pkg/schema"
	"github.com/spectrocloud-labs/herd"
)

// isUnder reports whether path is the dir itself or lives inside it, comparing
// on path-segment boundaries so that "/tmp-rootfs" is not considered under
// "/tmp".
func isUnder(path, dir string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator))
}

// kairos#5776: GenISO names its output after the image, never kairos.iso, so
// getIsoFile fell back to the first *.iso in the state dir and netboot
// extraction and cloud-config injection read an older ISO left there.
func TestRunIsoFilePrefersTheGeneratedISO(t *testing.T) {
	state := t.TempDir()
	old := filepath.Join(state, "kairos-quay.io-kairos-ubuntu-v4.0.0.iso")
	fresh := filepath.Join(state, "kairos-quay.io-kairos-ubuntu-v4.1.0.iso")
	for _, f := range []string{old, fresh} {
		if err := os.WriteFile(f, []byte(f), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	d := &Deployer{Config: schema.Config{State: state}}
	if got := d.runIsoFile(); got != old {
		t.Fatalf("precondition: with no generated ISO runIsoFile() = %s, want the scan result %s", got, old)
	}

	d.generatedISO = fresh
	if got := d.runIsoFile(); got != fresh {
		t.Errorf("runIsoFile() = %s, want the ISO this run generated %s", got, fresh)
	}
}

// TestTmpRootFsNotUnderStateDir is a regression test for
// https://github.com/kairos-io/kairos/issues/3922.
//
// state_dir is commonly a host bind mount. On Docker Desktop for macOS that
// mount is backed by VirtioFS, which cannot represent all the Linux metadata
// containerd's tar-apply sets while unpacking a rootfs, so unpacking there
// fails with "failed to Lchown ... permission denied". The intermediate rootfs
// must therefore stay on the local filesystem (os.TempDir()), never under
// state_dir.
func TestTmpRootFsNotUnderStateDir(t *testing.T) {
	stateDir := "/output" // a bind-mounted state_dir, as in the bug report
	d := &Deployer{Config: schema.Config{State: stateDir}}

	tmp := d.tmpRootFs()

	if isUnder(tmp, stateDir) {
		t.Fatalf("tmpRootFs() must not live under state_dir %q, got %q", stateDir, tmp)
	}
	if !isUnder(tmp, os.TempDir()) {
		t.Fatalf("tmpRootFs() must live under the local temp dir %q, got %q", os.TempDir(), tmp)
	}
}

// TestTmpRootFsStable ensures the path is deterministic across calls, since it
// is evaluated both eagerly at step registration and lazily inside callbacks.
func TestTmpRootFsStable(t *testing.T) {
	d := &Deployer{Config: schema.Config{State: "/output"}}

	if a, b := d.tmpRootFs(), d.tmpRootFs(); a != b {
		t.Fatalf("tmpRootFs() must be stable across calls, got %q then %q", a, b)
	}
}

// TestTmpRootFsNormalizesStateDir ensures superficial differences in state_dir
// (a trailing slash, an unclean path) map to the same unpack directory, so the
// same logical build always resolves to one temp rootfs.
func TestTmpRootFsNormalizesStateDir(t *testing.T) {
	a := &Deployer{Config: schema.Config{State: "/output"}}
	b := &Deployer{Config: schema.Config{State: "/output/"}}
	c := &Deployer{Config: schema.Config{State: "/output/../output"}}

	if a.tmpRootFs() != b.tmpRootFs() || a.tmpRootFs() != c.tmpRootFs() {
		t.Fatalf("equivalent state_dirs must map to the same temp rootfs: %q, %q, %q",
			a.tmpRootFs(), b.tmpRootFs(), c.tmpRootFs())
	}
}

// TestTmpRootFsUniquePerStateDir guards the concurrency invariant: the internal
// builder runs multiple builds in parallel in the same process, each with a
// distinct state_dir. Their unpack directories must not collide, otherwise one
// build's PrepDirs RemoveAll would wipe another build's rootfs mid-unpack.
func TestTmpRootFsUniquePerStateDir(t *testing.T) {
	a := &Deployer{Config: schema.Config{State: "/builds/aaaa"}}
	b := &Deployer{Config: schema.Config{State: "/builds/bbbb"}}

	if a.tmpRootFs() == b.tmpRootFs() {
		t.Fatalf("distinct state_dirs must map to distinct temp rootfs dirs, both got %q", a.tmpRootFs())
	}
}

// newWorkDirDeployer builds a deployer with PrepDirs, StepCopyCloudConfig and
// one extra step that runs after the cloud config was written.
func newWorkDirDeployer(state, workDir, cloudConfig string, extra func() error) *Deployer {
	d := NewDeployer(schema.Config{State: state, CloudConfig: cloudConfig}, schema.ReleaseArtifact{}, herd.EnableInit)
	d.WorkDir = workDir
	_ = d.PrepDirs()
	_ = d.StepCopyCloudConfig()
	_ = d.Add("probe", herd.WithDeps(constants.OpCopyCloudConfig), herd.WithCallback(func(ctx context.Context) error {
		return extra()
	}))
	return d
}

// TestWorkDirKeepsHelperPathsOutOfState checks that with a WorkDir set, the
// cloud config, the rootfs and the netboot dir all live in the WorkDir and
// none of them in the state dir.
func TestWorkDirKeepsHelperPathsOutOfState(t *testing.T) {
	state := t.TempDir()
	work := t.TempDir()
	d := &Deployer{Config: schema.Config{State: state}, WorkDir: work}

	for name, p := range map[string]string{
		"tmpRootFs":       d.tmpRootFs(),
		"cloudConfigPath": d.cloudConfigPath(),
		"dstNetboot":      d.dstNetboot(),
	} {
		if !isUnder(p, work) {
			t.Errorf("%s() must live under WorkDir %q, got %q", name, work, p)
		}
		if isUnder(p, state) {
			t.Errorf("%s() must not live under state dir %q, got %q", name, state, p)
		}
	}
}

// TestWorkDirLeavesUserConfigInState checks that a run with a WorkDir neither
// overwrites nor deletes a config.yaml that already sits in the state dir.
func TestWorkDirLeavesUserConfigInState(t *testing.T) {
	state := t.TempDir()
	work := filepath.Join(t.TempDir(), "work")
	user := []byte("hostname: users-own\n")
	if err := os.WriteFile(filepath.Join(state, "config.yaml"), user, 0600); err != nil {
		t.Fatal(err)
	}

	d := newWorkDirDeployer(state, work, "hostname: from-flag\n", func() error { return nil })
	if err := d.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := d.CleanTmpDirs(); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(state)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	if !reflect.DeepEqual(names, []string{"config.yaml"}) {
		t.Fatalf("state dir must list exactly [config.yaml], got %v", names)
	}
	got, err := os.ReadFile(filepath.Join(state, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(user) {
		t.Fatalf("user config.yaml was changed: %q", got)
	}
	if _, err := os.Stat(work); !os.IsNotExist(err) {
		t.Fatalf("WorkDir must be removed after CleanTmpDirs, stat err: %v", err)
	}
}

// TestWorkDirConcurrentBuildsKeepOwnConfig checks that two builds that share a
// state dir but have their own WorkDir do not see each other's cloud config or
// share a rootfs.
func TestWorkDirConcurrentBuildsKeepOwnConfig(t *testing.T) {
	state := t.TempDir()
	base := t.TempDir()

	var barrier sync.WaitGroup
	barrier.Add(2)
	seen := make([]string, 2)
	deployers := make([]*Deployer, 2)
	for i := range deployers {
		i := i
		deployers[i] = newWorkDirDeployer(state, filepath.Join(base, fmt.Sprintf("w%d", i)), fmt.Sprintf("hostname: build-%d\n", i), func() error {
			barrier.Done()
			barrier.Wait()
			b, err := os.ReadFile(deployers[i].cloudConfigPath())
			seen[i] = string(b)
			return err
		})
	}

	var wg sync.WaitGroup
	for _, d := range deployers {
		wg.Add(1)
		go func(d *Deployer) {
			defer wg.Done()
			_ = d.Run(context.Background())
		}(d)
	}
	wg.Wait()

	for i, d := range deployers {
		if err := d.CollectErrors(); err != nil {
			t.Fatalf("build %d: %v", i, err)
		}
		if want := fmt.Sprintf("hostname: build-%d\n", i); seen[i] != want {
			t.Errorf("build %d read %q, want %q", i, seen[i], want)
		}
	}
	if deployers[0].tmpRootFs() == deployers[1].tmpRootFs() {
		t.Fatalf("builds with distinct WorkDirs must not share a rootfs, both got %q", deployers[0].tmpRootFs())
	}
}

// TestWorkDirRemovedOnStepFailure checks that a failing step is reported and
// that CleanTmpDirs still removes the WorkDir.
func TestWorkDirRemovedOnStepFailure(t *testing.T) {
	state := t.TempDir()
	work := filepath.Join(t.TempDir(), "work")

	d := newWorkDirDeployer(state, work, "hostname: x\n", func() error { return errors.New("boom") })
	_ = d.Run(context.Background())
	if d.CollectErrors() == nil {
		t.Fatal("CollectErrors() must report the failing step")
	}
	if err := d.CleanTmpDirs(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(work); !os.IsNotExist(err) {
		t.Fatalf("WorkDir must be removed after a failed step, stat err: %v", err)
	}
}
