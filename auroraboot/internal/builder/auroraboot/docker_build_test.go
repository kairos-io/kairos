package auroraboot

import (
	"context"
	"os"
	"path/filepath"

	"github.com/kairos-io/AuroraBoot/pkg/builder"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Docker build platform", func() {
	DescribeTable("selects platform arguments",
		func(arch string, want []string) {
			opts := builder.BuildOptions{}
			opts.Source.Arch = arch
			Expect(dockerBuildPlatformArgs(opts)).To(Equal(want))
		},
		Entry("target architecture", "amd64", []string{"--platform", "linux/amd64"}),
		Entry("unspecified architecture", "", nil),
	)

	DescribeTable("targets the requested platform",
		func(run func(*Builder, builder.BuildOptions, string) error) {
			baseDir := GinkgoT().TempDir()
			callsPath := filepath.Join(baseDir, "docker.calls")
			dockerPath := filepath.Join(baseDir, "docker")
			script := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"" + callsPath + "\"\n" +
				"if [ \"$1\" = inspect ] || [ \"$1\" = run ]; then exit 1; fi\n"
			Expect(os.WriteFile(dockerPath, []byte(script), 0o755)).To(Succeed())
			GinkgoT().Setenv("PATH", baseDir+string(os.PathListSeparator)+os.Getenv("PATH"))

			b := New(baseDir, nil, noopArtifactStore{})
			opts := builder.BuildOptions{ID: "cross-arch"}
			opts.Source.Arch = "amd64"
			Expect(run(b, opts, baseDir)).To(Succeed())

			calls, err := os.ReadFile(callsPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(calls)).To(And(
				ContainSubstring("build"),
				ContainSubstring("--platform linux/amd64"),
			))
		},
		Entry("custom Dockerfile",
			func(b *Builder, opts builder.BuildOptions, outputDir string) error {
				opts.Dockerfile = "FROM scratch\n"
				_, err := b.dockerBuild(context.Background(), opts, outputDir, nil)
				return err
			}),
		Entry("kairosification",
			func(b *Builder, opts builder.BuildOptions, outputDir string) error {
				_, err := b.ensureKairosified(context.Background(), "example.invalid/base:latest", opts, outputDir, nil)
				return err
			}),
	)
})

type noopArtifactStore struct{}

func (noopArtifactStore) Create(context.Context, *store.ArtifactRecord) error { return nil }
func (noopArtifactStore) GetByID(context.Context, string) (*store.ArtifactRecord, error) {
	return nil, nil
}
func (noopArtifactStore) List(context.Context) ([]*store.ArtifactRecord, error) { return nil, nil }
func (noopArtifactStore) Update(context.Context, *store.ArtifactRecord) error   { return nil }
func (noopArtifactStore) UpdatePhaseMessage(context.Context, string, string, string) error {
	return nil
}
func (noopArtifactStore) UpdateFiles(context.Context, string, []string) error           { return nil }
func (noopArtifactStore) UpdateNameSaved(context.Context, string, *string, *bool) error { return nil }
func (noopArtifactStore) ClearUploadToken(context.Context, string) error                { return nil }
func (noopArtifactStore) Delete(context.Context, string) error                          { return nil }
func (noopArtifactStore) DeleteByPhase(context.Context, string) error                   { return nil }
func (noopArtifactStore) GetLogs(context.Context, string) (string, error)               { return "", nil }
func (noopArtifactStore) AppendLog(context.Context, string, string) error               { return nil }
