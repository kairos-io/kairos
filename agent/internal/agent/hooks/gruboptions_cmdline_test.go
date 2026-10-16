package hook

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/kairos-io/kairos/v4/sdk/collector"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

// scanValues runs the real collector over a cloud-config written to a
// temporary directory, so the nested maps extractKcryptCmdline reads have the
// types the collector produces at install time rather than hand built ones.
func scanValues(cloudConfig string) collector.ConfigValues {
	GinkgoHelper()

	dir := GinkgoT().TempDir()
	Expect(os.WriteFile(filepath.Join(dir, "kairos.yaml"), []byte(cloudConfig), 0644)).To(Succeed())

	o := &collector.Options{NoLogs: true}
	Expect(o.Apply(collector.Directories(dir))).To(Succeed())

	c, err := collector.Scan(o, func(d []byte) ([]byte, error) { return d, nil })
	Expect(err).ToNot(HaveOccurred())

	return c.Values
}

// scanCmdline runs the collector the way sdk/kcrypt runs it on an encrypted
// OEM: no configuration directory to read, only the kernel command line.
func scanCmdline(cmdline string) collector.ConfigValues {
	GinkgoHelper()

	dir := GinkgoT().TempDir()
	path := filepath.Join(dir, "cmdline")
	Expect(os.WriteFile(path, []byte(cmdline), 0644)).To(Succeed())

	o := &collector.Options{NoLogs: true, MergeBootCMDLine: true}
	Expect(o.Apply(
		collector.Directories(GinkgoT().TempDir()),
		collector.WithBootCMDLineFile(path),
	)).To(Succeed())

	c, err := collector.Scan(o, func(d []byte) ([]byte, error) { return d, nil })
	Expect(err).ToNot(HaveOccurred())

	return c.Values
}

func configWith(values collector.ConfigValues) *sdkConfig.Config {
	return &sdkConfig.Config{
		Logger:    sdkLogger.NewNullLogger(),
		Collector: collector.Config{Values: values},
	}
}

var _ = Describe("extractKcryptCmdline", func() {
	It("writes the local TPM settings under the keys the kcrypt reader uses", func() {
		values := scanValues(`#cloud-config
kcrypt:
  nv_index: "0x1500000"
  c_index: "0x1500001"
  tpm_device: "/dev/tpmrm1"
  challenger:
    challenger_server: "https://challenger.example.org"
    mdns: true
`)

		Expect(strings.Fields(extractKcryptCmdline(configWith(values)))).To(ConsistOf(
			"kcrypt.challenger.challenger_server=https://challenger.example.org",
			"kcrypt.challenger.mdns=true",
			"kcrypt.nv_index=0x1500000",
			"kcrypt.c_index=0x1500001",
			"kcrypt.tpm_device=/dev/tpmrm1",
		))
	})

	It("carries the settings back to where sdk/kcrypt reads them", func() {
		cmdline := extractKcryptCmdline(configWith(scanValues(`#cloud-config
kcrypt:
  nv_index: "0x1500000"
  c_index: "0x1500001"
  tpm_device: "/dev/tpmrm1"
  challenger:
    challenger_server: "https://challenger.example.org"
`)))

		// extractKcryptConfigFromCollector in sdk/kcrypt/config.go reads the
		// TPM settings from the top level of the kcrypt block and the server
		// from kcrypt.challenger. This is that exact shape, rebuilt from the
		// command line alone, the way a node with an encrypted OEM sees it.
		kcrypt, ok := scanCmdline(cmdline)["kcrypt"].(collector.ConfigValues)
		Expect(ok).To(BeTrue())
		Expect(kcrypt["nv_index"]).To(Equal("0x1500000"))
		Expect(kcrypt["c_index"]).To(Equal("0x1500001"))
		Expect(kcrypt["tpm_device"]).To(Equal("/dev/tpmrm1"))

		challenger, ok := kcrypt["challenger"].(collector.ConfigValues)
		Expect(ok).To(BeTrue())
		Expect(challenger["challenger_server"]).To(Equal("https://challenger.example.org"))
	})

	It("emits the TPM settings of a node that has no challenger server", func() {
		values := scanValues(`#cloud-config
kcrypt:
  tpm_device: "/dev/tpmrm1"
`)

		Expect(extractKcryptCmdline(configWith(values))).To(Equal("kcrypt.tpm_device=/dev/tpmrm1"))
	})

	It("ignores TPM settings written inside the challenger block", func() {
		// kcrypt.challenger declares challenger_server, mdns and certificate
		// and nothing else, so these three are read by no one and must not be
		// mistaken for configuration.
		values := scanValues(`#cloud-config
kcrypt:
  challenger:
    challenger_server: "https://challenger.example.org"
    nv_index: "0x1500000"
    c_index: "0x1500001"
    tpm_device: "/dev/tpmrm1"
`)

		Expect(extractKcryptCmdline(configWith(values))).To(
			Equal("kcrypt.challenger.challenger_server=https://challenger.example.org"))
	})

	It("writes nothing when there is no kcrypt block", func() {
		Expect(extractKcryptCmdline(configWith(scanValues("#cloud-config\ndebug: true\n")))).To(BeEmpty())
	})
})
