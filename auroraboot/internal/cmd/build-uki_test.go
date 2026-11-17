package cmd_test

import (
	"bytes"
	"os"

	cmdpkg "github.com/kairos-io/AuroraBoot/internal/cmd"
	"github.com/kairos-io/AuroraBoot/pkg/uki"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/urfave/cli/v2"
)

var _ = Describe("build-uki", Label("uki", "cmd"), func() {
	var app *cli.App
	var err error
	var buf *bytes.Buffer

	BeforeEach(func() {
		buf = new(bytes.Buffer)
		app = cmdpkg.GetApp("v0.0.0")
		app.Writer = buf
	})

	It("Accepts the allow-insecure-registries flag", Label("flags"), func() {
		err = app.Run([]string{"", "build-uki", "--allow-insecure-registries", "--public-keys", "/tmp", "some/image:latest"})
		// Fails later in the build, but the flag must be accepted (not rejected at parse time).
		Expect(err).ToNot(BeNil())
		Expect(err.Error()).ToNot(ContainSubstring("flag provided but not defined"))
	})

	It("accepts repeatable extension flags", Label("flags"), func() {
		err = app.Run([]string{"", "build-uki", "--tpm-pcr-private-key", "pcr.key", "--sb-key", "sb.key", "--sb-cert", "sb.pem", "--extension", "tool", "--extension", "debug@v2", "--extensions-catalog", "catalog.yaml", "some/image:latest"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).ToNot(ContainSubstring("flag provided but not defined"))
	})

	It("accepts an extension with no catalog, which reads the default one", Label("flags"), func() {
		err = app.Run([]string{"", "build-uki", "--tpm-pcr-private-key", "pcr.key", "--sb-key", "sb.key", "--sb-cert", "sb.pem", "--extension", "tool", "some/image:latest"})
		// Fails later in the build (this is not root, and there is no such
		// image), but no longer on the missing catalog.
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).ToNot(ContainSubstring("extensions-catalog is required"))
	})

	It("rejects malformed extension requests", Label("flags"), func() {
		err = app.Run([]string{"", "build-uki", "--tpm-pcr-private-key", "pcr.key", "--sb-key", "sb.key", "--sb-cert", "sb.pem", "--extension", "tool@", "--extensions-catalog", "catalog.yaml", "some/image:latest"})
		Expect(err).To(MatchError(ContainSubstring("invalid extension request")))
	})

	It("rejects a missing required flag before the root check", Label("flags"), func() {
		err = app.Run([]string{"", "build-uki", "--sb-key", "/tmp/sb.key", "--sb-cert", "/tmp/sb.pem", "some/image:latest"})

		Expect(err).ToNot(BeNil())
		Expect(err.Error()).To(ContainSubstring("Required flag \"tpm-pcr-private-key\" not set"))
		Expect(err.Error()).ToNot(ContainSubstring("this command requires root privileges"))
		Expect(err.Error()).ToNot(ContainSubstring("flag provided but not defined"))
	})

	It("accepts the app-level --cloud-config flag before the subcommand", Label("flags"), func() {
		cc := GinkgoT().TempDir() + "/cc.yaml"
		Expect(os.WriteFile(cc, []byte("#cloud-config\nusers:\n  - name: kairos\n"), 0o644)).To(Succeed())

		err = app.Run([]string{"", "--cloud-config", cc, "build-uki", "some/image:latest"})

		Expect(err).ToNot(BeNil())
		Expect(err.Error()).ToNot(ContainSubstring("flag provided but not defined"))
		Expect(err.Error()).ToNot(ContainSubstring("unknown flag"))
	})

	It("passes the pre-subcommand --cloud-config value to ReadCloudConfig", Label("flags"), func() {
		if os.Geteuid() != 0 {
			Skip("the build-uki Action (and thus the cloud-config read) only runs as root; run this test as root to exercise the wiring")
		}

		missing := "/nonexistent/cc.yaml"
		err = app.Run([]string{
			"",
			"--cloud-config", missing,
			"build-uki",
			"--tpm-pcr-private-key", "/nonexistent/key.pem",
			"--sb-key", "/nonexistent/sb.key",
			"--sb-cert", "/nonexistent/sb.pem",
			"some/image:latest",
		})

		Expect(err).ToNot(BeNil())
		Expect(err.Error()).To(ContainSubstring("reading cloud config"))
		Expect(err.Error()).To(ContainSubstring("file '" + missing + "' not found"))
	})

	It("accepts the subcommand --cloud-config flag after build-uki", Label("flags"), func() {
		cc := GinkgoT().TempDir() + "/cc.yaml"
		Expect(os.WriteFile(cc, []byte("#cloud-config\nusers:\n  - name: kairos\n"), 0o644)).To(Succeed())

		err = app.Run([]string{"", "build-uki", "--cloud-config", cc, "some/image:latest"})

		Expect(err).ToNot(BeNil())
		Expect(err.Error()).ToNot(ContainSubstring("flag provided but not defined"))
		Expect(err.Error()).ToNot(ContainSubstring("unknown flag"))
	})

	It("passes the post-subcommand --cloud-config value to ReadCloudConfig", Label("flags"), func() {
		if os.Geteuid() != 0 {
			Skip("the build-uki Action (and thus the cloud-config read) only runs as root; run this test as root to exercise the wiring")
		}

		missing := "/nonexistent/cc.yaml"
		err = app.Run([]string{
			"",
			"build-uki",
			"--tpm-pcr-private-key", "/nonexistent/key.pem",
			"--sb-key", "/nonexistent/sb.key",
			"--sb-cert", "/nonexistent/sb.pem",
			"--cloud-config", missing,
			"some/image:latest",
		})

		Expect(err).ToNot(BeNil())
		Expect(err.Error()).To(ContainSubstring("reading cloud config"))
		Expect(err.Error()).To(ContainSubstring("file '" + missing + "' not found"))
	})

	Describe("cmdline flags", Label("flags"), func() {
		var opts uki.Options

		// runParsed runs the real app and flag wiring, swapping only the
		// root-gated Before and the build itself for a capture of the options.
		runParsed := func(args ...string) error {
			for i, c := range app.Commands {
				if c.Name != "build-uki" {
					continue
				}
				parsed := *c
				parsed.Before = nil
				parsed.Action = func(ctx *cli.Context) error {
					var err error
					opts, err = cmdpkg.BuildUKIOptions(ctx)
					return err
				}
				app.Commands[i] = &parsed
			}
			return app.Run(append([]string{"", "build-uki", "--tpm-pcr-private-key", "pcr.key", "--sb-key", "sb.key", "--sb-cert", "sb.pem"}, args...))
		}

		It("keeps commas inside a single-efi-cmdline value", func() {
			Expect(runParsed(
				"--single-efi-cmdline", "Serial: console=ttyS0,115200n8",
				"--single-efi-cmdline", "My Entry: cmdline,options,here",
				"some/image:latest",
			)).To(Succeed())
			Expect(opts.SingleEfiCmdlines).To(Equal([]string{
				"Serial: console=ttyS0,115200n8",
				"My Entry: cmdline,options,here",
			}))
		})

		It("keeps commas inside an extra-cmdline value", func() {
			Expect(runParsed(
				"--extra-cmdline", "console=ttyS0,115200n8 rd.debug",
				"--extra-cmdline", "a,b",
				"some/image:latest",
			)).To(Succeed())
			Expect(opts.ExtraCmdlines).To(Equal([]string{"console=ttyS0,115200n8 rd.debug", "a,b"}))
		})

		It("accepts the short alias and does not carry values over to the next run", func() {
			Expect(runParsed("-s", "First: a=1,b=2", "some/image:latest")).To(Succeed())
			Expect(opts.SingleEfiCmdlines).To(Equal([]string{"First: a=1,b=2"}))
			app = cmdpkg.GetApp("v0.0.0")
			app.Writer = buf
			Expect(runParsed("some/image:latest")).To(Succeed())
			Expect(opts.SingleEfiCmdlines).To(BeEmpty())
		})

		It("still rejects extra-cmdline together with extend-cmdline", func() {
			err = app.Run([]string{"", "build-uki", "--tpm-pcr-private-key", "pcr.key", "--sb-key", "sb.key", "--sb-cert", "sb.pem", "--extra-cmdline", "console=ttyS0,115200n8", "--extend-cmdline", "rd.debug", "some/image:latest"})
			Expect(err).To(MatchError(ContainSubstring("mutually exclusive")))
		})
	})
})
