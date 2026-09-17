package ciguard_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestCIGuard(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CI Guard Suite")
}
