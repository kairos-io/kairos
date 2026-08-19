package attestation_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAttestation(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Attestation Suite")
}
