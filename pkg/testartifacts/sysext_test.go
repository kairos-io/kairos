package testartifacts_test

import (
	"github.com/kairos-io/kairos/v4/pkg/testartifacts"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("BuildSysext", func() {
	It("rejects a key without a certificate", func() {
		_, err := testartifacts.BuildSysext(GinkgoT().Context(), testartifacts.SysextOptions{Dir: GinkgoT().TempDir(), Name: "work", Arch: "amd64", KeyFile: "/nonexistent.key"})
		Expect(err).To(MatchError(ContainSubstring("KeyFile and CertFile")))
	})
})
