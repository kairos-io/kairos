package schema_test

import (
	"strings"

	. "github.com/kairos-io/kairos/v4/sdk/schema"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Users Schema", func() {
	var config *KConfig
	var err error
	var yaml string

	JustBeforeEach(func() {
		config, err = NewConfigFromYAML(yaml, UserSchema{})
		Expect(err).ToNot(HaveOccurred())
	})

	Context("When a user has no name", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
passwd: foobar`
		})

		It("errors", func() {
			Expect(config.IsValid()).NotTo(BeTrue())
			Expect(config.ValidationError.Error()).To(MatchRegexp("missing properties: 'name'"))
		})
	})

	Context("When a user name doesn't fit the pattern", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
name: "007"
passwd: "bond"`
		})

		It("errors", func() {
			Expect(config.IsValid()).NotTo(BeTrue())
			Expect(
				strings.Contains(config.ValidationError.Error(),
					"does not match pattern '^[a-z_][a-z0-9_-]{0,31}$'",
				),
			).To(BeTrue())
		})
	})

	Context("When a user name only contains a valid piece", func() {
		BeforeEach(func() {
			yaml = "#cloud-config\nname: kairos"
		})

		for _, name := range []string{"Bad User!", "root;rm -rf /", "ADMIN", strings.Repeat("a", 33)} {
			name := name
			It("rejects "+name, func() {
				config, err = NewConfigFromYAML("#cloud-config\nname: \""+name+"\"\npasswd: foobar", UserSchema{})
				Expect(err).ToNot(HaveOccurred())
				Expect(config.IsValid()).To(BeFalse())
			})
		}

		for _, name := range []string{"kairos", "_svc", "kairos-user", "user_1", strings.Repeat("a", 32)} {
			name := name
			It("accepts "+name, func() {
				config, err = NewConfigFromYAML("#cloud-config\nname: "+name+"\npasswd: foobar", UserSchema{})
				Expect(err).ToNot(HaveOccurred())
				Expect(config.IsValid()).To(BeTrue())
			})
		}
	})

	Context("With only the required attributes", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
name: "kairos"`
		})

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})

	Context("With lock_passwd", func() {
		It("accepts a boolean", func() {
			config, err = NewConfigFromYAML("#cloud-config\nname: kairos\nlock_passwd: true", UserSchema{})
			Expect(err).ToNot(HaveOccurred())
			Expect(config.IsValid()).To(BeTrue())
		})

		It("rejects a value that is not a boolean", func() {
			config, err = NewConfigFromYAML("#cloud-config\nname: kairos\nlock_passwd: \"nope\"", UserSchema{})
			Expect(err).ToNot(HaveOccurred())
			Expect(config.IsValid()).To(BeFalse())
			Expect(config.ValidationError.Error()).To(ContainSubstring("lock_passwd"))
		})

		It("reports lockPasswd as an unknown key that means lock_passwd", func() {
			config, err = NewConfigFromYAML("#cloud-config\nname: kairos\nlockPasswd: true", UserSchema{})
			Expect(err).ToNot(HaveOccurred())
			Expect(config.SuggestUnknownKeys()).To(ConsistOf(
				`unknown configuration key "lockPasswd", which is ignored; did you mean "lock_passwd"?`,
			))
		})
	})

	Context("With all possible attributes", func() {
		BeforeEach(func() {
			yaml = `#cloud-config
name: "kairos"
passwd: "kairos"
lock_passwd: true
groups: 
 - "admin"
ssh_authorized_keys:
  - github:mudler`
		})

		It("succeeds", func() {
			Expect(config.IsValid()).To(BeTrue())
		})
	})
})
