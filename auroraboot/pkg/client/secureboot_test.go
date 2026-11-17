package client_test

import (
	"context"
	"net/http"
	"net/http/httptest"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/client"
)

var _ = Describe("SecureBootService", func() {
	It("lists key sets with their TPM PCR key flag", func() {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.Method).To(Equal(http.MethodGet))
			Expect(r.URL.Path).To(Equal("/api/v1/secureboot-keys"))
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`[{"id":"ks-1","name":"prod","hasTpmPcrKey":true,"secureBootEnroll":"if-safe"}]`))
		}))
		DeferCleanup(server.Close)

		cli := client.New(server.URL, client.WithAdminPassword("s3cret"))
		sets, err := cli.SecureBoot.List(context.Background())
		Expect(err).NotTo(HaveOccurred())
		Expect(sets).To(HaveLen(1))
		Expect(sets[0].Name).To(Equal("prod"))
		Expect(sets[0].HasTPMPCRKey).To(BeTrue())
	})
})
