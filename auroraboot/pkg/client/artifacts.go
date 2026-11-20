package client

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
)

// ArtifactsService groups the image-build endpoints.
type ArtifactsService struct{ c *Client }

// Create kicks off an asynchronous build. The response is the initial
// record with phase Pending; poll Get or subscribe to the UI
// WebSocket to watch it progress.
func (s *ArtifactsService) Create(ctx context.Context, req CreateArtifactRequest) (*Artifact, error) {
	var out Artifact
	if err := s.c.do(ctx, http.MethodPost, "/api/v1/artifacts", nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// List returns every known artifact build.
func (s *ArtifactsService) List(ctx context.Context) ([]Artifact, error) {
	var out []Artifact
	if err := s.c.do(ctx, http.MethodGet, "/api/v1/artifacts", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Get fetches one artifact by ID.
func (s *ArtifactsService) Get(ctx context.Context, id string) (*Artifact, error) {
	var out Artifact
	if err := s.c.do(ctx, http.MethodGet, "/api/v1/artifacts/"+id, nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Logs returns the full build log snapshot as a plain string. For
// real-time updates subscribe to the UI WebSocket.
func (s *ArtifactsService) Logs(ctx context.Context, id string) (string, error) {
	body, _, err := s.c.doRaw(ctx, http.MethodGet, "/api/v1/artifacts/"+id+"/logs", nil, nil, "")
	if err != nil {
		return "", err
	}
	defer body.Close()
	b, err := io.ReadAll(body)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Cancel aborts a running build.
func (s *ArtifactsService) Cancel(ctx context.Context, id string) error {
	return s.c.do(ctx, http.MethodPost, "/api/v1/artifacts/"+id+"/cancel", nil, nil, nil)
}

// Delete removes an artifact and its output files.
func (s *ArtifactsService) Delete(ctx context.Context, id string) error {
	return s.c.do(ctx, http.MethodDelete, "/api/v1/artifacts/"+id, nil, nil, nil)
}

// ClearFailed deletes every artifact currently in the Error phase.
func (s *ArtifactsService) ClearFailed(ctx context.Context) error {
	return s.c.do(ctx, http.MethodDelete, "/api/v1/artifacts/failed", nil, nil, nil)
}

// Update patches artifact metadata (name and/or saved flag).
func (s *ArtifactsService) Update(ctx context.Context, id string, req UpdateArtifactRequest) (*Artifact, error) {
	var out Artifact
	if err := s.c.do(ctx, http.MethodPatch, "/api/v1/artifacts/"+id, nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Download streams a produced artifact file (an ISO, UKI, raw disk,
// netboot file...). The caller owns the returned ReadCloser and must
// close it.
func (s *ArtifactsService) Download(ctx context.Context, id, filename string) (io.ReadCloser, error) {
	body, _, err := s.c.doRaw(ctx, http.MethodGet, "/api/v1/artifacts/"+id+"/download/"+filename, nil, nil, "")
	return body, err
}

// OverlayFile is one file sent to UploadOverlay. Name is the file name
// the server stores it under (only its base name is kept). A name ending
// in .tar.gz or .tgz marks an archive, which the server extracts into
// the overlay instead of storing as is.
//
// Content may be read at any point until UploadOverlay returns, and
// never after. The client does not close it.
type OverlayFile struct {
	Name    string
	Content io.Reader
}

// UploadOverlay stores files as a rootfs overlay on the server and
// returns its ID. Pass the ID as CreateArtifactRequest.OverlayID to copy
// the overlay on top of a build's rootfs.
//
// The files are streamed as a multipart form, one "files" part each, so
// large archives are not buffered in memory. The call returns only once
// it has stopped reading every Content, so a Read that blocks also
// delays the return after ctx is cancelled. Closing Content is up to
// the caller.
func (s *ArtifactsService) UploadOverlay(ctx context.Context, files ...OverlayFile) (string, error) {
	if len(files) == 0 {
		return "", errors.New("client: UploadOverlay requires at least one file")
	}
	for i, f := range files {
		if f.Name == "" {
			return "", fmt.Errorf("client: overlay file %d has no name", i)
		}
		if f.Content == nil {
			return "", fmt.Errorf("client: overlay file %q has no content", f.Name)
		}
	}

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	contentType := mw.FormDataContentType()
	done := make(chan struct{})
	var writeErr error
	go func() {
		defer close(done)
		writeErr = writeOverlayParts(mw, files)
		_ = pw.CloseWithError(writeErr)
	}()
	// stopWriter makes the writer goroutine give up and waits for it, so
	// no Content is read once UploadOverlay returns. Closing the read side
	// fails the writer's next pipe write; a Read already in progress has
	// to return on its own.
	stopWriter := func() {
		_ = pr.Close()
		<-done
	}
	defer stopWriter()

	body, _, err := s.c.doRaw(ctx, http.MethodPost, "/api/v1/artifacts/upload-overlay", nil, pr, contentType)
	if err != nil {
		stopWriter()
		// A failed file read is the root cause of the request failing,
		// unless the server answered first or the pipe was closed by
		// stopWriter.
		var apiErr *APIError
		if writeErr != nil && !errors.As(err, &apiErr) && !errors.Is(writeErr, io.ErrClosedPipe) {
			return "", writeErr
		}
		return "", err
	}
	defer body.Close()
	var out struct {
		ID string `json:"id"`
	}
	if err := decodeJSON(body, &out); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	if out.ID == "" {
		return "", errors.New("decode response: no overlay id")
	}
	return out.ID, nil
}

func writeOverlayParts(mw *multipart.Writer, files []OverlayFile) error {
	for _, f := range files {
		part, err := mw.CreateFormFile("files", f.Name)
		if err != nil {
			return fmt.Errorf("create overlay part %q: %w", f.Name, err)
		}
		if _, err := io.Copy(part, f.Content); err != nil {
			return fmt.Errorf("copy overlay file %q: %w", f.Name, err)
		}
	}
	return mw.Close()
}
