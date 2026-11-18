package cmd_test

import (
	"bytes"
	"os"
	"path/filepath"

	cmdpkg "github.com/kairos-io/AuroraBoot/internal/cmd"
	"github.com/kairos-io/AuroraBoot/pkg/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/urfave/cli/v2"
)

var _ = Describe("build-iso", Label("iso", "cmd"), func() {
	var app *cli.App
	var err error
	var buf *bytes.Buffer

	BeforeEach(func() {
		buf = new(bytes.Buffer)

		app = cmdpkg.GetApp("v0.0.0")
		app.Writer = buf
	})

	It("errors out if no rootfs sources are defined", func() {
		err = app.Run([]string{"", "build-iso"}) // first arg is the path to the program
		Expect(err.Error()).To(Equal("no source defined"))
	})

	It("Errors out if rootfs is a non valid argument", Label("flags"), func() {
		err = app.Run([]string{"", "build-iso", "/no/image/reference"})
		Expect(err).ToNot(BeNil())
		Expect(err.Error()).To(ContainSubstring("invalid image reference"))
	})

	It("Errors out if overlay roofs path does not exist", Label("flags"), func() {
		err = app.Run([]string{"", "build-iso", "--overlay-rootfs", "/nonexistingpath", "system/cos"})
		Expect(err).ToNot(BeNil())
		Expect(err.Error()).To(ContainSubstring("invalid path"))
	})

	It("Errors out if overlay iso path does not exist", Label("flags"), func() {
		err = app.Run([]string{"", "build-iso", "--overlay-iso", "/nonexistingpath", "some/image:latest"})
		Expect(err).ToNot(BeNil())
		Expect(err.Error()).To(ContainSubstring("invalid path"))
	})

	It("Errors out if arch is invalid", Label("flags"), func() {
		err = app.Run([]string{"", "build-iso", "--arch", "invalid", "some/image:latest"})
		Expect(err).ToNot(BeNil())
		Expect(err.Error()).To(ContainSubstring("invalid architecture"))
		Expect(err.Error()).To(ContainSubstring("must be 'amd64', 'arm64', or 'riscv64'"))
	})

	It("Accepts amd64 as a valid arch", Label("flags"), func() {
		err = app.Run([]string{"", "build-iso", "--arch", "amd64", "system/cos"})
		// This will still error out because system/cos is not a valid image reference,
		// but it should not error on the arch validation
		Expect(err).ToNot(BeNil())
		Expect(err.Error()).ToNot(ContainSubstring("invalid architecture"))
	})

	It("Accepts arm64 as a valid arch", Label("flags"), func() {
		err = app.Run([]string{"", "build-iso", "--arch", "arm64", "system/cos"})
		// This will still error out because system/cos is not a valid image reference,
		// but it should not error on the arch validation
		Expect(err).ToNot(BeNil())
		Expect(err.Error()).ToNot(ContainSubstring("invalid architecture"))
	})

	It("Accepts riscv64 as a valid arch", Label("flags"), func() {
		err = app.Run([]string{"", "build-iso", "--arch", "riscv64", "system/cos"})
		// This will still error out because system/cos is not a valid image reference,
		// but it should not error on the arch validation
		Expect(err).ToNot(BeNil())
		Expect(err.Error()).ToNot(ContainSubstring("invalid architecture"))
	})

	It("Accepts extend-live-cmdline flag", Label("flags"), func() {
		err = app.Run([]string{"", "build-iso", "--extend-live-cmdline", "rd.debug", "system/cos"})
		// Fails on image reference, but flag should be accepted (no "unknown flag" or similar)
		Expect(err).ToNot(BeNil())
		Expect(err.Error()).ToNot(ContainSubstring("extend-live-cmdline"))
		Expect(err.Error()).ToNot(ContainSubstring("unknown"))
	})

	It("Accepts extend-live-cmdline with multiple options", Label("flags"), func() {
		err = app.Run([]string{"", "build-iso", "--extend-live-cmdline", "rd.debug rd.shell root=live:LABEL=TEST", "system/cos"})
		Expect(err).ToNot(BeNil())
		Expect(err.Error()).ToNot(ContainSubstring("extend-live-cmdline"))
	})

	It("Accepts the live-console flag", Label("flags"), func() {
		err = app.Run([]string{"", "build-iso", "--live-console", "console=ttyUSB0,115200", "system/cos"})
		Expect(err).ToNot(BeNil())
		Expect(err.Error()).ToNot(ContainSubstring("live-console"))
		Expect(err.Error()).ToNot(ContainSubstring("unknown"))
	})

	It("Accepts the default-grub-entry flag", Label("flags"), func() {
		err = app.Run([]string{"", "build-iso", "--default-grub-entry", constants.LiveGrubEntryBootLocal, "system/cos"})
		Expect(err).ToNot(BeNil())
		Expect(err.Error()).ToNot(ContainSubstring("default-grub-entry"))
		Expect(err.Error()).ToNot(ContainSubstring("unknown"))
	})

	It("Accepts the allow-insecure-registries flag", Label("flags"), func() {
		err = app.Run([]string{"", "build-iso", "--allow-insecure-registries", "system/cos"})
		// Fails on image reference, but the flag should be accepted (no "unknown flag" error)
		Expect(err).ToNot(BeNil())
		Expect(err.Error()).ToNot(ContainSubstring("flag provided but not defined"))
	})

	It("Accepts the deprecated insecure alias", Label("flags"), func() {
		err = app.Run([]string{"", "build-iso", "--insecure", "system/cos"})
		// The deprecated alias must keep working for v0.22.0 scripts.
		Expect(err).ToNot(BeNil())
		Expect(err.Error()).ToNot(ContainSubstring("flag provided but not defined"))
	})

	It("accepts an extension with no catalog, which reads the default one", Label("flags"), func() {
		err = app.Run([]string{"", "build-iso", "--extension", "foo", "system/cos"})
		// Still fails, on the source image this box has no way to pull, but
		// not on a missing catalog any more.
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).ToNot(ContainSubstring("extensions-catalog"))
	})

	It("rejects invalid extension requests", Label("flags"), func() {
		err = app.Run([]string{"", "build-iso", "--extensions-catalog", "catalog.yaml", "--extension", "foo@", "system/cos"})
		Expect(err).To(MatchError(ContainSubstring("invalid extension request")))
	})

	It("accepts repeatable extension catalogs", Label("flags"), func() {
		err = app.Run([]string{"", "build-iso", "--extensions-catalog", "first.json", "--extensions-catalog", "second.json", "--extension", "foo", "system/cos"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).ToNot(ContainSubstring("flag provided but not defined"))
		Expect(err.Error()).ToNot(ContainSubstring("extensions-catalog is required"))
	})

	It("accepts repeatable extension flags", Label("flags"), func() {
		err = app.Run([]string{"", "build-iso", "--extensions-catalog", "catalog.yaml", "--extension", "foo", "--extension", "bar@v1", "system/cos"})
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).ToNot(ContainSubstring("flag provided but not defined"))
		Expect(err.Error()).ToNot(ContainSubstring("extensions-catalog is required"))
		Expect(err.Error()).ToNot(ContainSubstring("invalid extension request"))
	})
})

var _ = Describe("build-iso work files", Label("iso", "cmd"), func() {
	var (
		app *cli.App
		out string
		tmp string
	)

	listDir := func(dir string) []string {
		entries, err := os.ReadDir(dir)
		Expect(err).ToNot(HaveOccurred())
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return names
	}

	BeforeEach(func() {
		app = cmdpkg.GetApp("v0.0.0")
		app.Writer = new(bytes.Buffer)

		// Create every temp dir first, so none of them lands inside tmp.
		out = GinkgoT().TempDir()
		tmp = GinkgoT().TempDir()
		GinkgoT().Setenv("TMPDIR", tmp)
	})

	It("keeps a config.yaml it did not create in --output", func() {
		user := []byte("hostname: users-own\n")
		Expect(os.WriteFile(filepath.Join(out, "config.yaml"), user, 0o600)).To(Succeed())
		other := filepath.Join(GinkgoT().TempDir(), "other.yaml")
		Expect(os.WriteFile(other, []byte("hostname: from-flag\n"), 0o600)).To(Succeed())

		err := app.Run([]string{"", "build-iso", "--output", out, "-c", other, "/no/image/reference"})
		Expect(err).To(HaveOccurred())

		got, readErr := os.ReadFile(filepath.Join(out, "config.yaml"))
		Expect(readErr).ToNot(HaveOccurred())
		Expect(got).To(Equal(user))
		Expect(listDir(out)).To(Equal([]string{"config.yaml"}))
	})

	It("defaults --output to the current directory", func() {
		var output *cli.StringFlag
		for _, f := range cmdpkg.BuildISOCmd.Flags {
			if sf, ok := f.(*cli.StringFlag); ok && sf.Name == "output" {
				output = sf
			}
		}
		Expect(output).ToNot(BeNil())
		Expect(output.Value).To(Equal("."))
		Expect(output.Usage).To(ContainSubstring("current directory"))
	})

	It("leaves the current directory clean when --output is omitted", func() {
		cwd := GinkgoT().TempDir()
		user := []byte("hostname: users-own\n")
		Expect(os.WriteFile(filepath.Join(cwd, "config.yaml"), user, 0o600)).To(Succeed())
		GinkgoT().Chdir(cwd)

		err := app.Run([]string{"", "build-iso", "/no/image/reference"})
		Expect(err).To(HaveOccurred())

		got, readErr := os.ReadFile(filepath.Join(cwd, "config.yaml"))
		Expect(readErr).ToNot(HaveOccurred())
		Expect(got).To(Equal(user))
		Expect(listDir(cwd)).To(Equal([]string{"config.yaml"}))
	})

	It("removes its private work dir when a step fails", func() {
		err := app.Run([]string{"", "build-iso", "--output", out, "/no/image/reference"})
		Expect(err).To(HaveOccurred())

		Expect(listDir(tmp)).To(BeEmpty())
	})
})
