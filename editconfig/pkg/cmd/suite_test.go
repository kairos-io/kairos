package cmd

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestEditConfigCmdSuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "edit-config cmd Suite")
}
