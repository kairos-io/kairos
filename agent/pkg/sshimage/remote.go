// Package sshimage copies a Kairos system image from another node over SSH.
//
// Nothing runs on the other node but what any Linux box has: the client runs
// the ssh client of the system with plain commands: cat /proc/cmdline to find
// out what the node booted from, stat for the size, sha256sum for the digest,
// and cat or tail -c to read the file. The account used has to be able to read
// the image.
package sshimage

import (
	"context"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/kairos-io/kairos/v4/sdk/types/images"
)

// Where a Kairos node keeps what the client needs. Variables so tests can move them.
var (
	remoteCmdline  = "/proc/cmdline"
	remoteStateDir = "/run/initramfs/cos-state"
)

// quote makes s one word for the remote shell.
func quote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// readCommand reads the file from byte offset on.
func readCommand(p string, offset int64) string {
	if offset == 0 {
		return "cat " + quote(p)
	}
	return "tail -c +" + strconv.FormatInt(offset+1, 10) + " " + quote(p)
}

// resolvePath returns the file on the remote node the target means. An
// absolute path is used as given, active and passive mean the images in the
// state partition and no path means the image the node booted from.
func resolvePath(ctx context.Context, c *remote, t images.SSHTarget) (string, error) {
	switch t.Path {
	case images.SSHPathActive, images.SSHPathPassive:
		return imageFile(strings.TrimPrefix(t.Path, "/")), nil
	case images.SSHPathBooted:
		out, err := c.run(ctx, "cat "+quote(remoteCmdline))
		if err != nil {
			return "", fmt.Errorf("finding out which image %s booted from: %w", t.Host, err)
		}
		name, err := bootedImage(out)
		if err != nil {
			return "", fmt.Errorf("%s: %w", t.Host, err)
		}
		return imageFile(name), nil
	default:
		return t.Path, nil
	}
}

func imageFile(name string) string {
	return path.Join(remoteStateDir, "cOS", name+".img")
}

var cmdlineImage = regexp.MustCompile(`(?:^|\s)cos-img/filename=(\S+)`)

// bootedImage tells active or passive from the kernel command line of a node.
// The boot loader names the image with cos-img/filename=/cOS/active.img, the
// root label COS_ACTIVE or COS_PASSIVE is the fallback. A node that fell back
// to passive therefore says passive. Anything else, recovery, the live medium,
// a reset or a command line that says nothing, is an error: the user has to
// name the file.
func bootedImage(cmdline string) (string, error) {
	if strings.Contains(cmdline, "kairos.reset") {
		return "", fmt.Errorf("it is running a state reset, name the image to fetch")
	}
	if m := cmdlineImage.FindStringSubmatch(cmdline); m != nil {
		switch path.Base(m[1]) {
		case "active.img":
			return "active", nil
		case "passive.img":
			return "passive", nil
		}
		return "", fmt.Errorf("it booted from %s, name the image to fetch", m[1])
	}
	switch {
	case strings.Contains(cmdline, "COS_RECOVERY"), strings.Contains(cmdline, "COS_SYSTEM"), strings.Contains(cmdline, "recovery-mode"):
		return "", fmt.Errorf("it booted its recovery system, name the image to fetch")
	case strings.Contains(cmdline, "COS_ACTIVE"):
		return "active", nil
	case strings.Contains(cmdline, "COS_PASSIVE"):
		return "passive", nil
	}
	return "", fmt.Errorf("its kernel command line does not say which image it booted from, name the image to fetch")
}

// statRemote reads size, mtime and inode of the file.
func statRemote(ctx context.Context, c *remote, p string) (Header, error) {
	out, err := c.run(ctx, "stat -c '%s %Y %i' "+quote(p))
	if err != nil {
		return Header{}, fmt.Errorf("%s is not readable on the remote node: %w", p, err)
	}
	f := strings.Fields(out)
	if len(f) != 3 {
		return Header{}, fmt.Errorf("unexpected stat output %q", strings.TrimSpace(out))
	}
	size, err := strconv.ParseInt(f[0], 10, 64)
	if err != nil || size <= 0 {
		return Header{}, fmt.Errorf("%s has no usable size (%q)", p, f[0])
	}
	return Header{Path: p, Size: size, identity: strings.Join(f, " ")}, nil
}

// remoteSHA256 hashes the file with sha256sum on the remote node, which reads all of it.
func remoteSHA256(ctx context.Context, c *remote, p string) (string, error) {
	out, err := c.run(ctx, "sha256sum "+quote(p))
	if err != nil {
		return "", fmt.Errorf("hashing %s on the remote node, it needs sha256sum: %w", p, err)
	}
	f := strings.Fields(out)
	if len(f) == 0 || !hexDigest.MatchString(strings.ToLower(f[0])) {
		return "", fmt.Errorf("unexpected digest output %q", strings.TrimSpace(out))
	}
	return strings.ToLower(f[0]), nil
}
