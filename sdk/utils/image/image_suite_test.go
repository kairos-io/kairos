package image_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestImage(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Image utils Suite")
}
