package github

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/google/go-github/v40/github"
	"golang.org/x/oauth2"
)

func newHTTPClient(ctx context.Context, token string) *http.Client {
	if token == "" {
		return http.DefaultClient
	}
	src := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	return oauth2.NewClient(ctx, src)
}

// FindReleases finds the releases from the given repo (slug) and returns a parsed semver.Collection
// where the first item is the highest version as its sorted.
func FindReleases(ctx context.Context, token, slug string, preReleases bool) (semver.Collection, error) {
	hc := newHTTPClient(ctx, token)
	cli := github.NewClient(hc)

	repo := strings.Split(slug, "/")
	if len(repo) != 2 || repo[0] == "" || repo[1] == "" {
		return nil, fmt.Errorf("Invalid slug format. It should be 'owner/name': %s", slug)
	}

	// Get at least 30 releases
	opts := github.ListOptions{PerPage: 30}
	rels, res, err := cli.Repositories.ListReleases(ctx, repo[0], repo[1], &opts)
	if err != nil {
		log.Println("API returned an error response:", err)
		if res != nil && res.StatusCode == 404 {
			// 404 means repository not found or release not found. It's not an error here.
			err = nil
			log.Println("API returned 404. Repository or release not found")
		}
		return nil, err
	}

	var versions semver.Collection
	for _, rel := range rels {
		if strings.HasPrefix(*rel.Name, "v") {
			v := semver.MustParse(*rel.Name)
			if v.Prerelease() == "" {
				versions = append(versions, v)
			}
			if v.Prerelease() != "" && preReleases {
				versions = append(versions, v)
			}
		}
	}
	// Return them reversed sorted so the higher is the first one in the collection!
	sort.Sort(sort.Reverse(versions))
	return versions, nil
}
