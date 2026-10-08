package token

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/kairos-io/kairos/v4/sdk/collector"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

const configWithToken = `#cloud-config

p2p:
  network_token: "OLDTOKEN"
`

// findInBackground runs FindYAMLWithKey on dir and reports what it returned.
// It runs in a goroutine so that a spec can tell "returned nothing" apart from
// "never returned", which is the point of the specs below: the bug is a
// blocking open, and a spec that called the function straight would hang with
// it rather than report it.
func findInBackground(dir string) []string {
	done := make(chan []string, 1)

	go func() {
		defer GinkgoRecover()
		found, err := FindYAMLWithKey("p2p.network_token", collector.Directories(dir))
		Expect(err).ToNot(HaveOccurred())
		done <- found
	}()

	var found []string
	Eventually(done, 10*time.Second).Should(Receive(&found))

	return found
}

var _ = Describe("FindYAMLWithKey", func() {
	var dir, config string

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		config = filepath.Join(dir, "config.yaml")
		Expect(os.WriteFile(config, []byte(configWithToken), 0600)).To(Succeed())
	})

	It("finds a config that carries the key", func() {
		Expect(findInBackground(dir)).To(ConsistOf(config))
	})

	It("leaves a config that does not carry the key out", func() {
		other := filepath.Join(dir, "other.yaml")
		Expect(os.WriteFile(other, []byte("#cloud-config\nhostname: node\n"), 0600)).To(Succeed())

		Expect(findInBackground(dir)).To(ConsistOf(config))
	})

	It("offers the reader the yaml candidates only", func() {
		// Grub modules, EFI binaries and kernels sit next to the configs on
		// the live media (kairos-io/kairos#2064). The walk used to hand every
		// one of them to the reader, which read it whole to find out it was
		// not yaml and printed a warning for it.
		Expect(os.WriteFile(filepath.Join(dir, "grubx64.efi"), []byte{0x4d, 0x5a}, 0600)).To(Succeed())
		yml := filepath.Join(dir, "other.yml")
		Expect(os.WriteFile(yml, []byte("a: b\n"), 0600)).To(Succeed())

		Expect(listFiles(dir)).To(ConsistOf(config, yml))
	})

	It("does not block on a named pipe that is named like a config", func() {
		// Named .yaml, so the extension filter hands it on and the open is
		// what has to refuse it. os.ReadFile parks here until a writer
		// arrives, and on a node none ever does.
		Expect(syscall.Mkfifo(filepath.Join(dir, "pipe.yaml"), 0600)).To(Succeed())

		Expect(findInBackground(dir)).To(ConsistOf(config))
	})

	It("skips a socket that is named like a config", func() {
		sock := filepath.Join(dir, "sock.yaml")
		listener, err := net.Listen("unix", sock)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(listener.Close)

		Expect(findInBackground(dir)).To(ConsistOf(config))
	})

	It("skips a file above the size a config can be, key or no key", func() {
		// A candidate is read whole into memory, so the bound is what keeps a
		// rotate-token pointed at the live media out of the squashfs. The file
		// here carries the key, so only the bound can leave it out.
		padding := strings.Repeat("# padding\n", maxConfigFileSize/10+1)
		big := filepath.Join(dir, "big.yaml")
		Expect(os.WriteFile(big, []byte(padding+configWithToken), 0600)).To(Succeed())

		Expect(findInBackground(dir)).To(ConsistOf(config))
	})
})
