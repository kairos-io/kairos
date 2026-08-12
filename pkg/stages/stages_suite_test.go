package stages_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestStages(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Stages Suite")
}
