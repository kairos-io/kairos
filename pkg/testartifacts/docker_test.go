package testartifacts_test

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"os"
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

var _ = Describe("Docker-built artifacts", Label("docker"), func() {
	var ctx context.Context

	BeforeEach(func() {
		if !testartifacts.DockerAvailable() {
			Fail("these specs build artifacts with Docker, and no Docker daemon is reachable")
		}
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(context.Background(), 10*time.Minute)
		DeferCleanup(cancel)
	})

	It("generates a Secure Boot and TPM PCR key set the caller can read", func() {
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

	It("rejects a key without a certificate", func() {
		_, err := testartifacts.BuildSysext(ctx, testartifacts.SysextOptions{Dir: GinkgoT().TempDir(), Name: "work", Arch: "amd64", KeyFile: "/nonexistent.key"})
		Expect(err).To(HaveOccurred())
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
})
