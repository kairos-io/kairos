package splash

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestDefaultWordmarkRowsAreAllTheSameWidth(t *testing.T) {
	b := DefaultBranding()
	want := utf8.RuneCountInString(b.Wordmark[0])
	for i, row := range b.Wordmark {
		if got := utf8.RuneCountInString(row); got != want {
			t.Errorf("row %d is %d cells, row 1 is %d: %q", i+1, got, want, row)
		}
	}
	if want != b.Width() {
		t.Errorf("Width() = %d, rows are %d cells", b.Width(), want)
	}
	if b.Height() != 6 {
		t.Errorf("Height() = %d, want 6", b.Height())
	}
}

// The kernel virtual console has 16 colours. A 256-colour code is accepted by
// the terminal and renders as a different hue, so this is not caught by
// anything at runtime and has to be pinned here.
func TestDefaultPaletteIsVGA16(t *testing.T) {
	for _, c := range DefaultBranding().Palette {
		if !ValidColor(c) {
			t.Errorf("colour %d is not a VGA-16 foreground code", c)
		}
	}
}

func TestValidColorBoundaries(t *testing.T) {
	for _, tc := range []struct {
		color int
		want  bool
	}{
		{29, false}, {30, true}, {37, true}, {38, false},
		{68, false}, {89, false}, {90, true}, {97, true}, {98, false},
	} {
		if got := ValidColor(tc.color); got != tc.want {
			t.Errorf("ValidColor(%d) = %v, want %v", tc.color, got, tc.want)
		}
	}
}

func TestLoadBrandingMissingDirIsTheSilentDefault(t *testing.T) {
	b, errs := LoadBranding(filepath.Join(t.TempDir(), "absent"))
	if len(errs) != 0 {
		t.Fatalf("errors for an absent dir: %v", errs)
	}
	if b.Name != DefaultBranding().Name {
		t.Errorf("Name = %q, want the default", b.Name)
	}
}

func TestLoadBrandingEmptyPathIsTheDefault(t *testing.T) {
	b, errs := LoadBranding("")
	if len(errs) != 0 || b.Width() != DefaultBranding().Width() {
		t.Fatalf("got %d errors, width %d", len(errs), b.Width())
	}
}

func writeBranding(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLoadBrandingReadsEveryFile(t *testing.T) {
	dir := writeBranding(t, map[string]string{
		"wordmark": "AAA\nBBB\n",
		"tagline":  "a downstream tagline\n",
		"palette":  "31 32\n93\n",
		"name":     "DOWNSTREAM\n",
	})
	b, errs := LoadBranding(dir)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(b.Wordmark) != 2 || b.Wordmark[0] != "AAA" || b.Wordmark[1] != "BBB" {
		t.Errorf("Wordmark = %q", b.Wordmark)
	}
	if b.Tagline != "a downstream tagline" {
		t.Errorf("Tagline = %q", b.Tagline)
	}
	if len(b.Palette) != 3 || b.Palette[0] != 31 || b.Palette[2] != 93 {
		t.Errorf("Palette = %v", b.Palette)
	}
	if b.Name != "DOWNSTREAM" {
		t.Errorf("Name = %q", b.Name)
	}
}

// Ragged art shifts every row but the longest off-centre, which reads as a
// rendering bug rather than as bad input, so it is rejected outright.
func TestLoadBrandingRejectsRaggedWordmark(t *testing.T) {
	dir := writeBranding(t, map[string]string{"wordmark": "AAAA\nBB\n"})
	b, errs := LoadBranding(dir)
	if len(errs) != 1 {
		t.Fatalf("errs = %v, want exactly one", errs)
	}
	if !strings.Contains(errs[0].Error(), "wordmark") {
		t.Errorf("error does not name the file: %v", errs[0])
	}
	if b.Width() != DefaultBranding().Width() {
		t.Errorf("did not fall back to the default wordmark: width %d", b.Width())
	}
}

func TestLoadBrandingAcceptsMultibyteArtOfEqualCellWidth(t *testing.T) {
	// Four bytes, two cells: rejecting this would reject every block-art
	// wordmark, including the built-in one.
	dir := writeBranding(t, map[string]string{"wordmark": "██\n╚═\n"})
	b, errs := LoadBranding(dir)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if b.Width() != 2 {
		t.Errorf("Width() = %d, want 2 cells", b.Width())
	}
}

func TestLoadBrandingDropsTrailingBlankLinesFromArt(t *testing.T) {
	dir := writeBranding(t, map[string]string{"wordmark": "AAA\nBBB\n\n\n"})
	b, errs := LoadBranding(dir)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if b.Height() != 2 {
		t.Errorf("Height() = %d, want 2", b.Height())
	}
}

func TestLoadBrandingRejects256ColorPalette(t *testing.T) {
	// 68 and 39 are the hadron splash's 256-colour indices. On a virtual
	// console they are not blue, they are whatever the 16-colour table has
	// at those positions modulo nothing at all.
	dir := writeBranding(t, map[string]string{"palette": "68 39 98"})
	b, errs := LoadBranding(dir)
	if len(errs) != 1 {
		t.Fatalf("errs = %v, want exactly one", errs)
	}
	if !strings.Contains(errs[0].Error(), "VGA-16") {
		t.Errorf("error should say why: %v", errs[0])
	}
	for _, c := range b.Palette {
		if !ValidColor(c) {
			t.Errorf("fell back to an invalid palette: %v", b.Palette)
		}
	}
}

// One bad entry falls the whole ramp back, rather than leaving a gap that
// would paint one frame of the animation unstyled.
func TestLoadBrandingPaletteIsAllOrNothing(t *testing.T) {
	dir := writeBranding(t, map[string]string{"palette": "31 999 32"})
	b, errs := LoadBranding(dir)
	if len(errs) != 1 {
		t.Fatalf("errs = %v", errs)
	}
	if len(b.Palette) != len(DefaultBranding().Palette) {
		t.Errorf("Palette = %v, want the default ramp", b.Palette)
	}
}

func TestLoadBrandingPaletteRejectsNonNumbers(t *testing.T) {
	dir := writeBranding(t, map[string]string{"palette": "blue"})
	_, errs := LoadBranding(dir)
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "not a number") {
		t.Fatalf("errs = %v", errs)
	}
}

func TestLoadBrandingPaletteAcceptsACommaSeparatedList(t *testing.T) {
	dir := writeBranding(t, map[string]string{"palette": "31, 32, 33"})
	b, errs := LoadBranding(dir)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if len(b.Palette) != 3 {
		t.Errorf("Palette = %v", b.Palette)
	}
}

// A blank tagline file is a deliberate "no tagline", not an error.
func TestLoadBrandingBlankTaglineIsNotAnError(t *testing.T) {
	dir := writeBranding(t, map[string]string{"tagline": "\n"})
	b, errs := LoadBranding(dir)
	if len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if b.Tagline != "" {
		t.Errorf("Tagline = %q, want empty", b.Tagline)
	}
}

func TestLoadBrandingEmptyNameIsAnError(t *testing.T) {
	dir := writeBranding(t, map[string]string{"name": "  \n"})
	b, errs := LoadBranding(dir)
	if len(errs) != 1 {
		t.Fatalf("errs = %v", errs)
	}
	if b.Name == "" {
		t.Error("Name fell back to empty, so the fallback line would print nothing")
	}
}

func TestLoadBrandingEmptyWordmarkIsAnError(t *testing.T) {
	dir := writeBranding(t, map[string]string{"wordmark": "\n\n"})
	b, errs := LoadBranding(dir)
	if len(errs) != 1 {
		t.Fatalf("errs = %v", errs)
	}
	if b.Height() == 0 {
		t.Error("Height() = 0, the animation would paint nothing")
	}
}
