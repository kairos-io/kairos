package sshimage_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestSSHImage(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "SSH image source Suite")
}
