package isoserve_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestIsoserve(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Isoserve Suite")
}
