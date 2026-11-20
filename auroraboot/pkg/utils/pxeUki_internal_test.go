package utils

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/kairos-io/kairos/v4/sdk/types/logger"
)

// isoServeMux must build a fresh, private mux each call so it can be constructed
// repeatedly without panicking on the global http.DefaultServeMux ("multiple
// registrations for /"), and must serve the configured ISO file for any path.
func TestISOServeMuxNoGlobalState(t *testing.T) {
	log := logger.NewKairosLogger("test", "fatal", false)

	dir := t.TempDir()
	isoFile := filepath.Join(dir, "kairos.iso")
	want := []byte("iso-bytes")
	if err := os.WriteFile(isoFile, want, 0o644); err != nil {
		t.Fatalf("writing iso file: %v", err)
	}

	// Constructing the mux twice must not panic (it would on DefaultServeMux).
	mux1 := isoServeMux(isoFile, log)
	mux2 := isoServeMux(isoFile, log)
	if mux1 == nil || mux2 == nil {
		t.Fatal("isoServeMux returned nil")
	}

	// Both muxes serve the ISO content for an arbitrary path.
	for i, mux := range []*http.ServeMux{mux1, mux2} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/anything", nil)
		mux.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("mux %d: status = %d, want 200", i, rec.Code)
		}
		if rec.Body.String() != string(want) {
			t.Fatalf("mux %d: body = %q, want %q", i, rec.Body.String(), string(want))
		}
	}
}
