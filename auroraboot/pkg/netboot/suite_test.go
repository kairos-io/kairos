package netboot

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestNetboot(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Netboot test suite")
}
