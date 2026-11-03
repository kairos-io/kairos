package bundled_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

const homeDirOwnerSentinel = "/usr/local/.kairos/skip-home-directory-ownership-fix"

// homeDirOwnerScript returns the fix-home-dir-ownership script exactly as
// 00_home_dir_owner_fix.yaml writes it to the image.
func homeDirOwnerScript() string {
	raw, err := bundled.EmbeddedConfigs.ReadFile("cloudconfigs/00_home_dir_owner_fix.yaml")
	Expect(err).ToNot(HaveOccurred())

	var config struct {
		Stages struct {
			Network []struct {
				Files []struct {
					Path    string `yaml:"path"`
					Content string `yaml:"content"`
				} `yaml:"files"`
			} `yaml:"network"`
		} `yaml:"stages"`
	}
	Expect(yaml.Unmarshal(raw, &config)).To(Succeed())

	for _, stage := range config.Stages.Network {
		for _, file := range stage.Files {
			if file.Path == "/usr/local/bin/fix-home-dir-ownership" {
				return file.Content
			}
		}
	}
	Fail("00_home_dir_owner_fix.yaml no longer writes /usr/local/bin/fix-home-dir-ownership")
	return ""
}

// fakeChown stands in for chown, which needs root. Like the real one it
// refuses a group name that has no group, and it logs every call it accepts.
// Only numeric groups exist in the stand-in system.
const fakeChown = `#!/bin/sh
group="${2#*:}"
case "$group" in
  ''|*[!0-9]*) echo "chown: invalid group: '$2'" >&2; exit 1 ;;
esac
echo "$@" >> "$CHOWN_LOG"
`

var _ = Describe("Home directory ownership fix", func() {
	It("uses each user's primary group, so a user without a same-named group does not stop the fix", func() {
		root := GinkgoT().TempDir()
		home := filepath.Join(root, "home")
		for _, user := range []string{"alice", "bob"} {
			Expect(os.MkdirAll(filepath.Join(home, user), 0o755)).To(Succeed())
		}

		// alice was created with primary_group: admin, so her gid is admin's
		// and no group called alice exists.
		passwd := filepath.Join(root, "passwd")
		Expect(os.WriteFile(passwd, []byte(strings.Join([]string{
			"root:x:0:0:root:/root:/bin/bash",
			"alice:x:1001:900::" + home + "/alice:/bin/bash",
			"bob:x:1002:1002::" + home + "/bob:/bin/bash",
		}, "\n")+"\n"), 0o644)).To(Succeed())

		bin := filepath.Join(root, "bin")
		Expect(os.MkdirAll(bin, 0o755)).To(Succeed())
		Expect(os.WriteFile(filepath.Join(bin, "chown"), []byte(fakeChown), 0o755)).To(Succeed())

		sentinel := filepath.Join(root, "sentinel")
		script := homeDirOwnerScript()
		for old, replacement := range map[string]string{
			"/etc/passwd":        passwd,
			homeDirOwnerSentinel: sentinel,
			`$6 ~ /^\/home\//`:   `$6 ~ /^` + strings.ReplaceAll(home, "/", `\/`) + `\//`,
		} {
			Expect(script).To(ContainSubstring(old))
			script = strings.ReplaceAll(script, old, replacement)
		}

		log := filepath.Join(root, "chown.log")
		cmd := exec.Command("bash", "-c", script)
		cmd.Env = append(os.Environ(), "PATH="+bin+":"+os.Getenv("PATH"), "CHOWN_LOG="+log)
		out, err := cmd.CombinedOutput()
		Expect(err).ToNot(HaveOccurred(), "script failed: %s", string(out))

		calls, err := os.ReadFile(log)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(calls)).To(Equal(
			"-R alice:900 " + home + "/alice\n" +
				"-R bob:1002 " + home + "/bob\n"))
		Expect(sentinel).To(BeAnExistingFile())
	})
})
