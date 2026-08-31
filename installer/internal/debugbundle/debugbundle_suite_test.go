package debugbundle_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestDebugBundle(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DebugBundle Suite")
}
