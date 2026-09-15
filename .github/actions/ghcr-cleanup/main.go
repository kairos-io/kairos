// Command ghcr-cleanup deletes a list of ghcr.io container image tags
// via the GitHub packages REST API.
//
// GHCR exposes only per-version DELETE (no bulk endpoint, no tag-scoped
// delete), so this tool walks each package's version list, matches the
// requested tag inside metadata.container.tags, and deletes each hit
// one at a time. Missing tags, missing packages, and non-ghcr.io refs
// are warnings, not failures, unless -fail-on-error is set -- which
// keeps `if: always()` post-step usage safe against races and partial
// state.
//
// The composite action wraps this binary. See action.yml for inputs.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	defaultAPIBase = "https://api.github.com"
	pageSize       = 100
	requestTimeout = 30 * time.Second
)

// pkgVersion is the subset of the API response we need.
type pkgVersion struct {
	ID       int     `json:"id"`
	Metadata pkgMeta `json:"metadata"`
}

type pkgMeta struct {
	Container containerMeta `json:"container"`
}

type containerMeta struct {
	Tags []string `json:"tags"`
}

// client wraps the packages REST endpoints. All methods take a ctx so
// tests (and CI) can bound each request.
type client struct {
	apiBase string
	token   string
	http    *http.Client
}

// report captures the cleanup outcome for the final summary log.
type report struct {
	Deleted    int // versions actually removed
	Failed     int // DELETE returned an error
	NotFound   int // ref parsed and scope resolved, but no version carried the tag
	Unresolved int // neither /orgs/<owner> nor /users/<owner> lists the package
	Skipped    int // ref malformed (non-ghcr.io, missing tag, ...)
}

func (r report) String() string {
	return fmt.Sprintf("deleted=%d failed=%d not-found=%d unresolved=%d skipped=%d",
		r.Deleted, r.Failed, r.NotFound, r.Unresolved, r.Skipped)
}

func main() {
	var (
		imagesFlag = flag.String("images", "", "newline-separated ghcr.io refs to delete (default: $IMAGES)")
		token      = flag.String("token", "", "GitHub token (default: $GH_TOKEN)")
		apiBase    = flag.String("api-base", defaultAPIBase, "GitHub API base URL (override in tests)")
		failOnErr  = flag.Bool("fail-on-error", false, "exit non-zero if any delete fails (default: $FAIL_ON_ERROR == \"true\")")
	)
	flag.Parse()

	images := *imagesFlag
	if images == "" {
		images = os.Getenv("IMAGES")
	}
	if *token == "" {
		*token = os.Getenv("GH_TOKEN")
	}
	if !*failOnErr && os.Getenv("FAIL_ON_ERROR") == "true" {
		*failOnErr = true
	}

	refs := parseRefs(images)
	if len(refs) == 0 {
		fmt.Println("ghcr-cleanup: no refs to process")
		return
	}
	if *token == "" {
		fmt.Fprintln(os.Stderr, "::error::ghcr-cleanup: no token provided (set GH_TOKEN or -token)")
		os.Exit(2)
	}

	c := &client{
		apiBase: *apiBase,
		token:   *token,
		http:    &http.Client{Timeout: requestTimeout},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	rpt, _ := deleteRefs(ctx, c, refs)
	fmt.Printf("ghcr-cleanup: %s\n", rpt)
	if *failOnErr && rpt.Failed > 0 {
		os.Exit(1)
	}
}

// parseRefs splits the multiline IMAGES input into a slice of trimmed
// refs, dropping blank lines and shell-style '#' comments.
func parseRefs(raw string) []string {
	var out []string
	for _, line := range strings.Split(raw, "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

// parseImageRef breaks ghcr.io/<owner>/<pkg-path>:<tag> into the pieces
// the API endpoints need. The package path is URL-encoded because
// GHCR's REST API takes it as a single path segment: nested packages
// like kairos/kairos-uki are addressed as kairos%2Fkairos-uki.
func parseImageRef(ref string) (owner, pkg, tag string, err error) {
	const prefix = "ghcr.io/"
	if !strings.HasPrefix(ref, prefix) {
		return "", "", "", fmt.Errorf("%q is not a ghcr.io ref", ref)
	}
	rest := strings.TrimPrefix(ref, prefix)
	colon := strings.LastIndex(rest, ":")
	if colon < 0 || colon == len(rest)-1 {
		return "", "", "", fmt.Errorf("%q missing :tag suffix", ref)
	}
	tag = rest[colon+1:]
	path := rest[:colon]
	slash := strings.Index(path, "/")
	if slash < 0 {
		return "", "", "", fmt.Errorf("%q missing package segment (want <owner>/<pkg>)", ref)
	}
	owner = path[:slash]
	pkg = url.PathEscape(path[slash+1:])
	return owner, pkg, tag, nil
}

// deleteRefs processes each parsed ref: resolve the scope, then delete
// every version whose tag list contains the requested tag. Per-ref
// failures are reported in the returned report and do NOT abort the run
// -- callers can check rpt.Failed to decide.
//
// Returns a nil error today; the signature keeps room for wiring
// authentication or config-level fatals up without a breaking change.
func deleteRefs(ctx context.Context, c *client, refs []string) (report, error) {
	var rpt report
	for _, ref := range refs {
		owner, pkg, tag, err := parseImageRef(ref)
		if err != nil {
			fmt.Printf("::warning::skipping %s: %v\n", ref, err)
			rpt.Skipped++
			continue
		}
		scope, err := c.resolveScope(ctx, owner, pkg)
		if err != nil {
			fmt.Printf("::warning::could not list versions for %s under orgs/ or users/ (missing package or token scope?): %v\n", ref, err)
			rpt.Unresolved++
			continue
		}
		hits, delErrs := c.deleteMatchingVersions(ctx, scope, owner, pkg, tag, ref)
		rpt.Deleted += hits
		rpt.Failed += delErrs
		if hits == 0 && delErrs == 0 {
			fmt.Printf("::warning::no matching version for %s (already deleted?)\n", ref)
			rpt.NotFound++
		}
	}
	return rpt, nil
}

// resolveScope returns "orgs" if /orgs/<owner>/packages/... answers 2xx,
// "users" if the users endpoint does, or an error if neither does.
// A 2xx with an empty body still counts as resolved -- the package
// exists but its versions were all deleted, which is fine.
func (c *client) resolveScope(ctx context.Context, owner, encodedPkg string) (string, error) {
	var lastErr error
	for _, scope := range []string{"orgs", "users"} {
		u := fmt.Sprintf("%s/%s/%s/packages/container/%s/versions?per_page=1",
			c.apiBase, scope, owner, encodedPkg)
		ok, err := c.probe(ctx, u)
		if err != nil {
			lastErr = err
			continue
		}
		if ok {
			return scope, nil
		}
	}
	if lastErr != nil {
		return "", lastErr
	}
	return "", errors.New("not found under /orgs or /users")
}

// deleteMatchingVersions paginates versions and issues DELETE for every
// hit. Returns (deleted, failed) counts.
func (c *client) deleteMatchingVersions(ctx context.Context, scope, owner, encodedPkg, tag, ref string) (int, int) {
	deleted, failed := 0, 0
	page := 1
	for {
		listURL := fmt.Sprintf("%s/%s/%s/packages/container/%s/versions?per_page=%d&page=%d",
			c.apiBase, scope, owner, encodedPkg, pageSize, page)
		versions, err := c.listVersions(ctx, listURL)
		if err != nil {
			fmt.Printf("::warning::failed to list versions (page %d) for %s: %v\n", page, ref, err)
			failed++
			return deleted, failed
		}
		if len(versions) == 0 {
			return deleted, failed
		}
		for _, v := range versions {
			if !hasTag(v.Metadata.Container.Tags, tag) {
				continue
			}
			fmt.Printf("deleting %s (version id %d under /%s/%s)\n", ref, v.ID, scope, owner)
			delURL := fmt.Sprintf("%s/%s/%s/packages/container/%s/versions/%d",
				c.apiBase, scope, owner, encodedPkg, v.ID)
			if err := c.deleteVersion(ctx, delURL); err != nil {
				fmt.Printf("::warning::DELETE failed for %s (version %d): %v\n", ref, v.ID, err)
				failed++
				continue
			}
			deleted++
		}
		if len(versions) < pageSize {
			return deleted, failed
		}
		page++
	}
}

func hasTag(tags []string, want string) bool {
	for _, t := range tags {
		if t == want {
			return true
		}
	}
	return false
}

// probe does a GET and returns (true, nil) on 2xx, (false, nil) on 404,
// and an error on any other outcome. Used for scope resolution.
func (c *client) probe(ctx context.Context, url string) (bool, error) {
	req, err := c.newRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return true, nil
	}
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	return false, fmt.Errorf("GET %s: %s", url, resp.Status)
}

func (c *client) listVersions(ctx context.Context, url string) ([]pkgVersion, error) {
	req, err := c.newRequest(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return nil, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	var out []pkgVersion
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return out, nil
}

func (c *client) deleteVersion(ctx context.Context, url string) error {
	req, err := c.newRequest(ctx, http.MethodDelete, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("DELETE %s: %s", url, resp.Status)
	}
	return nil
}

func (c *client) newRequest(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Authorization", "Bearer "+c.token)
	return req, nil
}
