package auroraboot_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/internal/builder/auroraboot"
	"github.com/kairos-io/AuroraBoot/pkg/builder"
	"github.com/kairos-io/AuroraBoot/pkg/extensions"
	"github.com/kairos-io/AuroraBoot/pkg/schema"
	"github.com/kairos-io/AuroraBoot/pkg/store"
	"github.com/kairos-io/AuroraBoot/pkg/uki"
)

func assertError(msg string) error { return errors.New(msg) }

var _ = Describe("AuroraBoot Builder", func() {
	var (
		b       *auroraboot.Builder
		baseDir string
		// capturedConfig and capturedArtifact let tests inspect what was passed to the deployer.
		capturedConfig   schema.Config
		capturedArtifact schema.ReleaseArtifact
		mu               sync.Mutex
		deployCalled     chan struct{}
	)

	BeforeEach(func() {
		baseDir = GinkgoT().TempDir()
		ch := make(chan struct{}, 1)
		deployCalled = ch

		mockDeploy := func(_ context.Context, config schema.Config, artifact schema.ReleaseArtifact, _ string, _ io.Writer) error {
			mu.Lock()
			capturedConfig = config
			capturedArtifact = artifact
			mu.Unlock()
			ch <- struct{}{} // use local var to avoid race with BeforeEach reassignment
			return nil
		}

		b = auroraboot.New(baseDir, mockDeploy, nil)
		// Build returns while its goroutine is still writing into baseDir,
		// and baseDir is a Ginkgo TempDir removed as soon as the spec ends.
		// Join the goroutine first or the two race.
		DeferCleanup(func() { Expect(b.Shutdown(context.Background())).To(Succeed()) })
	})

	// waitForBuild waits until the mock deployer has been invoked.
	waitForBuild := func() {
		Eventually(deployCalled, 5*time.Second).Should(Receive())
		// Give a tiny bit of time for status to update after deployFunc returns.
		time.Sleep(50 * time.Millisecond)
	}

	Describe("Build", func() {
		It("should create a build entry with Pending status", func() {
			status, err := b.Build(context.Background(), builder.BuildOptions{
				ID:        "test-1",
				BaseImage: "quay.io/kairos/ubuntu:latest",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(status.ID).To(Equal("test-1"))
			Expect(status.Phase).To(Equal(builder.BuildPending))
		})

		It("should transition to Building status", func() {
			// Use a deployer that blocks until we release it.
			blocked := make(chan struct{})
			slowDeploy := func(ctx context.Context, _ schema.Config, _ schema.ReleaseArtifact, _ string, _ io.Writer) error {
				select {
				case <-blocked:
				case <-ctx.Done():
				}
				return nil
			}
			slowBuilder := auroraboot.New(baseDir, slowDeploy, nil)
			DeferCleanup(func() { Expect(slowBuilder.Shutdown(context.Background())).To(Succeed()) })

			_, err := slowBuilder.Build(context.Background(), builder.BuildOptions{
				ID:        "test-building",
				BaseImage: "quay.io/kairos/ubuntu:latest",
			})
			Expect(err).NotTo(HaveOccurred())

			// Wait a moment for the goroutine to start and transition to Building.
			Eventually(func() string {
				s, _ := slowBuilder.Status(context.Background(), "test-building")
				if s == nil {
					return ""
				}
				return s.Phase
			}, 2*time.Second, 10*time.Millisecond).Should(Equal(builder.BuildBuilding))

			close(blocked)
		})

		It("should set OverlayRootfs in AuroraBoot config when provided", func() {
			_, err := b.Build(context.Background(), builder.BuildOptions{
				ID:            "test-overlay",
				BaseImage:     "quay.io/kairos/ubuntu:latest",
				OverlayRootfs: "/tmp/overlay",
			})
			Expect(err).NotTo(HaveOccurred())
			waitForBuild()

			mu.Lock()
			defer mu.Unlock()
			Expect(capturedConfig.ISO.OverlayRootfs).To(Equal("/tmp/overlay"))
		})

		It("should carry the catalog extensions and the catalog override into the ISO config", func() {
			_, err := b.Build(context.Background(), builder.BuildOptions{
				ID:                 "test-extensions",
				BaseImage:          "quay.io/kairos/ubuntu:latest",
				Extensions:         []string{"nvidia", "tailscale@v1.2.3"},
				ExtensionsCatalogs: []string{"https://example.test/releases.json"},
			})
			Expect(err).NotTo(HaveOccurred())
			waitForBuild()

			mu.Lock()
			defer mu.Unlock()
			Expect(capturedConfig.ISO.Extensions).To(Equal([]extensions.Request{
				{Name: "nvidia"},
				{Name: "tailscale", Version: "v1.2.3"},
			}))
			Expect(capturedConfig.ISO.ExtensionsCatalogs).To(Equal([]string{"https://example.test/releases.json"}))
		})

		It("should leave the catalog list empty when none is given, so the build reads the default one", func() {
			_, err := b.Build(context.Background(), builder.BuildOptions{
				ID:         "test-extensions-default",
				BaseImage:  "quay.io/kairos/ubuntu:latest",
				Extensions: []string{"nvidia"},
			})
			Expect(err).NotTo(HaveOccurred())
			waitForBuild()

			mu.Lock()
			defer mu.Unlock()
			Expect(capturedConfig.ISO.Extensions).To(Equal([]extensions.Request{{Name: "nvidia"}}))
			Expect(capturedConfig.ISO.ExtensionsCatalogs).To(BeEmpty())
		})

		It("should refuse an extension name the catalog cannot be asked for", func() {
			_, err := b.Build(context.Background(), builder.BuildOptions{
				ID:         "test-extensions-invalid",
				BaseImage:  "quay.io/kairos/ubuntu:latest",
				Extensions: []string{"nvidia@"},
			})
			Expect(err).To(MatchError(builder.ErrInvalidBuildOptions))
			Expect(err.Error()).To(ContainSubstring("invalid extension request"))
		})

		It("should set Disk.MAAS and Disk.EFI when the MAAS output is selected", func() {
			_, err := b.Build(context.Background(), builder.BuildOptions{
				ID:        "test-maas",
				BaseImage: "quay.io/kairos/ubuntu:latest",
				Outputs: builder.OutputOptions{
					MAAS: true,
				},
			})
			Expect(err).NotTo(HaveOccurred())
			waitForBuild()

			mu.Lock()
			defer mu.Unlock()
			Expect(capturedConfig.Disk.MAAS).To(BeTrue())
			// MAAS is a raw-disk variant, so it implies the EFI raw build.
			Expect(capturedConfig.Disk.EFI).To(BeTrue())
		})

		It("should inject cloud-config into AuroraBoot config", func() {
			cc := "#cloud-config\nusers:\n- name: test\n"
			_, err := b.Build(context.Background(), builder.BuildOptions{
				ID:          "test-cc",
				BaseImage:   "quay.io/kairos/ubuntu:latest",
				CloudConfig: cc,
			})
			Expect(err).NotTo(HaveOccurred())
			waitForBuild()

			mu.Lock()
			defer mu.Unlock()
			Expect(capturedConfig.CloudConfig).To(Equal(cc))
		})

		It("should set ContainerImage from BaseImage", func() {
			_, err := b.Build(context.Background(), builder.BuildOptions{
				ID:        "test-img",
				BaseImage: "quay.io/kairos/ubuntu:24.04-core-amd64-generic-v3.6.0",
			})
			Expect(err).NotTo(HaveOccurred())
			waitForBuild()

			mu.Lock()
			defer mu.Unlock()
			Expect(capturedArtifact.ContainerImage).To(Equal("quay.io/kairos/ubuntu:24.04-core-amd64-generic-v3.6.0"))
		})

		It("should reach Ready status after successful build", func() {
			_, err := b.Build(context.Background(), builder.BuildOptions{
				ID:        "test-ready",
				BaseImage: "quay.io/kairos/ubuntu:latest",
			})
			Expect(err).NotTo(HaveOccurred())
			waitForBuild()

			status, err := b.Status(context.Background(), "test-ready")
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(builder.BuildReady))
		})
	})

	Describe("Status", func() {
		It("should return status for known build ID", func() {
			_, err := b.Build(context.Background(), builder.BuildOptions{
				ID:        "known-id",
				BaseImage: "quay.io/kairos/ubuntu:latest",
			})
			Expect(err).NotTo(HaveOccurred())

			status, err := b.Status(context.Background(), "known-id")
			Expect(err).NotTo(HaveOccurred())
			Expect(status.ID).To(Equal("known-id"))
		})

		It("should return error for unknown ID", func() {
			_, err := b.Status(context.Background(), "does-not-exist")
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("not found"))
		})
	})

	Describe("List", func() {
		It("should return all builds", func() {
			_, err := b.Build(context.Background(), builder.BuildOptions{
				ID:        "list-1",
				BaseImage: "img1",
			})
			Expect(err).NotTo(HaveOccurred())
			waitForBuild()

			// Reset channel for second build.
			deployCalled = make(chan struct{}, 1)
			// Need a new builder to use the new channel — but we can just add a second build.
			_, err = b.Build(context.Background(), builder.BuildOptions{
				ID:        "list-2",
				BaseImage: "img2",
			})
			Expect(err).NotTo(HaveOccurred())

			list, err := b.List(context.Background())
			Expect(err).NotTo(HaveOccurred())
			Expect(len(list)).To(BeNumerically(">=", 2))

			ids := make([]string, len(list))
			for i, s := range list {
				ids[i] = s.ID
			}
			Expect(ids).To(ContainElements("list-1", "list-2"))
		})
	})

	Describe("Cancel", func() {
		It("should cancel a running build", func() {
			blocked := make(chan struct{})
			slowDeploy := func(ctx context.Context, _ schema.Config, _ schema.ReleaseArtifact, _ string, _ io.Writer) error {
				select {
				case <-blocked:
				case <-ctx.Done():
					return ctx.Err()
				}
				return nil
			}
			cancelBuilder := auroraboot.New(baseDir, slowDeploy, nil)
			DeferCleanup(func() { Expect(cancelBuilder.Shutdown(context.Background())).To(Succeed()) })

			_, err := cancelBuilder.Build(context.Background(), builder.BuildOptions{
				ID:        "cancel-me",
				BaseImage: "quay.io/kairos/ubuntu:latest",
			})
			Expect(err).NotTo(HaveOccurred())

			// Wait for it to reach Building.
			Eventually(func() string {
				s, _ := cancelBuilder.Status(context.Background(), "cancel-me")
				if s == nil {
					return ""
				}
				return s.Phase
			}, 2*time.Second, 10*time.Millisecond).Should(Equal(builder.BuildBuilding))

			err = cancelBuilder.Cancel(context.Background(), "cancel-me")
			Expect(err).NotTo(HaveOccurred())

			status, err := cancelBuilder.Status(context.Background(), "cancel-me")
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Phase).To(Equal(builder.BuildError))
			Expect(status.Message).To(ContainSubstring("cancel"))
		})

		// After a restart, the in-memory builds map is empty but the store
		// still holds rows in Phase=Building. Cancel must not surface a
		// spurious "not found" for those orphaned rows: from the builder's
		// perspective there is genuinely nothing to cancel, so the correct
		// answer is nil. This matches the operator backend's NotFound
		// short-circuit and lets the HTTP handler treat Cancel as idempotent.
		It("returns nil for an unknown build ID (idempotent)", func() {
			err := b.Cancel(context.Background(), "no-such-build")
			Expect(err).NotTo(HaveOccurred())
		})
	})

	Describe("buildUKI wiring", func() {
		// These tests swap pkg/uki.Build for a capture function so we can
		// assert the options we build flow through unchanged — we're testing
		// the option mapping, not the actual UKI pipeline (AuroraBoot owns
		// that).
		var (
			ukiMu       sync.Mutex
			capturedUKI uki.Options
			ukiCalled   chan struct{}
		)

		BeforeEach(func() {
			ukiCalled = make(chan struct{}, 1)
			capture := func(o uki.Options) error {
				ukiMu.Lock()
				capturedUKI = o
				ukiMu.Unlock()
				ukiCalled <- struct{}{}
				return nil
			}
			// Override the default uki.Build with our capture function.
			b.WithUKIBuildFunc(capture)
		})

		It("passes SecureBoot keys, enroll mode, overlay and source through", func() {
			_, err := b.Build(context.Background(), builder.BuildOptions{
				ID:            "uki-ok",
				BaseImage:     "quay.io/kairos/ubuntu:latest",
				OverlayRootfs: "/tmp/my-overlay",
				Outputs: builder.OutputOptions{
					ISO:         true, // so the deployer step runs and we hit buildUKI after
					TrustedBoot: true,
				},
				Signing: builder.SigningOptions{
					UKISecureBootKey:    "/keys/db.key",
					UKISecureBootCert:   "/keys/db.pem",
					UKITPMPCRKey:        "/keys/tpm.pem",
					UKIPublicKeysDir:    "/keys",
					UKISecureBootEnroll: "force",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			// Wait for both the deployer step and the uki capture.
			Eventually(deployCalled, 5*time.Second).Should(Receive())
			Eventually(ukiCalled, 5*time.Second).Should(Receive())

			ukiMu.Lock()
			defer ukiMu.Unlock()
			Expect(capturedUKI.Source).To(Equal("quay.io/kairos/ubuntu:latest"))
			Expect(capturedUKI.OutputDir).To(ContainSubstring("uki-ok"))
			Expect(capturedUKI.OutputType).To(Equal("iso"))
			Expect(capturedUKI.Name).To(Equal("kairos"))
			Expect(capturedUKI.SBKey).To(Equal("/keys/db.key"))
			Expect(capturedUKI.SBCert).To(Equal("/keys/db.pem"))
			Expect(capturedUKI.TPMPCRPrivateKey).To(Equal("/keys/tpm.pem"))
			Expect(capturedUKI.PublicKeysDir).To(Equal("/keys"))
			Expect(capturedUKI.SecureBootEnroll).To(Equal("force"))
			Expect(capturedUKI.OverlayRootfs).To(Equal("/tmp/my-overlay"))
		})

		It("passes the catalog extensions through to a UKI build", func() {
			_, err := b.Build(context.Background(), builder.BuildOptions{
				ID:                 "uki-extensions",
				BaseImage:          "quay.io/kairos/ubuntu:latest",
				Extensions:         []string{"nvidia@v1"},
				ExtensionsCatalogs: []string{"https://example.test/releases.json"},
				Outputs: builder.OutputOptions{
					ISO:         true,
					TrustedBoot: true,
				},
				Signing: builder.SigningOptions{
					UKISecureBootKey:  "/keys/db.key",
					UKISecureBootCert: "/keys/db.pem",
					UKITPMPCRKey:      "/keys/tpm.pem",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			Eventually(deployCalled, 5*time.Second).Should(Receive())
			Eventually(ukiCalled, 5*time.Second).Should(Receive())

			ukiMu.Lock()
			defer ukiMu.Unlock()
			Expect(capturedUKI.Extensions).To(Equal([]extensions.Request{{Name: "nvidia", Version: "v1"}}))
			Expect(capturedUKI.ExtensionsCatalogs).To(Equal([]string{"https://example.test/releases.json"}))
		})

		It("passes a schemed source reference through to UKI unchanged", func() {
			_, err := b.Build(context.Background(), builder.BuildOptions{
				ID:        "uki-schemed",
				BaseImage: "oci:quay.io/kairos/ubuntu:latest",
				Outputs: builder.OutputOptions{
					ISO:         true, // so the deployer step runs and we hit buildUKI after
					TrustedBoot: true,
				},
				Signing: builder.SigningOptions{
					UKISecureBootKey:    "/keys/db.key",
					UKISecureBootCert:   "/keys/db.pem",
					UKITPMPCRKey:        "/keys/tpm.pem",
					UKIPublicKeysDir:    "/keys",
					UKISecureBootEnroll: "force",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			Eventually(deployCalled, 5*time.Second).Should(Receive())
			Eventually(ukiCalled, 5*time.Second).Should(Receive())

			ukiMu.Lock()
			defer ukiMu.Unlock()
			// The scheme must survive untouched: no extra "docker:" prefixing.
			Expect(capturedUKI.Source).To(Equal("oci:quay.io/kairos/ubuntu:latest"))
		})

		It("fails the build with a helpful message when UKI keys are missing", func() {
			_, err := b.Build(context.Background(), builder.BuildOptions{
				ID:        "uki-nokeys",
				BaseImage: "quay.io/kairos/ubuntu:latest",
				Outputs: builder.OutputOptions{
					ISO:         true,
					TrustedBoot: true,
				},
				// No Signing populated.
			})
			Expect(err).NotTo(HaveOccurred())

			// Deployer still runs (iso), but uki.Build should never be called
			// because the pre-check rejects the empty keys first.
			Eventually(deployCalled, 5*time.Second).Should(Receive())
			Consistently(ukiCalled, 200*time.Millisecond).ShouldNot(Receive())

			// The build transitions to Error with a message mentioning the missing keys.
			Eventually(func() string {
				s, _ := b.Status(context.Background(), "uki-nokeys")
				if s == nil {
					return ""
				}
				return s.Phase
			}, 2*time.Second, 10*time.Millisecond).Should(Equal(builder.BuildError))

			status, err := b.Status(context.Background(), "uki-nokeys")
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Message).To(ContainSubstring("SecureBoot keys"))
		})

		It("surfaces pkg/uki.Build errors as a build failure", func() {
			// Override capture with a failing func.
			b.WithUKIBuildFunc(func(_ uki.Options) error {
				return assertError("synthetic uki failure")
			})

			_, err := b.Build(context.Background(), builder.BuildOptions{
				ID:        "uki-boom",
				BaseImage: "quay.io/kairos/ubuntu:latest",
				Outputs: builder.OutputOptions{
					ISO:         true,
					TrustedBoot: true,
				},
				Signing: builder.SigningOptions{
					UKISecureBootKey:  "/keys/db.key",
					UKISecureBootCert: "/keys/db.pem",
					UKITPMPCRKey:      "/keys/tpm.pem",
				},
			})
			Expect(err).NotTo(HaveOccurred())
			Eventually(deployCalled, 5*time.Second).Should(Receive())

			Eventually(func() string {
				s, _ := b.Status(context.Background(), "uki-boom")
				if s == nil {
					return ""
				}
				return s.Phase
			}, 2*time.Second, 10*time.Millisecond).Should(Equal(builder.BuildError))

			status, err := b.Status(context.Background(), "uki-boom")
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Message).To(ContainSubstring("synthetic uki failure"))
		})

		It("passes the cloud config through to uki.Options", func() {
			cc := "#cloud-config\ninstall:\n  selinux:\n    enabled: true\n    mode: permissive\n"
			_, err := b.Build(context.Background(), builder.BuildOptions{
				ID:          "uki-cc",
				BaseImage:   "quay.io/kairos/ubuntu:latest",
				CloudConfig: cc,
				Outputs: builder.OutputOptions{
					ISO:         true,
					TrustedBoot: true,
				},
				Signing: builder.SigningOptions{
					UKISecureBootKey:  "/keys/db.key",
					UKISecureBootCert: "/keys/db.pem",
					UKITPMPCRKey:      "/keys/tpm.pem",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			Eventually(deployCalled, 5*time.Second).Should(Receive())
			Eventually(ukiCalled, 5*time.Second).Should(Receive())

			ukiMu.Lock()
			defer ukiMu.Unlock()
			Expect(capturedUKI.CloudConfig).To(Equal(cc))
		})

		// A Trusted Boot build asks for UKI without the plain ISO, because the
		// UKI step writes the bootable ISO itself. That ISO is the build's
		// output and must survive the removal of unrequested plain ISOs.
		It("keeps the UKI ISO when the plain ISO was not requested", func() {
			b.WithUKIBuildFunc(func(o uki.Options) error {
				for _, name := range []string{"kairos.iso", "kairos.iso.sha256"} {
					if err := os.WriteFile(filepath.Join(o.OutputDir, name), []byte("uki"), 0o644); err != nil {
						return err
					}
				}
				return nil
			})

			_, err := b.Build(context.Background(), builder.BuildOptions{
				ID:          "uki-no-plain-iso",
				BaseImage:   "quay.io/kairos/ubuntu:latest",
				TrustedBoot: true,
				Outputs: builder.OutputOptions{
					ISO:         true,
					TrustedBoot: true,
				},
				Signing: builder.SigningOptions{
					UKISecureBootKey:  "/keys/db.key",
					UKISecureBootCert: "/keys/db.pem",
					UKITPMPCRKey:      "/keys/tpm.pem",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() string {
				s, _ := b.Status(context.Background(), "uki-no-plain-iso")
				if s == nil {
					return ""
				}
				return s.Phase
			}, 5*time.Second, 10*time.Millisecond).Should(Equal(builder.BuildReady))

			isoPath := filepath.Join(baseDir, "uki-no-plain-iso", "kairos.iso")
			Expect(isoPath).To(BeAnExistingFile())
			status, err := b.Status(context.Background(), "uki-no-plain-iso")
			Expect(err).NotTo(HaveOccurred())
			Expect(status.Artifacts).To(ContainElement(isoPath))
		})

		// The deployer writes a plain GRUB ISO as scaffolding. A Trusted Boot
		// build asks for the ISO and means the UKI one, so the scaffolding ISO
		// goes and only the UKI ISO is left to collect. Without that, the
		// build would offer two ISOs and the plain one does not boot a
		// Trusted Boot image.
		It("removes the scaffolding ISO but keeps the UKI ISO", func() {
			scaffoldingDeploy := func(_ context.Context, config schema.Config, _ schema.ReleaseArtifact, _ string, _ io.Writer) error {
				return os.WriteFile(filepath.Join(config.State, "plain.iso"), []byte("plain"), 0o644)
			}
			nb := auroraboot.New(baseDir, scaffoldingDeploy, nil)
			nb.WithUKIBuildFunc(func(o uki.Options) error {
				return os.WriteFile(filepath.Join(o.OutputDir, "kairos.iso"), []byte("uki"), 0o644)
			})

			_, err := nb.Build(context.Background(), builder.BuildOptions{
				ID:          "uki-netboot",
				BaseImage:   "quay.io/kairos/ubuntu:latest",
				ISO:         true,
				TrustedBoot: true,
				Outputs: builder.OutputOptions{
					ISO:         true,
					TrustedBoot: true,
				},
				Signing: builder.SigningOptions{
					UKISecureBootKey:  "/keys/db.key",
					UKISecureBootCert: "/keys/db.pem",
					UKITPMPCRKey:      "/keys/tpm.pem",
				},
			})
			Expect(err).NotTo(HaveOccurred())

			Eventually(func() string {
				s, _ := nb.Status(context.Background(), "uki-netboot")
				if s == nil {
					return ""
				}
				return s.Phase
			}, 5*time.Second, 10*time.Millisecond).Should(Equal(builder.BuildReady))

			outputDir := filepath.Join(baseDir, "uki-netboot")
			Expect(filepath.Join(outputDir, "plain.iso")).NotTo(BeAnExistingFile())
			Expect(filepath.Join(outputDir, "kairos.iso")).To(BeAnExistingFile())
		})
	})
})

// The cross-arch preflight is the only thing standing between a user and the
// exec format error from kairos-io/kairos#4088, so pin what a refusal does:
// the build ends in BuildError, the row says so too, and the deployer never
// runs. It also has to reach that verdict without pulling anything.
var _ = Describe("AuroraBoot Builder cross-arch preflight", func() {
	It("fails the build and the row when the daemon cannot emulate the arch", func() {
		s := newRecStore()
		deployed := make(chan struct{}, 1)
		deploy := func(_ context.Context, _ schema.Config, _ schema.ReleaseArtifact, _ string, _ io.Writer) error {
			deployed <- struct{}{}
			return nil
		}

		b := auroraboot.New(GinkgoT().TempDir(), deploy, s).
			// A host that can only build for itself.
			WithPlatformsFunc(func(context.Context) ([]string, error) {
				return []string{"linux/amd64"}, nil
			})

		_, err := b.Build(context.Background(), builder.BuildOptions{
			ID:         "binfmt-refused",
			Dockerfile: "FROM quay.io/kairos/ubuntu:latest\nRUN echo hi\n",
			Source:     builder.ImageSource{Arch: "arm64"},
			Outputs:    builder.OutputOptions{ISO: true},
		})
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() string {
			st, _ := b.Status(context.Background(), "binfmt-refused")
			if st == nil {
				return ""
			}
			return st.Phase
		}, 5*time.Second, 10*time.Millisecond).Should(Equal(builder.BuildError))

		status, err := b.Status(context.Background(), "binfmt-refused")
		Expect(err).NotTo(HaveOccurred())
		Expect(status.Message).To(And(
			ContainSubstring("cannot build for linux/arm64"),
			ContainSubstring("binfmt_misc"),
		))

		// The artifact row carries the same verdict, so the UI shows it.
		rec, err := s.GetByID(context.Background(), "binfmt-refused")
		Expect(err).NotTo(HaveOccurred())
		Expect(rec.Phase).To(Equal(store.ArtifactError))
		Expect(rec.Message).To(ContainSubstring("binfmt_misc"))

		// And nothing was built or deployed.
		Consistently(deployed, 200*time.Millisecond).ShouldNot(Receive())
	})

	// A Dockerfile that only COPYs onto an already-kairosified base executes no
	// target-architecture binary, so it needs no emulation. The preflight used
	// to run at the top of the build and refused these too.
	It("does not consult the builder when no step runs a target binary", func() {
		probed := make(chan struct{}, 1)
		b := auroraboot.New(GinkgoT().TempDir(), noopDeploy, nil).
			WithPlatformsFunc(func(context.Context) ([]string, error) {
				probed <- struct{}{}
				return []string{"linux/amd64"}, nil
			})

		_, err := b.Build(context.Background(), builder.BuildOptions{
			ID:        "copy-only",
			BaseImage: "quay.io/kairos/ubuntu:latest",
			Source:    builder.ImageSource{Arch: "arm64"},
			Outputs:   builder.OutputOptions{ISO: true},
		})
		Expect(err).NotTo(HaveOccurred())

		Consistently(probed, 500*time.Millisecond).ShouldNot(Receive())
	})
})
