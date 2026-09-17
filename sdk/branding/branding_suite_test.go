package branding

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestBranding(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Branding Suite")
}
