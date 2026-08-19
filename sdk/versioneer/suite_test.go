package versioneer_test

import (
	"encoding/json"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestConfig(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Versioneer Suite")
}

func getFakeTags() []string {
	var fakeTags []string
	// To regenerate this file, just remove the inspector from the artifact
	// below and let the default inspector query the quay.io repository.
	tagsJSON, err := os.ReadFile("assets/test_tags.json")
	Expect(err).ToNot(HaveOccurred())
	err = json.Unmarshal(tagsJSON, &fakeTags)
	Expect(err).ToNot(HaveOccurred())

	return fakeTags
}
