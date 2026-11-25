package config_test

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	pkgConfig "github.com/kairos-io/kairos/v4/agent/pkg/config"
	"github.com/kairos-io/kairos/v4/sdk/collector"
	"github.com/kairos-io/kairos/v4/sdk/imagesum"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("image-checksum config", func() {
	scan := func(cloudConfig, cmdline string) imagesum.Config {
		cmd := filepath.Join(GinkgoT().TempDir(), "cmdline")
		Expect(os.WriteFile(cmd, []byte(cmdline), 0o600)).To(Succeed())
		c, err := pkgConfig.ScanNoLogs(
			collector.Readers(strings.NewReader(cloudConfig)),
			collector.MergeBootLine,
			collector.WithBootCMDLineFile(cmd),
		)
		Expect(err).ToNot(HaveOccurred())
		return c.ImageChecksum
	}

	It("is off and does not verify by default", func() {
		c := scan("#cloud-config\n", "root=LABEL=COS_ACTIVE")
		Expect(c.CreateEnabled()).To(BeFalse())
		Expect(c.Verify).To(BeEmpty())
		Expect(c.Policy().Active()).To(BeFalse())
	})

	It("reads the cloud config block", func() {
		c := scan("#cloud-config\nimage-checksum:\n  create: true\n  verify: warn\n  require: true\n", "")
		Expect(c.CreateEnabled()).To(BeTrue())
		Expect(c.Verify).To(Equal(imagesum.Warn))
		Expect(c.RequireEnabled()).To(BeTrue())
	})

	It("reads the keys from the kernel cmdline in dot notation", func() {
		c := scan("#cloud-config\n", "root=LABEL=COS_ACTIVE image-checksum.create=true image-checksum.verify=fail")
		Expect(c.CreateEnabled()).To(BeTrue())
		Expect(c.Verify).To(Equal(imagesum.Fail))
	})

	It("reads the cmdline keys in a scan that does not merge the whole cmdline, like upgrade and reset", func() {
		cmd := filepath.Join(GinkgoT().TempDir(), "cmdline")
		Expect(os.WriteFile(cmd, []byte("image-checksum.create=true image-checksum.verify=warn"), 0o600)).To(Succeed())
		c, err := pkgConfig.ScanNoLogs(
			collector.Readers(strings.NewReader("#cloud-config\n")),
			collector.WithBootCMDLineFile(cmd),
		)
		Expect(err).ToNot(HaveOccurred())
		Expect(c.ImageChecksum.CreateEnabled()).To(BeTrue())
		Expect(c.ImageChecksum.Verify).To(Equal(imagesum.Warn))
	})

	It("reads verify-timeout from the cloud config and lets the cmdline override it, 0 included", func() {
		c := scan("#cloud-config\nimage-checksum:\n  verify: warn\n  verify-timeout: 120\n", "")
		Expect(c.VerifyBudget()).To(Equal(120 * time.Second))
		c = scan("#cloud-config\nimage-checksum:\n  verify-timeout: 120\n", "image-checksum.verify-timeout=0")
		Expect(c.VerifyTimeout).ToNot(BeNil())
		Expect(c.VerifyBudget()).To(BeZero())
		c = scan("#cloud-config\n", "")
		Expect(c.VerifyBudget()).To(Equal(imagesum.DefaultVerifyTimeout))
	})

	It("does not read the old rd.immucore spellings", func() {
		c := scan("#cloud-config\n", "rd.immucore.verify_image=fail rd.immucore.verify_image_require")
		Expect(c.Verify).To(BeEmpty())
		Expect(c.RequireEnabled()).To(BeFalse())
	})

	It("lets the cmdline override the cloud config", func() {
		c := scan("#cloud-config\nimage-checksum:\n  create: true\n  verify: fail\n", "image-checksum.create=false image-checksum.verify=none")
		Expect(c.CreateEnabled()).To(BeFalse())
		Expect(c.Create).ToNot(BeNil(), "an explicit false must survive the cmdline filter")
		Expect(c.Verify).To(Equal(imagesum.None))
	})
})
