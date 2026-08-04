package bundled_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestBundled(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Bundled Suite")
}
