package client

import (
	"context"
	"io"
	"net/http"
	"net/url"
)

// SecureBootService groups the UKI signing key-set endpoints.
type SecureBootService struct{ c *Client }

// List returns every generated or imported key set.
func (s *SecureBootService) List(ctx context.Context) ([]SecureBootKeySet, error) {
	var out []SecureBootKeySet
	if err := s.c.do(ctx, http.MethodGet, "/api/v1/secureboot-keys", nil, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Generate produces a new PK / KEK / db key set plus a TPM PCR
// policy key and stores it under the instance's keys dir.
func (s *SecureBootService) Generate(ctx context.Context, req GenerateKeySetRequest) (*SecureBootKeySet, error) {
	var out SecureBootKeySet
	if err := s.c.do(ctx, http.MethodPost, "/api/v1/secureboot-keys/generate", nil, req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Delete removes the store record for a key set. Key files on disk
// are left in place.
func (s *SecureBootService) Delete(ctx context.Context, id string) error {
	return s.c.do(ctx, http.MethodDelete, "/api/v1/secureboot-keys/"+id, nil, nil, nil)
}

// Export streams the tar.gz export of a key set. Caller closes the
// returned ReadCloser.
func (s *SecureBootService) Export(ctx context.Context, id string) (io.ReadCloser, error) {
	body, _, err := s.c.doRaw(ctx, http.MethodGet, "/api/v1/secureboot-keys/"+id+"/export", nil, nil, "")
	return body, err
}

// Import uploads a tar.gz previously produced by Export.
// If `nameOverride` is set, the imported key set is renamed to avoid
// a collision with an existing record on the target instance.
//
// Note: this method uses a raw body and application/gzip content-type
// for simplicity. The server's multipart handler accepts either
// shape because the file is read from r.FormFile OR, for convenience
// here, directly from the body — but if a future server version
// strictly requires multipart you may need to rewrap this.
func (s *SecureBootService) Import(ctx context.Context, r io.Reader, nameOverride string) (*SecureBootKeySet, error) {
	q := url.Values{}
	if nameOverride != "" {
		q.Set("name", nameOverride)
	}
	body, _, err := s.c.doRaw(ctx, http.MethodPost, "/api/v1/secureboot-keys/import", q, r, "application/gzip")
	if err != nil {
		return nil, err
	}
	defer body.Close()
	var out SecureBootKeySet
	if err := decodeJSON(body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
