//go:build integration

package sshimage_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/kairos-io/kairos/v4/agent/pkg/sshimage"
	"github.com/kairos-io/kairos/v4/sdk/types/images"
)

const itStateDir = "/run/initramfs/cos-state"

// Runs against a plain OpenSSH server: no forced command, nothing of Kairos
// installed on it, see integration/run.sh. Skipped unless the script sets the
// environment.
var _ = Describe("against a plain sshd", Label("integration"), func() {
	var (
		host, key, known string
		opts             sshimage.Options
	)

	BeforeEach(func() {
		host, key, known = os.Getenv("IT_HOST"), os.Getenv("IT_KEY"), os.Getenv("IT_KNOWN_HOSTS")
		if host == "" {
			Skip("IT_HOST is not set, run integration/run.sh")
		}
		opts = sshimage.Options{SSHSourceOptions: images.SSHSourceOptions{IdentityFile: key, KnownHostsFile: known}}
	})

	fetch := func(uri string, o sshimage.Options) (*os.File, sshimage.Header, error) {
		src, err := images.NewSrcFromURI(uri)
		Expect(err).ToNot(HaveOccurred())
		t, err := src.SSHTarget()
		Expect(err).ToNot(HaveOccurred())
		dst, err := os.CreateTemp("", "fetched-*.img")
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() { dst.Close(); os.Remove(dst.Name()) })
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		h, err := sshimage.Fetch(ctx, o, t, dst)
		return dst, h, err
	}
	localSum := func(f *os.File) string {
		out, err := exec.Command("sha256sum", f.Name()).Output()
		Expect(err).ToNot(HaveOccurred())
		return strings.Fields(string(out))[0]
	}

	It("fetches /active and the bytes hash to what the server holds", func() {
		dst, h, err := fetch("ssh://root@"+host+"/active", opts)
		Expect(err).ToNot(HaveOccurred())
		Expect(h.SHA256).To(Equal(os.Getenv("IT_SHA256_ACTIVE")))
		Expect(localSum(dst)).To(Equal(os.Getenv("IT_SHA256_ACTIVE")))
	})

	It("fetches the file the URI names", func() {
		dst, _, err := fetch("ssh://root@"+host+itStateDir+"/cOS/passive.img", opts)
		Expect(err).ToNot(HaveOccurred())
		Expect(localSum(dst)).To(Equal(os.Getenv("IT_SHA256_PASSIVE")))
	})

	It("fetches what the node booted from, passive when it fell back", func() {
		for cmdline, want := range map[string]string{
			"/w/cmdline_active":  os.Getenv("IT_SHA256_ACTIVE"),
			"/w/cmdline_passive": os.Getenv("IT_SHA256_PASSIVE"),
		} {
			restore := sshimage.SetRemoteLayout(cmdline, itStateDir)
			dst, _, err := fetch("ssh://root@"+host, opts)
			restore()
			Expect(err).ToNot(HaveOccurred())
			Expect(localSum(dst)).To(Equal(want))
		}
	})

	It("asks for a name when the node booted its recovery", func() {
		defer sshimage.SetRemoteLayout("/w/cmdline_recovery", itStateDir)()
		_, _, err := fetch("ssh://root@"+host, opts)
		Expect(err).To(MatchError(ContainSubstring("name the image")))
	})

	It("says when the file does not exist", func() {
		_, _, err := fetch("ssh://root@"+host+"/nope/missing.img", opts)
		Expect(err).To(MatchError(ContainSubstring("not readable")))
	})

	It("uses the user of the config when the URI names none", func() {
		o := opts
		o.User = "root"
		dst, _, err := fetch("ssh://"+host+"/active", o)
		Expect(err).ToNot(HaveOccurred())
		Expect(localSum(dst)).To(Equal(os.Getenv("IT_SHA256_ACTIVE")))
	})

	It("accepts the server when its host key matches a pinned fingerprint", func() {
		o := opts
		o.KnownHostsFile = ""
		o.HostKeyFingerprints = []string{os.Getenv("IT_FINGERPRINT")}
		dst, _, err := fetch("ssh://root@"+host+"/active", o)
		Expect(err).ToNot(HaveOccurred())
		Expect(localSum(dst)).To(Equal(os.Getenv("IT_SHA256_ACTIVE")))
	})

	It("refuses the server when no host key matches the pinned fingerprints", func() {
		o := opts
		o.KnownHostsFile = ""
		o.HostKeyFingerprints = []string{os.Getenv("IT_WRONG_FINGERPRINT")}
		_, _, err := fetch("ssh://root@"+host+"/active", o)
		Expect(err).To(MatchError(ContainSubstring("does not match a configured fingerprint")))
	})

	It("refuses a server it knows nothing about", func() {
		o := opts
		o.KnownHostsFile = ""
		_, _, err := fetch("ssh://root@"+host+"/active", o)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("Host key verification failed"))
	})

	It("does not trust a server whose key is not on file", func() {
		o := opts
		o.KnownHostsFile = os.Getenv("IT_WRONG_KNOWN_HOSTS")
		_, _, err := fetch("ssh://root@"+host+"/active", o)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("Host key verification failed"))
	})
})
