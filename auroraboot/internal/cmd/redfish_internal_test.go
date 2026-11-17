package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kairos-io/AuroraBoot/pkg/redfish"
)

func TestResolveBootMode(t *testing.T) {
	tests := []struct {
		name      string
		flag      string
		want      redfish.BootMode
		wantErr   bool
		errSubstr string
	}{
		{name: "empty leaves the current mode (no override)", flag: "", want: ""},
		{name: "whitespace-only is treated as empty", flag: "  ", want: ""},
		{name: "uefi lowercase", flag: "uefi", want: redfish.BootModeUEFI},
		{name: "uefi mixed case", flag: "UeFi", want: redfish.BootModeUEFI},
		{name: "legacy lowercase", flag: "legacy", want: redfish.BootModeLegacy},
		{name: "legacy mixed case", flag: "Legacy", want: redfish.BootModeLegacy},
		{name: "invalid value errors", flag: "bios", wantErr: true, errSubstr: "invalid --boot-mode"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := resolveBootMode(tc.flag)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got mode %q", got)
				}
				if tc.errSubstr != "" && !strings.Contains(err.Error(), tc.errSubstr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestDeriveServeBindAddr(t *testing.T) {
	tests := []struct {
		name      string
		serveURL  string
		want      string
		wantErr   bool
		errSubstr string
	}{
		{name: "host and port", serveURL: "http://10.0.0.5:8090", want: "10.0.0.5:8090"},
		{name: "https host and port", serveURL: "https://10.0.0.5:8443", want: "10.0.0.5:8443"},
		{name: "no port errors actionably", serveURL: "http://10.0.0.5", wantErr: true, errSubstr: "set --redfish-serve-addr explicitly, e.g. 10.0.0.5:8090"},
		{name: "no host errors", serveURL: "http:///path", wantErr: true, errSubstr: "has no host"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := deriveServeBindAddr(tc.serveURL)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got addr %q", got)
				}
				if tc.errSubstr != "" && !strings.Contains(err.Error(), tc.errSubstr) {
					t.Fatalf("error %q does not contain %q", err.Error(), tc.errSubstr)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestResolveRedfishPassword(t *testing.T) {
	dir := t.TempDir()
	pwFile := filepath.Join(dir, "pw")
	if err := os.WriteFile(pwFile, []byte("from-file\n"), 0600); err != nil {
		t.Fatalf("write pw file: %v", err)
	}
	emptyFile := filepath.Join(dir, "empty")
	if err := os.WriteFile(emptyFile, []byte("\n"), 0600); err != nil {
		t.Fatalf("write empty file: %v", err)
	}

	tests := []struct {
		name         string
		flag         string
		file         string
		env          string
		stdin        bool
		stdinContent string
		want         string
		wantErr      bool
	}{
		{name: "flag wins over everything", flag: "from-flag", file: pwFile, env: "from-env", stdin: true, stdinContent: "from-stdin", want: "from-flag"},
		{name: "file beats env and stdin", file: pwFile, env: "from-env", stdin: true, stdinContent: "from-stdin", want: "from-file"},
		{name: "env beats stdin", env: "from-env", stdin: true, stdinContent: "from-stdin", want: "from-env"},
		{name: "stdin last resort", stdin: true, stdinContent: "from-stdin\n", want: "from-stdin"},
		{name: "none provided errors", wantErr: true},
		{name: "empty file errors", file: emptyFile, wantErr: true},
		{name: "empty stdin errors", stdin: true, stdinContent: "", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.env != "" {
				t.Setenv(redfishPasswordEnv, tc.env)
			} else {
				// Ensure no ambient value leaks in; t.Setenv restores on cleanup.
				t.Setenv(redfishPasswordEnv, "")
				_ = os.Unsetenv(redfishPasswordEnv)
			}

			got, err := resolveRedfishPassword(tc.flag, tc.file, tc.stdin, strings.NewReader(tc.stdinContent))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got password %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestValidateServeTLSFlags(t *testing.T) {
	tests := []struct {
		name      string
		serveTLS  bool
		cert      string
		key       string
		serveURL  string
		errSubstr string
	}{
		{
			name:     "plain HTTP serve with an http URL",
			serveURL: "http://10.0.0.5:8090",
		},
		{
			name:     "TLS serve with a cert pair and an https URL",
			serveTLS: true,
			cert:     "/etc/ssl/bmc.crt",
			key:      "/etc/ssl/bmc.key",
			serveURL: "https://10.0.0.5:8090",
		},
		{
			name:      "--serve-tls without a cert",
			serveTLS:  true,
			key:       "/etc/ssl/bmc.key",
			serveURL:  "https://10.0.0.5:8090",
			errSubstr: "requires --serve-tls-cert and --serve-tls-key",
		},
		{
			name:      "--serve-tls without a key",
			serveTLS:  true,
			cert:      "/etc/ssl/bmc.crt",
			serveURL:  "https://10.0.0.5:8090",
			errSubstr: "requires --serve-tls-cert and --serve-tls-key",
		},
		{
			name:      "a cert pair without --serve-tls",
			cert:      "/etc/ssl/bmc.crt",
			key:       "/etc/ssl/bmc.key",
			serveURL:  "https://10.0.0.5:8090",
			errSubstr: "without --serve-tls",
		},
		{
			name:      "--serve-tls with an http serve URL",
			serveTLS:  true,
			cert:      "/etc/ssl/bmc.crt",
			key:       "/etc/ssl/bmc.key",
			serveURL:  "http://10.0.0.5:8090",
			errSubstr: "is not https",
		},
		{
			name:      "an https serve URL with no certificate",
			serveURL:  "https://10.0.0.5:8090",
			errSubstr: "has no certificate",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateServeTLSFlags(tc.serveTLS, tc.cert, tc.key, tc.serveURL)
			if tc.errSubstr == "" {
				if err != nil {
					t.Fatalf("validateServeTLSFlags() = %v, want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("validateServeTLSFlags() = nil, want an error containing %q", tc.errSubstr)
			}
			if !strings.Contains(err.Error(), tc.errSubstr) {
				t.Fatalf("validateServeTLSFlags() = %q, want it to contain %q", err.Error(), tc.errSubstr)
			}
		})
	}
}
