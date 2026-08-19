package phonehome_test

import (
	"testing"

	"github.com/kairos-io/kairos-agent/v2/internal/phonehome"
	sdkLogger "github.com/kairos-io/kairos-sdk/types/logger"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestPhoneHome(t *testing.T) {
	RegisterFailHandler(Fail)
	// Silence the package Logger for the whole suite — handler specs
	// otherwise dump "running: kairos-agent …" lines to stderr under
	// ginkgo, which is just noise.
	phonehome.SetLogger(sdkLogger.NewNullLogger())
	RunSpecs(t, "PhoneHome Suite")
}
