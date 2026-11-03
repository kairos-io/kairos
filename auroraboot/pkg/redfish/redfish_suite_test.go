package redfish_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestRedfish(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Redfish Suite")
}
