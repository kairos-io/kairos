package provider

import "testing"

func listenerOptionsForTest(apiAddress string, userEnv map[string]string) map[string]string {
	opts := map[string]string{"APILISTEN": normalizeAPIAddress(apiAddress)}
	applyAPIListenerEnv(opts, userEnv)
	return opts
}

func TestDefaultEdgeVPNListenerUsesSecureUnixSocket(t *testing.T) {
	opts := listenerOptionsForTest("", nil)
	if opts["APILISTEN"] != DefaultEdgeVPNAPIAddress {
		t.Fatalf("APILISTEN = %q, want %q", opts["APILISTEN"], DefaultEdgeVPNAPIAddress)
	}
	if opts["APILISTENUNIXMODE"] != DefaultEdgeVPNAPISocketMode {
		t.Fatalf("APILISTENUNIXMODE = %q, want %q", opts["APILISTENUNIXMODE"], DefaultEdgeVPNAPISocketMode)
	}
}

func TestListenerModeFollowsFinalAPListenOverride(t *testing.T) {
	t.Run("TCP to Unix", func(t *testing.T) {
		opts := listenerOptionsForTest("http://localhost:8080", map[string]string{
			"APILISTEN": "unix:///run/custom-edgevpn.sock",
		})
		if opts["APILISTENUNIXMODE"] != DefaultEdgeVPNAPISocketMode {
			t.Fatalf("APILISTENUNIXMODE = %q, want %q", opts["APILISTENUNIXMODE"], DefaultEdgeVPNAPISocketMode)
		}
	})

	t.Run("Unix to TCP", func(t *testing.T) {
		opts := listenerOptionsForTest(DefaultEdgeVPNAPIAddress, map[string]string{
			"APILISTEN": "127.0.0.1:8080",
		})
		if _, exists := opts["APILISTENUNIXMODE"]; exists {
			t.Fatalf("unexpected APILISTENUNIXMODE for TCP listener: %q", opts["APILISTENUNIXMODE"])
		}
	})
}

func TestExplicitUnixSocketModeIsPreserved(t *testing.T) {
	opts := listenerOptionsForTest("", map[string]string{"APILISTENUNIXMODE": "0660"})
	if opts["APILISTENUNIXMODE"] != "0660" {
		t.Fatalf("APILISTENUNIXMODE = %q, want explicit mode", opts["APILISTENUNIXMODE"])
	}
}
