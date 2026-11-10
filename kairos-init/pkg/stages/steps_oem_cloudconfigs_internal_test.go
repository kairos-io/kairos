package stages

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
)

func oemLog() logger.KairosLogger {
	return logger.NewKairosLogger("test", "fatal", true)
}

func TestWriteOemCloudConfigsWritesEveryEmbeddedFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "system", "oem")

	if err := writeOemCloudConfigs(oemLog(), dir); err != nil {
		t.Fatalf("writeOemCloudConfigs: %v", err)
	}

	entries, err := bundled.EmbeddedConfigs.ReadDir("cloudconfigs")
	if err != nil {
		t.Fatalf("read embedded cloudconfigs: %v", err)
	}
	written := 0
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		want, err := bundled.EmbeddedConfigs.ReadFile(filepath.Join("cloudconfigs", e.Name()))
		if err != nil {
			t.Fatalf("read embedded %s: %v", e.Name(), err)
		}
		got, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("%s was not written: %v", e.Name(), err)
		}
		if string(got) != string(want) {
			t.Errorf("%s content differs from the embedded file", e.Name())
		}
		written++
	}
	if written == 0 {
		t.Fatal("no embedded cloud configs found")
	}
}

func TestWriteOemCloudConfigsFailsWhenDestinationIsAFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "oem")
	if err := os.WriteFile(dir, []byte("not a dir"), 0644); err != nil {
		t.Fatal(err)
	}

	err := writeOemCloudConfigs(oemLog(), dir)
	if err == nil {
		t.Fatal("expected an error when the destination is a regular file")
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("error %q does not name the destination %s", err, dir)
	}
}

func TestWriteOemCloudConfigsFailsWhenDestinationIsReadOnly(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	dir := filepath.Join(t.TempDir(), "oem")
	if err := os.Mkdir(dir, 0555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0755) })

	err := writeOemCloudConfigs(oemLog(), dir)
	if err == nil {
		t.Fatal("expected an error when the destination is not writable")
	}
	if !strings.Contains(err.Error(), dir) {
		t.Errorf("error %q does not name the destination %s", err, dir)
	}
}
