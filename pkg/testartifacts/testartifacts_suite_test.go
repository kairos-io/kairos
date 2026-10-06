package testartifacts_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestTestArtifacts(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "testartifacts suite")
}
