package auroraboot

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kairos-io/AuroraBoot/pkg/builder"
	"github.com/kairos-io/AuroraBoot/pkg/schema"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

type kairosifyTestStore struct{}

func (*kairosifyTestStore) Create(context.Context, *store.ArtifactRecord) error { return nil }
func (*kairosifyTestStore) GetByID(context.Context, string) (*store.ArtifactRecord, error) {
	return nil, errors.New("not implemented")
}
func (*kairosifyTestStore) List(context.Context) ([]*store.ArtifactRecord, error) {
	return nil, errors.New("not implemented")
}
func (*kairosifyTestStore) Update(context.Context, *store.ArtifactRecord) error { return nil }
func (*kairosifyTestStore) UpdatePhaseMessage(context.Context, string, string, string) error {
	return nil
}
func (*kairosifyTestStore) UpdateFiles(context.Context, string, []string) error           { return nil }
func (*kairosifyTestStore) UpdateNameSaved(context.Context, string, *string, *bool) error { return nil }
func (*kairosifyTestStore) ClearUploadToken(context.Context, string) error                { return nil }
func (*kairosifyTestStore) Delete(context.Context, string) error                          { return nil }
func (*kairosifyTestStore) DeleteByPhase(context.Context, string) error                   { return nil }
func (*kairosifyTestStore) GetLogs(context.Context, string) (string, error)               { return "", nil }
func (*kairosifyTestStore) AppendLog(context.Context, string, string) error               { return nil }

func installFakeDocker(t *testing.T) string {
	t.Helper()
	tmpDir := t.TempDir()
	dockerCalls := filepath.Join(tmpDir, "docker.calls")
	dockerPath := filepath.Join(tmpDir, "docker")
	dockerScript := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$DOCKER_CALLS\"\n"
	if err := os.WriteFile(dockerPath, []byte(dockerScript), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tmpDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOCKER_CALLS", dockerCalls)
	return dockerCalls
}

func TestEnsureKairosifiedDerivesPrebuiltImage(t *testing.T) {
	tmpDir := t.TempDir()
	dockerCalls := installFakeDocker(t)

	b := New(tmpDir, nil, &kairosifyTestStore{})
	got, err := b.kairosify(context.Background(), "quay.io/kairos/hadron:v0.2.0-core-amd64-generic-v4.1.0", builder.BuildOptions{
		ID:                "artifact-123",
		KubernetesDistro:  "k3s",
		KubernetesVersion: "v1.33.1+k3s1",
	}, tmpDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != "auroraboot-kairos:artifact-123" {
		t.Fatalf("expected a per-artifact image, got %q", got)
	}

	dockerfile, err := os.ReadFile(filepath.Join(tmpDir, "Dockerfile.kairosify"))
	if err != nil {
		t.Fatal(err)
	}
	contents := string(dockerfile)
	for _, want := range []string{
		"FROM quay.io/kairos/hadron:v0.2.0-core-amd64-generic-v4.1.0",
		"-p k3s",
		"--provider-k3s-version v1.33.1+k3s1",
	} {
		if !strings.Contains(contents, want) {
			t.Errorf("generated Dockerfile does not contain %q:\n%s", want, contents)
		}
	}

	calls, err := os.ReadFile(dockerCalls)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(calls), "build -t auroraboot-kairos:artifact-123") {
		t.Fatalf("expected a per-artifact docker build, got calls:\n%s", calls)
	}
}

func TestBuildUsesGeneratedIDForDerivedImage(t *testing.T) {
	tmpDir := t.TempDir()
	installFakeDocker(t)
	deployedImage := make(chan string, 1)
	b := New(tmpDir, func(_ context.Context, _ schema.Config, artifact schema.ReleaseArtifact, _ string, _ io.Writer) error {
		deployedImage <- artifact.ContainerImage
		return nil
	}, &kairosifyTestStore{})

	status, err := b.Build(context.Background(), builder.BuildOptions{
		BaseImage:        "quay.io/kairos/hadron:v0.2.0-core-amd64-generic-v4.1.0",
		KubernetesDistro: "k3s",
	})
	if err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-deployedImage:
		want := "auroraboot-kairos:" + status.ID
		if got != want {
			t.Fatalf("expected generated ID in derived image %q, got %q", want, got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for deployer")
	}
}

func TestEnsureKairosifiedKeepsCompleteDockerfileImage(t *testing.T) {
	tmpDir := t.TempDir()
	dockerCalls := installFakeDocker(t)
	b := New(tmpDir, nil, &kairosifyTestStore{})

	image := "auroraboot-build:artifact-123"
	got, err := b.ensureKairosified(context.Background(), image, builder.BuildOptions{
		ID:         "artifact-123",
		Dockerfile: "FROM quay.io/kairos/hadron:latest",
	}, tmpDir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got != image {
		t.Fatalf("expected Dockerfile-built Kairos image %q, got %q", image, got)
	}

	calls, err := os.ReadFile(dockerCalls)
	if err != nil {
		t.Fatal(err)
	}
	for _, call := range strings.Split(string(calls), "\n") {
		if strings.HasPrefix(call, "build ") {
			t.Fatalf("expected no second docker build, got calls:\n%s", calls)
		}
	}
}
