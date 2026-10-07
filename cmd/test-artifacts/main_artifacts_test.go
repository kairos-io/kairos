//go:build testartifacts

package main

import (
	"context"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("test-artifacts with the AuroraBoot binary", Label("testartifacts"), func() {
	It("generates a key set", func() {
		out := GinkgoT().TempDir()
		Expect(run(context.Background(), []string{"keys", "--out", out})).To(Succeed())
		Expect(filepath.Join(out, "db.key")).To(BeARegularFile())
		Expect(filepath.Join(out, "tpm2-pcr-private.pem")).To(BeARegularFile())
	})
})
