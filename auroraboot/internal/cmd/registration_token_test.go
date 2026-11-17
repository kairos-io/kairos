package cmd_test

import (
	"os"
	"path/filepath"

	cmdpkg "github.com/kairos-io/AuroraBoot/internal/cmd"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("ResolveRegistrationToken", Label("cmd", "registration-token"), func() {
	var (
		dir       string
		tokenPath string
		seedPath  string
	)

	BeforeEach(func() {
		dir = GinkgoT().TempDir()
		tokenPath = filepath.Join(dir, "registration-token")
		seedPath = filepath.Join(dir, "registration-token.seed")
	})

	fileContents := func(path string) string {
		data, err := os.ReadFile(path)
		Expect(err).NotTo(HaveOccurred())
		return string(data)
	}

	fileMissing := func(path string) bool {
		_, err := os.Stat(path)
		return os.IsNotExist(err)
	}

	When("no env value and no files exist", func() {
		It("generates a token and persists only the token file", func() {
			token, err := cmdpkg.ResolveRegistrationToken("", tokenPath, seedPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(token).NotTo(BeEmpty())
			Expect(fileContents(tokenPath)).To(Equal(token))
			Expect(fileMissing(seedPath)).To(BeTrue(), "seed marker must not be created without a seed")
		})
	})

	When("no env value but token file exists", func() {
		It("returns the file contents unchanged", func() {
			Expect(os.WriteFile(tokenPath, []byte("existing-token"), 0600)).To(Succeed())

			token, err := cmdpkg.ResolveRegistrationToken("", tokenPath, seedPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(token).To(Equal("existing-token"))
			Expect(fileContents(tokenPath)).To(Equal("existing-token"))
			Expect(fileMissing(seedPath)).To(BeTrue())
		})
	})

	When("first boot with an env value", func() {
		It("persists the token AND the seed marker", func() {
			token, err := cmdpkg.ResolveRegistrationToken("seed-1", tokenPath, seedPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(token).To(Equal("seed-1"))
			Expect(fileContents(tokenPath)).To(Equal("seed-1"))
			Expect(fileContents(seedPath)).To(Equal("seed-1"))
		})
	})

	When("the env value matches the recorded seed and the token was rotated at runtime", func() {
		It("keeps the runtime-rotated token", func() {
			Expect(os.WriteFile(seedPath, []byte("seed-1"), 0600)).To(Succeed())
			Expect(os.WriteFile(tokenPath, []byte("rotated-at-runtime"), 0600)).To(Succeed())

			token, err := cmdpkg.ResolveRegistrationToken("seed-1", tokenPath, seedPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(token).To(Equal("rotated-at-runtime"))
			Expect(fileContents(tokenPath)).To(Equal("rotated-at-runtime"))
			Expect(fileContents(seedPath)).To(Equal("seed-1"))
		})
	})

	When("the env value differs from the recorded seed", func() {
		It("treats the change as an operator-driven rotation and overwrites both files", func() {
			Expect(os.WriteFile(seedPath, []byte("seed-1"), 0600)).To(Succeed())
			Expect(os.WriteFile(tokenPath, []byte("rotated-at-runtime"), 0600)).To(Succeed())

			token, err := cmdpkg.ResolveRegistrationToken("seed-2", tokenPath, seedPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(token).To(Equal("seed-2"))
			Expect(fileContents(tokenPath)).To(Equal("seed-2"))
			Expect(fileContents(seedPath)).To(Equal("seed-2"))
		})
	})

	When("env value set but the token file is missing", func() {
		It("uses the env value and writes it back", func() {
			Expect(os.WriteFile(seedPath, []byte("seed-1"), 0600)).To(Succeed())

			token, err := cmdpkg.ResolveRegistrationToken("seed-1", tokenPath, seedPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(token).To(Equal("seed-1"))
			Expect(fileContents(tokenPath)).To(Equal("seed-1"))
		})
	})

	When("env value is removed after a previous run", func() {
		It("keeps the file value and does not touch the seed marker", func() {
			Expect(os.WriteFile(seedPath, []byte("seed-1"), 0600)).To(Succeed())
			Expect(os.WriteFile(tokenPath, []byte("still-in-use"), 0600)).To(Succeed())

			token, err := cmdpkg.ResolveRegistrationToken("", tokenPath, seedPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(token).To(Equal("still-in-use"))
			Expect(fileContents(seedPath)).To(Equal("seed-1"))
		})
	})

	When("the env value has trailing whitespace", func() {
		It("trims it before comparing to the seed marker", func() {
			Expect(os.WriteFile(seedPath, []byte("seed-1"), 0600)).To(Succeed())
			Expect(os.WriteFile(tokenPath, []byte("rotated-at-runtime"), 0600)).To(Succeed())

			token, err := cmdpkg.ResolveRegistrationToken("seed-1\n", tokenPath, seedPath)
			Expect(err).NotTo(HaveOccurred())
			Expect(token).To(Equal("rotated-at-runtime"), "trimmed env value must match the seed marker")
		})
	})
})
