package server_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/AuroraBoot/pkg/server"
)

var _ = Describe("Client IP", func() {
	newServer := func(trusted ...string) *server.Config {
		nets, err := server.ParseTrustedProxies(trusted)
		Expect(err).NotTo(HaveOccurred())
		return &server.Config{
			NodeStore:      &fakeNodeStore{},
			CommandStore:   &fakeCommandStore{},
			GroupStore:     &fakeGroupStore{},
			Builder:        &fakeBuilder{},
			AdminPassword:  "admin-pass",
			RegToken:       "reg-token",
			AuroraBootURL:  "http://localhost:8080",
			TrustedProxies: nets,
		}
	}

	realIP := func(cfg *server.Config, remoteAddr string, headers map[string]string) string {
		e := server.New(*cfg)
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.RemoteAddr = remoteAddr
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		return e.NewContext(req, httptest.NewRecorder()).RealIP()
	}

	It("ignores a client supplied X-Forwarded-For and X-Real-IP by default", func() {
		ip := realIP(newServer(), "10.1.2.3:4567", map[string]string{
			"X-Forwarded-For": "198.51.100.7",
			"X-Real-IP":       "198.51.100.8",
		})
		Expect(ip).To(Equal("10.1.2.3"))
	})

	It("takes the client from X-Forwarded-For when the peer is a trusted proxy", func() {
		ip := realIP(newServer("10.0.0.0/8"), "10.1.2.3:4567", map[string]string{
			"X-Forwarded-For": "198.51.100.7",
		})
		Expect(ip).To(Equal("198.51.100.7"))
	})

	It("ignores X-Forwarded-For from a peer outside the trusted proxies", func() {
		ip := realIP(newServer("10.0.0.0/8"), "192.168.1.9:4567", map[string]string{
			"X-Forwarded-For": "198.51.100.7",
		})
		Expect(ip).To(Equal("192.168.1.9"))
	})

	It("does not let a client prepend a spoofed hop through a trusted proxy", func() {
		ip := realIP(newServer("10.1.2.3"), "10.1.2.3:4567", map[string]string{
			"X-Forwarded-For": "203.0.113.1, 198.51.100.7",
		})
		Expect(ip).To(Equal("198.51.100.7"))
	})

	It("keeps a client rotating X-Forwarded-For in one registration bucket", func() {
		cfg := newServer()
		cfg.RegisterRateLimitRPS = 0.001
		cfg.RegisterRateLimitBurst = 2
		ts := httptest.NewServer(server.New(*cfg))
		defer ts.Close()

		body := `{"registrationToken":"wrong","machineID":"m1","hostname":"h1"}`
		codes := make([]int, 0, 5)
		for i := 0; i < 5; i++ {
			req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/nodes/register", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Forwarded-For", fmt.Sprintf("198.51.100.%d", i+1))
			resp, err := http.DefaultClient.Do(req)
			Expect(err).NotTo(HaveOccurred())
			resp.Body.Close()
			codes = append(codes, resp.StatusCode)
		}
		Expect(codes).To(ContainElement(http.StatusTooManyRequests))
	})

	DescribeTable("parsing --trusted-proxy",
		func(in []string, want []string, wantErr bool) {
			nets, err := server.ParseTrustedProxies(in)
			if wantErr {
				Expect(err).To(HaveOccurred())
				return
			}
			Expect(err).NotTo(HaveOccurred())
			got := make([]string, 0, len(nets))
			for _, n := range nets {
				got = append(got, n.String())
			}
			Expect(got).To(Equal(want))
		},
		Entry("empty", nil, []string{}, false),
		Entry("CIDR", []string{"10.0.0.0/8"}, []string{"10.0.0.0/8"}, false),
		Entry("bare IPv4", []string{"10.1.2.3"}, []string{"10.1.2.3/32"}, false),
		Entry("bare IPv6", []string{"fd00::1"}, []string{"fd00::1/128"}, false),
		Entry("blank entries skipped", []string{" ", "192.168.0.0/16"}, []string{"192.168.0.0/16"}, false),
		Entry("garbage", []string{"proxy.local"}, nil, true),
	)
})
