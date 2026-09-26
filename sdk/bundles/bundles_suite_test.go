package bundles_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestBundles(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Bundles Suite")
}
