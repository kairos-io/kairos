package ops

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestOps(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Ops test suite")
}
