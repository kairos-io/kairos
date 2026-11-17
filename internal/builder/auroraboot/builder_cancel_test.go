package auroraboot_test

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/internal/builder/auroraboot"
	"github.com/kairos-io/AuroraBoot/pkg/builder"
	"github.com/kairos-io/AuroraBoot/pkg/schema"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/kairos-io/AuroraBoot/pkg/uki"
)

// A step that fails because its context was cancelled must not replace the
// "cancelled" message Cancel recorded with its own step failure.
var _ = Describe("AuroraBoot Builder cancel during a step", func() {
	var (
		s       *recStore
		started chan struct{}
	)

	// installFakeDocker puts a docker on PATH that runs script for every call.
	installFakeDocker := func(script string) {
		dir := GinkgoT().TempDir()
		Expect(os.WriteFile(filepath.Join(dir, "docker"), []byte("#!/bin/sh\n"+script+"\n"), 0o755)).To(Succeed())
		GinkgoT().Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	}

	// blockingDeploy signals started, then returns the context error once the
	// build is cancelled.
	blockingDeploy := func(ctx context.Context, _ schema.Config, _ schema.ReleaseArtifact, _ string, _ io.Writer) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}

	blockingKairosify := func(ctx context.Context, _ string, _ builder.BuildOptions, _ string, _ io.Writer) (string, error) {
		close(started)
		<-ctx.Done()
		return "", ctx.Err()
	}

	BeforeEach(func() {
		s = newRecStore()
		started = make(chan struct{})
	})

	expectCancelled := func(b *auroraboot.Builder, id string) {
		Eventually(started, 10*time.Second).Should(BeClosed())
		Expect(b.Cancel(context.Background(), id)).To(Succeed())

		// Give the build goroutine time to see the failed step and return.
		Consistently(func() string {
			r, err := s.GetByID(context.Background(), id)
			if err != nil {
				return ""
			}
			return r.Message
		}, 2*time.Second, 100*time.Millisecond).Should(Equal("cancelled"))

		st, err := b.Status(context.Background(), id)
		Expect(err).NotTo(HaveOccurred())
		Expect(st.Phase).To(Equal(builder.BuildError))
		Expect(st.Message).To(Equal("cancelled"))

		rec, err := s.GetByID(context.Background(), id)
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Phase).To(Equal(store.ArtifactError))
	}

	It("keeps the cancelled message when the docker build is killed", func() {
		marker := filepath.Join(GinkgoT().TempDir(), "started")
		installFakeDocker(`case "$1" in build) touch ` + marker + `; exec sleep 30 ;; esac; exit 0`)
		go func() {
			defer GinkgoRecover()
			Eventually(func() error { _, err := os.Stat(marker); return err }, 10*time.Second).Should(Succeed())
			close(started)
		}()
		b := auroraboot.New(GinkgoT().TempDir(), noopDeploy, s).WithKairosifyFunc(noopKairosify)

		_, err := b.Build(context.Background(), builder.BuildOptions{
			ID:         "cancel-docker",
			Dockerfile: "FROM quay.io/kairos/hadron:latest",
		})
		Expect(err).NotTo(HaveOccurred())
		expectCancelled(b, "cancel-docker")
	})

	It("keeps the cancelled message when kairosify fails", func() {
		b := auroraboot.New(GinkgoT().TempDir(), noopDeploy, s).WithKairosifyFunc(blockingKairosify)

		_, err := b.Build(context.Background(), builder.BuildOptions{
			ID:        "cancel-kairosify",
			BaseImage: "quay.io/kairos/hadron:latest",
		})
		Expect(err).NotTo(HaveOccurred())
		expectCancelled(b, "cancel-kairosify")
	})

	It("keeps the cancelled message when the deploy step fails", func() {
		b := auroraboot.New(GinkgoT().TempDir(), blockingDeploy, s).WithKairosifyFunc(noopKairosify)

		_, err := b.Build(context.Background(), builder.BuildOptions{
			ID:        "cancel-deploy",
			BaseImage: "quay.io/kairos/hadron:latest",
		})
		Expect(err).NotTo(HaveOccurred())
		expectCancelled(b, "cancel-deploy")
	})

	It("keeps the cancelled message when the UKI build is interrupted", func() {
		installFakeDocker(`case "$*" in *kairos-release*) printf 'KAIROS_VERSION="v4.3.0"\nKAIROS_TRUSTED_BOOT="true"\n' ;; esac; exit 0`)
		b := auroraboot.New(GinkgoT().TempDir(), noopDeploy, s).
			WithKairosifyFunc(noopKairosify).
			WithUKIBuildFunc(func(uki.Options) error {
				close(started)
				time.Sleep(30 * time.Second)
				return nil
			})

		_, err := b.Build(context.Background(), builder.BuildOptions{
			ID:          "cancel-uki",
			BaseImage:   "quay.io/kairos/hadron:latest",
			TrustedBoot: true,
			Outputs:     builder.OutputOptions{TrustedBoot: true, ISO: true},
			Signing: builder.SigningOptions{
				UKISecureBootKey:  "/keys/db.key",
				UKISecureBootCert: "/keys/db.pem",
				UKITPMPCRKey:      "/keys/tpm.pem",
			},
		})
		Expect(err).NotTo(HaveOccurred())
		expectCancelled(b, "cancel-uki")
	})
})
