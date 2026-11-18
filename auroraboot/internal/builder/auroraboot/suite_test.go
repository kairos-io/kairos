package auroraboot_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAuroraBootBuilder(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AuroraBoot Builder Suite")
}
