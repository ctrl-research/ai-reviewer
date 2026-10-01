// Package forge talks to the GitHub and Forgejo/Gitea REST APIs: it reads a
// pull request's metadata and diff and maintains the sticky review comment.
package forge

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ctrl-research/ego/internal/httpx"
)

// Platform selects API differences between forges.
type Platform string

const (
	GitHub  Platform = "github"
	Forgejo Platform = "forgejo"
	Gitea   Platform = "gitea"
)

// Client is a forge API client scoped to one repository.
type Client struct {
	Platform Platform
	APIURL   string // e.g. https://api.github.com or https://codeberg.org/api/v1
	Repo     string // owner/name
	Token    string
	HTTP     *http.Client
	Retry    httpx.Retry // applied to idempotent reads only
}

// NewClient returns a Client with the default HTTP client and retry policy.
func NewClient(p Platform, apiURL, repo, token string) *Client {
	return &Client{
		Platform: p,
		APIURL:   strings.TrimRight(apiURL, "/"),
		Repo:     repo,
		Token:    token,
		HTTP:     httpx.NewClient(60 * time.Second),
		Retry:    httpx.Retry{Attempts: 3, Delay: 5 * time.Second},
	}
}

// PullRequest is the subset of PR metadata used to build the prompt.
type PullRequest struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// maxJSONBody bounds metadata and comment-list responses.
const maxJSONBody = 32 << 20

// PullRequest fetches the PR's title and description.
func (c *Client) PullRequest(ctx context.Context, number int) (*PullRequest, error) {
	body, _, err := httpx.Do(ctx, c.HTTP, c.Retry,
		c.request(http.MethodGet, c.repoURL("pulls", strconv.Itoa(number)), "application/json", nil),
		maxJSONBody, http.StatusOK)
	if err != nil {
		return nil, fmt.Errorf("fetch PR metadata: %w", err)
	}
	var pr PullRequest
	if err := json.Unmarshal(body, &pr); err != nil {
		return nil, fmt.Errorf("decode PR metadata: %w", err)
	}
	return &pr, nil
}

// Diff fetches the PR's unified diff, reading at most maxBytes of it. The bool
// result reports whether the diff was longer than maxBytes.
func (c *Client) Diff(ctx context.Context, number int, maxBytes int64) ([]byte, bool, error) {
	url, accept := c.repoURL("pulls", strconv.Itoa(number)), "application/vnd.github.v3.diff"
	if c.Platform != GitHub {
		url, accept = c.repoURL("pulls", strconv.Itoa(number)+".diff"), "text/plain"
	}
	diff, truncated, err := httpx.Do(ctx, c.HTTP, c.Retry, c.request(http.MethodGet, url, accept, nil), maxBytes, http.StatusOK)
	if err != nil {
		return nil, false, fmt.Errorf("fetch PR diff: %w", err)
	}
	return diff, truncated, nil
}

type comment struct {
	ID   int64  `json:"id"`
	Body string `json:"body"`
}

// maxCommentPages guards against a forge that ignores the page parameter and
// returns the same comments forever.
const maxCommentPages = 100

// FindComment returns the ID of the first issue comment on the PR whose body
// starts with marker, or 0 if there is none.
func (c *Client) FindComment(ctx context.Context, number int, marker string) (int64, error) {
	// GitHub pages with per_page (max 100); Gitea/Forgejo use limit (default
	// max 50) and ignore per_page.
	sizeParam := "per_page=100"
	if c.Platform != GitHub {
		sizeParam = "limit=50"
	}
	seen := map[int64]bool{}
	for page := 1; page <= maxCommentPages; page++ {
		url := fmt.Sprintf("%s?%s&page=%d", c.repoURL("issues", strconv.Itoa(number), "comments"), sizeParam, page)
		body, _, err := httpx.Do(ctx, c.HTTP, c.Retry, c.request(http.MethodGet, url, "application/json", nil), maxJSONBody, http.StatusOK)
		if err != nil {
			return 0, fmt.Errorf("list PR comments: %w", err)
		}
		var comments []comment
		if err := json.Unmarshal(body, &comments); err != nil {
			return 0, fmt.Errorf("decode PR comments: %w", err)
		}
		// Stop on an empty page, or when a page repeats comments already seen
		// (the endpoint is not actually paginating).
		if len(comments) == 0 || seen[comments[0].ID] {
			return 0, nil
		}
		for _, cm := range comments {
			if strings.HasPrefix(cm.Body, marker) {
				return cm.ID, nil
			}
			seen[cm.ID] = true
		}
	}
	return 0, nil
}

// UpsertComment updates the sticky comment identified by marker, or creates it
// if none exists. body must already start with marker. It reports whether a
// new comment was created and the comment's ID when known.
func (c *Client) UpsertComment(ctx context.Context, number int, marker, body string) (created bool, id int64, err error) {
	id, err = c.FindComment(ctx, number, marker)
	if err != nil {
		return false, 0, err
	}
	payload, err := json.Marshal(map[string]string{"body": body})
	if err != nil {
		return false, 0, err
	}

	if id != 0 {
		_, _, err := httpx.Do(ctx, c.HTTP, httpx.Retry{},
			c.request(http.MethodPatch, c.repoURL("issues", "comments", strconv.FormatInt(id, 10)), "application/json", payload),
			maxJSONBody, http.StatusOK, http.StatusCreated)
		if err == nil {
			return false, id, nil
		}
		// Anyone can post a comment that starts with the marker. If the match
		// isn't ours to edit, post a fresh comment instead of failing.
		var serr *httpx.StatusError
		if !errors.As(err, &serr) || (serr.Status != http.StatusForbidden && serr.Status != http.StatusNotFound) {
			return false, id, fmt.Errorf("update review comment %d: %w", id, err)
		}
	}

	resp, _, err := httpx.Do(ctx, c.HTTP, httpx.Retry{},
		c.request(http.MethodPost, c.repoURL("issues", strconv.Itoa(number), "comments"), "application/json", payload),
		maxJSONBody, http.StatusCreated, http.StatusOK)
	if err != nil {
		return false, 0, fmt.Errorf("create review comment: %w", err)
	}
	var cm comment
	_ = json.Unmarshal(resp, &cm) // the ID is informational only
	return true, cm.ID, nil
}

func (c *Client) repoURL(parts ...string) string {
	return c.APIURL + "/repos/" + c.Repo + "/" + strings.Join(parts, "/")
}

func (c *Client) request(method, url, accept string, body []byte) func(context.Context) (*http.Request, error) {
	scheme := "token"
	if c.Platform == GitHub {
		scheme = "Bearer"
	}
	return func(ctx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", scheme+" "+c.Token)
		req.Header.Set("Accept", accept)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		return req, nil
	}
}
