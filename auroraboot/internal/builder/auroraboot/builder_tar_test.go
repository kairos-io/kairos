package auroraboot_test

import (
	"context"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/internal/builder/auroraboot"
	"github.com/kairos-io/AuroraBoot/pkg/builder"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

var _ = Describe("AuroraBoot Builder container tar save", func() {
	var baseDir string

	// installFakeDocker puts a docker on PATH that runs script for every call.
	installFakeDocker := func(script string) {
		dir := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(dir, "docker"), []byte("#!/bin/sh\n"+script+"\n"), 0o755)).To(Succeed())
		GinkgoT().Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	}

	tarBuild := func(s *recStore, deploy auroraboot.DeployerFunc) *auroraboot.Builder {
		return auroraboot.New(baseDir, deploy, s).WithKairosifyFunc(noopKairosify)
	}

	BeforeEach(func() {
		baseDir = GinkgoT().TempDir()
	})

	It("fails the build when docker save fails", func() {
		installFakeDocker("exit 1")
		s := newRecStore()
		b := tarBuild(s, noopDeploy)

		_, err := b.Build(context.Background(), builder.BuildOptions{
			ID:        "tar-fails",
			BaseImage: "quay.io/kairos/hadron:latest",
			Outputs:   builder.OutputOptions{Tar: true},
		})
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() string {
			r, err := s.GetByID(context.Background(), "tar-fails")
			if err != nil {
				return ""
			}
			return r.Phase
		}, 10*time.Second).Should(Equal(store.ArtifactError))

		rec, err := s.GetByID(context.Background(), "tar-fails")
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Message).To(ContainSubstring("TAR"))

		st, err := b.Status(context.Background(), "tar-fails")
		Expect(err).NotTo(HaveOccurred())
		Expect(st.Phase).To(Equal(builder.BuildError))
		Expect(st.Artifacts).To(BeEmpty())
	})

	It("keeps a build cancelled during docker save in the Error state", func() {
		started := filepath.Join(GinkgoT().TempDir(), "started")
		// Write a partial tar like a killed docker save would, then hang.
		installFakeDocker(`out=""; while [ $# -gt 0 ]; do [ "$1" = "-o" ] && out="$2"; shift; done
echo partial > "$out"; touch ` + started + `; exec sleep 30`)
		s := newRecStore()
		b := tarBuild(s, noopDeploy)

		_, err := b.Build(context.Background(), builder.BuildOptions{
			ID:        "tar-cancel",
			BaseImage: "quay.io/kairos/hadron:latest",
			Outputs:   builder.OutputOptions{Tar: true},
		})
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() error { _, err := os.Stat(started); return err }, 10*time.Second).Should(Succeed())
		Expect(b.Cancel(context.Background(), "tar-cancel")).To(Succeed())

		// Give the build goroutine time to see the killed save and finish.
		Consistently(func() string {
			st, err := b.Status(context.Background(), "tar-cancel")
			if err != nil {
				return ""
			}
			return st.Phase
		}, 2*time.Second, 100*time.Millisecond).Should(Equal(builder.BuildError))

		st, err := b.Status(context.Background(), "tar-cancel")
		Expect(err).NotTo(HaveOccurred())
		Expect(st.Message).To(Equal("cancelled"))
		Expect(st.Artifacts).To(BeEmpty())

		rec, err := s.GetByID(context.Background(), "tar-cancel")
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Phase).To(Equal(store.ArtifactError))
		Expect(rec.Message).To(Equal("cancelled"))
	})
})
