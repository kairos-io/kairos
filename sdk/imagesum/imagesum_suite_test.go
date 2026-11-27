package imagesum_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestImageSum(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Imagesum suite")
}
