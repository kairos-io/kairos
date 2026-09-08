package mcp

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
)

// httpServer serves s over the real streamable HTTP transport and returns the
// endpoint, so the tests below exercise what an agent on the network reaches
// rather than an in-memory pipe.
func httpServer(t *testing.T, s *Server) string {
	t.Helper()

	mux := http.NewServeMux()
	mux.Handle(Path, handlerFor(s))

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	return ts.URL + Path
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

// ListenAndServe is what main calls, so bind, serve and shutdown are checked
// here rather than only the handler underneath them.
func TestListenAndServeBindsAndShutsDownWithTheContext(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	go func() { stopped <- ListenAndServe(ctx, sdkLogger.NewKairosLogger("test", "fatal", true), addr) }()

	// ListenAndServe binds on its own goroutine, so wait for the port rather
	// than racing it.
	waitForPort(t, addr)

	session := httpClient(t, "http://"+addr+Path)

	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("listing tools against the real listener: %v", err)
	}
	if len(tools.Tools) == 0 {
		t.Fatal("no tools advertised by the real listener")
	}

	cancel()

	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("ListenAndServe returned %v, want nil on a cancelled context", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("ListenAndServe did not return after the context was cancelled")
	}
}

// A port already taken has to come back as an error, so main can log it
// instead of leaving the operator to guess why no agent can connect.
func TestListenAndServeReportsAPortItCannotBind(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	err = ListenAndServe(context.Background(), sdkLogger.NewKairosLogger("test", "fatal", true), ln.Addr().String())
	if err == nil {
		t.Fatal("binding an address already in use returned no error")
	}
}

func waitForPort(t *testing.T, addr string) {
	t.Helper()

	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, time.Second)
		if err == nil {
			conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("nothing listening on %s", addr)
}
