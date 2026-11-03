package extensions

import (
	"strings"
	"testing"
)

// Moved here with extensionFileNameFromURI itself, which used to live in
// agent/pkg/action. The entries are the ones the action package pinned.
func TestExtensionFileNameFromURI(t *testing.T) {
	for _, tc := range []struct {
		name     string
		uri      string
		expected string
	}{
		{"plain http URL", "http://host/path/foo.sysext.raw", "foo.sysext.raw"},
		// The query must not end up in the name, or the .raw suffix detection
		// that ListExtensions relies on stops matching.
		{"strips a token query string", "http://host/path/foo.sysext.raw?token=abc123", "foo.sysext.raw"},
		{"nested https path with several query params", "https://h/a/b/c/bar.confext.raw?x=1&y=2", "bar.confext.raw"},
		{"bare filename", "foo.sysext.raw", "foo.sysext.raw"},
		{"absolute local path", "/var/lib/x/foo.sysext.raw", "foo.sysext.raw"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extensionFileNameFromURI(tc.uri)
			if err != nil {
				t.Fatalf("extensionFileNameFromURI(%q) failed: %v", tc.uri, err)
			}
			if got != tc.expected {
				t.Fatalf("extensionFileNameFromURI(%q) = %q, want %q", tc.uri, got, tc.expected)
			}
		})
	}
}

// A URI whose path does not end in a .raw name gives a name the merge skips.
// Writing it anyway installs a file that `sysext list` never reports and
// systemd-sysext never merges, which is the silent no-op in
// kairos-io/kairos#5369.
func TestExtensionFileNameFromURIRefusesANameThatIsNeverMerged(t *testing.T) {
	for _, tc := range []struct {
		name string
		uri  string
		// The name the error has to quote, so the message says what was
		// derived and not only that something was wrong.
		derived string
	}{
		{"pixiecore keeps the identity in the query", "http://10.0.0.1:8090/_/file?name=other-2", "file"},
		{"second pixiecore URL derives the same name", "http://10.0.0.1:8090/_/file?name=other-3", "file"},
		{"path ends in a directory", "https://host/extensions/", "extensions"},
		{"compressed image", "https://host/path/foo.sysext.raw.gz", "foo.sysext.raw.gz"},
		{"no suffix at all", "https://host/path/foo", "foo"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extensionFileNameFromURI(tc.uri)
			if err == nil {
				t.Fatalf("extensionFileNameFromURI(%q) returned %q with no error", tc.uri, got)
			}
			if got != tc.derived {
				t.Fatalf("extensionFileNameFromURI(%q) derived %q, want %q", tc.uri, got, tc.derived)
			}
			if !strings.Contains(err.Error(), tc.uri) || !strings.Contains(err.Error(), tc.derived) {
				t.Fatalf("error %q names neither the URI nor the derived name", err)
			}
		})
	}
}
