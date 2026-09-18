// Command ghcr-cleanup manages GHCR container image cleanup via the
// GitHub packages REST API. Two subcommands:
//
//	ghcr-cleanup delete   (default when no subcommand is given)
//	  Delete every version whose tag list contains one of the refs
//	  in $IMAGES. Missing tags, missing packages, and non-ghcr.io
//	  refs are warnings, not failures, unless -fail-on-error is set.
//
//	ghcr-cleanup prune
//	  Age-based sweep. For each package in $PACKAGES (one
//	  "ghcr.io/<owner>/<pkg>  <retention>" pair per line), list every
//	  version, keep any tagged version whose tag matches a glob in
//	  $PROTECTED_TAGS or whose created_at is younger than the
//	  retention, and keep any untagged version referenced by a
//	  kept-tagged manifest list (multi-arch children). Delete the
//	  rest.
//
// Untagged manifests are handled through a two-step referencing pass:
// the prune walk fetches the manifest of every tagged version it
// intends to KEEP via the OCI distribution API and records the child
// digests of any image index / manifest list. Untagged versions whose
// digest is not in that set (and are past retention) get deleted; the
// ones referenced by a still-tagged parent stay put, so `docker pull`
// of the parent keeps working. An aged tagged parent that we delete
// this run leaves its children unreferenced going forward -- the
// next run's referencing pass no longer covers them and they get
// pruned then.
//
// GHCR exposes only per-version DELETE (no bulk endpoint, no tag-scoped
// delete), so both subcommands walk versions one at a time.
//
// One API rule shapes both: a DELETE that would leave a package with no
// tagged version is refused with 400 "You cannot delete the last tagged
// version of a package. You must delete the package instead." It counts
// tagged versions only, so an untagged version is always deletable.
// Neither subcommand issues such a request; each keeps one tagged
// version back and reports it as "retained".
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
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultAPIBase = "https://api.github.com"
	pageSize       = 100
	requestTimeout = 30 * time.Second
)

// pkgVersion is the subset of the API response we need.
type pkgVersion struct {
	ID        int       `json:"id"`
	Name      string    `json:"name"` // usually "sha256:<digest>"
	CreatedAt time.Time `json:"created_at"`
	Metadata  pkgMeta   `json:"metadata"`
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
	Retained   int // aged out, but held back as the package's last tagged version
}

func (r report) String() string {
	return fmt.Sprintf("deleted=%d failed=%d not-found=%d unresolved=%d skipped=%d retained=%d",
		r.Deleted, r.Failed, r.NotFound, r.Unresolved, r.Skipped, r.Retained)
}

func main() {
	// First positional argument selects the subcommand. No argument =
	// "delete" for ad-hoc ref-list use (the ghcr-prune workflow calls
	// with an explicit "prune" argument).
	args := os.Args[1:]
	sub := "delete"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub = args[0]
		args = args[1:]
	}
	switch sub {
	case "delete":
		runDelete(args)
	case "prune":
		runPrune(args)
	default:
		fmt.Fprintf(os.Stderr, "::error::ghcr-cleanup: unknown subcommand %q (want delete|prune)\n", sub)
		os.Exit(2)
	}
}

func runDelete(args []string) {
	fs := flag.NewFlagSet("delete", flag.ExitOnError)
	imagesFlag := fs.String("images", "", "newline-separated ghcr.io refs to delete (default: $IMAGES)")
	token := fs.String("token", "", "GitHub token (default: $GH_TOKEN)")
	apiBase := fs.String("api-base", defaultAPIBase, "GitHub API base URL (override in tests)")
	failOnErr := fs.Bool("fail-on-error", false, "exit non-zero if any delete fails (default: $FAIL_ON_ERROR == \"true\")")
	_ = fs.Parse(args)

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

	c := newClient(*apiBase, *token)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	rpt, _ := deleteRefs(ctx, c, refs)
	fmt.Printf("ghcr-cleanup: %s\n", rpt)
	if *failOnErr && rpt.Failed > 0 {
		os.Exit(1)
	}
}

func runPrune(args []string) {
	fs := flag.NewFlagSet("prune", flag.ExitOnError)
	pkgsFlag := fs.String("packages", "", "newline-separated \"ghcr.io/<owner>/<pkg>  <retention>\" pairs (default: $PACKAGES)")
	protectedFlag := fs.String("protected-tags", "", "newline-separated glob patterns for tags to keep (default: $PROTECTED_TAGS)")
	token := fs.String("token", "", "GitHub token (default: $GH_TOKEN)")
	apiBase := fs.String("api-base", defaultAPIBase, "GitHub API base URL (override in tests)")
	failOnErr := fs.Bool("fail-on-error", false, "exit non-zero if any delete fails (default: $FAIL_ON_ERROR == \"true\")")
	dryRun := fs.Bool("dry-run", false, "list what would be deleted without issuing DELETE (default: $DRY_RUN == \"true\")")
	_ = fs.Parse(args)

	pkgsInput := *pkgsFlag
	if pkgsInput == "" {
		pkgsInput = os.Getenv("PACKAGES")
	}
	protectedInput := *protectedFlag
	if protectedInput == "" {
		protectedInput = os.Getenv("PROTECTED_TAGS")
	}
	if *token == "" {
		*token = os.Getenv("GH_TOKEN")
	}
	if !*failOnErr && os.Getenv("FAIL_ON_ERROR") == "true" {
		*failOnErr = true
	}
	if !*dryRun && os.Getenv("DRY_RUN") == "true" {
		*dryRun = true
	}

	specs, err := parsePackageSpecs(pkgsInput)
	if err != nil {
		fmt.Fprintf(os.Stderr, "::error::ghcr-cleanup: %v\n", err)
		os.Exit(2)
	}
	if len(specs) == 0 {
		fmt.Println("ghcr-cleanup prune: no packages configured (nothing to do)")
		return
	}
	if *token == "" {
		fmt.Fprintln(os.Stderr, "::error::ghcr-cleanup: no token provided (set GH_TOKEN or -token)")
		os.Exit(2)
	}
	protected, err := parseProtectedGlobs(protectedInput)
	if err != nil {
		fmt.Fprintf(os.Stderr, "::error::ghcr-cleanup: %v\n", err)
		os.Exit(2)
	}

	c := newClient(*apiBase, *token)
	mf := newGHCRManifestClient()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()

	rpt, _ := pruneAged(ctx, c, mf, specs, protected, time.Now(), *dryRun)
	fmt.Printf("ghcr-cleanup prune: %s\n", rpt)
	if *failOnErr && rpt.Failed > 0 {
		os.Exit(1)
	}
}

func newClient(apiBase, token string) *client {
	return &client{
		apiBase: apiBase,
		token:   token,
		http:    &http.Client{Timeout: requestTimeout},
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
		hits, delErrs, held := c.deleteMatchingVersions(ctx, scope, owner, pkg, tag, ref)
		rpt.Deleted += hits
		rpt.Failed += delErrs
		rpt.Retained += held
		if hits == 0 && delErrs == 0 && held == 0 {
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
// hit. Returns (deleted, failed, retained) counts, where retained is
// the last-tagged-version hold-back described below.
//
// Collect-then-delete: walk every page accumulating matching IDs, then
// issue DELETEs. Deleting while paginating would shrink the list under
// the cursor -- if page 1 returned 100 versions and any of them got
// deleted, page=2 with per_page=100 would now index into a shorter
// list and skip the versions that were originally at indices 100..N.
// A shared-tag ref straddling the page boundary would then survive and
// the tally would still read "matched" for the earlier hits, so the
// miss goes unreported.
func (c *client) deleteMatchingVersions(ctx context.Context, scope, owner, encodedPkg, tag, ref string) (int, int, int) {
	deleted, failed, retained := 0, 0, 0
	var toDelete []int
	taggedTotal := 0
	page := 1
	for {
		listURL := fmt.Sprintf("%s/%s/%s/packages/container/%s/versions?per_page=%d&page=%d",
			c.apiBase, scope, owner, encodedPkg, pageSize, page)
		versions, err := c.listVersions(ctx, listURL)
		if err != nil {
			fmt.Printf("::warning::failed to list versions (page %d) for %s: %v\n", page, ref, err)
			failed++
			return deleted, failed, retained
		}
		if len(versions) == 0 {
			break
		}
		for _, v := range versions {
			if len(v.Metadata.Container.Tags) > 0 {
				taggedTotal++
			}
			if hasTag(v.Metadata.Container.Tags, tag) {
				toDelete = append(toDelete, v.ID)
			}
		}
		if len(versions) < pageSize {
			break
		}
		page++
	}

	// The same rule prune has to respect: GHCR answers 400 when a
	// DELETE would leave the package with no tagged version, so a tag
	// that is the only tagged thing in its package cannot be removed
	// on its own. Keep the first of them (the API lists newest first)
	// and name the one action that does work, rather than send a
	// request that can only come back as "400 Bad Request" and take
	// the exit code with it.
	if taggedTotal > 0 && len(toDelete) == taggedTotal {
		held := toDelete[0]
		toDelete = toDelete[1:]
		fmt.Printf("::warning::keeping %s (version %d): it is the package's last tagged version, which GHCR refuses to delete. Delete the package itself to remove it.\n",
			ref, held)
		retained++
	}

	for _, id := range toDelete {
		fmt.Printf("deleting %s (version id %d under /%s/%s)\n", ref, id, scope, owner)
		delURL := fmt.Sprintf("%s/%s/%s/packages/container/%s/versions/%d",
			c.apiBase, scope, owner, encodedPkg, id)
		if err := c.deleteVersion(ctx, delURL); err != nil {
			fmt.Printf("::warning::DELETE failed for %s (version %d): %v\n", ref, id, err)
			failed++
			continue
		}
		deleted++
	}
	return deleted, failed, retained
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
// errRateLimited on rate-limit responses, and an error on any other
// outcome. Used for scope resolution.
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
	if err := checkRateLimit(resp); err != nil {
		return false, err
	}
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
	if err := checkRateLimit(resp); err != nil {
		return nil, err
	}
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
	body, _ := io.ReadAll(io.LimitReader(resp.Body, errBodyLimit))
	if err := checkRateLimit(resp); err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("DELETE %s: %s%s", url, resp.Status, apiMessage(body))
	}
	return nil
}

// errBodyLimit caps how much of an error response is read. The
// packages API explains a failure in one short sentence; the cap is
// only there so a proxy returning an HTML page cannot be logged whole.
const errBodyLimit = 2048

// apiMessage renders the "message" field of a GitHub error body as a
// suffix for the status line, and "" when there is nothing to add.
// Without it a rejected DELETE logs only "400 Bad Request", which
// says nothing about which rule was broken or whether a retry could
// ever pass.
func apiMessage(body []byte) string {
	var e struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &e); err != nil {
		return ""
	}
	if msg := strings.TrimSpace(e.Message); msg != "" {
		return ": " + msg
	}
	return ""
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

// errRateLimited is a sentinel returned when the API is telling us to
// back off. Callers unwrap and stop the run instead of counting each
// throttled response as a permanent failure. The workflow token is
// bounded at ~1000 REST requests/hour and a first-run catch-up can
// easily need more; the daily cadence then finishes the sweep across
// runs.
var errRateLimited = errors.New("github rate limit reached")

// checkRateLimit inspects a response for GitHub's rate-limit signals.
// Returns errRateLimited on:
//   - HTTP 429 with a Retry-After header, OR
//   - HTTP 403 with X-RateLimit-Remaining=0 (documented rate-limit
//     error, distinct from the 403s the API returns for permission
//     issues -- those keep X-RateLimit-Remaining > 0).
func checkRateLimit(resp *http.Response) error {
	if resp.StatusCode == http.StatusTooManyRequests {
		return errRateLimited
	}
	if resp.StatusCode == http.StatusForbidden && resp.Header.Get("X-RateLimit-Remaining") == "0" {
		return errRateLimited
	}
	return nil
}

// packageSpec is one entry in the prune config: which package, how
// long to keep versions of it.
type packageSpec struct {
	Ref       string        // "ghcr.io/<owner>/<pkg>" as written in the config
	Owner     string        // "<owner>"
	EncPkg    string        // "<pkg>" URL-encoded (nested paths become %2F)
	Retention time.Duration // versions younger than this are always kept
}

// parsePackageSpecs turns the newline-separated PACKAGES input into
// packageSpec entries. Each line is "<ref-prefix>  <retention>",
// whitespace-separated; blank lines and '#'-comment lines are skipped.
func parsePackageSpecs(raw string) ([]packageSpec, error) {
	var out []packageSpec
	for lineno, line := range strings.Split(raw, "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("packages line %d: want \"<ref>  <retention>\", got %q", lineno+1, line)
		}
		owner, pkg, err := parsePackageRef(fields[0])
		if err != nil {
			return nil, fmt.Errorf("packages line %d: %w", lineno+1, err)
		}
		ret, err := parseRetention(fields[1])
		if err != nil {
			return nil, fmt.Errorf("packages line %d: %w", lineno+1, err)
		}
		out = append(out, packageSpec{
			Ref:       fields[0],
			Owner:     owner,
			EncPkg:    pkg,
			Retention: ret,
		})
	}
	return out, nil
}

// parsePackageRef breaks ghcr.io/<owner>/<pkg-path> (no tag) into the
// pieces the API endpoints need. Mirrors parseImageRef's shape but
// rejects a trailing :tag: prune operates on packages, not refs.
func parsePackageRef(ref string) (owner, pkg string, err error) {
	const prefix = "ghcr.io/"
	if !strings.HasPrefix(ref, prefix) {
		return "", "", fmt.Errorf("%q is not a ghcr.io ref", ref)
	}
	rest := strings.TrimPrefix(ref, prefix)
	if strings.Contains(rest, ":") {
		return "", "", fmt.Errorf("%q must not include a :tag for prune (got %q)", ref, rest)
	}
	slash := strings.Index(rest, "/")
	if slash < 0 {
		return "", "", fmt.Errorf("%q missing package segment (want <owner>/<pkg>)", ref)
	}
	owner = rest[:slash]
	pkg = url.PathEscape(rest[slash+1:])
	if owner == "" || pkg == "" {
		return "", "", fmt.Errorf("%q has empty owner or package", ref)
	}
	return owner, pkg, nil
}

// parseRetention extends Go's time.ParseDuration with 'd' (days) and
// 'w' (weeks) so the config can carry human-legible durations like
// the "2w" / "6h" values quay's expires-after used.
func parseRetention(s string) (time.Duration, error) {
	if s == "" {
		return 0, errors.New("empty retention")
	}
	last := s[len(s)-1]
	if last == 'd' || last == 'w' {
		nStr := s[:len(s)-1]
		n, err := strconv.Atoi(nStr)
		if err != nil {
			return 0, fmt.Errorf("retention %q: %w", s, err)
		}
		if n < 0 {
			return 0, fmt.Errorf("retention %q: negative", s)
		}
		hoursPerUnit := 24
		if last == 'w' {
			hoursPerUnit = 24 * 7
		}
		return time.Duration(n*hoursPerUnit) * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil {
		return 0, fmt.Errorf("retention %q: %w", s, err)
	}
	if d < 0 {
		return 0, fmt.Errorf("retention %q: negative", s)
	}
	return d, nil
}

// parseProtectedGlobs pulls the newline-separated PROTECTED_TAGS input
// into a slice of patterns. Blank lines and '#'-comments are dropped.
// Every pattern is validated with path.Match at parse time so a typo
// like an unmatched '[' aborts the whole prune before any DELETE fires
// -- otherwise ErrBadPattern would be discarded per-tag inside
// tagMatchesProtected and the pattern would silently lose its
// protection.
func parseProtectedGlobs(raw string) ([]string, error) {
	var out []string
	for lineno, line := range strings.Split(raw, "\n") {
		if i := strings.Index(line, "#"); i >= 0 {
			line = line[:i]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if _, err := path.Match(line, "probe"); err != nil {
			return nil, fmt.Errorf("protected_tags line %d: %q is not a valid glob: %w", lineno+1, line, err)
		}
		out = append(out, line)
	}
	return out, nil
}

// tagMatchesProtected reports whether any protected glob matches the
// tag. Uses path.Match's shell-glob semantics (*, ?, [range]).
func tagMatchesProtected(tag string, protected []string) bool {
	for _, pat := range protected {
		if ok, _ := path.Match(pat, tag); ok {
			return true
		}
	}
	return false
}

// holdBackLastTagged keeps the prune walk from proposing a DELETE that
// GHCR will always refuse:
//
//	400 "You cannot delete the last tagged version of a package.
//	     You must delete the package instead."
//
// The rule counts TAGGED versions only. An untagged version deletes
// fine even when it is the only one left, which is why a package with
// a protected tag never meets this. kairos-uki and bundles-test do: no
// tag of theirs is protected and their retention is 6h, so once the
// last per-run tag ages out every tagged version is a candidate, one
// DELETE can never succeed, and FAIL_ON_ERROR reds the whole scheduled
// run. It stays red forever, because the version it trips on is the one
// version the sweep can never remove.
//
// So when cands covers every tagged version of the package, drop the
// newest from the list and report it as held back. Deleting the package
// outright is the other half of GitHub's advice and is not worth
// taking: a re-created package does not inherit the visibility of the
// one it replaced, and these are public so that this pruner's own
// manifest reads and the UKI upgrade test can pull them anonymously.
// The held-back version is pruned by the first run after a newer tag
// arrives.
func holdBackLastTagged(ref string, cands []pruneCandidate, taggedTotal int) ([]pruneCandidate, int) {
	if taggedTotal == 0 || len(cands) != taggedTotal {
		return cands, 0
	}
	newest := 0
	for i, c := range cands {
		if c.age < cands[newest].age {
			newest = i
		}
	}
	held := cands[newest]
	fmt.Printf("  keeping %s (id %d, age %s, tags %v): GHCR cannot delete a package's last tagged version\n",
		ref, held.id, held.age.Truncate(time.Hour), held.tags)
	return append(cands[:newest], cands[newest+1:]...), 1
}

// countTagged reports how many of the versions carry at least one tag.
// The last-tagged-version rule GHCR enforces on DELETE counts versions,
// not distinct tags, so this counts versions too.
func countTagged(versions []pkgVersion) int {
	n := 0
	for _, v := range versions {
		if len(v.Metadata.Container.Tags) > 0 {
			n++
		}
	}
	return n
}

// manifestFetcher is the small subset of the OCI distribution API this
// tool uses. Extracted as an interface so tests can plug in an
// httptest-backed stub without spinning up a real registry.
//
// The reference must be a digest ("sha256:..."), not a tag: the REST
// packages API can report the same tag on multiple versions during
// transient states, so resolving a tag returns only one version's
// manifest and misses the children of the others.
type manifestFetcher interface {
	referencedDigests(ctx context.Context, owner, pkg, digest string) ([]string, error)
}

// pruneAged is the entry point for the "prune" subcommand. For each
// packageSpec it lists every version, computes the set of digests
// referenced by kept-tagged manifest lists (for multi-arch child
// protection), and deletes:
//   - tagged versions whose tag list does NOT overlap PROTECTED_TAGS
//     and whose age > retention; and
//   - untagged versions whose age > retention AND whose digest is not
//     in the referenced set.
//
// See the file header for the two-step referencing rationale.
func pruneAged(ctx context.Context, c *client, mf manifestFetcher, specs []packageSpec, protected []string, now time.Time, dryRun bool) (report, error) {
	var rpt report
	for _, spec := range specs {
		fmt.Printf("prune: %s (retention %s)\n", spec.Ref, spec.Retention)
		scope, err := c.resolveScope(ctx, spec.Owner, spec.EncPkg)
		if err != nil {
			if errors.Is(err, errRateLimited) {
				fmt.Printf("::warning::rate-limited while resolving %s; stopping this run so the next daily prune can pick up.\n", spec.Ref)
				return rpt, nil
			}
			fmt.Printf("::warning::could not list versions for %s under orgs/ or users/: %v\n", spec.Ref, err)
			rpt.Unresolved++
			continue
		}
		toDelete, retained, err := c.collectPrunableVersions(ctx, scope, mf, spec, protected, now)
		rpt.Retained += retained
		if err != nil {
			if errors.Is(err, errRateLimited) {
				fmt.Printf("::warning::rate-limited while listing %s; stopping this run so the next daily prune can pick up.\n", spec.Ref)
				return rpt, nil
			}
			fmt.Printf("::warning::listing failed for %s: %v\n", spec.Ref, err)
			rpt.Failed++
			continue
		}
		// Delete oldest-first so a run that gets cut off part way still
		// makes forward progress.
		sort.SliceStable(toDelete, func(i, j int) bool { return toDelete[i].age > toDelete[j].age })
		for _, cand := range toDelete {
			label := "tagged"
			if len(cand.tags) == 0 {
				label = "orphaned untagged"
			}
			if dryRun {
				fmt.Printf("[dry-run] would delete %s (%s, id %d, age %s, tags %v, digest %s)\n",
					spec.Ref, label, cand.id, cand.age.Truncate(time.Hour), cand.tags, cand.digest)
				rpt.Deleted++
				continue
			}
			fmt.Printf("deleting %s (%s, id %d, age %s, tags %v, digest %s)\n",
				spec.Ref, label, cand.id, cand.age.Truncate(time.Hour), cand.tags, cand.digest)
			delURL := fmt.Sprintf("%s/%s/%s/packages/container/%s/versions/%d",
				c.apiBase, scope, spec.Owner, spec.EncPkg, cand.id)
			if err := c.deleteVersion(ctx, delURL); err != nil {
				if errors.Is(err, errRateLimited) {
					fmt.Printf("::warning::rate-limited while deleting %s; stopping this run so the next daily prune can pick up.\n", spec.Ref)
					return rpt, nil
				}
				fmt.Printf("::warning::DELETE failed for %s (version %d): %v\n", spec.Ref, cand.id, err)
				rpt.Failed++
				continue
			}
			rpt.Deleted++
		}
	}
	return rpt, nil
}

// pruneCandidate carries the trace information the delete log needs.
type pruneCandidate struct {
	id     int
	digest string
	age    time.Duration
	tags   []string
}

// collectPrunableVersions paginates through every version of the
// package and returns the ones that are safe to delete.
//
// Two-pass:
//  1. Walk every version. Tagged versions decide keep vs delete on
//     the spot (protected pattern OR age < retention keeps).
//  2. For each KEEP-tagged version, fetch its manifest via the OCI
//     distribution API and add any manifest-list child digests to a
//     "referenced" set so multi-arch children stay pullable.
//  3. Walk untagged versions: keep if age < retention OR digest in
//     referenced set; otherwise mark for delete.
//
// mf may be nil, in which case the referenced set is left empty --
// pruning then behaves as if every untagged version is prunable when
// aged out. That mode exists only so unit tests that do not care
// about multi-arch semantics can pass a nil fetcher.
func (c *client) collectPrunableVersions(ctx context.Context, scope string, mf manifestFetcher, spec packageSpec, protected []string, now time.Time) (out []pruneCandidate, retained int, err error) {
	var all []pkgVersion
	page := 1
	for {
		listURL := fmt.Sprintf("%s/%s/%s/packages/container/%s/versions?per_page=%d&page=%d",
			c.apiBase, scope, spec.Owner, spec.EncPkg, pageSize, page)
		versions, err := c.listVersions(ctx, listURL)
		if err != nil {
			return nil, 0, err
		}
		if len(versions) == 0 {
			break
		}
		all = append(all, versions...)
		if len(versions) < pageSize {
			break
		}
		page++
		// Log every few pages so a big package (hadron easily has
		// thousands of versions across per-commit builds) does not
		// look like it is hung during the list phase.
		if page%5 == 0 {
			fmt.Printf("  listed %d versions so far...\n", len(all))
		}
	}
	fmt.Printf("  listed %d versions total\n", len(all))

	// Collect every tagged version's digest so we can fetch each
	// version's manifest (by digest -- fetching by tag can return
	// only one manifest when the API reports the same tag on
	// multiple versions).
	//
	// Include BOTH keep-tagged and delete-tagged parents in the
	// referenced-set pass: if an aged tagged parent's DELETE later
	// fails, its untagged children must not have been pruned in the
	// same run -- that would leave a still-live manifest list with
	// missing children. Covering their digests here defers the
	// children by one run; when the parent is really gone (this
	// run's DELETE succeeded, or a later one), the next prune's
	// referenced set no longer covers them and they age out.
	var taggedDigests []string
	seenDigest := map[string]bool{}
	for _, v := range all {
		if len(v.Metadata.Container.Tags) == 0 {
			continue
		}
		if v.Name == "" || seenDigest[v.Name] {
			continue
		}
		seenDigest[v.Name] = true
		taggedDigests = append(taggedDigests, v.Name)
	}

	// Build the referenced-digest set from every tagged manifest.
	// Fan the deduped fetches across a bounded worker pool; the
	// manifest API is what dominates wall time on a big package.
	//
	// Track fetch failures: any single failed manifest fetch means
	// we do not know a parent's children, so pruning untagged
	// versions for THIS package would risk deleting a still-referenced
	// child. Fail closed on that path -- skip untagged pruning for
	// the package on error, keep the tagged pass (it does not depend
	// on the referenced set).
	referenced := map[string]bool{}
	manifestFetchFailed := false
	if mf != nil && len(taggedDigests) > 0 {
		fmt.Printf("  fetching manifests for %d tagged versions...\n", len(taggedDigests))
		const manifestWorkers = 8
		sem := make(chan struct{}, manifestWorkers)
		var wg sync.WaitGroup
		var refMu sync.Mutex
		var doneMu sync.Mutex
		done := 0
		for _, digest := range taggedDigests {
			wg.Add(1)
			sem <- struct{}{}
			go func(digest string) {
				defer wg.Done()
				defer func() { <-sem }()
				children, err := mf.referencedDigests(ctx, spec.Owner, spec.EncPkg, digest)
				doneMu.Lock()
				done++
				n := done
				if err != nil {
					manifestFetchFailed = true
				}
				doneMu.Unlock()
				if err != nil {
					fmt.Printf("::warning::manifest fetch for %s@%s failed: %v\n",
						spec.Ref, digest, err)
				} else if len(children) > 0 {
					refMu.Lock()
					for _, d := range children {
						referenced[d] = true
					}
					refMu.Unlock()
				}
				if n%50 == 0 || n == len(taggedDigests) {
					fmt.Printf("  fetched %d/%d manifests\n", n, len(taggedDigests))
				}
			}(digest)
		}
		wg.Wait()
	}

	// Tagged versions: decide keep vs delete. Independent of the
	// referenced set, so still runs when a manifest fetch failed.
	for _, v := range all {
		tags := v.Metadata.Container.Tags
		if len(tags) == 0 {
			continue
		}
		protectedHit := false
		for _, t := range tags {
			if tagMatchesProtected(t, protected) {
				protectedHit = true
				break
			}
		}
		age := now.Sub(v.CreatedAt)
		if protectedHit || age < spec.Retention {
			continue
		}
		out = append(out, pruneCandidate{
			id:     v.ID,
			digest: v.Name,
			age:    age,
			tags:   tags,
		})
	}

	out, retained = holdBackLastTagged(spec.Ref, out, countTagged(all))

	// Untagged versions: prune only if aged AND not referenced.
	// Skip entirely if any manifest fetch above failed (fail closed).
	if manifestFetchFailed {
		fmt.Printf("::warning::skipping untagged-version pruning for %s -- one or more manifest fetches failed and the referenced set is incomplete.\n",
			spec.Ref)
	} else {
		for _, v := range all {
			if len(v.Metadata.Container.Tags) != 0 {
				continue
			}
			age := now.Sub(v.CreatedAt)
			if age < spec.Retention {
				continue
			}
			if referenced[v.Name] {
				continue
			}
			out = append(out, pruneCandidate{
				id:     v.ID,
				digest: v.Name,
				age:    age,
				tags:   nil,
			})
		}
	}
	return out, retained, nil
}

// ghcrManifestClient fetches manifests from GHCR via the OCI
// distribution API, doing the token exchange the registry redirects
// to on the first request.
type ghcrManifestClient struct {
	base   string // "https://ghcr.io"
	client *http.Client
	// tokens are scoped per repository ("<owner>/<pkg>"); cache them
	// for the lifetime of a run so a prune of one package with many
	// kept tags only pays the exchange cost once.
	mu     sync.Mutex // populated in tests via injection; package sync unused otherwise
	tokens map[string]string
}

func newGHCRManifestClient() *ghcrManifestClient {
	return &ghcrManifestClient{
		base:   "https://ghcr.io",
		client: &http.Client{Timeout: requestTimeout},
		tokens: map[string]string{},
	}
}

// referencedDigests fetches the manifest at owner/pkg@digest. If it
// is an image index / manifest list, returns every child manifest's
// digest. If it is a single-arch manifest, returns an empty list.
func (g *ghcrManifestClient) referencedDigests(ctx context.Context, owner, encPkg, digest string) ([]string, error) {
	// encPkg came from parsePackageRef which URL-encoded slashes;
	// undo that for the repository name the OCI API expects (it takes
	// nested paths as multiple segments, not encoded).
	repo := owner + "/" + strings.ReplaceAll(encPkg, "%2F", "/")
	token, err := g.tokenFor(ctx, repo)
	if err != nil {
		return nil, err
	}
	url := fmt.Sprintf("%s/v2/%s/manifests/%s", g.base, repo, digest)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", strings.Join([]string{
		"application/vnd.oci.image.index.v1+json",
		"application/vnd.docker.distribution.manifest.list.v2+json",
		"application/vnd.oci.image.manifest.v1+json",
		"application/vnd.docker.distribution.manifest.v2+json",
	}, ","))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := g.client.Do(req)
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
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var m struct {
		Manifests []struct {
			Digest string `json:"digest"`
		} `json:"manifests"`
	}
	if err := json.Unmarshal(body, &m); err != nil {
		return nil, fmt.Errorf("decode manifest: %w", err)
	}
	digests := make([]string, 0, len(m.Manifests))
	for _, c := range m.Manifests {
		if c.Digest != "" {
			digests = append(digests, c.Digest)
		}
	}
	return digests, nil
}

// tokenFor exchanges anonymously for a repository:pull-scoped bearer
// token against ghcr.io/token, cached per repo. Public packages
// answer; private ones require credentials we don't wire in today
// (all kairos-io/kairos/* packages are public).
func (g *ghcrManifestClient) tokenFor(ctx context.Context, repo string) (string, error) {
	g.mu.Lock()
	if t, ok := g.tokens[repo]; ok {
		g.mu.Unlock()
		return t, nil
	}
	g.mu.Unlock()

	url := fmt.Sprintf("%s/token?service=ghcr.io&scope=repository:%s:pull", g.base, repo)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	var body struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return "", fmt.Errorf("decode token: %w", err)
	}
	g.mu.Lock()
	g.tokens[repo] = body.Token
	g.mu.Unlock()
	return body.Token, nil
}
