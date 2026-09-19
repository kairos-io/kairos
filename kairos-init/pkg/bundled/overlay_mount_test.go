package bundled_test

import (
	"strings"

	"github.com/kairos-io/kairos/v4/kairos-init/pkg/bundled"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"gopkg.in/yaml.v3"
)

// mountValueFlags are the mount(8) options that consume the token after them,
// so the operand count below does not mistake an option argument for a source
// or a target.
var mountValueFlags = map[string]bool{
	"-t": true, "--types": true,
	"-o": true, "--options": true,
	"-O": true, "--test-opts": true,
	"-L": true, "--label": true,
	"-U": true, "--uuid": true,
	"-N": true, "--namespace": true,
	"--source": true, "--target": true, "--target-prefix": true,
}

// mountOperands returns the non-option words of a mount invocation. mount(8)
// reads a single operand as either a source or a target and then looks it up in
// /etc/fstab, so a `-t <type>` mount that means to mount something new needs
// two: the source and the mountpoint.
func mountOperands(command string) []string {
	words := strings.Fields(command)
	Expect(words[0]).To(Equal("mount"))

	operands := []string{}
	for i := 1; i < len(words); i++ {
		word := words[i]
		switch {
		case mountValueFlags[word]:
			i++
		case strings.HasPrefix(word, "-"):
		default:
			operands = append(operands, word)
		}
	}
	return operands
}

// shellCommands walks a decoded cloud-config and returns every string under a
// `commands:` key, with backslash line continuations joined so one logical
// command is one entry. A cloud-config written out through a `content:` block
// is descended into as well: 12_nvidia.yaml lays one down at
// /system/oem/mount.yaml and yip runs its stages like any other.
func shellCommands(node any) []string {
	out := []string{}

	switch value := node.(type) {
	case map[string]any:
		for key, child := range value {
			if key == "commands" {
				if list, ok := child.([]any); ok {
					for _, entry := range list {
						if command, ok := entry.(string); ok {
							out = append(out, splitLines(command)...)
						}
					}
					continue
				}
			}
			if key == "content" {
				if nested, ok := child.(string); ok && strings.Contains(nested, "stages:") {
					var doc any
					if yaml.Unmarshal([]byte(nested), &doc) == nil {
						out = append(out, shellCommands(doc)...)
						continue
					}
				}
			}
			out = append(out, shellCommands(child)...)
		}
	case []any:
		for _, item := range value {
			out = append(out, shellCommands(item)...)
		}
	}

	return out
}

// splitLines joins backslash continuations and returns one entry per simple
// command, trimmed. A `commands:` entry is often a whole script, and the Jetson
// one chains four commands with `&&` on a single logical line.
func splitLines(script string) []string {
	joined := strings.ReplaceAll(script, "\\\n", " ")
	for _, separator := range []string{"&&", "||", ";", "|"} {
		joined = strings.ReplaceAll(joined, separator, "\n")
	}

	lines := []string{}
	for _, line := range strings.Split(joined, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// bundledCommands returns every shell command in every shipped cloud-config,
// keyed by the file it came from, so a new file is covered without touching
// this test.
func bundledCommands() map[string][]string {
	entries, err := bundled.EmbeddedConfigs.ReadDir("cloudconfigs")
	Expect(err).ToNot(HaveOccurred())
	Expect(entries).ToNot(BeEmpty())

	commands := map[string][]string{}
	for _, entry := range entries {
		raw, err := bundled.EmbeddedConfigs.ReadFile("cloudconfigs/" + entry.Name())
		Expect(err).ToNot(HaveOccurred())

		var doc any
		Expect(yaml.Unmarshal(raw, &doc)).To(Succeed())
		commands[entry.Name()] = shellCommands(doc)
	}
	return commands
}

var _ = Describe("mount commands in the bundled cloud-configs", func() {
	// mount(8) given one operand ignores -t and -o and looks the path up in
	// /etc/fstab instead, so the filesystem is never mounted:
	//
	//   $ mount -f -t overlay -o lowerdir=...,upperdir=...,workdir=... /merged
	//   mount: /merged: can't find in /etc/fstab.
	//
	// On the Jetson that left `umount /usr/local` succeeding with nothing put
	// back, which dropped COS_PERSISTENT off /usr/local for the rest of the
	// boot. The same form is written correctly in alpineInit/initramfs-init.
	It("names both a source and a target on every -t mount", func() {
		offenders := []string{}

		for file, commands := range bundledCommands() {
			for _, command := range commands {
				if !strings.HasPrefix(command, "mount ") {
					continue
				}
				operands := mountOperands(command)
				if !strings.Contains(command, "-t ") {
					continue
				}
				if len(operands) != 2 {
					offenders = append(offenders, file+": "+command)
				}
			}
		}

		Expect(offenders).To(BeEmpty(),
			"a -t mount with one operand is an fstab lookup, not a mount")
	})

	// The check above is a rule about shape, so pin the one invocation it was
	// written for. Without this the rule keeps passing if the stage is dropped.
	It("mounts the Jetson /usr/local overlay onto the persistent partition", func() {
		var overlay string

		for _, command := range bundledCommands()["12_nvidia.yaml"] {
			if strings.HasPrefix(command, "mount -t overlay") {
				overlay = command
			}
		}
		Expect(overlay).ToNot(BeEmpty(), "12_nvidia.yaml no longer mounts an overlay")

		Expect(mountOperands(overlay)).To(Equal([]string{"overlay", "/usr/local"}))
		Expect(overlay).To(ContainSubstring("lowerdir=/usr/local"))
		Expect(overlay).To(ContainSubstring("upperdir=/run/mount/persistent/local"))
		Expect(overlay).To(ContainSubstring("workdir=/run/mount/persistent/work"))
	})
})
