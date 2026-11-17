package cmd_test

import (
	"os"
	"path/filepath"
	"strings"

	cmdpkg "github.com/kairos-io/AuroraBoot/internal/cmd"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/urfave/cli/v2"
)

var _ = Describe("redfish deploy", Label("redfish", "cmd"), func() {
	var tempDir string
	var isoPath string
	var app *cli.App

	BeforeEach(func() {
		var err error
		tempDir, err = os.MkdirTemp("", "redfish-test")
		Expect(err).NotTo(HaveOccurred())

		isoPath = filepath.Join(tempDir, "test.iso")
		err = os.WriteFile(isoPath, []byte("test iso content"), 0644)
		Expect(err).NotTo(HaveOccurred())

		app = &cli.App{
			Commands: []*cli.Command{
				&cmdpkg.RedFishDeployCmd,
			},
		}
	})

	AfterEach(func() {
		if tempDir != "" {
			_ = os.RemoveAll(tempDir)
		}
	})

	DescribeTable("deploy command with different vendors",
		func(vendor string, expectError bool) {
			args := []string{
				"auroraboot",
				"redfish",
				"deploy",
				"--endpoint", "https://example.com",
				"--username", "admin",
				"--password", "password",
				// URL-pull mode (Phase 1b): the BMC fetches the ISO from a URL, so
				// supply one the SSRF guard accepts instead of relying on the local
				// ISO-serve (which would require --redfish-serve-url).
				"--image-url", "http://10.0.0.5:8090/kairos.iso",
				"--vendor", vendor,
				"--verify-ssl", "true",
				"--min-memory", "4",
				"--min-cpus", "2",
				"--required-features", "UEFI",
				"--timeout", "5m",
				isoPath,
			}

			err := app.Run(args)

			// Unknown vendor still validates fine here (the flow reaches connect),
			// so for every known vendor we expect a connection/auth error against
			// the fake endpoint. Accept any HTTP error status or connect failure.
			if err != nil && (strings.Contains(err.Error(), "failed with status: 403") ||
				strings.Contains(err.Error(), "failed with status: 405") ||
				strings.Contains(err.Error(), "authentication failed") ||
				strings.Contains(err.Error(), "connecting to RedFish endpoint")) {
				// Expected when connecting to a fake/unreachable endpoint.
				Expect(err).To(HaveOccurred())
				return
			}

			if expectError {
				Expect(err).To(HaveOccurred())
			} else {
				Expect(err).NotTo(HaveOccurred())
			}
		},
		Entry("Generic Vendor", "generic", false),
		Entry("SuperMicro Vendor", "supermicro", false),
		Entry("HPE iLO Vendor", "ilo", false),
		Entry("DMTF Vendor", "dmtf", false),
		Entry("Unknown Vendor", "unknown", true),
	)

	Describe("validation", func() {
		It("errors out if required flags are missing", func() {
			args := []string{
				"auroraboot",
				"redfish",
				"deploy",
				isoPath,
			}

			err := app.Run(args)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Required flags"))
		})

		It("errors out if min-memory is invalid", func() {
			args := []string{
				"auroraboot",
				"redfish",
				"deploy",
				"--endpoint", "https://example.com",
				"--username", "admin",
				"--password", "password",
				"--min-memory", "-1",
				isoPath,
			}

			err := app.Run(args)
			Expect(err).To(HaveOccurred())
		})

		It("errors out if min-cpus is invalid", func() {
			args := []string{
				"auroraboot",
				"redfish",
				"deploy",
				"--endpoint", "https://example.com",
				"--username", "admin",
				"--password", "password",
				"--min-cpus", "0",
				isoPath,
			}

			err := app.Run(args)
			Expect(err).To(HaveOccurred())
		})

		It("errors out if timeout is invalid", func() {
			args := []string{
				"auroraboot",
				"redfish",
				"deploy",
				"--endpoint", "https://example.com",
				"--username", "admin",
				"--password", "password",
				"--timeout", "0s",
				isoPath,
			}

			err := app.Run(args)
			Expect(err).To(HaveOccurred())
		})

		It("errors with a clear message when no password source is provided", func() {
			args := []string{
				"auroraboot",
				"redfish",
				"deploy",
				"--endpoint", "https://example.com",
				"--username", "admin",
				"--image-url", "http://10.0.0.5:8090/kairos.iso",
				isoPath,
			}

			err := app.Run(args)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("no RedFish password provided"))
		})

		It("accepts --password-file and proceeds past credential resolution", func() {
			pwFile := filepath.Join(tempDir, "pw")
			Expect(os.WriteFile(pwFile, []byte("filepass\n"), 0600)).To(Succeed())

			args := []string{
				"auroraboot",
				"redfish",
				"deploy",
				"--endpoint", "https://example.com",
				"--username", "admin",
				"--password-file", pwFile,
				"--image-url", "http://10.0.0.5:8090/kairos.iso",
				isoPath,
			}

			// Resolution succeeds; the deploy then fails at connect against the
			// fake endpoint. The point is that it gets past credential resolution.
			err := app.Run(args)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).NotTo(ContainSubstring("no RedFish password provided"))
		})
	})

	// The local ISO server's TLS and the InsertMedia transfer protocol used to be
	// decided independently, so a certificate pair alone served HTTPS behind an
	// http:// advertisement. These drive the CLI with an IP endpoint so the flow
	// reaches the flag checks without resolving or contacting anything.
	Describe("serve TLS posture", func() {
		baseArgs := func(extra ...string) []string {
			args := []string{
				"auroraboot",
				"redfish",
				"deploy",
				"--endpoint", "https://10.0.0.9",
				"--username", "admin",
				"--password", "password",
			}
			return append(args, extra...)
		}

		It("refuses a certificate pair without --serve-tls", func() {
			err := app.Run(baseArgs(
				"--redfish-serve-url", "http://10.0.0.5:8090",
				"--serve-tls-cert", "/etc/ssl/bmc.crt",
				"--serve-tls-key", "/etc/ssl/bmc.key",
				isoPath,
			))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("without --serve-tls"))
		})

		It("refuses --serve-tls when the advertised serve URL is not https", func() {
			err := app.Run(baseArgs(
				"--redfish-serve-url", "http://10.0.0.5:8090",
				"--serve-tls",
				"--serve-tls-cert", "/etc/ssl/bmc.crt",
				"--serve-tls-key", "/etc/ssl/bmc.key",
				isoPath,
			))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("is not https"))
		})

		It("refuses an https serve URL with no certificate", func() {
			err := app.Run(baseArgs(
				"--redfish-serve-url", "https://10.0.0.5:8090",
				isoPath,
			))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("has no certificate"))
		})

		It("refuses the serve TLS flags alongside --image-url, which bypasses the local server", func() {
			err := app.Run(baseArgs(
				"--image-url", "https://10.0.0.5:8090/kairos.iso",
				"--serve-tls",
			))
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("configure the local ISO server"))
		})
	})
})
