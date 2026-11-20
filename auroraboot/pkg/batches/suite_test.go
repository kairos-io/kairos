package batches_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestBatches(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Batches Suite")
}
