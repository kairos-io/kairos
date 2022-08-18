package github

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"

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

func FindReleases(ctx context.Context, token, slug string) ([]string, error) {
	hc := newHTTPClient(ctx, token)
	cli := github.NewClient(hc)

	repo := strings.Split(slug, "/")
	if len(repo) != 2 || repo[0] == "" || repo[1] == "" {
		return nil, fmt.Errorf("Invalid slug format. It should be 'owner/name': %s", slug)
	}

	rels, res, err := cli.Repositories.ListReleases(ctx, repo[0], repo[1], nil)
	if err != nil {
		log.Println("API returned an error response:", err)
		if res != nil && res.StatusCode == 404 {
			// 404 means repository not found or release not found. It's not an error here.
			err = nil
			log.Println("API returned 404. Repository or release not found")
		}
		return nil, err
	}

	versions := []string{}
	for _, rel := range rels {
		if strings.HasPrefix(*rel.Name, "v") {
			versions = append(versions, *rel.Name)
		}
	}
	return versions, nil
}
