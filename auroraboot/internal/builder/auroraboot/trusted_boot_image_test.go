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
	"github.com/kairos-io/AuroraBoot/pkg/uki"
)

// A Dockerfile build whose image already carries /etc/kairos-release skips
// kairos-init, so the image is a Trusted Boot one only if the Dockerfile made
// it so. The UKI step packs whatever image it gets into a signed UKI.
var _ = Describe("AuroraBoot Builder Trusted Boot on a Dockerfile-built Kairos image", func() {
	var (
		s         *recStore
		b         *auroraboot.Builder
		ukiCalled chan struct{}
	)

	// fakeDockerWithRelease puts a docker stub on PATH that succeeds for every
	// call and answers the builder's probe of /etc/kairos-release with release.
	fakeDockerWithRelease := func(release string) {
		bin := GinkgoT().TempDir()
		script := "#!/bin/sh\ncase \"$*\" in *kairos-release*) printf '%s' \"$KAIROS_RELEASE\" ;; esac\nexit 0\n"
		Expect(os.WriteFile(filepath.Join(bin, "docker"), []byte(script), 0o755)).To(Succeed())
		GinkgoT().Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
		GinkgoT().Setenv("KAIROS_RELEASE", release)
	}

	BeforeEach(func() {
		s = newRecStore()
		ukiCalled = make(chan struct{}, 1)
		b = auroraboot.New(GinkgoT().TempDir(), noopDeploy, s).WithUKIBuildFunc(func(uki.Options) error {
			ukiCalled <- struct{}{}
			return nil
		})
	})

	build := func(id string) {
		_, err := b.Build(context.Background(), builder.BuildOptions{
			ID:          id,
			Dockerfile:  "FROM quay.io/kairos/hadron:latest",
			TrustedBoot: true,
			Outputs:     builder.OutputOptions{TrustedBoot: true, ISO: true},
			Signing: builder.SigningOptions{
				UKISecureBootKey:  "/keys/db.key",
				UKISecureBootCert: "/keys/db.pem",
				UKITPMPCRKey:      "/keys/tpm.pem",
			},
		})
		Expect(err).NotTo(HaveOccurred())
	}

	phase := func(id string) func() string {
		return func() string {
			r, err := s.GetByID(context.Background(), id)
			if err != nil {
				return ""
			}
			return r.Phase
		}
	}

	It("fails the build before the UKI step when the image was not built for Trusted Boot", func() {
		fakeDockerWithRelease("KAIROS_VERSION=\"v4.3.0\"\nKAIROS_TRUSTED_BOOT=\"false\"\n")

		build("standard-image")

		Eventually(phase("standard-image"), 10*time.Second).Should(Equal(store.ArtifactError))
		r, err := s.GetByID(context.Background(), "standard-image")
		Expect(err).NotTo(HaveOccurred())
		Expect(r.Message).To(ContainSubstring("Trusted Boot"))
		Consistently(ukiCalled, 200*time.Millisecond).ShouldNot(Receive())
	})

	It("builds the UKI ISO when the image was built for Trusted Boot", func() {
		fakeDockerWithRelease("KAIROS_VERSION=\"v4.3.0\"\nKAIROS_TRUSTED_BOOT=\"true\"\n")

		build("trusted-boot-image")

		Eventually(ukiCalled, 10*time.Second).Should(Receive())
		Eventually(phase("trusted-boot-image"), 10*time.Second).Should(Equal(store.ArtifactReady))
	})
})
