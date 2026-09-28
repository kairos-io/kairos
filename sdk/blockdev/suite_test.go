package blockdev

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestBlockdev(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Blockdev Suite")
}
