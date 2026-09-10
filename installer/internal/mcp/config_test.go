package mcp

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/kairos-io/kairos/v4/sdk/branding"
)

// The listener has no other off-switch on a real boot: kairos-agent execs the
// installer with a fixed argument list, so an operator only ever reaches it
// through this config.
func TestListenAddressFor(t *testing.T) {
	for _, tc := range []struct {
		name string
		mcp  branding.MCP
		want string
	}{
		{
			name: "nothing configured keeps the default",
			want: "127.0.0.1:8090",
		},
		{name: "disable means do not listen", mcp: branding.MCP{Disable: true}, want: ""},
		{
			name: "a listen address moves the listener",
			mcp:  branding.MCP{ListenAddress: ":8090"},
			want: ":8090",
		},
		{
			name: "disable wins over an address that is also set",
			mcp:  branding.MCP{Disable: true, ListenAddress: "0.0.0.0:9999"},
			want: "",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := listenAddressFor(tc.mcp); got != tc.want {
				t.Errorf("listenAddressFor(%+v) = %q, want %q", tc.mcp, got, tc.want)
			}
		})
	}
}

// The knobs are only worth anything if the key an operator writes is the key
// that is read, so this goes through a real agent.yaml rather than the struct.
func TestListenAddressFromAnAgentConfigFile(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{
			name: "disable",
			body: "mcp:\n  disable: true\n",
			want: "",
		},
		{
			name: "listen address",
			body: "mcp:\n  listen_address: \":8090\"\n",
			want: ":8090",
		},
		// The exact case the loopback default exists for: this operator turned
		// the unauthenticated network installer off, and must not get another
		// one on a new port.
		{
			name: "webui disabled and nothing said about mcp stays on loopback",
			body: "webui:\n  disable: true\n",
			want: "127.0.0.1:8090",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "agent.yaml")
			if err := os.WriteFile(path, []byte(tc.body), 0o644); err != nil {
				t.Fatal(err)
			}

			if got := ListenAddressFromConfig(path); got != tc.want {
				t.Errorf("address = %q, want %q for:\n%s", got, tc.want, tc.body)
			}
		})
	}
}

// An unbranded live image has no agent.yaml at all, which is the normal case
// and must not be read as "do not listen".
func TestListenAddressWithNoConfigFileAtAll(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "there-is-no-agent.yaml")

	if got := ListenAddressFromConfig(missing); got != "127.0.0.1:8090" {
		t.Errorf("address = %q, want the loopback default", got)
	}
}
