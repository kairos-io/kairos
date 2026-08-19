package webui_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestWebUISuite(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "WebUI test suite")
}

