package tui

import (
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/tredoe/osutil/user/crypt/sha512_crypt"
)

var _ = Describe("installer passwords", func() {
	It("creates distinct SHA-512 crypt hashes that verify", func() {
		first, err := hashPassword("correct horse battery staple")
		Expect(err).NotTo(HaveOccurred())
		second, err := hashPassword("correct horse battery staple")
		Expect(err).NotTo(HaveOccurred())

		Expect(first).To(HavePrefix("$6$"))
		Expect(second).To(HavePrefix("$6$"))
		Expect(second).NotTo(Equal(first))
		Expect(sha512_crypt.New().Verify(first, []byte("correct horse battery staple"))).To(Succeed())
	})

	It("stores only the hash, clears input, and navigates after hashing", func() {
		oldHasher := passwordHasher
		DeferCleanup(func() { passwordHasher = oldHasher })
		passwordHasher = func(password string) (string, error) {
			Expect(password).To(Equal("cleartext-sentinel"))
			return "$6$salt$hash", nil
		}
		mainModel = Model{username: "old-user", passwordHash: "$6$old$hash"}
		page := newUserPasswordPage()
		page.usernameInput.SetValue("kairos")
		page.passwordInput.SetValue("cleartext-sentinel")

		_, cmd := page.Update(tea.KeyMsg{Type: tea.KeyEnter})

		Expect(mainModel.username).To(Equal("kairos"))
		Expect(mainModel.passwordHash).To(Equal("$6$salt$hash"))
		Expect(page.passwordInput.Value()).To(BeEmpty())
		Expect(page.View()).NotTo(ContainSubstring("cleartext-sentinel"))
		Expect(cmd()).To(Equal(GoToPageMsg{PageID: "customization"}))
	})

	It("keeps model and input unchanged and shows an error when hashing fails", func() {
		oldHasher := passwordHasher
		DeferCleanup(func() { passwordHasher = oldHasher })
		passwordHasher = func(string) (string, error) { return "", errors.New("random source failed") }
		mainModel = Model{username: "old-user", passwordHash: "$6$old$hash"}
		page := newUserPasswordPage()
		page.usernameInput.SetValue("kairos")
		page.passwordInput.SetValue("cleartext-sentinel")

		_, cmd := page.Update(tea.KeyMsg{Type: tea.KeyEnter})

		Expect(cmd).To(BeNil())
		Expect(mainModel.username).To(Equal("old-user"))
		Expect(mainModel.passwordHash).To(Equal("$6$old$hash"))
		Expect(page.passwordInput.Value()).To(Equal("cleartext-sentinel"))
		Expect(strings.ToLower(page.View())).To(ContainSubstring("could not hash password"))
	})
})
