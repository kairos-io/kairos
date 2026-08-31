package fsutils_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestFs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "FsUtils test suite")
}
