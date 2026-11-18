package e2e_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/format"
)

// buildRaw runs an `auroraboot sysext|confext` build, retrying on the host
// loop-device contention flake. systemd-repart --make-ddi allocates host loop
// devices for the final "Applying changes" step; on a host with few loop
// devices, back-to-back builds race ("Failed to make loopback device: Device
// or resource busy"). That's a host-resource flake, not a logic bug — the same
// retry guards the REST tier (see buildExtension / isLoopbackRace). A genuine
// failure (e.g. bad arch) isn't a loopback race, so it returns on the first try.
func buildRaw(aurora *Auroraboot, args ...string) (string, error) {
	var out string
	var err error
	for attempt := 1; attempt <= 4; attempt++ {
		out, err = aurora.ContainerRun("auroraboot", args...)
		if err == nil || !isLoopbackRace(out) {
			return out, err
		}
		// Give the host loop subsystem a moment to release devices.
		time.Sleep(time.Duration(attempt*3) * time.Second)
	}
	return out, err
}

// hierarchyFixtureImage carries one file in each of the three hierarchies the
// payload spec checks. alpine ships /opt and /srv empty, so an extension built
// from it cannot tell "hierarchy dropped" from "hierarchy had nothing in it".
const hierarchyFixtureImage = "auroraboot-sysext-fixture:test"

func buildHierarchyFixtureImage() string {
	dockerfile := `FROM alpine:3.21
RUN mkdir -p /usr/local/bin /opt/qa /srv/qa && \
    echo usr > /usr/local/bin/qa-usr && \
    echo opt > /opt/qa/qa-opt && \
    echo srv > /srv/qa/qa-srv
`
	// Pin the platform: GetImage takes the image from the local daemon only
	// when its architecture matches the one the build asks for, and the specs
	// build for amd64.
	cmd := exec.Command("docker", "build", "--network", "host",
		"--platform", "linux/amd64", "-t", hierarchyFixtureImage, "-")
	cmd.Stdin = strings.NewReader(dockerfile)
	out, err := cmd.CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), string(out))
	return hierarchyFixtureImage
}

// listSysextPayload returns the absolute paths of the regular files inside a
// sysext .raw. It reads the erofs root partition straight out of the DDI
// rather than mounting it: sfdisk gives the partition offset, dd slices it out
// and fsck.erofs unpacks it, so the spec needs neither a loop device (which
// the build deliberately avoids, see --offline=yes) nor an erofs kernel
// module on the runner. sfdisk, jq and erofs-utils all ship in the auroraboot
// image already.
func listSysextPayload(aurora *Auroraboot, raw string) []string {
	// 4f68bce3-... is the discoverable-partitions GUID for an x86-64 root
	// partition, which is what sysext.repart.d's 10-root.conf declares.
	const script = `set -euo pipefail
raw="$1"
table=$(sfdisk --json "$raw")
start=$(echo "$table" | jq -r '.partitiontable.partitions[] | select((.type|ascii_downcase)=="4f68bce3-e8cd-4db1-96e7-fbcaf984b709") | .start')
size=$(echo "$table" | jq -r '.partitiontable.partitions[] | select((.type|ascii_downcase)=="4f68bce3-e8cd-4db1-96e7-fbcaf984b709") | .size')
test -n "$start" && test -n "$size"
dd if="$raw" of=/tmp/root.erofs bs=512 skip="$start" count="$size" status=none
rm -rf /tmp/payload
fsck.erofs --extract=/tmp/payload /tmp/root.erofs >/dev/null
cd /tmp/payload && find . -type f | sed 's|^\.||' | sort
`
	out, err := aurora.ContainerRun("bash", "-c", script, "bash", raw)
	Expect(err).ToNot(HaveOccurred(), out)

	var files []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "/") {
			files = append(files, line)
		}
	}
	return files
}

// listSysextNonRootPaths returns the paths inside a sysext .raw that are not
// owned by root, the extension-release.d directory the build writes itself
// included. It slices the erofs root partition out of the DDI the same way
// listSysextPayload does; fsck.erofs restores uid and gid, so a stat on the
// extracted tree reads the ownership the image carries.
func listSysextNonRootPaths(aurora *Auroraboot, raw string) []string {
	const script = `set -euo pipefail
raw="$1"
table=$(sfdisk --json "$raw")
start=$(echo "$table" | jq -r '.partitiontable.partitions[] | select((.type|ascii_downcase)=="4f68bce3-e8cd-4db1-96e7-fbcaf984b709") | .start')
size=$(echo "$table" | jq -r '.partitiontable.partitions[] | select((.type|ascii_downcase)=="4f68bce3-e8cd-4db1-96e7-fbcaf984b709") | .size')
test -n "$start" && test -n "$size"
dd if="$raw" of=/tmp/root.erofs bs=512 skip="$start" count="$size" status=none
rm -rf /tmp/owners
fsck.erofs --extract=/tmp/owners /tmp/root.erofs >/dev/null
cd /tmp/owners && find . -mindepth 1 \( ! -uid 0 -o ! -gid 0 \) -printf '%p %U:%G\n' | sed 's|^\./|/|' | sort
`
	out, err := aurora.ContainerRun("bash", "-c", script, "bash", raw)
	Expect(err).ToNot(HaveOccurred(), out)

	var paths []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "/") {
			paths = append(paths, line)
		}
	}
	return paths
}

// These specs exercise the `auroraboot sysext` / `auroraboot confext`
// subcommands that build a signed .raw system/config extension from the last
// layer of a container image. They are the CLI half of the extensions
// feature — the REST + phonehome half lives in extensions_web_test.go.
//
// The container image used as the extraction source is intentionally tiny
// (alpine) so the specs stay fast: each build is a pull + systemd-repart run.
// Serial: each spec runs `systemd-repart --make-ddi`, which allocates host loop
// devices. Running these in parallel (ginkgo -p) with each other or with the
// other loop-heavy suites (see disks_test.go) exhausts the host loop pool.
var _ = Describe("sysext/confext generation", Label("sysext", "e2e"), Serial, func() {
	const srcImage = "alpine:3.21"

	var resultDir string
	var err error
	var aurora *Auroraboot

	BeforeEach(func() {
		format.MaxLength = 0
		resultDir, err = os.MkdirTemp("", "auroraboot-sysext-test-")
		Expect(err).ToNot(HaveOccurred())
		aurora = NewAuroraboot(resultDir)
	})

	AfterEach(func() {
		os.RemoveAll(resultDir)
	})

	// Regression guard for the flag-ordering bug: urfave/cli v2 only parses
	// flags that precede the positional args, so `--output <dir>` placed after
	// `<name> <container>` was silently dropped and the .raw landed in the
	// process cwd instead of the requested output dir.
	It("builds a sysext into the --output dir (flags before positional args)", func() {
		out, err := buildRaw(aurora, "sysext",
			"--arch", "amd64",
			"--output", resultDir,
			"e2e-tools", srcImage,
		)
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).To(ContainSubstring("Done sysext creation"), out)

		raw := filepath.Join(resultDir, "e2e-tools.sysext.raw")
		info, statErr := os.Stat(raw)
		Expect(statErr).ToNot(HaveOccurred(), "expected the .raw in the --output dir, not the container cwd")
		Expect(info.Size()).To(BeNumerically(">", 0))
	})

	It("builds a confext into the --output dir", func() {
		out, err := buildRaw(aurora, "confext",
			"--arch", "amd64",
			"--output", resultDir,
			"e2e-confext", srcImage,
		)
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).To(ContainSubstring("Done confext creation"), out)

		_, statErr := os.Stat(filepath.Join(resultDir, "e2e-confext.confext.raw"))
		Expect(statErr).ToNot(HaveOccurred())
	})

	It("extends the sysext allowlist with --include-path", func() {
		out, err := buildRaw(aurora, "sysext",
			"--debug",
			"--arch", "amd64",
			"--output", resultDir,
			"--include-path", "/opt",
			"--include-path", "/srv",
			"e2e-paths", srcImage,
		)
		Expect(err).ToNot(HaveOccurred(), out)
		// The handler logs the extended allowlist at debug level.
		Expect(out).To(ContainSubstring("extending sysext allowlist"), out)
		_, statErr := os.Stat(filepath.Join(resultDir, "e2e-paths.sysext.raw"))
		Expect(statErr).ToNot(HaveOccurred())
	})

	// The previous spec only proves the allowlist widened, which is the
	// extraction half. The packing half is systemd-repart, and
	// `--make-ddi=sysext` reads systemd's own definitions, which copy /usr and
	// /opt and nothing else. So `--include-path /srv` used to extract /srv and
	// then drop it, exit 0, and hand the operator an extension missing its
	// payload. Assert on what is inside the .raw, not on the log line.
	It("packs every --include-path hierarchy into the image", func() {
		fixture := buildHierarchyFixtureImage()

		out, err := buildRaw(aurora, "sysext",
			"--debug",
			"--arch", "amd64",
			"--output", resultDir,
			"--include-path", "/opt",
			"--include-path", "/srv",
			"e2e-payload", fixture,
		)
		Expect(err).ToNot(HaveOccurred(), out)

		raw := filepath.Join(resultDir, "e2e-payload.sysext.raw")
		Expect(raw).To(BeAnExistingFile())

		files := listSysextPayload(aurora, raw)
		Expect(files).To(ContainElement("/usr/local/bin/qa-usr"), files)
		Expect(files).To(ContainElement("/opt/qa/qa-opt"), files)
		Expect(files).To(ContainElement("/srv/qa/qa-srv"), files)
	})

	// systemd-repart copies the staging tree with the uid and gid it finds,
	// and nothing in the build chowns anything, so the extension carries the
	// uid that ran auroraboot. A node that merges one built under a uid it
	// does not have reports every path in it to `find / -xdev -nouser` and
	// fails CIS DIL 6.1.11 and 6.1.12 (kairos-io/kairos#5335). The build is
	// refused outside root for that reason; this is the other half, that the
	// root build really does pack root owned content, extension-release.d
	// included.
	It("packs the extension with root owned files", func() {
		out, err := buildRaw(aurora, "sysext",
			"--arch", "amd64",
			"--output", resultDir,
			"e2e-owners", srcImage,
		)
		Expect(err).ToNot(HaveOccurred(), out)

		raw := filepath.Join(resultDir, "e2e-owners.sysext.raw")
		Expect(raw).To(BeAnExistingFile())

		Expect(listSysextNonRootPaths(aurora, raw)).To(BeEmpty())
	})

	It("keeps --with-opt working as a deprecated alias for --include-path=/opt", func() {
		out, err := buildRaw(aurora, "sysext",
			"--debug",
			"--arch", "amd64",
			"--output", resultDir,
			"--with-opt",
			"e2e-withopt", srcImage,
		)
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).To(ContainSubstring("--with-opt is deprecated"), out)
		_, statErr := os.Stat(filepath.Join(resultDir, "e2e-withopt.sysext.raw"))
		Expect(statErr).ToNot(HaveOccurred())
	})

	It("signs the sysext when a private key and certificate are given", func() {
		// Generate a signing key/cert pair (produces db.key + db.pem in resultDir).
		out, err := aurora.ContainerRun("auroraboot", "genkey", "-o", resultDir, "e2e-signing")
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(filepath.Join(resultDir, "db.key")).To(BeAnExistingFile())
		Expect(filepath.Join(resultDir, "db.pem")).To(BeAnExistingFile())

		// Build an unsigned sysext for a size baseline.
		out, err = buildRaw(aurora, "sysext",
			"--arch", "amd64", "--output", resultDir, "e2e-unsigned", srcImage)
		Expect(err).ToNot(HaveOccurred(), out)
		unsigned, statErr := os.Stat(filepath.Join(resultDir, "e2e-unsigned.sysext.raw"))
		Expect(statErr).ToNot(HaveOccurred())

		// Build a signed sysext from the same source.
		out, err = buildRaw(aurora, "sysext",
			"--arch", "amd64",
			"--output", resultDir,
			"--private-key", filepath.Join(resultDir, "db.key"),
			"--certificate", filepath.Join(resultDir, "db.pem"),
			"e2e-signed", srcImage,
		)
		Expect(err).ToNot(HaveOccurred(), out)
		Expect(out).To(ContainSubstring("Done sysext creation"), out)

		signed, statErr := os.Stat(filepath.Join(resultDir, "e2e-signed.sysext.raw"))
		Expect(statErr).ToNot(HaveOccurred())
		// The signature partition makes the signed image strictly larger.
		Expect(signed.Size()).To(BeNumerically(">", unsigned.Size()),
			fmt.Sprintf("signed (%d) should exceed unsigned (%d)", signed.Size(), unsigned.Size()))
	})

	It("rejects an unsupported architecture", func() {
		out, err := aurora.ContainerRun("auroraboot", "sysext",
			"--arch", "i386",
			"--output", resultDir,
			"e2e-badarch", srcImage,
		)
		Expect(err).To(HaveOccurred(), out)
		Expect(out).To(ContainSubstring("unsupported architecture"), out)
	})
})
