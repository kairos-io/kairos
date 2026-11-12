/*
Copyright © 2022 SUSE LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package http

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/cavaliergopher/grab/v3"
	"github.com/kairos-io/kairos/v4/agent/pkg/constants"
	"github.com/kairos-io/kairos/v4/sdk/types/logger"
)

type Client struct {
	client *grab.Client
}

func NewClient() *Client {
	client := grab.NewClient()
	client.UserAgent = "Mozilla/5.0 (X11; Linux i686; rv:109.0) Gecko/20100101 Firefox/117.0"
	// No overall Client.Timeout: it also covers reading the body, so a large
	// download that is still making progress would be cut off. Connect, TLS
	// and response headers are bounded by the transport, and the body by the
	// stall timeout below.
	client.HTTPClient = NewStallClient(time.Second * constants.HTTPTimeout)
	return &Client{client: client}
}

// NewStallClient returns an http.Client that gives up when connecting, the
// TLS handshake or the response headers take longer than timeout, or when no
// body bytes arrive for timeout. A slow transfer that keeps making progress is
// never cut off.
func NewStallClient(timeout time.Duration) *http.Client {
	return &http.Client{Transport: &stallTransport{
		base:    newBaseTransport(timeout),
		timeout: timeout,
	}}
}

func newBaseTransport(timeout time.Duration) *http.Transport {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DialContext = (&net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}).DialContext
	t.TLSHandshakeTimeout = timeout
	t.ResponseHeaderTimeout = timeout
	return t
}

// stallTransport cancels a request when no body bytes arrive for `timeout`.
// The timer restarts on every read, so a slow but steady transfer completes.
type stallTransport struct {
	base    http.RoundTripper
	timeout time.Duration
}

// StallError is what a request through a stall client fails with when the
// server stopped sending. It is a net.Error whose Timeout reports true, so a
// caller can retry it the way it retries any other network timeout.
type StallError struct {
	After time.Duration
}

func (e *StallError) Error() string {
	return fmt.Sprintf("no data received for %s", e.After)
}

// Timeout reports true: a stall is a timeout.
func (e *StallError) Timeout() bool { return true }

// Temporary reports true: another attempt may get the data.
func (e *StallError) Temporary() bool { return true }

func (s *stallTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// The transport fails a read cut short by a cancel with its cause, so
	// a stall reaches the caller as a StallError rather than a bare cancel.
	ctx, cancel := context.WithCancelCause(req.Context())
	timer := time.AfterFunc(s.timeout, func() { cancel(&StallError{After: s.timeout}) })
	resp, err := s.base.RoundTrip(req.Clone(ctx))
	if err != nil {
		timer.Stop()
		cancel(nil)
		return nil, err
	}
	// Headers are in: the body stall window starts now, not at dial time.
	timer.Reset(s.timeout)
	resp.Body = &stallBody{body: resp.Body, timer: timer, timeout: s.timeout, cancel: cancel}
	return resp, nil
}

type stallBody struct {
	body    io.ReadCloser
	timer   *time.Timer
	timeout time.Duration
	cancel  context.CancelCauseFunc
	once    sync.Once
}

func (b *stallBody) Read(p []byte) (int, error) {
	n, err := b.body.Read(p)
	if n > 0 {
		b.timer.Reset(b.timeout)
	}
	if err != nil {
		b.stop()
	}
	return n, err
}

func (b *stallBody) Close() error {
	err := b.body.Close()
	b.stop()
	return err
}

func (b *stallBody) stop() {
	b.once.Do(func() {
		b.timer.Stop()
		b.cancel(nil)
	})
}

// GetURL attempts to download the contents of the given URL to the given destination.
func (c Client) GetURL(log logger.KairosLogger, url string, destination string) error { // nolint:revive
	req, err := grab.NewRequest(destination, url)
	if err != nil {
		log.Errorf("Failed creating a request to '%s'", url)
		return err
	}

	// grab treats an existing destination as a partially completed download:
	// it appends to a shorter file, rejects a longer one with ErrBadLength and
	// keeps a same-sized one. Every caller here wants the remote body, not a
	// resume, so restart the transfer instead.
	req.NoResume = true

	// NoResume alone only covers a destination grab could stat up front. When
	// the destination is a directory, grab takes the file name from the
	// response and never stats it, so its own truncate is skipped and a
	// shorter remote body leaves the stale file's tail in place. Truncate the
	// resolved file ourselves, once grab knows its name and has it open.
	req.BeforeCopy = func(resp *grab.Response) error {
		if resp.Filename == "" {
			return nil
		}

		return os.Truncate(resp.Filename, 0)
	}

	// start download
	log.Infof("Downloading %v...", req.URL())
	resp := c.client.Do(req)

	// start UI loop
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()

Loop:
	for {
		select {
		case <-t.C:
			log.Debugf("  transferred %v / %v bytes (%.2f%%)\n",
				resp.BytesComplete(),
				resp.Size(),
				100*resp.Progress())

		case <-resp.Done:
			// download is complete
			break Loop
		}
	}

	// check for errors
	if err := resp.Err(); err != nil {
		log.Errorf("Download failed: %v\n", err)
		return err
	}

	log.Debugf("Download saved to ./%v \n", resp.Filename)
	return nil
}
