package sshimage_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/kairos-io/kairos/v4/agent/pkg/sshimage"
	"github.com/kairos-io/kairos/v4/sdk/types/images"
)

var _ = Describe("which image a node booted from", func() {
	DescribeTable("reads the kernel command line",
		func(cmdline, want, wantErr string) {
			got, err := sshimage.BootedImage(cmdline)
			if wantErr != "" {
				Expect(err).To(MatchError(ContainSubstring(wantErr)))
				return
			}
			Expect(err).ToNot(HaveOccurred())
			Expect(got).To(Equal(want))
		},
		Entry("active", "BOOT_IMAGE=/vmlinuz root=LABEL=COS_ACTIVE cos-img/filename=/cOS/active.img rd.immucore.debug", "active", ""),
		Entry("passive, the node fell back", "root=LABEL=COS_ACTIVE cos-img/filename=/cOS/passive.img", "passive", ""),
		Entry("passive by label only", "root=LABEL=COS_PASSIVE", "passive", ""),
		Entry("active by label only", "root=LABEL=COS_ACTIVE", "active", ""),
		Entry("recovery image", "root=LABEL=COS_RECOVERY cos-img/filename=/cOS/recovery.img", "", "recovery.img"),
		Entry("recovery by label", "root=LABEL=COS_RECOVERY", "", "recovery"),
		Entry("state reset", "root=LABEL=COS_RECOVERY kairos.reset", "", "reset"),
		Entry("nothing useful", "quiet splash", "", "does not say"),
	)
})

var _ = Describe("fetching over ssh", func() {
	var (
		dir     string
		state   string
		image   []byte
		node    *fakeNode
		keyFile string
		known   string
		cmdline string
		opts    sshimage.Options
		target  images.SSHTarget
		dst     *os.File
	)

	// a 5 MiB image with data, a long run of zeros and data again
	makeImage := func() []byte {
		b := make([]byte, 5<<20)
		_, _ = rand.Read(b[:1<<20])
		_, _ = rand.Read(b[4<<20:])
		return b
	}
	writeImage := func(name string, b []byte) string {
		p := filepath.Join(state, "cOS", name)
		Expect(os.WriteFile(p, b, 0o644)).To(Succeed())
		return p
	}
	writeKnown := func(n *fakeNode) string {
		p := filepath.Join(dir, "known_hosts")
		line := knownhosts.Line([]string{fmt.Sprintf("[127.0.0.1]:%s", n.Port())}, n.hostKey.PublicKey())
		Expect(os.WriteFile(p, []byte(line+"\n"), 0o600)).To(Succeed())
		return p
	}

	BeforeEach(func() {
		for _, tool := range []string{"ssh", "ssh-keyscan", "ssh-keygen"} {
			if _, err := exec.LookPath(tool); err != nil {
				Skip(tool + " is not installed, the ssh source needs the OpenSSH client")
			}
		}
		dir = GinkgoT().TempDir()
		state = filepath.Join(dir, "state")
		Expect(os.MkdirAll(filepath.Join(state, "cOS"), 0o755)).To(Succeed())
		image = makeImage()
		writeImage("active.img", image)

		cmdline = filepath.Join(dir, "cmdline")
		Expect(os.WriteFile(cmdline, []byte("root=LABEL=COS_ACTIVE cos-img/filename=/cOS/active.img\n"), 0o644)).To(Succeed())
		DeferCleanup(sshimage.SetRemoteLayout(cmdline, state))

		signer, priv := newSigner()
		keyFile = writeKey(dir, priv, 0o600)
		node = startNode(signer.PublicKey())
		DeferCleanup(node.Close)
		known = writeKnown(node)

		opts = sshimage.Options{
			SSHSourceOptions: images.SSHSourceOptions{IdentityFile: keyFile, KnownHostsFile: known},
			Backoff:          10 * time.Millisecond,
			DialTimeout:      5 * time.Second,
		}
		target = images.SSHTarget{User: "root", Host: "127.0.0.1", Port: node.Port(), Path: "/active"}

		var err error
		dst, err = os.OpenFile(filepath.Join(dir, "out.img"), os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
		Expect(err).ToNot(HaveOccurred())
		DeferCleanup(func() { dst.Close() })
	})

	contents := func() []byte {
		b, err := os.ReadFile(dst.Name())
		Expect(err).ToNot(HaveOccurred())
		return b
	}
	sumOf := func(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

	It("copies the image and verifies it", func() {
		h, err := sshimage.Fetch(context.Background(), opts, target, dst)
		Expect(err).ToNot(HaveOccurred())
		Expect(h.SHA256).To(Equal(sumOf(image)))
		Expect(h.Size).To(BeEquivalentTo(len(image)))
		Expect(h.Path).To(Equal(filepath.Join(state, "cOS", "active.img")))
		Expect(bytes.Equal(contents(), image)).To(BeTrue())
	})

	It("leaves holes where the image is zeros", func() {
		_, err := sshimage.Fetch(context.Background(), opts, target, dst)
		Expect(err).ToNot(HaveOccurred())
		st, err := os.Stat(dst.Name())
		Expect(err).ToNot(HaveOccurred())
		Expect(st.Size()).To(BeEquivalentTo(len(image)))
		if blocks, ok := blocksOf(st); ok && blocks > 0 {
			Expect(blocks * 512).To(BeNumerically("<", int64(len(image))))
		}
	})

	Describe("choosing the file", func() {
		It("fetches the passive image when the node booted from passive", func() {
			passive := makeImage()
			writeImage("passive.img", passive)
			Expect(os.WriteFile(cmdline, []byte("root=LABEL=COS_ACTIVE cos-img/filename=/cOS/passive.img\n"), 0o644)).To(Succeed())
			target.Path = ""
			h, err := sshimage.Fetch(context.Background(), opts, target, dst)
			Expect(err).ToNot(HaveOccurred())
			Expect(h.SHA256).To(Equal(sumOf(passive)))
			Expect(bytes.Equal(contents(), passive)).To(BeTrue())
		})

		It("fetches the active image when the node booted from active", func() {
			target.Path = ""
			h, err := sshimage.Fetch(context.Background(), opts, target, dst)
			Expect(err).ToNot(HaveOccurred())
			Expect(h.SHA256).To(Equal(sumOf(image)))
		})

		It("asks for a name when the node booted from neither", func() {
			Expect(os.WriteFile(cmdline, []byte("root=LABEL=COS_RECOVERY\n"), 0o644)).To(Succeed())
			target.Path = ""
			_, err := sshimage.Fetch(context.Background(), opts, target, dst)
			Expect(err).To(MatchError(ContainSubstring("name the image")))
		})

		It("fetches the file the user points at, even with odd characters in its name", func() {
			other := makeImage()
			p := filepath.Join(dir, "it's an image.img")
			Expect(os.WriteFile(p, other, 0o644)).To(Succeed())
			target.Path = p
			h, err := sshimage.Fetch(context.Background(), opts, target, dst)
			Expect(err).ToNot(HaveOccurred())
			Expect(h.SHA256).To(Equal(sumOf(other)))
		})

		It("says so when the file is not there", func() {
			target.Path = "/active"
			Expect(os.Remove(filepath.Join(state, "cOS", "active.img"))).To(Succeed())
			_, err := sshimage.Fetch(context.Background(), opts, target, dst)
			Expect(err).To(MatchError(ContainSubstring("not readable on the remote node")))
		})

		It("does not run a command smuggled in a path", func() {
			marker := filepath.Join(dir, "pwned")
			target.Path = "/x'; touch " + marker + "; echo '"
			_, err := sshimage.Fetch(context.Background(), opts, target, dst)
			Expect(err).To(HaveOccurred())
			Expect(marker).ToNot(BeAnExistingFile())
		})
	})

	It("computes the digest on the node", func() {
		h, err := sshimage.Fetch(context.Background(), opts, target, dst)
		Expect(err).ToNot(HaveOccurred())
		Expect(h.SHA256).To(Equal(sumOf(image)))
		Expect(node.Commands()).To(ContainElement(HavePrefix("sha256sum ")))
	})

	It("fails clearly when the node cannot hash", func() {
		// the node's sha256sum is not there: a path it cannot hash stands in
		target.Path = filepath.Join(dir, "cOS")
		_, err := sshimage.Fetch(context.Background(), opts, target, dst)
		Expect(err).To(HaveOccurred())
	})

	It("measures the image without hashing or transferring it", func() {
		h, err := sshimage.Stat(context.Background(), opts, target)
		Expect(err).ToNot(HaveOccurred())
		Expect(h.Size).To(BeEquivalentTo(len(image)))
		for _, c := range node.Commands() {
			Expect(c).ToNot(HavePrefix("sha256sum"))
			Expect(c).ToNot(HavePrefix("cat '" + state))
		}
	})

	It("resumes after the connection drops", func() {
		node.DropAfter(1<<20+123, nil)
		_, err := sshimage.Fetch(context.Background(), opts, target, dst)
		Expect(err).ToNot(HaveOccurred())
		Expect(bytes.Equal(contents(), image)).To(BeTrue())
		var tails []string
		for _, c := range node.Commands() {
			if len(c) > 5 && c[:5] == "tail " {
				tails = append(tails, c)
			}
		}
		Expect(tails).ToNot(BeEmpty())
	})

	It("fails when the file changes between reconnects", func() {
		node.DropAfter(1<<20, func() {
			// replaced by another file, as an upgrade of the remote node does
			p := filepath.Join(state, "cOS", "active.img")
			_ = os.Remove(p)
			_ = os.WriteFile(p, makeImage(), 0o644)
		})
		_, err := sshimage.Fetch(context.Background(), opts, target, dst)
		Expect(err).To(MatchError(sshimage.ErrVerification))
	})

	It("fails when the server cannot be reached", func() {
		node.Close()
		opts.Reconnects = 1
		_, err := sshimage.Fetch(context.Background(), opts, target, dst)
		Expect(err).To(HaveOccurred())
	})

	Describe("host keys", func() {
		It("fails when the server key is not the one on file", func() {
			other, _ := newSigner()
			line := knownhosts.Line([]string{fmt.Sprintf("[127.0.0.1]:%s", node.Port())}, other.PublicKey())
			Expect(os.WriteFile(known, []byte(line+"\n"), 0o600)).To(Succeed())
			_, err := sshimage.Fetch(context.Background(), opts, target, dst)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Host key verification failed"))
			Expect(node.Commands()).To(BeEmpty())
		})

		It("fails when the server is not in the known hosts file", func() {
			Expect(os.WriteFile(known, nil, 0o600)).To(Succeed())
			_, err := sshimage.Fetch(context.Background(), opts, target, dst)
			Expect(err).To(HaveOccurred())
			Expect(node.Commands()).To(BeEmpty())
		})

		It("accepts a pinned fingerprint", func() {
			opts.KnownHostsFile = ""
			opts.HostKeyFingerprints = []string{ssh.FingerprintSHA256(node.hostKey.PublicKey())}
			_, err := sshimage.Fetch(context.Background(), opts, target, dst)
			Expect(err).ToNot(HaveOccurred())
		})

		It("refuses a wrong pinned fingerprint", func() {
			other, _ := newSigner()
			opts.KnownHostsFile = ""
			opts.HostKeyFingerprints = []string{ssh.FingerprintSHA256(other.PublicKey())}
			_, err := sshimage.Fetch(context.Background(), opts, target, dst)
			Expect(err).To(HaveOccurred())
			Expect(node.Commands()).To(BeEmpty())
		})

		It("still checks the host key when nothing is configured, against the known hosts of the user", func() {
			opts.KnownHostsFile = ""
			_, err := sshimage.Fetch(context.Background(), opts, target, dst)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Host key verification failed"))
			Expect(node.Commands()).To(BeEmpty())
		})

		It("refuses a path that cannot be passed as an ssh option", func() {
			opts.KnownHostsFile = known + " /etc/ssh/ssh_known_hosts"
			_, err := sshimage.Fetch(context.Background(), opts, target, dst)
			Expect(err).To(MatchError(ContainSubstring("no spaces")))
		})

		It("refuses a fingerprint in the wrong format", func() {
			opts.HostKeyFingerprints = []string{"MD5:aa:bb"}
			_, err := sshimage.Fetch(context.Background(), opts, target, dst)
			Expect(err).To(MatchError(ContainSubstring("SHA256:")))
		})
	})

	Describe("the key of this node", func() {
		It("fails fast when the server does not know it", func() {
			_, priv := newSigner()
			opts.IdentityFile = writeKey(GinkgoT().TempDir(), priv, 0o600)
			_, err := sshimage.Fetch(context.Background(), opts, target, dst)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("Permission denied"))
			Expect(node.Commands()).To(BeEmpty())
		})

		It("refuses a key file other users can read", func() {
			Expect(os.Chmod(keyFile, 0o644)).To(Succeed())
			_, err := sshimage.Fetch(context.Background(), opts, target, dst)
			Expect(err).To(MatchError(ContainSubstring("chmod 600")))
		})

		It("does not fall back to other keys when one is configured", func() {
			Expect(os.WriteFile(keyFile, []byte("nope"), 0o600)).To(Succeed())
			_, err := sshimage.Fetch(context.Background(), opts, target, dst)
			Expect(err).To(HaveOccurred())
			Expect(node.Commands()).To(BeEmpty())
		})

		It("connects as the user of the source", func() {
			target.User = "alice"
			_, err := sshimage.Fetch(context.Background(), opts, target, dst)
			Expect(err).ToNot(HaveOccurred())
			Expect(node.Users()).To(HaveEach("alice"))
		})

		It("connects as the current user when the source names none", func() {
			target.User = ""
			_, err := sshimage.Fetch(context.Background(), opts, target, dst)
			Expect(err).ToNot(HaveOccurred())
			me, err := user.Current()
			Expect(err).ToNot(HaveOccurred())
			Expect(node.Users()).To(HaveEach(me.Username))
		})
	})

	It("stops when the context is cancelled", func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := sshimage.Fetch(ctx, opts, target, dst)
		Expect(err).To(HaveOccurred())
	})
})
