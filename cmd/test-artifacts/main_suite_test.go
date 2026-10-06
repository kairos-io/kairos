package main

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestTestArtifactsCLI(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "test-artifacts CLI suite")
}
