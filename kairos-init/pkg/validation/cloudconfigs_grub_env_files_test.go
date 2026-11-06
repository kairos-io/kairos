package validation_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/mudler/yip/pkg/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

const grubEnvFilesStep = "Create empty GRUB environment files"

// fakeTools writes stand-ins for the commands the step calls, so its body runs
// unprivileged against two temporary directories. blkid answers from the
// FAKE_OEM_DEV and FAKE_OEM_TYPE variables; grub2-editenv writes a marker.
func fakeTools(dir string) {
	tools := map[string]string{
		"blkid": `#!/bin/sh
if [ "$1" = "-L" ]; then
  [ -n "$FAKE_OEM_DEV" ] || exit 2
  echo "$FAKE_OEM_DEV"
else
  echo "$FAKE_OEM_TYPE"
fi
`,
		"grub2-editenv": `#!/bin/sh
[ "$2" = "create" ] || exit 1
echo "# GRUB Environment Block" > "$1"
`,
		"mount":  "#!/bin/sh\nexit 0\n",
		"umount": "#!/bin/sh\nexit 0\n",
	}
	for name, body := range tools {
		Expect(os.WriteFile(filepath.Join(dir, name), []byte(body), 0755)).To(Succeed())
	}
}

var _ = Describe("Bundled cloudconfigs GRUB environment files", func() {
	var (
		config     schema.YipConfig
		state, oem string
		bin        string
	)

	// run executes the step of the given stage the way yip does, with sh -c,
	// with the STATE and OEM mount points moved to temporary directories.
	run := func(stage, oemDev, oemType string) {
		var step *schema.Stage
		for i := range config.Stages[stage] {
			if config.Stages[stage][i].Name == grubEnvFilesStep {
				step = &config.Stages[stage][i]
			}
		}
		Expect(step).NotTo(BeNil(), "%s has no %q step", stage, grubEnvFilesStep)
		Expect(step.Commands).To(HaveLen(1))

		command := strings.ReplaceAll(step.Commands[0], "/tmp/mnt/STATE", state)
		command = strings.ReplaceAll(command, "/tmp/mnt/OEM", oem)
		cmd := exec.Command("sh", "-c", command)
		cmd.Env = append(os.Environ(),
			"PATH="+bin+string(os.PathListSeparator)+os.Getenv("PATH"),
			"FAKE_OEM_DEV="+oemDev,
			"FAKE_OEM_TYPE="+oemType,
		)
		out, err := cmd.CombinedOutput()
		Expect(err).NotTo(HaveOccurred(), "step body failed: %s", out)
	}

	BeforeEach(func() {
		content, err := os.ReadFile(filepath.Join("..", "bundled", "cloudconfigs", grubCloudConfig))
		Expect(err).NotTo(HaveOccurred())
		Expect(yaml.Unmarshal(content, &config)).To(Succeed())

		root := GinkgoT().TempDir()
		state = filepath.Join(root, "state")
		oem = filepath.Join(root, "oem")
		bin = filepath.Join(root, "bin")
		for _, d := range []string{state, oem, bin} {
			Expect(os.Mkdir(d, 0755)).To(Succeed())
		}
		fakeTools(bin)
	})

	for _, stage := range []string{"after-install", "after-reset", "after-upgrade"} {
		It("creates boot_assessment on STATE and grubenv on OEM in "+stage, func() {
			run(stage, "/dev/vda2", "ext4")

			Expect(filepath.Join(state, "boot_assessment")).To(BeAnExistingFile())
			Expect(filepath.Join(oem, "grubenv")).To(BeAnExistingFile())
			Expect(filepath.Join(state, "grubenv")).NotTo(BeAnExistingFile(),
				"two grubenv files: GRUB loads whichever it finds first")
		})
	}

	It("keeps existing files", func() {
		for _, f := range []string{filepath.Join(state, "boot_assessment"), filepath.Join(oem, "grubenv")} {
			Expect(os.WriteFile(f, []byte("next_entry=recovery\n"), 0600)).To(Succeed())
		}

		run("after-install", "/dev/vda2", "ext4")

		for _, f := range []string{filepath.Join(state, "boot_assessment"), filepath.Join(oem, "grubenv")} {
			Expect(os.ReadFile(f)).To(Equal([]byte("next_entry=recovery\n")), f)
		}
	})

	DescribeTable("puts grubenv on STATE when GRUB cannot read OEM",
		func(oemDev, oemType string) {
			run("after-install", oemDev, oemType)

			Expect(filepath.Join(state, "grubenv")).To(BeAnExistingFile())
			Expect(filepath.Join(oem, "grubenv")).NotTo(BeAnExistingFile())
		},
		Entry("locked LUKS container", "/dev/vda2", "crypto_LUKS"),
		Entry("no partition labelled COS_OEM", "", ""),
		Entry("unlocked LUKS mapping", "/dev/mapper/vda2", "ext4"),
	)
})
