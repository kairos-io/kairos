package cmd

import (
	"fmt"
	"io/fs"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// mergedSysextHierarchies mirrors SYSTEMD_SYSEXT_HIERARCHIES as kairos-init
// ships it in cloudconfigs/99_sysext.yaml. systemd merges nothing outside this
// list, so anything else a sysext carries is packed into the image and then
// never appears on the node. Nothing under /usr/local is merged: Kairos mounts
// the persistent partition there, and a merged hierarchy turns read-only.
//
// The list cannot be read from the kairos module at run time: it lives inside
// a yip cloud config, and the package that embeds it
// (kairos-init/pkg/bundled) also embeds binaries that are absent from the
// published module, so it does not build here. TestMergedHierarchiesMatchKairos
// reads the yaml out of the kairos module instead and fails when the two
// disagree, which is what turns a dependency bump into a visible decision.
var mergedSysextHierarchies = []string{
	"/usr/bin",
	"/usr/share",
	"/usr/lib",
	"/usr/include",
	"/usr/src",
	"/usr/sbin",
}

// sysextHierarchiesAssignment matches an assignment of
// SYSTEMD_SYSEXT_HIERARCHIES in both forms 99_sysext.yaml uses: a systemd
// drop-in `Environment="SYSTEMD_SYSEXT_HIERARCHIES=..."` and a shell
// `export SYSTEMD_SYSEXT_HIERARCHIES="..."`.
var sysextHierarchiesAssignment = regexp.MustCompile(`SYSTEMD_SYSEXT_HIERARCHIES="?([^"\s]*)`)

// parseSysextHierarchies returns every distinct SYSTEMD_SYSEXT_HIERARCHIES
// value found in the given cloud config, each already split on ":" and
// normalized. One entry per assignment, in the order they appear, so a caller
// can tell two disagreeing assignments apart from one repeated.
func parseSysextHierarchies(config []byte) [][]string {
	var out [][]string
	for _, m := range sysextHierarchiesAssignment.FindAllSubmatch(config, -1) {
		var paths []string
		for _, p := range strings.Split(string(m[1]), ":") {
			if p = normalizeHierarchy(p); p != "" {
				paths = append(paths, p)
			}
		}
		out = append(out, paths)
	}
	return out
}

// normalizeHierarchy turns a hierarchy as written into a cleaned absolute
// path, or "" for one that names the root or nothing at all.
func normalizeHierarchy(p string) string {
	p = "/" + strings.Trim(strings.TrimSpace(p), "/")
	if p = path.Clean(p); p == "/" {
		return ""
	}
	return p
}

// unmergedPrefix reports the shallowest ancestor of p that Kairos will not
// merge, or "" when p lands inside a merged hierarchy.
//
// It walks p from the root down. A prefix that is itself merged, or that sits
// under one, ends the walk: the file is merged. A prefix that is an ancestor
// of some merged hierarchy is inconclusive — /usr is not merged on its own,
// but /usr/bin is — so the walk continues. The first prefix that is neither is
// the answer, and it is the most useful thing to name: /usr/libexec rather
// than each of the hundred files under it.
func unmergedPrefix(p string, merged []string) string {
	p = normalizeHierarchy(p)
	if p == "" {
		return ""
	}
	parts := strings.Split(strings.TrimPrefix(p, "/"), "/")
	prefix := ""
	for _, part := range parts {
		prefix += "/" + part
		if isUnderHierarchy(prefix, merged) {
			return ""
		}
		if isAncestorOfHierarchy(prefix, merged) {
			continue
		}
		return prefix
	}
	return ""
}

// isUnderHierarchy reports whether p is one of the hierarchies or lives inside
// one of them.
func isUnderHierarchy(p string, merged []string) bool {
	for _, h := range merged {
		if p == h || strings.HasPrefix(p, h+"/") {
			return true
		}
	}
	return false
}

// isAncestorOfHierarchy reports whether p is a strict ancestor of some
// hierarchy, that is, whether descending further can still reach a merged path.
func isAncestorOfHierarchy(p string, merged []string) bool {
	for _, h := range merged {
		if strings.HasPrefix(h, p+"/") {
			return true
		}
	}
	return false
}

// unmergedPrefixes walks the staged extension tree and returns the sorted,
// deduplicated set of prefixes holding files that Kairos will not merge.
// Directories are reported only through the files they hold: an empty
// directory contributes nothing to the node either way.
func unmergedPrefixes(root string, merged []string) ([]string, error) {
	seen := map[string]struct{}{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, rerr := filepath.Rel(root, p)
		if rerr != nil {
			return rerr
		}
		if prefix := unmergedPrefix(filepath.ToSlash(rel), merged); prefix != "" {
			seen[prefix] = struct{}{}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(seen))
	for p := range seen {
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

// unmergedHierarchyWarnings returns one warning line per prefix the staged
// tree carries and Kairos does not merge.
//
// A prefix the operator named with --include-path gets a different line: they
// have already said they want those files packed, so the useful thing to tell
// them is that packing is not enough, not that they should move the files.
func unmergedHierarchyWarnings(prefixes, includes []string) []string {
	var asked []string
	for _, p := range includes {
		if n := normalizeHierarchy(p); n != "" {
			asked = append(asked, n)
		}
	}
	out := make([]string, 0, len(prefixes))
	for _, p := range prefixes {
		if isUnderHierarchy(p, asked) {
			out = append(out, fmt.Sprintf(
				"⚠ %s is packed as you asked, but Kairos does not merge it (SYSTEMD_SYSEXT_HIERARCHIES). "+
					"Add %s to the hierarchies of the image this extension is for, or the files will not appear on the node.", p, p))
			continue
		}
		out = append(out, fmt.Sprintf(
			"⚠ %s is in this extension but Kairos does not merge it (SYSTEMD_SYSEXT_HIERARCHIES). "+
				"Those files will not appear on the node. Move them under a merged hierarchy (%s), "+
				"or add %s to the hierarchies of the image this extension is for.",
			p, strings.Join(mergedSysextHierarchies, ", "), p))
	}
	return out
}

// warnUnmergedHierarchies prints a warning for every prefix of the staged tree
// that Kairos will not merge. A tree that stays inside the merged hierarchies
// prints nothing. The extension is still built either way: refusing here would
// break every extension that packs /usr/libexec today.
func warnUnmergedHierarchies(root string, includes []string, warn func(string)) error {
	prefixes, err := unmergedPrefixes(root, mergedSysextHierarchies)
	if err != nil {
		return err
	}
	for _, line := range unmergedHierarchyWarnings(prefixes, includes) {
		warn(line)
	}
	return nil
}
