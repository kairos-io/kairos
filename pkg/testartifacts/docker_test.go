//go:build testartifacts

package testartifacts_test

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	diskfs "github.com/diskfs/go-diskfs"
	"github.com/diskfs/go-diskfs/partition/gpt"
	"github.com/kairos-io/kairos/v4/pkg/testartifacts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// Partition type GUIDs from the Discoverable Partitions Specification.
const (
	rootX86          = "4f68bce3-e8cd-4db1-96e7-fbcaf984b709"
	rootX86Verity    = "2c7357ed-ebd2-46d9-aec1-23d437ec2bf5"
	rootX86VeritySig = "41092b05-9fc8-4523-994f-2def0408b176"
	rootArm64        = "b921b045-1df0-41c3-af44-4c6f280d3fae"
	rootArm64Verity  = "df3300ce-d69f-4c92-978c-9bfb0f38d820"
)

func partitionTypes(path string) []string {
	d, err := diskfs.Open(path, diskfs.WithOpenMode(diskfs.ReadOnly))
	Expect(err).ToNot(HaveOccurred())
	defer d.Close()
	pt, err := d.GetPartitionTable()
	Expect(err).ToNot(HaveOccurred())
	table, ok := pt.(*gpt.Table)
	Expect(ok).To(BeTrue(), "expected a GPT partition table")
	var types []string
	for _, p := range table.Partitions {
		if p.Type == gpt.Unused {
			continue
		}
		types = append(types, strings.ToLower(string(p.Type)))
	}
	return types
}

// extractRootPartition copies the root partition of a sysext DDI into its
// own file and unpacks its erofs filesystem with the fsck.erofs that ships in
// the AuroraBoot image, returning the directory it was unpacked into.
func extractRootPartition(ctx context.Context, path string) string {
	d, err := diskfs.Open(path, diskfs.WithOpenMode(diskfs.ReadOnly))
	Expect(err).ToNot(HaveOccurred())
	defer d.Close()
	pt, err := d.GetPartitionTable()
	Expect(err).ToNot(HaveOccurred())
	table, ok := pt.(*gpt.Table)
	Expect(ok).To(BeTrue(), "expected a GPT partition table")

	var root *gpt.Partition
	for _, p := range table.Partitions {
		if t := strings.ToLower(string(p.Type)); t == rootX86 || t == rootArm64 {
			root = p
		}
	}
	Expect(root).ToNot(BeNil(), "no root partition in %s", path)

	data, err := os.ReadFile(path)
	Expect(err).ToNot(HaveOccurred())
	sector := uint64(table.LogicalSectorSize)
	if sector == 0 {
		sector = 512
	}
	start := root.Start * sector
	end := (root.End + 1) * sector
	dir := GinkgoT().TempDir()
	Expect(os.WriteFile(filepath.Join(dir, "root.erofs"), data[start:end], 0o644)).To(Succeed())

	out := filepath.Join(dir, "tree")
	runInAuroraBoot(ctx, dir, "fsck.erofs", "--extract=/work/tree", "/work/root.erofs")
	return out
}

// runInAuroraBoot runs one of the tools the AuroraBoot image ships, as the
// calling user, with dir mounted at /work.
func runInAuroraBoot(ctx context.Context, dir string, tool string, args ...string) {
	cmdArgs := []string{"run", "--rm", "--user", fmt.Sprintf("%d:%d", os.Getuid(), os.Getgid()),
		"-v", dir + ":/work", "--entrypoint", tool, testartifacts.AuroraBootImage}
	out, err := exec.CommandContext(ctx, "docker", append(cmdArgs, args...)...).CombinedOutput()
	Expect(err).ToNot(HaveOccurred(), string(out))
}

// expectHelloPayload checks that tree carries the test payload where the
// end-to-end suite looks for it: /usr/bin/hello.sh, printing "Hello world".
func expectHelloPayload(tree string) {
	content, err := os.ReadFile(filepath.Join(tree, "usr", "bin", "hello.sh"))
	ExpectWithOffset(1, err).ToNot(HaveOccurred())
	ExpectWithOffset(1, string(content)).To(ContainSubstring(`echo "Hello world"`))
}

var _ = Describe("key sets generated with the AuroraBoot binary", func() {
	It("generates a Secure Boot and TPM PCR key set the caller can read", func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		DeferCleanup(cancel)
		dir := GinkgoT().TempDir()
		Expect(testartifacts.GenerateKeySet(ctx, dir)).To(Succeed())

		for _, kind := range []string{"PK", "KEK", "db"} {
			for _, ext := range []string{".key", ".pem", ".der", ".esl", ".auth"} {
				Expect(filepath.Join(dir, kind+ext)).To(BeARegularFile())
			}
		}
		Expect(filepath.Join(dir, "tpm2-pcr-private.pem")).To(BeARegularFile())

		keyPEM, err := os.ReadFile(filepath.Join(dir, "db.key"))
		Expect(err).ToNot(HaveOccurred())
		block, _ := pem.Decode(keyPEM)
		Expect(block).ToNot(BeNil())

		certPEM, err := os.ReadFile(filepath.Join(dir, "db.pem"))
		Expect(err).ToNot(HaveOccurred())
		block, _ = pem.Decode(certPEM)
		Expect(block).ToNot(BeNil())
		_, err = x509.ParseCertificate(block.Bytes)
		Expect(err).ToNot(HaveOccurred())
	})
})

var _ = Describe("artifacts built with AuroraBoot", func() {
	var ctx context.Context

	BeforeEach(func() {
		if !testartifacts.DockerAvailable() {
			Fail("these specs build artifacts with Docker, and no Docker daemon is reachable")
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Minute)
		DeferCleanup(cancel)
	})

	It("builds a verity and signed extension when given a key and certificate", func() {
		keys := GinkgoT().TempDir()
		Expect(testartifacts.GenerateKeySet(ctx, keys)).To(Succeed())
		out := GinkgoT().TempDir()

		path, err := testartifacts.BuildSysext(ctx, testartifacts.SysextOptions{
			Dir: out, Name: "work", Arch: "amd64",
			KeyFile: filepath.Join(keys, "db.key"), CertFile: filepath.Join(keys, "db.pem"),
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(path).To(Equal(filepath.Join(out, "work.sysext.raw")))
		Expect(partitionTypes(path)).To(ConsistOf(rootX86, rootX86Verity, rootX86VeritySig))
	})

	It("builds a verity-only extension without a key", func() {
		out := GinkgoT().TempDir()
		path, err := testartifacts.BuildSysext(ctx, testartifacts.SysextOptions{Dir: out, Name: "work", Arch: "amd64"})
		Expect(err).ToNot(HaveOccurred())
		Expect(partitionTypes(path)).To(ConsistOf(rootX86, rootX86Verity))
	})

	It("builds an arm64 extension with arm64 partition types", func() {
		out := GinkgoT().TempDir()
		path, err := testartifacts.BuildSysext(ctx, testartifacts.SysextOptions{Dir: out, Name: "work", Arch: "arm64"})
		Expect(err).ToNot(HaveOccurred())
		Expect(partitionTypes(path)).To(ConsistOf(rootArm64, rootArm64Verity))
	})

	It("builds a plain squashfs extension with no partition table", func() {
		out := GinkgoT().TempDir()
		path, err := testartifacts.BuildPlainSquashfsSysext(ctx, out, "hello-broke")
		Expect(err).ToNot(HaveOccurred())
		Expect(path).To(Equal(filepath.Join(out, "hello-broke.sysext.raw")))
		data, err := os.ReadFile(path)
		Expect(err).ToNot(HaveOccurred())
		Expect(string(data[:4])).To(Equal("hsqs"))
	})

	It("puts the payload of a verity extension at /usr/bin/hello.sh", func() {
		out := GinkgoT().TempDir()
		path, err := testartifacts.BuildSysext(ctx, testartifacts.SysextOptions{Dir: out, Name: "work", Arch: "amd64"})
		Expect(err).ToNot(HaveOccurred())
		expectHelloPayload(extractRootPartition(ctx, path))
	})

	It("puts the payload of a plain squashfs extension at /usr/bin/hello.sh", func() {
		out := GinkgoT().TempDir()
		_, err := testartifacts.BuildPlainSquashfsSysext(ctx, out, "hello-broke")
		Expect(err).ToNot(HaveOccurred())
		runInAuroraBoot(ctx, out, "unsquashfs", "-d", "/work/tree", "/work/hello-broke.sysext.raw")
		expectHelloPayload(filepath.Join(out, "tree"))
	})
})
