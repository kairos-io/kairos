package config

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/twpayne/go-vfs/v5/vfst"
)

// openFDs counts this process's open descriptors, which is how the spec below
// sees a file that was opened and never closed.
func openFDs() int {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		Skip("no /proc/self/fd on this system: " + err.Error())
	}

	return len(entries)
}

var _ = Describe("kairosVersion", func() {
	var fs *vfst.TestFS
	var cleanup func()

	newFS := func(files map[string]interface{}) {
		var err error
		fs, cleanup, err = vfst.NewTestFS(files)
		Expect(err).ToNot(HaveOccurred())
	}

	AfterEach(func() {
		if cleanup != nil {
			cleanup()
			cleanup = nil
		}
	})

	It("reads the version from kairos-release", func() {
		// kairos-init writes KAIROS_VERSION to kairos-release only, so
		// reading os-release alone finds nothing on any image it built.
		newFS(map[string]interface{}{
			"/etc/kairos-release": "KAIROS_ID=\"kairos\"\nKAIROS_VERSION=\"v3.7.2\"\n",
			"/etc/os-release":     "NAME=\"Ubuntu\"\nID=ubuntu\nVERSION_ID=\"24.04\"\n",
		})

		Expect(kairosVersion(fs, kairosReleaseFile, osReleaseFile)).To(Equal("v3.7.2"))
	})

	It("falls back to os-release", func() {
		// An image that predates kairos-release carries the key there.
		newFS(map[string]interface{}{
			"/etc/os-release": "NAME=\"Ubuntu\"\nKAIROS_VERSION=\"v2.4.3\"\n",
		})

		Expect(kairosVersion(fs, kairosReleaseFile, osReleaseFile)).To(Equal("v2.4.3"))
	})

	It("skips a file that does not carry the key", func() {
		// kairos-release exists but says nothing about the version, so the
		// fallback still has to be consulted.
		newFS(map[string]interface{}{
			"/etc/kairos-release": "KAIROS_ID=\"kairos\"\n",
			"/etc/os-release":     "NAME=\"Ubuntu\"\nKAIROS_VERSION=\"v2.4.3\"\n",
		})

		Expect(kairosVersion(fs, kairosReleaseFile, osReleaseFile)).To(Equal("v2.4.3"))
	})

	It("reports nothing when neither file exists", func() {
		// That is an answer of its own, not a failure: the version only
		// feeds a log line.
		newFS(map[string]interface{}{"/etc/hosts": "127.0.0.1 localhost\n"})

		Expect(kairosVersion(fs, kairosReleaseFile, osReleaseFile)).To(BeEmpty())
	})

	It("closes what it opens", func() {
		// Every path is opened and none carries the key, so this is the
		// call that opens the most files.
		newFS(map[string]interface{}{
			"/etc/kairos-release": "KAIROS_ID=\"kairos\"\n",
			"/etc/os-release":     "NAME=\"Ubuntu\"\n",
		})

		before := openFDs()
		for i := 0; i < 50; i++ {
			kairosVersion(fs, kairosReleaseFile, osReleaseFile)
		}

		Expect(openFDs()).To(BeNumerically("<=", before))
	})
})
