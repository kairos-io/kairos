package mcp

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// endpointPath is where these specs mount the handler. The real route is
// registered by the package that owns the web installer's router; this is only
// so a spec has a URL to talk to.
const endpointPath = "/mcp"

// httpServer serves s over the real streamable HTTP transport and returns the
// endpoint, so the tests below exercise what an agent on the network reaches
// rather than an in-memory pipe.
func httpServer(t *testing.T, s *Server) string {
	t.Helper()

	mux := http.NewServeMux()
	mux.Handle(endpointPath, handlerFor(s))

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	return ts.URL + endpointPath
}

func httpClient(t *testing.T, endpoint string) *mcp.ClientSession {
	t.Helper()

	client := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v1"}, nil)
	session, err := client.Connect(context.Background(), &mcp.StreamableClientTransport{Endpoint: endpoint}, nil)
	if err != nil {
		t.Fatalf("connecting over HTTP: %v", err)
	}
	t.Cleanup(func() { session.Close() })

	return session
}

func TestToolsAreReachableOverHTTP(t *testing.T) {
	s := newFakeServer(t, nil)
	session := httpClient(t, httpServer(t, s))

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("listing tools over HTTP: %v", err)
	}
	if len(tools.Tools) == 0 {
		t.Fatal("no tools advertised over HTTP")
	}

	res := call(t, session, ToolListDisks, listDisksInput{})
	if res.IsError {
		t.Fatalf("list_disks over HTTP: %s", resultText(t, res))
	}
	if !strings.Contains(resultText(t, res), "/dev/sda") {
		t.Fatalf("list_disks over HTTP did not report the disk: %s", resultText(t, res))
	}
}

// The stdio server got one install per process because it got one client per
// process. Over HTTP anyone can open a second session, so the guard has to
// live on the shared Server or a client gets a second wipe by reconnecting.
func TestSecondInstallIsRefusedOnANewHTTPSession(t *testing.T) {
	fake := &fakeInstaller{agentBin: "/usr/bin/kairos-agent"}
	s := newFakeServer(t, func(s *Server) { s.installer = fake })
	endpoint := httpServer(t, s)

	args := installInput{Device: "/dev/sda", Confirm: true, FinishAction: FinishNone}

	if res := call(t, httpClient(t, endpoint), ToolInstall, args); res.IsError {
		t.Fatalf("first install: %s", resultText(t, res))
	}

	res := call(t, httpClient(t, endpoint), ToolInstall, args)
	if !res.IsError {
		t.Fatal("a second HTTP session was allowed to install again")
	}
	if fake.calls != 1 {
		t.Fatalf("the installer ran %d times, want 1", fake.calls)
	}
}

// A person driving the TUI has a browser on the same machine. Without this,
// any page they open could POST an install to the installer's port.
func TestCrossOriginPostIsRejected(t *testing.T) {
	endpoint := httpServer(t, newFakeServer(t, nil))

	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "cross-site")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("a cross-site POST got %d, want %d", resp.StatusCode, http.StatusForbidden)
	}
}

// The cross-origin wrapper is a browser control, not an authorization one:
// http.CrossOriginProtection decides on Sec-Fetch-Site and Origin, and a
// non-browser caller sends neither. Pinning that here so the test above is not
// read as saying the port is protected. Anything that can reach it can call
// every tool, which is the same exposure kairos-webui has on :8080 today.
func TestARequestWithNoBrowserHeadersIsNotRejected(t *testing.T) {
	endpoint := httpServer(t, newFakeServer(t, nil))

	req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list"}`))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusForbidden {
		t.Fatalf("a plain POST with no Origin and no Sec-Fetch-Site got %d: "+
			"the cross-origin wrapper is not what keeps a non-browser caller out", resp.StatusCode)
	}
}
