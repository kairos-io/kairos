package extensions_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	agentConfig "github.com/kairos-io/kairos/v4/agent/pkg/config"
	installer "github.com/kairos-io/kairos/v4/agent/pkg/extensions"
	kairoshttp "github.com/kairos-io/kairos/v4/agent/pkg/implementations/http"
	sdkConfig "github.com/kairos-io/kairos/v4/sdk/types/config"
	sdkLogger "github.com/kairos-io/kairos/v4/sdk/types/logger"
	"github.com/twpayne/go-vfs/v5"
)

// extensionServer serves "new image" at /good.raw and, at /broken.raw,
// announces 1000 bytes, sends 10 and drops the connection.
func extensionServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/good.raw", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("new image"))
	})
	mux.HandleFunc("/broken.raw", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", "1000")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("0123456789"))
		if flusher, ok := w.(http.Flusher); ok {
			flusher.Flush()
		}
		hijacker, ok := w.(http.Hijacker)
		if !ok {
			t.Error("test server cannot drop the connection")
			return
		}
		conn, _, err := hijacker.Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		_ = conn.Close()
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server
}

func httpTestConfig(t *testing.T) (*sdkConfig.Config, string) {
	t.Helper()
	cfg := agentConfig.NewConfig(
		agentConfig.WithFs(vfs.OSFS),
		agentConfig.WithLogger(sdkLogger.NewNullLogger()),
		agentConfig.WithClient(kairoshttp.NewClient()),
	)
	return cfg, filepath.Join(t.TempDir(), "extensions")
}

func TestInstallOverHTTPReplacesAnInstalledExtension(t *testing.T) {
	server := extensionServer(t)
	cfg, target := httpTestConfig(t)
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "good.raw"), []byte("old and longer image"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, ".good.raw.partial"), []byte("stale"), 0600); err != nil {
		t.Fatal(err)
	}

	if err := installer.Install(cfg, server.URL+"/good.raw", target); err != nil {
		t.Fatal(err)
	}

	assertFileContent(t, cfg, filepath.Join(target, "good.raw"), "new image")
	assertOnlyEntries(t, cfg, target, "good.raw")
}

func TestInstallOverHTTPKeepsTheInstalledExtensionWhenTheTransferFails(t *testing.T) {
	server := extensionServer(t)
	cfg, target := httpTestConfig(t)
	if err := os.MkdirAll(target, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "broken.raw"), []byte("installed"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := installer.Install(cfg, server.URL+"/broken.raw", target); err == nil {
		t.Fatal("Install reported success for a transfer cut short")
	}

	assertFileContent(t, cfg, filepath.Join(target, "broken.raw"), "installed")
	assertOnlyEntries(t, cfg, target, "broken.raw")
}

func TestInstallOverHTTPLeavesNothingWhenAFreshTransferFails(t *testing.T) {
	server := extensionServer(t)
	cfg, target := httpTestConfig(t)

	if err := installer.Install(cfg, server.URL+"/broken.raw", target); err == nil {
		t.Fatal("Install reported success for a transfer cut short")
	}

	assertOnlyEntries(t, cfg, target)
}
