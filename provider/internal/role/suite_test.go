package role

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestRole(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Role Suite")
}
