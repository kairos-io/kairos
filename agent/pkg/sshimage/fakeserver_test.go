package sshimage_test

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
)

// fakeNode is an in-process SSH server that runs every exec request in
// /bin/sh, like a plain sshd does. It knows nothing about Kairos.
type fakeNode struct {
	ln         net.Listener
	hostKey    ssh.Signer
	authorized ssh.PublicKey
	// dropAfter closes the first connection that reads the image after this many bytes, 0 disables
	dropAfter int64
	// onDrop runs when the connection is dropped
	onDrop   func()
	mu       sync.Mutex
	dropped  bool
	commands []string
	users    []string
	wg       sync.WaitGroup
}

func newSigner() (ssh.Signer, ed25519.PrivateKey) {
	_, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	s, err := ssh.NewSignerFromKey(priv)
	if err != nil {
		panic(err)
	}
	return s, priv
}

// writeKey stores an OpenSSH private key and returns its path.
func writeKey(dir string, priv ed25519.PrivateKey, mode os.FileMode) string {
	blk, err := ssh.MarshalPrivateKey(priv, "")
	if err != nil {
		panic(err)
	}
	p := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(p, pem.EncodeToMemory(blk), mode); err != nil {
		panic(err)
	}
	// WriteFile does not change the mode of an existing file
	_ = os.Chmod(p, mode)
	return p
}

func startNode(client ssh.PublicKey) *fakeNode {
	hk, _ := newSigner()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		panic(err)
	}
	n := &fakeNode{ln: ln, hostKey: hk, authorized: client}
	go n.accept()
	return n
}

func (n *fakeNode) Close() { n.ln.Close(); n.wg.Wait() }

func (n *fakeNode) Port() string {
	_, p, _ := net.SplitHostPort(n.ln.Addr().String())
	return p
}

func (n *fakeNode) PortInt() int { i, _ := strconv.Atoi(n.Port()); return i }

// DropAfter makes the next read of the image fail after that many bytes, calling f then.
func (n *fakeNode) DropAfter(bytes int64, f func()) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.dropAfter, n.onDrop = bytes, f
}

func (n *fakeNode) Users() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.users...)
}

func (n *fakeNode) Commands() []string {
	n.mu.Lock()
	defer n.mu.Unlock()
	return append([]string(nil), n.commands...)
}

func (n *fakeNode) accept() {
	cfg := &ssh.ServerConfig{
		PublicKeyCallback: func(c ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			if string(key.Marshal()) == string(n.authorized.Marshal()) {
				return nil, nil
			}
			return nil, io.ErrClosedPipe
		},
	}
	cfg.AddHostKey(n.hostKey)
	for {
		c, err := n.ln.Accept()
		if err != nil {
			return
		}
		n.wg.Add(1)
		go func() {
			defer n.wg.Done()
			n.handle(c, cfg)
		}()
	}
}

type dropWriter struct {
	w     io.Writer
	left  int64
	close func()
}

func (d *dropWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > d.left {
		w, _ := d.w.Write(p[:d.left])
		d.close()
		return w, io.ErrClosedPipe
	}
	d.left -= int64(len(p))
	return d.w.Write(p)
}

func (n *fakeNode) handle(c net.Conn, cfg *ssh.ServerConfig) {
	sconn, chans, reqs, err := ssh.NewServerConn(c, cfg)
	if err != nil {
		c.Close()
		return
	}
	defer sconn.Close()
	go ssh.DiscardRequests(reqs)
	for nc := range chans {
		if nc.ChannelType() != "session" {
			_ = nc.Reject(ssh.UnknownChannelType, "no")
			continue
		}
		ch, creqs, err := nc.Accept()
		if err != nil {
			return
		}
		go func() {
			for r := range creqs {
				if r.Type != "exec" {
					_ = r.Reply(false, nil)
					continue
				}
				var payload struct{ Cmd string }
				_ = ssh.Unmarshal(r.Payload, &payload)
				_ = r.Reply(true, nil)
				status := uint32(0)
				n.mu.Lock()
				n.commands = append(n.commands, payload.Cmd)
				n.users = append(n.users, sconn.User())
				reads := strings.HasPrefix(payload.Cmd, "cat ") && !strings.Contains(payload.Cmd, "cmdline") || strings.HasPrefix(payload.Cmd, "tail ")
				drop := n.dropAfter > 0 && !n.dropped && reads
				after, onDrop := n.dropAfter, n.onDrop
				if drop {
					n.dropped = true
				}
				n.mu.Unlock()
				var w io.Writer = ch
				if drop {
					w = &dropWriter{w: ch, left: after, close: func() {
						if onDrop != nil {
							onDrop()
						}
						sconn.Close()
					}}
				}
				cmd := exec.Command("/bin/sh", "-c", payload.Cmd)
				cmd.Stdout = w
				cmd.Stderr = ch.Stderr()
				if err := cmd.Run(); err != nil {
					status = 1
					var ee *exec.ExitError
					if errors.As(err, &ee) {
						status = uint32(ee.ExitCode())
					}
				}
				_, _ = ch.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
				ch.Close()
				return
			}
		}()
	}
}
