package diffupgrade_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestDiffUpgrade(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Diff upgrade Suite")
}
