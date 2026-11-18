package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kairos-io/AuroraBoot/pkg/isoserve"
)

// TestCheckServeURLScheme covers the launch-time guard that keeps the scheme
// advertised to a BMC and the ISO server's transport from being chosen
// independently. Both disagreeing directions must be refused: the request the
// deploy handler later builds takes the URL from one and the InsertMedia
// transfer protocol from the other, so either direction emits a request that
// contradicts itself.
func TestCheckServeURLScheme(t *testing.T) {
	tests := []struct {
		name      string
		serveURL  string
		source    string
		usesTLS   bool
		wantErr   bool
		errSubstr string
	}{
		{name: "http advertised, no TLS material", serveURL: "http://10.0.0.5:8090", usesTLS: false},
		{name: "https advertised, TLS material", serveURL: "https://10.0.0.5:8090", usesTLS: true},
		{
			// redfish.md documents --redfish-serve-tls-cert/key as activating HTTPS
			// on the ISO server, while its worked example advertises an http:// URL.
			// Following both is this case.
			name:      "TLS material with the documented http example",
			serveURL:  "http://10.0.0.5:8090",
			usesTLS:   true,
			wantErr:   true,
			errSubstr: "--redfish-serve-tls-cert",
		},
		{
			name:      "https advertised with no TLS material",
			serveURL:  "https://10.0.0.5:8090",
			usesTLS:   false,
			wantErr:   true,
			errSubstr: "would serve cleartext under an https:// URL",
		},
		{
			// The fallback to --url, which web.go generates as http://<host><listen>
			// when --url is unset. With TLS flags set this is the same mismatch, and
			// the error must name --url rather than a flag the operator never passed.
			name:      "fallback --url reported as its own source",
			serveURL:  "http://myhost:8080",
			source:    "--url (--redfish-serve-url is not set)",
			usesTLS:   true,
			wantErr:   true,
			errSubstr: "--url (--redfish-serve-url is not set)",
		},
		{name: "scheme a BMC cannot fetch", serveURL: "ftp://10.0.0.5:8090", wantErr: true, errSubstr: "must use http or https"},
		{name: "no scheme at all", serveURL: "10.0.0.5/iso", wantErr: true, errSubstr: "must use http or https"},
		// A bare host:port does not reach the scheme switch: url.Parse rejects it
		// ("first path segment in URL cannot contain colon"). Still refused, and
		// still naming the flag, which is what matters at launch.
		{name: "bare host:port is a parse failure", serveURL: "10.0.0.5:8090", wantErr: true, errSubstr: "parsing --redfish-serve-url"},
		{name: "unparseable URL", serveURL: "http://[::1", wantErr: true, errSubstr: "parsing"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			source := tc.source
			if source == "" {
				source = "--redfish-serve-url"
			}

			err := checkServeURLScheme(tc.serveURL, source, tc.usesTLS)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("checkServeURLScheme(%q, tls=%v) = nil, want an error", tc.serveURL, tc.usesTLS)
				}
				if !strings.Contains(err.Error(), tc.errSubstr) {
					t.Fatalf("error %q does not mention %q", err.Error(), tc.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("checkServeURLScheme(%q, tls=%v) = %v, want nil", tc.serveURL, tc.usesTLS, err)
			}
		})
	}
}

// TestCheckServeURLSchemeMatchesServedURL ties the guard to the value it
// protects rather than to its own wording. isoserve.Server.Register mints the
// URL the BMC fetches, and UsesTLS reports the protocol the deploy handler
// advertises for it; whenever the guard passes, those two must agree. This
// fails if Register stops deriving the scheme from BaseURL, or if UsesTLS stops
// tracking the cert/key pair, either of which would make the guard check
// something other than what ships.
func TestCheckServeURLSchemeMatchesServedURL(t *testing.T) {
	iso := filepath.Join(t.TempDir(), "artifact.iso")
	if err := os.WriteFile(iso, []byte("iso"), 0o600); err != nil {
		t.Fatalf("writing fixture ISO: %v", err)
	}

	tests := []struct {
		name     string
		serveURL string
		cert     string
		key      string
	}{
		{name: "plain", serveURL: "http://10.0.0.5:8090"},
		{name: "tls", serveURL: "https://10.0.0.5:8090", cert: "cert.pem", key: "key.pem"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			usesTLS := tc.cert != "" && tc.key != ""
			if err := checkServeURLScheme(tc.serveURL, "--redfish-serve-url", usesTLS); err != nil {
				t.Fatalf("guard rejected a consistent configuration: %v", err)
			}

			srv := isoserve.New(isoserve.Config{
				BaseURL:  tc.serveURL,
				BindAddr: "127.0.0.1:0",
				CertFile: tc.cert,
				KeyFile:  tc.key,
			})
			servedURL, _, err := srv.Register(iso, time.Minute)
			if err != nil {
				t.Fatalf("Register: %v", err)
			}

			// This is exactly the pairing pkg/handlers/deploy.go sends: the URL from
			// Register, the protocol from UsesTLS.
			advertisesHTTPS := strings.HasPrefix(servedURL, "https://")
			if advertisesHTTPS != srv.UsesTLS() {
				t.Fatalf("served URL %q advertises https=%v but UsesTLS()=%v; the guard let an inconsistent pair through",
					servedURL, advertisesHTTPS, srv.UsesTLS())
			}
		})
	}
}

// TestServedURLAndTransportDisagreeWithoutTheGuard pins the defect
// checkServeURLScheme exists to prevent. Without a launch-time check, a
// configuration the flags accept today produces a served URL whose scheme and a
// transfer protocol that contradict each other, and nothing between here and
// the BMC reconciles them. If this ever stops holding, the guard is no longer
// load-bearing and should be reconsidered rather than kept out of habit.
func TestServedURLAndTransportDisagreeWithoutTheGuard(t *testing.T) {
	iso := filepath.Join(t.TempDir(), "artifact.iso")
	if err := os.WriteFile(iso, []byte("iso"), 0o600); err != nil {
		t.Fatalf("writing fixture ISO: %v", err)
	}

	// --redfish-serve-url https://... with no --redfish-serve-tls-cert/key.
	srv := isoserve.New(isoserve.Config{
		BaseURL:  "https://10.0.0.5:8090",
		BindAddr: "127.0.0.1:0",
	})
	servedURL, _, err := srv.Register(iso, time.Minute)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	if !strings.HasPrefix(servedURL, "https://") {
		t.Fatalf("served URL %q does not take its scheme from BaseURL any more", servedURL)
	}
	if srv.UsesTLS() {
		t.Fatalf("UsesTLS() is true with no cert or key")
	}
	// The BMC would be handed an https:// URL with TransferProtocolType HTTP.
	if err := checkServeURLScheme("https://10.0.0.5:8090", "--redfish-serve-url", srv.UsesTLS()); err == nil {
		t.Fatal("checkServeURLScheme accepted the configuration that produces this contradiction")
	}
}
