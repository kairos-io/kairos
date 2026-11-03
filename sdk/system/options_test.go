package system_test

import (
	"errors"
	"testing"

	"github.com/kairos-io/kairos/v4/sdk/system"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSystem(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "System Suite")
}

// queue is a test Option that adds f to the changeset.
func queue(f func() error) system.Option {
	return func(c *system.Changeset) error {
		c.Add(f)
		return nil
	}
}

var _ = Describe("Apply", func() {
	It("returns nil when every change succeeds", func() {
		// multierror.Append always hands back a non-nil *multierror.Error, so
		// assigning it straight to an error interface used to report "0 errors
		// occurred" as a failure on a changeset where nothing failed.
		Expect(system.Apply(queue(func() error { return nil }))).To(BeNil())
	})

	It("returns nil when there is nothing to do", func() {
		Expect(system.Apply()).To(BeNil())
	})

	It("reports every change that failed", func() {
		err := system.Apply(
			queue(func() error { return errors.New("first change failed") }),
			queue(func() error { return nil }),
			queue(func() error { return errors.New("third change failed") }),
		)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("2 errors occurred"))
		Expect(err.Error()).To(ContainSubstring("first change failed"))
		Expect(err.Error()).To(ContainSubstring("third change failed"))
	})

	It("stops at the first option that cannot be applied", func() {
		applied := false
		err := system.Apply(
			func(c *system.Changeset) error { return errors.New("bad option") },
			queue(func() error { applied = true; return nil }),
		)
		Expect(err).To(MatchError(ContainSubstring("bad option")))
		Expect(applied).To(BeFalse())
	})
})
