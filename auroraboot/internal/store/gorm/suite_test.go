package gorm_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestGormStore(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Gorm Store Suite")
}
