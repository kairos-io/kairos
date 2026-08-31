package prereqs_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPrereqs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Prereqs Suite")
}
