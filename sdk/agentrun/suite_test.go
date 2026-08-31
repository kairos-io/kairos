package agentrun_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestAgentRun(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "AgentRun test suite")
}
