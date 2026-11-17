package deployer

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/kairos-io/AuroraBoot/pkg/schema"
	sdklogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/rs/zerolog"
	"github.com/spectrocloud-labs/herd"
)

// cloudConfigSecret is a value a real cloud-config would carry: a join token
// or a user password. It is long and distinctive so a substring match on the
// log output cannot hit it by accident.
const cloudConfigSecret = "s3cr3t-join-token-9f2ab41c7d"

const cloudConfigWithSecret = `#cloud-config
users:
- name: kairos
  passwd: ` + cloudConfigSecret + `
k3s:
  env:
    K3S_TOKEN: ` + cloudConfigSecret + `
`

// runCopyCloudConfig registers PrepDirs and StepCopyCloudConfig against a
// deployer whose log goes to a buffer at the given level, runs the graph, and
// returns the rendered log and the directory the config was written into.
func runCopyCloudConfig(t *testing.T, level zerolog.Level) (string, string) {
	t.Helper()

	dir := t.TempDir()
	var buf bytes.Buffer

	d := NewDeployer(
		schema.Config{State: dir, CloudConfig: cloudConfigWithSecret},
		schema.ReleaseArtifact{},
		herd.EnableInit,
	)
	d.Log = sdklogger.KairosLogger{Logger: zerolog.New(&buf).Level(level)}

	if err := d.PrepDirs(); err != nil {
		t.Fatalf("PrepDirs: %v", err)
	}
	if err := d.StepCopyCloudConfig(); err != nil {
		t.Fatalf("StepCopyCloudConfig: %v", err)
	}
	if err := d.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if err := d.CollectErrors(); err != nil {
		t.Fatalf("step errors: %v", err)
	}

	return buf.String(), dir
}

// TestStepCopyCloudConfigDoesNotLogTheConfigAtInfo is a regression test for
// https://github.com/kairos-io/kairos/issues/5098.
//
// StepCopyCloudConfig writes the cloud-config with mode 0600 because it holds
// user passwords, join tokens and kcrypt secrets. It used to log the whole
// document at Info one statement earlier, and --loglevel defaults to info, so
// a plain `auroraboot build-iso -c config.yaml` printed every one of those
// secrets to stdout and into whatever CI log captured it.
func TestStepCopyCloudConfigDoesNotLogTheConfigAtInfo(t *testing.T) {
	logged, dir := runCopyCloudConfig(t, zerolog.InfoLevel)

	if strings.Contains(logged, cloudConfigSecret) {
		t.Fatalf("cloud-config contents must not reach the log at info level, got:\n%s", logged)
	}

	// The step must still say it ran, so the default output does not go silent.
	if !strings.Contains(logged, "Copying cloud config") {
		t.Fatalf("expected the step to still report progress at info level, got:\n%s", logged)
	}

	// And it must still do its job.
	written, err := os.ReadFile(dir + "/config.yaml")
	if err != nil {
		t.Fatalf("reading written config: %v", err)
	}
	if string(written) != cloudConfigWithSecret {
		t.Fatalf("written config does not match the input:\n%s", written)
	}
	info, err := os.Stat(dir + "/config.yaml")
	if err != nil {
		t.Fatalf("stat written config: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0600 {
		t.Fatalf("written config must stay 0600, got %04o", perm)
	}
}

// TestStepCopyCloudConfigLogsTheConfigAtDebug keeps the document reachable for
// troubleshooting: -l debug is an explicit opt-in, and internal/config already
// logs the same value at that level.
func TestStepCopyCloudConfigLogsTheConfigAtDebug(t *testing.T) {
	logged, _ := runCopyCloudConfig(t, zerolog.DebugLevel)

	if !strings.Contains(logged, cloudConfigSecret) {
		t.Fatalf("cloud-config contents should still be available at debug level, got:\n%s", logged)
	}
}
