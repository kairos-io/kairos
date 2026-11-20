package ws_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestWS(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "WebSocket Suite")
}
