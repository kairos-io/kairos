package secureboot

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSecureboot(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "pkg/secureboot suite")
}
