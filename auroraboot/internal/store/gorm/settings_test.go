package gorm_test

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
)

var _ = Describe("settingStore", func() {
	var (
		ctx context.Context
		s   *gormstore.Store
	)

	BeforeEach(func() {
		ctx = context.Background()
		var err error
		s, err = gormstore.New(":memory:")
		Expect(err).NotTo(HaveOccurred())
	})

	It("reports a missing key as not-found without erroring", func() {
		v, found, err := s.SettingGet(ctx, "imageSource.defaultImageURL")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeFalse())
		Expect(v).To(BeEmpty())
	})

	It("sets and reads back a value", func() {
		Expect(s.SettingSet(ctx, "imageSource.defaultImageURL", "https://10.0.0.5/os.iso")).To(Succeed())

		v, found, err := s.SettingGet(ctx, "imageSource.defaultImageURL")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(v).To(Equal("https://10.0.0.5/os.iso"))
	})

	It("upserts on a repeated Set (last write wins)", func() {
		Expect(s.SettingSet(ctx, "imageSource.localServeEnabled", "false")).To(Succeed())
		Expect(s.SettingSet(ctx, "imageSource.localServeEnabled", "true")).To(Succeed())

		v, found, err := s.SettingGet(ctx, "imageSource.localServeEnabled")
		Expect(err).NotTo(HaveOccurred())
		Expect(found).To(BeTrue())
		Expect(v).To(Equal("true"))
	})

	It("returns every setting from GetAll", func() {
		Expect(s.SettingSet(ctx, "a", "1")).To(Succeed())
		Expect(s.SettingSet(ctx, "b", "2")).To(Succeed())

		all, err := s.SettingGetAll(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(all).To(HaveKeyWithValue("a", "1"))
		Expect(all).To(HaveKeyWithValue("b", "2"))
	})

	It("returns an empty map when no settings exist", func() {
		all, err := s.SettingGetAll(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(all).To(BeEmpty())
	})
})
