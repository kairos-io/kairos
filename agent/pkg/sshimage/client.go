package sshimage

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"time"

	"github.com/kairos-io/kairos/v4/sdk/types/images"
)

// Logger is the part of the agent logger the client needs.
type Logger interface {
	Infof(format string, args ...interface{})
	Warnf(format string, args ...interface{})
	Debugf(format string, args ...interface{})
}

// Options configures a fetch.
type Options struct {
	images.SSHSourceOptions
	Logger Logger
	// DialTimeout bounds the connection and handshake. Default 30s.
	DialTimeout time.Duration
	// Reconnects is how many times a dropped transfer is resumed. Default 5.
	Reconnects int
	// Backoff is the wait before a reconnect, doubled each time. Default 2s.
	Backoff time.Duration
	// ProgressEvery is how often a progress line is written at the latest.
	// A line is also written every 10% of the image. Default 10s.
	ProgressEvery time.Duration
}

func (o *Options) defaults() {
	if o.DialTimeout == 0 {
		o.DialTimeout = 30 * time.Second
	}
	if o.Reconnects == 0 {
		o.Reconnects = 5
	}
	if o.Backoff == 0 {
		o.Backoff = 2 * time.Second
	}
	if o.ProgressEvery == 0 {
		o.ProgressEvery = 10 * time.Second
	}
}

// ErrVerification means the received bytes do not hash to what the remote node reports.
var ErrVerification = errors.New("image verification failed")

// Header describes the remote image.
type Header struct {
	// Path is the file on the remote node that was, or would be, copied.
	Path string
	// Size is the size of the file in bytes.
	Size int64
	// SHA256 is the hex digest the remote node reports for the file.
	SHA256 string
	// identity is size, mtime and inode, to notice a file replaced between reconnects.
	identity string
}

// Stat connects, works out which file the target means and reads its size.
// Nothing is transferred or hashed.
func Stat(ctx context.Context, o Options, t images.SSHTarget) (Header, error) {
	o.defaults()
	c, cleanup, err := newRemote(ctx, o, t)
	if err != nil {
		return Header{}, err
	}
	defer cleanup()
	p, err := resolvePath(ctx, c, t)
	if err != nil {
		return Header{}, err
	}
	return statRemote(ctx, c, p)
}

// Fetch copies the remote image into dst and verifies it. dst must be open
// for reading and writing and empty. The image is written sparse, runs of
// zeros become holes. When the connection drops the transfer resumes where it
// stopped; when the remote file changes in between, Fetch fails.
func Fetch(ctx context.Context, o Options, t images.SSHTarget, dst *os.File) (Header, error) {
	o.defaults()
	var (
		hdr      Header
		hasher   = sha256.New()
		received int64
		attempt  int
		backoff  = o.Backoff
		lastLog  = time.Now()
		lastStep int64
	)
	logf := func(format string, a ...interface{}) {
		if o.Logger != nil {
			o.Logger.Infof(format, a...)
		}
	}
	for {
		err := func() error {
			c, cleanup, err := newRemote(ctx, o, t)
			if err != nil {
				return err
			}
			defer cleanup()
			if attempt == 0 {
				p, err := resolvePath(ctx, c, t)
				if err != nil {
					return err
				}
				if hdr, err = statRemote(ctx, c, p); err != nil {
					return err
				}
				logf("Hashing %s on %s", hdr.Path, t.Host)
				if hdr.SHA256, err = remoteSHA256(ctx, c, hdr.Path); err != nil {
					return err
				}
				logf("Fetching %s from %s (%d MiB, sha256 %s)", hdr.Path, t.Host, hdr.Size>>20, hdr.SHA256)
			} else {
				h, err := statRemote(ctx, c, hdr.Path)
				if err != nil {
					return err
				}
				if h.identity != hdr.identity {
					return fmt.Errorf("%w: %s changed on %s while it was transferred, start again", ErrVerification, hdr.Path, t.Host)
				}
			}
			return receive(ctx, c, hdr, dst, hasher, &received, func() {
				step := received * 10 / hdr.Size
				if step > lastStep || time.Since(lastLog) >= o.ProgressEvery {
					lastStep, lastLog = step, time.Now()
					logf("Transferred %d of %d MiB (%d%%)", received>>20, hdr.Size>>20, received*100/hdr.Size)
				}
			})
		}()
		if err == nil {
			break
		}
		// A first attempt that fails before any data is a wrong key, host or path: fail fast.
		// Once data has been exchanged a failure is a dropped link: resume.
		if attempt == 0 && received == 0 || errors.Is(err, ErrVerification) || ctx.Err() != nil || attempt >= o.Reconnects {
			return hdr, err
		}
		attempt++
		if o.Logger != nil {
			o.Logger.Warnf("Transfer from %s interrupted at %d MiB (%s), resuming in %s (%d/%d)", t.Host, received>>20, err, backoff, attempt, o.Reconnects)
		}
		select {
		case <-ctx.Done():
			return hdr, ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
	}

	if got := hex.EncodeToString(hasher.Sum(nil)); got != hdr.SHA256 {
		return hdr, fmt.Errorf("%w: received %s, %s reports %s", ErrVerification, got, t.Host, hdr.SHA256)
	}
	if err := dst.Truncate(hdr.Size); err != nil {
		return hdr, err
	}
	if err := dst.Sync(); err != nil {
		return hdr, err
	}
	logf("Image from %s received and verified (sha256 %s)", t.Host, hdr.SHA256)
	return hdr, nil
}

// receive reads the remote file from where the last attempt stopped into dst.
func receive(ctx context.Context, c *remote, h Header, dst *os.File, hash hash.Hash, received *int64, tick func()) error {
	r, wait, stop, err := c.stream(ctx, readCommand(h.Path, *received))
	if err != nil {
		return err
	}
	defer stop()
	buf := make([]byte, 1<<20)
	for *received < h.Size {
		want := int64(len(buf))
		if h.Size-*received < want {
			want = h.Size - *received
		}
		n, err := r.Read(buf[:want])
		if n > 0 {
			hash.Write(buf[:n])
			if werr := writeSparse(dst, buf[:n], *received); werr != nil {
				return werr
			}
			*received += int64(n)
			tick()
		}
		if err != nil {
			if err == io.EOF {
				err = io.ErrUnexpectedEOF
			}
			return err
		}
	}
	// The file must have nothing more, else it grew after it was measured.
	if extra, _ := io.ReadFull(r, make([]byte, 1)); extra > 0 {
		return fmt.Errorf("%w: %s is larger than the %d bytes it measured", ErrVerification, h.Path, h.Size)
	}
	return wait()
}

const sparseBlock = 64 << 10

var zeroBlock = make([]byte, sparseBlock)

// writeSparse writes p at off and leaves holes where p is all zeros.
func writeSparse(f *os.File, p []byte, off int64) error {
	for len(p) > 0 {
		n := len(p)
		if n > sparseBlock {
			n = sparseBlock
		}
		if !bytes.Equal(p[:n], zeroBlock[:n]) {
			if _, err := f.WriteAt(p[:n], off); err != nil {
				return err
			}
		}
		p = p[n:]
		off += int64(n)
	}
	return nil
}
