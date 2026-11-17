package gorm_test

import (
	"context"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/internal/secrets"
	gormstore "github.com/kairos-io/AuroraBoot/internal/store/gorm"
	"github.com/kairos-io/AuroraBoot/pkg/store"
)

var _ = Describe("BMCTarget password encryption at rest", func() {
	var (
		ctx    context.Context
		dbPath string
		key    []byte
	)

	BeforeEach(func() {
		ctx = context.Background()
		dbPath = filepath.Join(GinkgoT().TempDir(), "bmc.db")
		// Fixed 32-byte key so an independent cipher can decrypt the raw column.
		key = []byte("0123456789abcdef0123456789abcdef")
	})

	// newStore opens the temp-file DB with a cipher built from key.
	newStore := func() *gormstore.Store {
		c, err := secrets.NewCipher(key)
		Expect(err).NotTo(HaveOccurred())
		s, err := gormstore.New(dbPath)
		Expect(err).NotTo(HaveOccurred())
		return s.WithCipher(c)
	}

	It("never stores the plaintext password in the column and decrypts on read", func() {
		s := newStore()
		t := &store.BMCTarget{Name: "bmc1", Endpoint: "https://10.0.0.9", Username: "admin", Password: "s3cr3t-p@ss"}
		Expect(s.BMCTargetCreate(ctx, t)).To(Succeed())
		// The caller's struct keeps its plaintext (echoed back to the API).
		Expect(t.Password).To(Equal("s3cr3t-p@ss"))

		// Read back through the encrypting store: plaintext is recovered.
		got, err := s.BMCTargetGetByID(ctx, t.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Password).To(Equal("s3cr3t-p@ss"))

		// Open the SAME db file with NO cipher and confirm the stored column is
		// ciphertext, never the plaintext.
		raw, err := gormstore.New(dbPath)
		Expect(err).NotTo(HaveOccurred())
		rawTarget, err := raw.BMCTargetGetByID(ctx, t.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(rawTarget.Password).NotTo(Equal("s3cr3t-p@ss"))
		Expect(rawTarget.Password).NotTo(BeEmpty())

		// The ciphertext decrypts back to the original with the right key.
		c, err := secrets.NewCipher(key)
		Expect(err).NotTo(HaveOccurred())
		dec, err := c.Decrypt(rawTarget.Password)
		Expect(err).NotTo(HaveOccurred())
		Expect(dec).To(Equal("s3cr3t-p@ss"))
	})

	It("encrypts updated passwords too", func() {
		s := newStore()
		t := &store.BMCTarget{Name: "bmc1", Endpoint: "https://10.0.0.9", Username: "admin", Password: "first"}
		Expect(s.BMCTargetCreate(ctx, t)).To(Succeed())

		t.Password = "second"
		Expect(s.BMCTargetUpdate(ctx, t)).To(Succeed())

		got, err := s.BMCTargetGetByID(ctx, t.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Password).To(Equal("second"))

		raw, err := gormstore.New(dbPath)
		Expect(err).NotTo(HaveOccurred())
		rawTarget, err := raw.BMCTargetGetByID(ctx, t.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(rawTarget.Password).NotTo(Equal("second"))
	})

	It("stores an empty password as empty (no encrypted garbage)", func() {
		s := newStore()
		t := &store.BMCTarget{Name: "bmc1", Endpoint: "https://10.0.0.9", Username: "admin", Password: ""}
		Expect(s.BMCTargetCreate(ctx, t)).To(Succeed())

		raw, err := gormstore.New(dbPath)
		Expect(err).NotTo(HaveOccurred())
		rawTarget, err := raw.BMCTargetGetByID(ctx, t.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(rawTarget.Password).To(BeEmpty())
	})

	It("List decrypts every row", func() {
		s := newStore()
		Expect(s.BMCTargetCreate(ctx, &store.BMCTarget{Name: "a", Endpoint: "https://10.0.0.1", Password: "pa"})).To(Succeed())
		Expect(s.BMCTargetCreate(ctx, &store.BMCTarget{Name: "b", Endpoint: "https://10.0.0.2", Password: "pb"})).To(Succeed())

		list, err := s.BMCTargetList(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(list).To(HaveLen(2))
		pws := []string{list[0].Password, list[1].Password}
		Expect(pws).To(ConsistOf("pa", "pb"))
	})

	It("tolerates a legacy plaintext row (decrypt failure treated as plaintext)", func() {
		// Write a row with NO cipher: the column holds plaintext.
		plain, err := gormstore.New(dbPath)
		Expect(err).NotTo(HaveOccurred())
		t := &store.BMCTarget{Name: "legacy", Endpoint: "https://10.0.0.9", Password: "legacy-plain"}
		Expect(plain.BMCTargetCreate(ctx, t)).To(Succeed())

		// Read it back through the encrypting store: the bad-ciphertext decrypt
		// falls back to returning the stored value verbatim.
		s := newStore()
		got, err := s.BMCTargetGetByID(ctx, t.ID)
		Expect(err).NotTo(HaveOccurred())
		Expect(got.Password).To(Equal("legacy-plain"))
	})
})
