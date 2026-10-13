package extensions

import "testing"

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
			if got := extensionFileNameFromURI(tc.uri); got != tc.expected {
				t.Fatalf("extensionFileNameFromURI(%q) = %q, want %q", tc.uri, got, tc.expected)
			}
		})
	}
}
