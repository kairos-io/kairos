package tui

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestTUI(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "TUI Suite")
}
