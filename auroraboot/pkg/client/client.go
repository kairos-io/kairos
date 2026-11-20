package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// Option configures a Client.
type Option func(*Client)

// Client talks to a AuroraBoot instance. It is safe for concurrent use.
//
// A single Client can carry either an admin password (for operator /
// automation callers) or a node API key (for a registered Kairos node
// phoning home) or both — admin takes precedence when both are set.
// The zero-auth case is valid for unauthenticated calls like
// GET /healthz.
type Client struct {
	baseURL    string
	httpClient *http.Client
	userAgent  string

	adminPassword string
	nodeAPIKey    string

	// Service handles. Populated once in New so downstream users can
	// write `cli.Nodes.List(ctx, nil)` etc.
	Nodes      *NodesService
	Groups     *GroupsService
	Artifacts  *ArtifactsService
	Commands   *CommandsService
	Batches    *BatchesService
	SecureBoot *SecureBootService
	Settings   *SettingsService
}

// New constructs a client pointed at baseURL. baseURL should NOT
// include a trailing slash or any path — the client adds /api/v1/...
// itself.
//
//	cli := client.New("http://auroraboot.local:8080",
//	    client.WithAdminPassword("s3cret"))
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: http.DefaultClient,
		userAgent:  "auroraboot-go-client/0.1",
	}
	for _, o := range opts {
		o(c)
	}
	// Wire services after options so per-service handles see the
	// final configuration (same parent pointer, though).
	c.Nodes = &NodesService{c: c}
	c.Groups = &GroupsService{c: c}
	c.Artifacts = &ArtifactsService{c: c}
	c.Commands = &CommandsService{c: c}
	c.Batches = &BatchesService{c: c}
	c.SecureBoot = &SecureBootService{c: c}
	c.Settings = &SettingsService{c: c}
	return c
}

// WithAdminPassword authenticates subsequent calls with the admin
// password via `Authorization: Bearer <password>`.
func WithAdminPassword(password string) Option {
	return func(c *Client) { c.adminPassword = password }
}

// WithNodeAPIKey authenticates subsequent calls with a node's API key.
// Used by the phone-home agent after registration.
func WithNodeAPIKey(apiKey string) Option {
	return func(c *Client) { c.nodeAPIKey = apiKey }
}

// WithHTTPClient overrides the underlying http.Client. Use this to
// configure timeouts, a custom transport, retries etc.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.httpClient = h
		}
	}
}

// WithUserAgent sets a custom User-Agent header on outbound requests.
func WithUserAgent(ua string) Option {
	return func(c *Client) {
		if ua != "" {
			c.userAgent = ua
		}
	}
}

// BaseURL returns the server base URL the client is pointed at.
func (c *Client) BaseURL() string { return c.baseURL }

// WithNodeAPIKey returns a shallow copy of the client bound to a
// different node API key. Used by the registration flow to swap from
// a bootstrap (unauthenticated) client to one authenticated as the
// freshly-registered node:
//
//	cli := client.New(url)
//	reg, _ := cli.Nodes.Register(ctx, client.NodeRegisterRequest{...})
//	cli = cli.WithNodeAPIKey(reg.APIKey)
func (c *Client) WithNodeAPIKey(apiKey string) *Client {
	cpy := *c
	cpy.nodeAPIKey = apiKey
	cpy.Nodes = &NodesService{c: &cpy}
	cpy.Groups = &GroupsService{c: &cpy}
	cpy.Artifacts = &ArtifactsService{c: &cpy}
	cpy.Commands = &CommandsService{c: &cpy}
	cpy.Batches = &BatchesService{c: &cpy}
	cpy.SecureBoot = &SecureBootService{c: &cpy}
	cpy.Settings = &SettingsService{c: &cpy}
	return &cpy
}

// APIError is returned when the server responds with a non-2xx status.
// It wraps the parsed error envelope {"error": "..."} plus the raw
// HTTP status code so callers can branch on 404 vs 409 vs 500.
type APIError struct {
	StatusCode int    `json:"-"`
	ErrorMsg   string `json:"error"`
	Detail     string `json:"detail,omitempty"`
	// Code is the server's stable, machine-readable discriminator (e.g.
	// "NoCapacity", "ClaimMismatch") when several conditions share one status.
	Code string `json:"code,omitempty"`
	// NodeIDs lists the nodes a 409 from starting a batch names as already in
	// a running batch.
	NodeIDs []string `json:"nodeIDs,omitempty"`
}

// Error implements the error interface.
func (e *APIError) Error() string {
	if e.Detail != "" {
		return fmt.Sprintf("auroraboot api: %d %s: %s", e.StatusCode, e.ErrorMsg, e.Detail)
	}
	if e.ErrorMsg != "" {
		return fmt.Sprintf("auroraboot api: %d %s", e.StatusCode, e.ErrorMsg)
	}
	return fmt.Sprintf("auroraboot api: %d", e.StatusCode)
}

// IsNotFound reports whether err is an APIError with a 404 status.
func IsNotFound(err error) bool {
	var e *APIError
	return errorsAs(err, &e) && e.StatusCode == http.StatusNotFound
}

// IsConflict reports whether err is an APIError with a 409 status.
func IsConflict(err error) bool {
	var e *APIError
	return errorsAs(err, &e) && e.StatusCode == http.StatusConflict
}

// IsUnauthorized reports whether err is an APIError with a 401 status.
func IsUnauthorized(err error) bool {
	var e *APIError
	return errorsAs(err, &e) && e.StatusCode == http.StatusUnauthorized
}

// IsNoCapacity reports whether err is a claim rejected because the group has no
// unclaimed node (HTTP 409, code "NoCapacity"). A caller can branch on this to
// wait and retry rather than treat it as a hard failure.
func IsNoCapacity(err error) bool {
	var e *APIError
	return errorsAs(err, &e) && e.Code == "NoCapacity"
}

// errorsAs is a tiny stdlib shim so the helpers above don't need to
// import errors directly (keeps the import list minimal).
func errorsAs(err error, target **APIError) bool {
	for err != nil {
		if e, ok := err.(*APIError); ok {
			*target = e
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

// do executes a request and decodes the JSON response into `out`
// (which may be nil for 204 / empty-body responses). Non-2xx
// responses are returned as *APIError.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out interface{}) error {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequestWithContext(ctx, method, u, bodyReader)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	// Prefer admin password over node API key when both are set —
	// admins can hit every route the node can plus the admin-only
	// ones, so "admin wins" is the right default for an operator's
	// client.
	if c.adminPassword != "" {
		req.Header.Set("Authorization", "Bearer "+c.adminPassword)
	} else if c.nodeAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.nodeAPIKey)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("http: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return decodeError(resp)
	}

	if out == nil || resp.StatusCode == http.StatusNoContent {
		// Drain the body so the connection can be reused.
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

// doRaw returns the raw body for endpoints that serve non-JSON
// content (plain-text log bodies, tar.gz exports, binary downloads).
// The caller is responsible for closing the returned ReadCloser.
func (c *Client) doRaw(ctx context.Context, method, path string, query url.Values, body io.Reader, contentType string) (io.ReadCloser, *http.Response, error) {
	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if c.userAgent != "" {
		req.Header.Set("User-Agent", c.userAgent)
	}
	if c.adminPassword != "" {
		req.Header.Set("Authorization", "Bearer "+c.adminPassword)
	} else if c.nodeAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.nodeAPIKey)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		err := decodeError(resp)
		_ = resp.Body.Close()
		return nil, resp, err
	}
	return resp.Body, resp, nil
}

// decodeJSON decodes r into out. Small helper used by service methods
// that need to parse a response body from doRaw (which returns a raw
// ReadCloser rather than decoding into a target struct).
func decodeJSON(r io.Reader, out interface{}) error {
	return json.NewDecoder(r).Decode(out)
}

func decodeError(resp *http.Response) error {
	body, _ := io.ReadAll(resp.Body)
	ae := &APIError{StatusCode: resp.StatusCode}
	if len(body) > 0 {
		_ = json.Unmarshal(body, ae)
		if ae.ErrorMsg == "" {
			// Server returned a non-standard body; surface the
			// trimmed string so callers at least see something.
			ae.ErrorMsg = strings.TrimSpace(string(body))
		}
	}
	return ae
}
