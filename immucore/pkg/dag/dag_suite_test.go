package dag

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestDagSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "immucore dag suite")
}
