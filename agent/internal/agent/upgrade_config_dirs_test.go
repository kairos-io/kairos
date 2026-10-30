package agent

import (
	"os"

	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("hostConfigDirs", Label("upgrade", "k8s"), func() {
	// saveAndUnset stores the current value of an env var and unsets it, then
	// restores the original state (set or unset) after the spec.
	saveAndUnset := func(key string) {
		orig, present := os.LookupEnv(key)
		Expect(os.Unsetenv(key)).To(Succeed())
		DeferCleanup(func() {
			if present {
				Expect(os.Setenv(key, orig)).To(Succeed())
			} else {
				Expect(os.Unsetenv(key)).To(Succeed())
			}
		})
	}

	BeforeEach(func() {
		saveAndUnset("KUBERNETES_SERVICE_HOST")
		saveAndUnset("HOST_DIR")
	})

	// The assertions below are on the whole slice, not on ContainElement. The
	// defect this guards against added entries rather than losing them: the
	// list the upgrade scanned held one empty string per real directory, and
	// an empty scan directory is dropped by the collector without a word, so
	// nothing downstream could report it.
	It("returns one entry per directory, unchanged, outside kubernetes", func() {
		Expect(hostConfigDirs([]string{"/oem", "/etc/kairos"})).
			To(Equal([]string{"/oem", "/etc/kairos"}))
	})

	It("returns one entry per directory, under the host prefix, in a pod", func() {
		Expect(os.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")).To(Succeed())

		Expect(hostConfigDirs([]string{"/oem", "/etc/kairos"})).
			To(Equal([]string{"/host/oem", "/host/etc/kairos"}))
	})

	It("honours HOST_DIR when the pod mounts the node somewhere else", func() {
		Expect(os.Setenv("KUBERNETES_SERVICE_HOST", "10.0.0.1")).To(Succeed())
		Expect(os.Setenv("HOST_DIR", "/custom-host")).To(Succeed())

		Expect(hostConfigDirs([]string{"/oem"})).To(Equal([]string{"/custom-host/oem"}))
	})

	// What Upgrade actually passes. Four directories in, four out: the
	// upgrade must not scan a path nobody configured.
	It("scans exactly the directories the agent is configured to read", func() {
		dirs := constants.GetUserConfigDirs()

		Expect(hostConfigDirs(dirs)).To(Equal(dirs))
	})

	It("returns an empty list for an empty list", func() {
		Expect(hostConfigDirs(nil)).To(BeEmpty())
	})
})
