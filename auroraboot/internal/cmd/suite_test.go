package cmd_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestWhitebox(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CLI test suite")
}
