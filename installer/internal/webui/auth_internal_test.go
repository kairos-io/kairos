package webui

import (
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/net/websocket"

	"github.com/kairos-io/kairos/v4/sdk/branding"
)

var _ = Describe("the token the image can put in front of the web UI", func() {
	const token = "s3cr3t token/with+specials"

	// every route the server answers on, so a new one cannot be added
	// outside the check without this failing.
	routes := []struct {
		name string
		do   func(*http.Client, string, http.Header) *http.Response
	}{
		{"the form", get("/")},
		{"an asset", get("/index.html")},
		{"a path nothing serves", get("/nope")},
		{"validate", postForm("/validate", url.Values{"cloud-config": {"#cloud-config"}})},
		{"install", postForm("/install", url.Values{"installation-device": {"/dev/null"}})},
	}

	Describe("with no token set", func() {
		var srv *httptest.Server

		BeforeEach(func() {
			srv = httptest.NewServer(newServer(Options{}))
			DeferCleanup(srv.Close)
		})

		for _, r := range routes {
			It("serves "+r.name+" to anyone", func() {
				resp := r.do(noRedirect(), srv.URL, nil)
				Expect(resp.StatusCode).ToNot(Equal(http.StatusUnauthorized))
			})
		}
	})

	Describe("with a token set", func() {
		var srv *httptest.Server

		BeforeEach(func() {
			srv = httptest.NewServer(newServer(Options{WebUI: branding.WebUI{Token: token}}))
			DeferCleanup(srv.Close)
		})

		for _, r := range routes {
			r := r
			It("refuses "+r.name+" without one", func() {
				resp := r.do(noRedirect(), srv.URL, nil)
				Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
			})

			It("refuses "+r.name+" with the wrong one", func() {
				resp := r.do(noRedirect(), srv.URL, bearer("not the token"))
				Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
			})

			It("serves "+r.name+" to a bearer token", func() {
				resp := r.do(noRedirect(), srv.URL, bearer(token))
				Expect(resp.StatusCode).ToNot(Equal(http.StatusUnauthorized))
			})
		}

		It("refuses the progress websocket without one", func() {
			_, err := websocket.Dial(wsURLOf(srv), "", srv.URL)
			Expect(err).To(HaveOccurred())
		})

		It("accepts the token in the query parameter the printed URL carries", func() {
			resp := get("/")(noRedirect(), srv.URL+"/?"+branding.TokenParam+"="+url.QueryEscape(token), nil)
			Expect(resp.StatusCode).To(Equal(http.StatusOK))
		})

		It("refuses a query parameter that is not the token", func() {
			resp := get("/")(noRedirect(), srv.URL+"/?"+branding.TokenParam+"=wrong", nil)
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		})

		It("lets the pages the printed URL leads to follow on, through the cookie", func() {
			jar := withJar()

			// The URL off the QR code. Everything after it is what the
			// browser does on its own, with no token to carry.
			first := get("/")(jar, srv.URL+"/?"+branding.TokenParam+"="+url.QueryEscape(token), nil)
			Expect(first.StatusCode).To(Equal(http.StatusOK))

			follow := get("/")(jar, srv.URL, nil)
			Expect(follow.StatusCode).To(Equal(http.StatusOK))

			submit := postForm("/validate", url.Values{"cloud-config": {"#cloud-config"}})(jar, srv.URL, nil)
			Expect(submit.StatusCode).To(Equal(http.StatusOK))
		})

		It("refuses a forged cookie", func() {
			c := noRedirect()
			req, err := http.NewRequest(http.MethodGet, srv.URL+"/", nil)
			Expect(err).ToNot(HaveOccurred())
			req.AddCookie(&http.Cookie{Name: tokenCookie, Value: "not the token"})
			resp, err := c.Do(req)
			Expect(err).ToNot(HaveOccurred())
			DeferCleanup(resp.Body.Close)
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
		})

		It("says nothing about the machine in the refusal", func() {
			resp := get("/")(noRedirect(), srv.URL, nil)
			Expect(resp.StatusCode).To(Equal(http.StatusUnauthorized))
			Expect(resp.ContentLength).To(BeNumerically("<=", 0))
		})

		It("streams progress once the browser is through the cookie", func() {
			GinkgoT().Setenv("KAIROS_AGENT_BIN", stubAgent(`echo '{"event":"done"}'`))

			jar := withJar()
			Expect(get("/")(jar, srv.URL+"/?"+branding.TokenParam+"="+url.QueryEscape(token), nil).StatusCode).
				To(Equal(http.StatusOK))

			resp := postForm("/install", url.Values{"installation-device": {"/dev/null"}})(jar, srv.URL, nil)
			Expect(resp.StatusCode).To(Equal(http.StatusSeeOther))

			cfg, err := websocket.NewConfig(wsURLOf(srv), srv.URL)
			Expect(err).ToNot(HaveOccurred())
			u, err := url.Parse(srv.URL)
			Expect(err).ToNot(HaveOccurred())
			for _, ck := range jar.Jar.Cookies(u) {
				cfg.Header.Add("Cookie", ck.String())
			}
			conn, err := websocket.DialConfig(cfg)
			Expect(err).ToNot(HaveOccurred())
			defer conn.Close()

			var raw string
			Expect(websocket.Message.Receive(conn, &raw)).To(Succeed())
			var m Message
			Expect(json.Unmarshal([]byte(raw), &m)).To(Succeed())
			Expect(m.Type).ToNot(Equal(MessageError))
		})
	})
})

func wsURLOf(srv *httptest.Server) string {
	return "ws" + strings.TrimPrefix(srv.URL, "http") + "/ws"
}

func noRedirect() *http.Client {
	return &http.Client{
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}

func withJar() *http.Client {
	c := noRedirect()
	jar, err := cookiejar.New(nil)
	Expect(err).ToNot(HaveOccurred())
	c.Jar = jar
	return c
}

func bearer(t string) http.Header {
	return http.Header{"Authorization": {"Bearer " + t}}
}

func get(path string) func(*http.Client, string, http.Header) *http.Response {
	return func(c *http.Client, base string, h http.Header) *http.Response {
		target := base
		if !strings.Contains(base, "?") {
			target = base + path
		}
		req, err := http.NewRequest(http.MethodGet, target, nil)
		Expect(err).ToNot(HaveOccurred())
		for k, v := range h {
			req.Header[k] = v
		}
		resp, err := c.Do(req)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(resp.Body.Close)
		return resp
	}
}

func postForm(path string, form url.Values) func(*http.Client, string, http.Header) *http.Response {
	return func(c *http.Client, base string, h http.Header) *http.Response {
		req, err := http.NewRequest(http.MethodPost, base+path, strings.NewReader(form.Encode()))
		Expect(err).ToNot(HaveOccurred())
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		for k, v := range h {
			req.Header[k] = v
		}
		resp, err := c.Do(req)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(resp.Body.Close)
		return resp
	}
}
