package system

import (
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("setGRUBEnv", func() {
	It("sets every option", func() {
		got := map[string]string{}
		err := setGRUBEnv(map[string]string{"a": "1", "b": "2"}, func(k, v string) (string, error) {
			got[k] = v
			return "", nil
		})
		Expect(err).ToNot(HaveOccurred())
		Expect(got).To(Equal(map[string]string{"a": "1", "b": "2"}))
	})

	It("returns the failures and still tries the other options", func() {
		var tried []string
		err := setGRUBEnv(map[string]string{"bad": "1", "good": "2"}, func(k, v string) (string, error) {
			tried = append(tried, k)
			if k == "bad" {
				return "grub-editenv: error: cannot open grubenv\n", errors.New("exit status 1")
			}
			return "", nil
		})
		Expect(err).To(MatchError(ContainSubstring(`could not set boot option "bad"`)))
		Expect(err).To(MatchError(ContainSubstring("cannot open grubenv")))
		Expect(err).ToNot(MatchError(ContainSubstring(`"good"`)))
		Expect(tried).To(ConsistOf("bad", "good"))
	})
})
