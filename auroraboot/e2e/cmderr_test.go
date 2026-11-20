package e2e_test

import (
	"errors"
	"os/exec"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("withOutput", Label("e2e"), func() {
	// A real *exec.ExitError, because that is what the helpers return and its
	// own Error() is the useless "exit status 1" this is meant to replace.
	failed := func() error {
		return exec.Command("sh", "-c", "exit 1").Run()
	}

	It("returns nil when the command succeeded", func() {
		Expect(withOutput("docker pull foo", "some output", nil)).To(BeNil())
	})

	It("names the command and quotes the output", func() {
		err := withOutput("docker pull quay.io/kairos/fedora:40", "Error response from daemon: manifest unknown", failed())
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("docker pull quay.io/kairos/fedora:40"))
		Expect(err.Error()).To(ContainSubstring("manifest unknown"))
		Expect(err.Error()).To(ContainSubstring("exit status 1"))
	})

	It("keeps the cause unwrappable", func() {
		cause := failed()
		Expect(errors.Is(withOutput("cmd", "out", cause), cause)).To(BeTrue())
	})

	It("still names the command when the output is empty", func() {
		err := withOutput("docker pull foo", "  \n\t ", failed())
		Expect(err.Error()).To(Equal("docker pull foo: exit status 1"))
	})
})
