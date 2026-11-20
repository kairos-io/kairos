package uki

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestUki(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "pkg/uki suite")
}
