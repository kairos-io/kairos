package hook

import (
	"os"
	"testing"

	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	fsutils "github.com/kairos-io/kairos/v4/agent/pkg/utils/fs"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/twpayne/go-vfs/v5"
	"github.com/twpayne/go-vfs/v5/vfst"
)

// TestInstalledConfigDirs pins the seam the coder called out: the
// precedence list installedConfigDirs returns has to be
// constants.GetUserConfigDirs reversed, with the live-media entry (gone by
// the time the installed system boots) dropped.
func TestInstalledConfigDirs(t *testing.T) {
	want := []string{constants.OEMPath, "/usr/local/cloud-config", "/etc/kairos"}
	got := installedConfigDirs()
	if len(got) != len(want) {
		t.Fatalf("installedConfigDirs() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("installedConfigDirs() = %v, want %v", got, want)
		}
	}
	for _, d := range got {
		if len(d) >= len("/run/") && d[:5] == "/run/" {
			t.Fatalf("installedConfigDirs() kept a /run entry: %v", got)
		}
	}
}

func TestCloudConfigDirFallback(t *testing.T) {
	t.Run("falls back to /usr/local/cloud-config when it is the first usable candidate", func(t *testing.T) {
		fs, cleanup, err := vfst.NewTestFS(map[string]interface{}{})
		if err != nil {
			t.Fatalf("vfst.NewTestFS: %v", err)
		}
		defer cleanup()
		if err := fsutils.MkdirAll(fs, "/usr/local", os.ModeDir|os.ModePerm); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		c := sdkConfig.Config{Fs: fs, Logger: sdkLogger.NewNullLogger()}

		dir, cleanupDir, err := cloudConfigDir(c)
		if err != nil {
			t.Fatalf("cloudConfigDir() error = %v", err)
		}
		defer cleanupDir()
		if dir != "/usr/local/cloud-config" {
			t.Fatalf("cloudConfigDir() dir = %q, want %q", dir, "/usr/local/cloud-config")
		}
		ok, err := fsutils.Exists(fs, dir)
		if err != nil || !ok {
			t.Fatalf("cloudConfigDir() did not create %q: ok=%v err=%v", dir, ok, err)
		}
	})

	t.Run("falls back to /etc/kairos when /usr/local is not there", func(t *testing.T) {
		fs, cleanup, err := vfst.NewTestFS(map[string]interface{}{})
		if err != nil {
			t.Fatalf("vfst.NewTestFS: %v", err)
		}
		defer cleanup()
		if err := fsutils.MkdirAll(fs, "/etc", os.ModeDir|os.ModePerm); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		c := sdkConfig.Config{Fs: fs, Logger: sdkLogger.NewNullLogger()}

		dir, cleanupDir, err := cloudConfigDir(c)
		if err != nil {
			t.Fatalf("cloudConfigDir() error = %v", err)
		}
		defer cleanupDir()
		if dir != "/etc/kairos" {
			t.Fatalf("cloudConfigDir() dir = %q, want %q", dir, "/etc/kairos")
		}
	})

	t.Run("never reuses a stranded /oem directory left by a failed mount", func(t *testing.T) {
		fs, cleanup, err := vfst.NewTestFS(map[string]interface{}{})
		if err != nil {
			t.Fatalf("vfst.NewTestFS: %v", err)
		}
		defer cleanup()
		// machine.Mount creates its mountpoint before it fails to mount, so
		// an empty /oem (with a mountable parent, "/") is exactly the state
		// a failed mount leaves behind on the running installer.
		if err := fsutils.MkdirAll(fs, constants.OEMPath, os.ModeDir|os.ModePerm); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := fsutils.MkdirAll(fs, "/usr/local", os.ModeDir|os.ModePerm); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		c := sdkConfig.Config{Fs: fs, Logger: sdkLogger.NewNullLogger()}

		dir, cleanupDir, err := cloudConfigDir(c)
		if err != nil {
			t.Fatalf("cloudConfigDir() error = %v", err)
		}
		defer cleanupDir()
		if dir == constants.OEMPath {
			t.Fatalf("cloudConfigDir() reused the stranded %q", constants.OEMPath)
		}
		if dir != "/usr/local/cloud-config" {
			t.Fatalf("cloudConfigDir() dir = %q, want %q", dir, "/usr/local/cloud-config")
		}
	})

	t.Run("skips a candidate it cannot create and errors when none work", func(t *testing.T) {
		fs, cleanup, err := vfst.NewTestFS(map[string]interface{}{})
		if err != nil {
			t.Fatalf("vfst.NewTestFS: %v", err)
		}
		defer cleanup()
		if err := fsutils.MkdirAll(fs, "/usr/local", os.ModeDir|os.ModePerm); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		ro := vfs.NewReadOnlyFS(fs)
		c := sdkConfig.Config{Fs: ro, Logger: sdkLogger.NewNullLogger()}

		_, _, err = cloudConfigDir(c)
		if err == nil {
			t.Fatalf("cloudConfigDir() error = nil, want an error on a read-only fs")
		}
	})

	t.Run("errors listing every directory it tried when no candidate parent exists", func(t *testing.T) {
		fs, cleanup, err := vfst.NewTestFS(map[string]interface{}{})
		if err != nil {
			t.Fatalf("vfst.NewTestFS: %v", err)
		}
		defer cleanup()
		c := sdkConfig.Config{Fs: fs, Logger: sdkLogger.NewNullLogger()}

		_, _, err = cloudConfigDir(c)
		if err == nil {
			t.Fatalf("cloudConfigDir() error = nil, want an error")
		}
	})
}
