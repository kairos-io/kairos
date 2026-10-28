package image

import (
	"path"
	"strings"
)

// Excluded reports whether the image entry name is dropped by any of the
// exclude patterns.
//
// Both the patterns and name are read as paths from the image root: a
// leading "/" or "./" and a trailing "/" make no difference, so "/var/lib/foo",
// "var/lib/foo" and "var/lib/foo/" are the same pattern. A pattern is a glob
// in path.Match syntax and must match a whole path. An entry is excluded when
// it, or any directory above it, matches a pattern, so excluding a directory
// also drops everything inside it.
func Excluded(name string, patterns ...string) (bool, error) {
	name = cleanEntryPath(name)
	if name == "" {
		return false, nil
	}
	for _, pattern := range patterns {
		pattern = cleanEntryPath(pattern)
		if pattern == "" {
			continue
		}
		for p := name; p != "."; p = path.Dir(p) {
			matched, err := path.Match(pattern, p)
			if err != nil {
				return false, err
			}
			if matched {
				return true, nil
			}
		}
	}
	return false, nil
}

func cleanEntryPath(p string) string {
	return strings.TrimPrefix(path.Clean("/"+p), "/")
}
