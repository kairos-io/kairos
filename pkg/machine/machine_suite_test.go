package machine_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestInstaller(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Machine Suite")
}
