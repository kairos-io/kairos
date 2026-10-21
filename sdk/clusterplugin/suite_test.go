package clusterplugin

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestClusterPlugin(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Cluster Plugin Suite")
}
