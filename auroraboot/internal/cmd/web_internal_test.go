package cmd

import (
	"bytes"
	"strings"
	"testing"
)

// TestServeWithDecision verifies the TLS-vs-plaintext decision in serveWith:
// HTTPS only when BOTH cert and key are set, plaintext (with a prominent
// warning) otherwise. The actual Echo Start/StartTLS calls are stubbed so the
// test exercises only the decision and the warning.
func TestServeWithDecision(t *testing.T) {
	tests := []struct {
		name        string
		cert        string
		key         string
		wantTLS     bool
		wantWarning bool
	}{
		{name: "cert and key set -> TLS", cert: "cert.pem", key: "key.pem", wantTLS: true, wantWarning: false},
		{name: "only cert set -> plaintext", cert: "cert.pem", key: "", wantTLS: false, wantWarning: true},
		{name: "only key set -> plaintext", cert: "", key: "key.pem", wantTLS: false, wantWarning: true},
		{name: "neither set -> plaintext", cert: "", key: "", wantTLS: false, wantWarning: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var tlsCalled, plainCalled bool
			var w bytes.Buffer

			err := serveWith(&w, ":8080", tt.cert, tt.key,
				func() error { tlsCalled = true; return nil },
				func() error { plainCalled = true; return nil })
			if err != nil {
				t.Fatalf("serveWith returned error: %v", err)
			}

			if tlsCalled != tt.wantTLS {
				t.Errorf("startTLS called = %v, want %v", tlsCalled, tt.wantTLS)
			}
			if plainCalled == tt.wantTLS {
				t.Errorf("startPlain called = %v, want %v", plainCalled, !tt.wantTLS)
			}

			gotWarning := strings.Contains(w.String(), "WARNING") &&
				strings.Contains(w.String(), "UNENCRYPTED")
			if gotWarning != tt.wantWarning {
				t.Errorf("plaintext warning emitted = %v, want %v; output:\n%s", gotWarning, tt.wantWarning, w.String())
			}
		})
	}
}
