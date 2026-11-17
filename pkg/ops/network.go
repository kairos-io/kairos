package ops

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/cavaliergopher/grab/v3"
	"github.com/kairos-io/AuroraBoot/internal"
)

const UserAgent = "AuroraBoot"

// downloadAttempts and downloadRetryBaseDelay: grab makes exactly one
// HTTP attempt per call and returns whatever error it hit, including a
// transient one (e.g. an HTTP/2 "stream error: ... PROTOCOL_ERROR"
// from the peer mid-transfer). A multi-GB release asset has plenty of
// opportunity to hit one of those, so a single failed attempt should
// not be the whole download's answer.
const downloadAttempts = 3

// downloadRetryBaseDelay is a var, not a const, so tests can shrink the
// backoff instead of waiting on it in real time.
var downloadRetryBaseDelay = 2 * time.Second

// noListingFS wraps an http.FileSystem to suppress directory listings. Opening a
// directory returns os.ErrNotExist, so http.FileServer still serves files by
// their exact path but renders no browsable index — a request for a directory
// (including "/") 404s instead of leaking the whole artifact directory's
// contents to anyone who can reach the server on 0.0.0.0.
type noListingFS struct{ fs http.FileSystem }

func (n noListingFS) Open(name string) (http.File, error) {
	f, err := n.fs.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	if info.IsDir() {
		_ = f.Close()
		return nil, os.ErrNotExist
	}
	return f, nil
}

// ServeArtifacts serve local artifacts as standard http server
func ServeArtifacts(listenAddr string, dirFunc valueGetOnCall) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		dir := dirFunc()
		fs := http.FileServer(noListingFS{http.Dir(dir)})
		// Use a private mux instead of http.DefaultServeMux: registering "/" on
		// the global mux panics ("multiple registrations for /") if this runs
		// more than once in a process and leaks the handler across servers.
		mux := http.NewServeMux()
		mux.Handle("/", fs)
		serverOne := &http.Server{
			Addr:    listenAddr,
			Handler: mux,
		}
		go func() {
			<-ctx.Done()
			serverOne.Shutdown(context.Background())
		}()
		internal.WarnUnauthenticatedServe(listenAddr, "artifact HTTP server")
		internal.Log.Logger.Info().Msgf("Listening on %v...", listenAddr)
		return serverOne.ListenAndServe()
	}
}

// DownloadArtifact downloads artifacts remotely (e.g. http(s), ...)
func DownloadArtifact(url string, isoFunc valueGetOnCall) func(ctx context.Context) error {
	return func(ctx context.Context) error {
		// https://github.com/kairos-io/kairos/releases/download/v1.5.0/kairos-alpine-ubuntu-v1.5.0.iso
		dst := isoFunc()
		// grab keeps an existing file whose size matches the remote one and
		// resumes onto a shorter one, without checking it is the same
		// artifact. A file left by an earlier run (another version or URL)
		// would end up kept or spliced, so always start from an empty slot.
		if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("removing existing %s before download: %w", dst, err)
		}
		_, err := download(ctx, url, dst)
		if err == nil {
			internal.Log.Logger.Info().Str("artifact", url).Str("destination", dst).Msg("Artifact downloaded successfully")
		}
		return err
	}
}

// download retries downloadOnce on failure, up to downloadAttempts times.
// It does not retry after ctx is canceled -- that is a caller decision to
// stop, not a transient failure to recover from.
func download(ctx context.Context, url, dst string) (string, error) {
	var dstFile string
	var err error
	for attempt := 1; attempt <= downloadAttempts; attempt++ {
		dstFile, err = downloadOnce(ctx, url, dst)
		if err == nil {
			return dstFile, nil
		}
		if ctx.Err() != nil || attempt == downloadAttempts {
			return dstFile, err
		}
		internal.Log.Logger.Warn().Err(err).Str("artifact", url).Int("attempt", attempt).Msg("download failed, retrying")
		delay := downloadRetryBaseDelay * time.Duration(attempt)
		select {
		case <-ctx.Done():
			// Report the cancellation itself, not the transient error that
			// prompted this backoff -- a caller checking for ctx.Err() must
			// see it, not whatever downloadOnce last failed with.
			return dstFile, ctx.Err()
		case <-time.After(delay):
		}
	}
	return dstFile, err
}

func downloadOnce(ctx context.Context, url, dst string) (string, error) {
	// create client
	client := grab.NewClient()
	// https://github.com/cavaliergopher/grab/issues/104
	client.UserAgent = UserAgent
	req, _ := grab.NewRequest(dst, url)

	// start download
	internal.Log.Logger.Info().Msgf("Downloading %v...", req.URL())
	resp := client.Do(req)
	internal.Log.Logger.Printf("%s:  %v", url, resp.HTTPResponse.Status)

	// start UI loop
	t := time.NewTicker(500 * time.Millisecond)
	defer t.Stop()
	dstFile := filepath.Join(dst, resp.Filename)
Loop:
	for {
		select {
		case <-ctx.Done():
			defer os.RemoveAll(dstFile)
			return dst, fmt.Errorf("context canceled")
		case <-t.C:
			internal.Log.Printf("%s: transferred %v / %v bytes (%.2f%%)",
				url,
				resp.BytesComplete(),
				resp.Size(),
				100*resp.Progress())

		case <-resp.Done:
			// download is complete
			fmt.Printf("transferred %v / %v bytes (%.2f%%)    \n", resp.BytesComplete(), resp.Size(), 100*resp.Progress())
			break Loop
		}
	}

	// check for errors
	if err := resp.Err(); err != nil {
		defer os.RemoveAll(dstFile)
		return dstFile, err
	}

	return dstFile, nil
}
