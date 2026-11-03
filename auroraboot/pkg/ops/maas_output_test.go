package ops

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// MAAS consumes a custom image with the "ddgz" filetype: a gzip-compressed raw
// disk. Raw2MAAS gzips the raw produced by the EFI build into <raw>.gz so the
// artifact is directly uploadable to MAAS without a manual compression step.
func TestRaw2MAAS(t *testing.T) {
	dir := t.TempDir()
	raw := filepath.Join(dir, "kairos-maas.raw")
	payload := []byte("this is a fake raw disk image payload, repeated for some size\n")
	want := bytes.Repeat(payload, 1000)
	if err := os.WriteFile(raw, want, 0o644); err != nil {
		t.Fatalf("writing fake raw: %v", err)
	}

	out, err := Raw2MAAS(raw)
	if err != nil {
		t.Fatalf("Raw2MAAS: %v", err)
	}

	if out != raw+".gz" {
		t.Fatalf("output name = %q, want %q", out, raw+".gz")
	}
	if _, err := os.Stat(out); err != nil {
		t.Fatalf("gzip output not created: %v", err)
	}

	// The .gz must decompress back to the exact original raw bytes.
	f, err := os.Open(out)
	if err != nil {
		t.Fatalf("opening output: %v", err)
	}
	defer f.Close()
	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("not a valid gzip stream: %v", err)
	}
	got, err := io.ReadAll(gr)
	if err != nil {
		t.Fatalf("reading gzip stream: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("decompressed content does not match original raw (got %d bytes, want %d)", len(got), len(want))
	}

	// The original raw must still be present. Unlike Raw2Gce (which removes the
	// source) and Raw2Azure (which renames it to .vhd), Raw2MAAS keeps the raw
	// alongside the .gz so both artifacts can be surfaced.
	if _, err := os.Stat(raw); err != nil {
		t.Fatalf("original raw should be left in place: %v", err)
	}
}

// Raw2MAAS must surface an error (and not create a bogus output) when the source
// raw does not exist, so a failed build does not silently produce an empty .gz.
func TestRaw2MAASMissingSource(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "does-not-exist.raw")

	out, err := Raw2MAAS(missing)
	if err == nil {
		t.Fatalf("Raw2MAAS on missing source: expected error, got nil")
	}
	// The returned name is still the conventional <raw>.gz, but nothing must be
	// written to it since opening the source failed before any output was created.
	if out != missing+".gz" {
		t.Fatalf("output name = %q, want %q", out, missing+".gz")
	}
	if _, statErr := os.Stat(out); !os.IsNotExist(statErr) {
		t.Fatalf("no gzip output should be created on failure, stat err = %v", statErr)
	}
}

// ConvertRawDiskToMAAS globs the build dir for the single kairos-*.raw and
// compresses it in place. The happy path must produce a sibling <raw>.gz.
func TestConvertRawDiskToMAAS(t *testing.T) {
	dir := t.TempDir()
	raw := filepath.Join(dir, "kairos-maas.raw")
	if err := os.WriteFile(raw, bytes.Repeat([]byte("payload\n"), 100), 0o644); err != nil {
		t.Fatalf("writing fake raw: %v", err)
	}

	if err := ConvertRawDiskToMAAS(dir)(context.Background()); err != nil {
		t.Fatalf("ConvertRawDiskToMAAS: %v", err)
	}

	if _, err := os.Stat(raw + ".gz"); err != nil {
		t.Fatalf("expected %s.gz to be created: %v", raw, err)
	}
}

// ConvertRawDiskToMAAS must fail loudly when the build dir contains no raw disk,
// rather than succeeding with nothing to upload.
func TestConvertRawDiskToMAASNoRaw(t *testing.T) {
	dir := t.TempDir()

	err := ConvertRawDiskToMAAS(dir)(context.Background())
	if err == nil {
		t.Fatalf("expected error when no raw disk is present, got nil")
	}
	if !strings.Contains(err.Error(), "one and only one raw disk") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// ConvertRawDiskToMAAS must also fail when more than one raw disk is present,
// since it cannot know which one MAAS should receive.
func TestConvertRawDiskToMAASMultipleRaws(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"kairos-a.raw", "kairos-b.raw"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o644); err != nil {
			t.Fatalf("writing fake raw %s: %v", name, err)
		}
	}

	err := ConvertRawDiskToMAAS(dir)(context.Background())
	if err == nil {
		t.Fatalf("expected error when multiple raw disks are present, got nil")
	}
	if !strings.Contains(err.Error(), "one and only one raw disk") {
		t.Fatalf("unexpected error: %v", err)
	}
}
