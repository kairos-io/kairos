package main

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestKairosInit(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "kairos-init Suite")
}
