package isoserve_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/kairos-io/AuroraBoot/pkg/isoserve"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("isoserve.Server", func() {
	var (
		srv     *isoserve.Server
		ts      *httptest.Server
		isoPath string
		payload []byte
	)

	BeforeEach(func() {
		dir := GinkgoT().TempDir()
		isoPath = filepath.Join(dir, "kairos.iso")
		payload = []byte(strings.Repeat("AURORABOOT-ISO-BYTES-", 512))
		Expect(os.WriteFile(isoPath, payload, 0644)).To(Succeed())

		// BaseURL is set after ts is up so Register produces a fetchable URL.
		srv = isoserve.New(isoserve.Config{})
		ts = httptest.NewServer(srv.Handler())
		// Re-create with the real base URL now that we know it.
		srv = isoserve.New(isoserve.Config{BaseURL: ts.URL})
		ts.Config.Handler = srv.Handler()
	})

	AfterEach(func() {
		ts.Close()
	})

	get := func(url string, headers map[string]string) (*http.Response, []byte) {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		Expect(err).NotTo(HaveOccurred())
		for k, v := range headers {
			req.Header.Set(k, v)
		}
		resp, err := http.DefaultClient.Do(req)
		Expect(err).NotTo(HaveOccurred())
		body, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		_ = resp.Body.Close()
		return resp, body
	}

	It("serves the exact bytes for a valid token", func() {
		url, token, err := srv.Register(isoPath, time.Minute)
		Expect(err).NotTo(HaveOccurred())
		Expect(token).NotTo(BeEmpty())
		Expect(url).To(HavePrefix(ts.URL + "/redfish/iso/"))
		Expect(url).To(HaveSuffix("/kairos.iso"))

		resp, body := get(url, nil)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(body).To(Equal(payload))
	})

	It("supports Range requests", func() {
		url, _, err := srv.Register(isoPath, time.Minute)
		Expect(err).NotTo(HaveOccurred())

		resp, body := get(url, map[string]string{"Range": "bytes=0-9"})
		Expect(resp.StatusCode).To(Equal(http.StatusPartialContent))
		Expect(body).To(Equal(payload[0:10]))
		Expect(resp.Header.Get("Content-Range")).To(Equal(fmt.Sprintf("bytes 0-9/%d", len(payload))))
	})

	It("returns 404 for a wrong/unknown token", func() {
		resp, _ := get(ts.URL+"/redfish/iso/not-a-real-token/kairos.iso", nil)
		Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
	})

	It("returns 404 once the token has expired", func() {
		// Two tokens, so that no assertion has to win a race against the TTL.
		// The long-lived one carries the "still serves while valid" half; a
		// slow first request cannot turn it into a 404.
		liveURL, _, err := srv.Register(isoPath, time.Minute)
		Expect(err).NotTo(HaveOccurred())

		resp, body := get(liveURL, nil)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(body).To(Equal(payload))

		// The short-lived one carries the expiry half. It is only ever expected
		// to 404, so the request latency does not matter.
		expiringURL, _, err := srv.Register(isoPath, 20*time.Millisecond)
		Expect(err).NotTo(HaveOccurred())

		Eventually(func() int {
			resp, _ := get(expiringURL, nil)
			return resp.StatusCode
		}, time.Second, 10*time.Millisecond).Should(Equal(http.StatusNotFound))
	})

	It("keeps serving an extended token past its original TTL", func() {
		url, token, err := srv.Register(isoPath, 50*time.Millisecond)
		Expect(err).NotTo(HaveOccurred())
		Expect(srv.Extend(token, time.Minute)).To(BeTrue())

		time.Sleep(100 * time.Millisecond)
		resp, body := get(url, nil)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(body).To(Equal(payload))
	})

	It("does not extend an unknown, revoked or expired token", func() {
		Expect(srv.Extend("not-a-real-token", time.Minute)).To(BeFalse())

		_, revoked, err := srv.Register(isoPath, time.Minute)
		Expect(err).NotTo(HaveOccurred())
		srv.Revoke(revoked)
		Expect(srv.Extend(revoked, time.Minute)).To(BeFalse())

		expiredURL, expired, err := srv.Register(isoPath, 10*time.Millisecond)
		Expect(err).NotTo(HaveOccurred())
		time.Sleep(50 * time.Millisecond)
		Expect(srv.Extend(expired, time.Minute)).To(BeFalse())
		resp, _ := get(expiredURL, nil)
		Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
	})

	It("returns 404 after a token is revoked", func() {
		url, token, err := srv.Register(isoPath, time.Minute)
		Expect(err).NotTo(HaveOccurred())

		resp, _ := get(url, nil)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))

		srv.Revoke(token)

		resp, _ = get(url, nil)
		Expect(resp.StatusCode).To(Equal(http.StatusNotFound))
	})

	It("cannot be escaped with a ../ path after the token", func() {
		// Plant a secret file alongside the ISO; a traversal attempt must not
		// reach it. The token binds exactly one file, so the path after it is
		// inert.
		secret := filepath.Join(filepath.Dir(isoPath), "secret.txt")
		Expect(os.WriteFile(secret, []byte("TOP-SECRET"), 0644)).To(Succeed())

		_, token, err := srv.Register(isoPath, time.Minute)
		Expect(err).NotTo(HaveOccurred())

		// Percent-encode the traversal so the Go client does not collapse it and
		// the server actually receives "../../secret.txt" after the token. The
		// server ignores everything after the token and serves the bound ISO; it
		// never touches the secret.
		escape := fmt.Sprintf("%s/redfish/iso/%s/%%2e%%2e/%%2e%%2e/secret.txt", ts.URL, token)
		resp, body := get(escape, nil)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(body).To(Equal(payload))
		Expect(body).NotTo(ContainSubstring("TOP-SECRET"))
	})

	It("builds the URL on the given advertised base and still serves the token", func() {
		url, token, err := srv.RegisterAt("https://iso.example.com/proxy/", isoPath, time.Minute)
		Expect(err).NotTo(HaveOccurred())
		Expect(url).To(Equal("https://iso.example.com/proxy/redfish/iso/" + token + "/kairos.iso"))

		resp, body := get(ts.URL+strings.TrimPrefix(url, "https://iso.example.com/proxy"), nil)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(body).To(Equal(payload))
	})

	It("falls back to the launch BaseURL when the advertised base is empty", func() {
		url, token, err := srv.RegisterAt("", isoPath, time.Minute)
		Expect(err).NotTo(HaveOccurred())
		Expect(url).To(Equal(ts.URL + "/redfish/iso/" + token + "/kairos.iso"))
	})

	It("rejects a non-absolute or non-existent path at registration", func() {
		_, _, err := srv.Register("relative/path.iso", time.Minute)
		Expect(err).To(HaveOccurred())

		_, _, err = srv.Register(filepath.Join(filepath.Dir(isoPath), "missing.iso"), time.Minute)
		Expect(err).To(HaveOccurred())

		_, _, err = srv.Register(isoPath, 0)
		Expect(err).To(HaveOccurred())
	})

	It("is safe under concurrent reads of one token", func() {
		url, _, err := srv.Register(isoPath, time.Minute)
		Expect(err).NotTo(HaveOccurred())

		const readers = 16
		var wg sync.WaitGroup
		errs := make(chan error, readers)
		for i := 0; i < readers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				resp, body := get(url, nil)
				if resp.StatusCode != http.StatusOK {
					errs <- fmt.Errorf("status %d", resp.StatusCode)
					return
				}
				if string(body) != string(payload) {
					errs <- fmt.Errorf("body mismatch")
				}
			}()
		}
		wg.Wait()
		close(errs)
		for e := range errs {
			Expect(e).NotTo(HaveOccurred())
		}
	})

	It("starts and shuts down its own listener", func() {
		lifeSrv := isoserve.New(isoserve.Config{BindAddr: "127.0.0.1:0"})
		Expect(lifeSrv.Start(context.Background())).To(Succeed())
		Expect(lifeSrv.Addr()).NotTo(BeNil())

		// Point BaseURL at the now-known address by re-deriving the URL manually.
		url, _, err := lifeSrv.Register(isoPath, time.Minute)
		Expect(err).NotTo(HaveOccurred())
		// BaseURL was empty, so the URL is host-relative; fetch via the listener.
		fetch := "http://" + lifeSrv.Addr().String() + url
		resp, body := get(fetch, nil)
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(body).To(Equal(payload))

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		Expect(lifeSrv.Shutdown(ctx)).To(Succeed())
	})

	It("requires a bind address to start", func() {
		bad := isoserve.New(isoserve.Config{})
		Expect(bad.Start(context.Background())).To(HaveOccurred())
	})

	// The token in the URL is the whole capability, so a half-configured TLS
	// pair must not quietly become a cleartext listener.
	DescribeTable("refuses to start on a half-configured TLS pair",
		func(cfg isoserve.Config, wants string) {
			half := isoserve.New(cfg)
			err := half.Start(context.Background())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(wants))
			Expect(half.UsesTLS()).To(BeFalse())
			// No listener was bound, so nothing needs shutting down.
			Expect(half.Addr()).To(BeNil())
		},
		Entry("certificate without key",
			isoserve.Config{BindAddr: "127.0.0.1:0", CertFile: "/tmp/cert.pem"},
			"certificate was given without a key"),
		Entry("key without certificate",
			isoserve.Config{BindAddr: "127.0.0.1:0", KeyFile: "/tmp/key.pem"},
			"key was given without a certificate"),
	)

	// freeAddr returns a loopback address nothing is listening on, so a test can
	// check afterwards that a failed Start left the port unbound.
	freeAddr := func() string {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		Expect(err).NotTo(HaveOccurred())
		addr := ln.Addr().String()
		Expect(ln.Close()).To(Succeed())
		return addr
	}

	DescribeTable("refuses to start when the TLS pair cannot be loaded",
		func(write func(dir string) (cert, key string)) {
			cert, key := write(GinkgoT().TempDir())
			addr := freeAddr()
			bad := isoserve.New(isoserve.Config{BindAddr: addr, CertFile: cert, KeyFile: key})
			err := bad.Start(context.Background())
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(cert))
			Expect(err.Error()).To(ContainSubstring(key))
			Expect(bad.Addr()).To(BeNil())

			conn, dialErr := net.DialTimeout("tcp", addr, time.Second)
			if dialErr == nil {
				_ = conn.Close()
			}
			Expect(dialErr).To(HaveOccurred(), "nothing should be listening on %s", addr)
		},
		Entry("missing files", func(dir string) (string, string) {
			return filepath.Join(dir, "missing-cert.pem"), filepath.Join(dir, "missing-key.pem")
		}),
		Entry("garbage files", func(dir string) (string, string) {
			cert := filepath.Join(dir, "cert.pem")
			key := filepath.Join(dir, "key.pem")
			Expect(os.WriteFile(cert, []byte("not a certificate"), 0600)).To(Succeed())
			Expect(os.WriteFile(key, []byte("not a key"), 0600)).To(Succeed())
			return cert, key
		}),
	)

	It("serves over TLS with a valid certificate and key", func() {
		dir := GinkgoT().TempDir()
		cert, key, pool := writeSelfSigned(dir)

		tlsSrv := isoserve.New(isoserve.Config{BindAddr: "127.0.0.1:0", CertFile: cert, KeyFile: key})
		Expect(tlsSrv.Start(context.Background())).To(Succeed())
		defer func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			Expect(tlsSrv.Shutdown(ctx)).To(Succeed())
		}()
		Expect(tlsSrv.UsesTLS()).To(BeTrue())

		url, _, err := tlsSrv.Register(isoPath, time.Minute)
		Expect(err).NotTo(HaveOccurred())
		client := &http.Client{
			Timeout:   5 * time.Second,
			Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, ServerName: "127.0.0.1"}},
		}
		resp, err := client.Get("https://" + tlsSrv.Addr().String() + url)
		Expect(err).NotTo(HaveOccurred())
		body, err := io.ReadAll(resp.Body)
		Expect(err).NotTo(HaveOccurred())
		_ = resp.Body.Close()
		Expect(resp.StatusCode).To(Equal(http.StatusOK))
		Expect(body).To(Equal(payload))
	})

	It("still serves plain HTTP when neither TLS file is given", func() {
		plain := isoserve.New(isoserve.Config{BindAddr: "127.0.0.1:0"})
		Expect(plain.Start(context.Background())).To(Succeed())
		Expect(plain.UsesTLS()).To(BeFalse())

		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		Expect(plain.Shutdown(ctx)).To(Succeed())
	})
})

// writeSelfSigned writes a self-signed certificate for 127.0.0.1 and its key
// into dir, and returns their paths plus a pool that trusts the certificate.
func writeSelfSigned(dir string) (certPath, keyPath string, pool *x509.CertPool) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	Expect(err).NotTo(HaveOccurred())
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "isoserve-test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &priv.PublicKey, priv)
	Expect(err).NotTo(HaveOccurred())
	keyDER, err := x509.MarshalECPrivateKey(priv)
	Expect(err).NotTo(HaveOccurred())

	certPath = filepath.Join(dir, "cert.pem")
	keyPath = filepath.Join(dir, "key.pem")
	Expect(os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0600)).To(Succeed())
	Expect(os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0600)).To(Succeed())

	parsed, err := x509.ParseCertificate(der)
	Expect(err).NotTo(HaveOccurred())
	pool = x509.NewCertPool()
	pool.AddCert(parsed)
	return certPath, keyPath, pool
}
