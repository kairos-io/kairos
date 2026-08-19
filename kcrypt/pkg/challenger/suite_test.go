package challenger_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestChallenger(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Kcrypt challenger suite")
}
