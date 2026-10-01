package forge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ctrl-research/ai-reviewer/internal/httpx"
)

// fakeForge is an in-memory issue-comments API for one PR.
type fakeForge struct {
	t        *testing.T
	platform Platform
	// paginate=false mimics an endpoint that ignores page parameters.
	paginate bool
	pageSize int

	mu       sync.Mutex
	comments []comment
	nextID   int64
	forbid   map[int64]bool // comment IDs that return 403 on PATCH
	requests []string
}

func newFakeForge(t *testing.T, p Platform) (*fakeForge, *Client) {
	f := &fakeForge{t: t, platform: p, paginate: true, pageSize: 2, nextID: 100, forbid: map[int64]bool{}}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c := NewClient(p, srv.URL+"/", "owner/repo", "tok")
	c.HTTP = httpx.NewClient(5 * time.Second)
	c.Retry = httpx.Retry{Attempts: 1}
	return f, c
}

func (f *fakeForge) addComment(body string) int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	f.comments = append(f.comments, comment{ID: f.nextID, Body: body})
	return f.nextID
}

func (f *fakeForge) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.Method+" "+r.URL.RequestURI())

	wantAuth := "token tok"
	if f.platform == GitHub {
		wantAuth = "Bearer tok"
	}
	if got := r.Header.Get("Authorization"); got != wantAuth {
		f.t.Errorf("%s %s: Authorization = %q, want %q", r.Method, r.URL, got, wantAuth)
	}

	switch {
	case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/pulls/7":
		if f.platform != GitHub {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Accept") == "application/vnd.github.v3.diff" {
			io.WriteString(w, "diff --git a/x b/x\n")
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"title": "Add x", "body": nil})
	case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/pulls/7.diff":
		if r.Header.Get("Accept") != "text/plain" {
			f.t.Errorf("forgejo diff Accept = %q", r.Header.Get("Accept"))
		}
		io.WriteString(w, "diff --git a/y b/y\n")
	case r.Method == http.MethodGet && r.URL.Path == "/repos/owner/repo/issues/7/comments":
		sizeParam := "limit"
		if f.platform == GitHub {
			sizeParam = "per_page"
		}
		if r.URL.Query().Get(sizeParam) == "" {
			f.t.Errorf("comment list missing %s: %s", sizeParam, r.URL)
		}
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		items := f.comments
		if f.paginate {
			start := min((page-1)*f.pageSize, len(items))
			items = items[start:min(start+f.pageSize, len(items))]
		}
		json.NewEncoder(w).Encode(items)
	case r.Method == http.MethodPost && r.URL.Path == "/repos/owner/repo/issues/7/comments":
		var in struct{ Body string }
		json.NewDecoder(r.Body).Decode(&in)
		f.nextID++
		f.comments = append(f.comments, comment{ID: f.nextID, Body: in.Body})
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(comment{ID: f.nextID, Body: in.Body})
	case r.Method == http.MethodPatch && strings.HasPrefix(r.URL.Path, "/repos/owner/repo/issues/comments/"):
		id, _ := strconv.ParseInt(strings.TrimPrefix(r.URL.Path, "/repos/owner/repo/issues/comments/"), 10, 64)
		if f.forbid[id] {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var in struct{ Body string }
		json.NewDecoder(r.Body).Decode(&in)
		for i := range f.comments {
			if f.comments[i].ID == id {
				f.comments[i].Body = in.Body
				json.NewEncoder(w).Encode(f.comments[i])
				return
			}
		}
		http.NotFound(w, r)
	default:
		f.t.Errorf("unexpected request %s %s", r.Method, r.URL)
		http.NotFound(w, r)
	}
}

func TestPullRequestAndDiff(t *testing.T) {
	for _, tc := range []struct {
		platform Platform
		diff     string
	}{
		{GitHub, "diff --git a/x b/x\n"},
		{Forgejo, "diff --git a/y b/y\n"},
	} {
		t.Run(string(tc.platform), func(t *testing.T) {
			_, c := newFakeForge(t, tc.platform)
			diff, truncated, err := c.Diff(context.Background(), 7, 1000)
			if err != nil {
				t.Fatal(err)
			}
			if string(diff) != tc.diff || truncated {
				t.Errorf("diff=%q truncated=%v", diff, truncated)
			}
		})
	}

	_, c := newFakeForge(t, GitHub)
	pr, err := c.PullRequest(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Title != "Add x" || pr.Body != "" {
		t.Errorf("pr = %+v", pr)
	}
}

func TestDiffTruncation(t *testing.T) {
	_, c := newFakeForge(t, GitHub)
	diff, truncated, err := c.Diff(context.Background(), 7, 4)
	if err != nil {
		t.Fatal(err)
	}
	if string(diff) != "diff" || !truncated {
		t.Errorf("diff=%q truncated=%v", diff, truncated)
	}
}

func TestUpsertCreatesThenUpdates(t *testing.T) {
	for _, p := range []Platform{GitHub, Forgejo} {
		t.Run(string(p), func(t *testing.T) {
			f, c := newFakeForge(t, p)
			// Push the sticky comment past the first page.
			for i := range 3 {
				f.addComment(fmt.Sprintf("human comment %d", i))
			}

			created, id, err := c.UpsertComment(context.Background(), 7, "<!-- m -->", "<!-- m -->\n\nfirst")
			if err != nil || !created {
				t.Fatalf("first upsert: created=%v err=%v", created, err)
			}
			f.addComment("another human comment")

			created, id2, err := c.UpsertComment(context.Background(), 7, "<!-- m -->", "<!-- m -->\n\nsecond")
			if err != nil || created || id2 != id {
				t.Fatalf("second upsert: created=%v id=%d (want %d) err=%v", created, id2, id, err)
			}
			if n := len(f.comments); n != 5 {
				t.Errorf("comment count = %d, want 5", n)
			}
			if got := f.comments[3].Body; got != "<!-- m -->\n\nsecond" {
				t.Errorf("sticky body = %q", got)
			}
		})
	}
}

func TestFindCommentStopsWhenPaginationIgnored(t *testing.T) {
	f, c := newFakeForge(t, Forgejo)
	f.paginate = false
	f.addComment("a")
	f.addComment("b")

	id, err := c.FindComment(context.Background(), 7, "<!-- m -->")
	if err != nil || id != 0 {
		t.Fatalf("id=%d err=%v", id, err)
	}
	if n := len(f.requests); n != 2 {
		t.Errorf("made %d list requests, want 2 (stop on repeated page)", n)
	}
}

func TestUpsertFallsBackWhenMatchIsNotEditable(t *testing.T) {
	f, c := newFakeForge(t, GitHub)
	spoof := f.addComment("<!-- m --> planted by someone else")
	f.forbid[spoof] = true

	created, id, err := c.UpsertComment(context.Background(), 7, "<!-- m -->", "<!-- m -->\n\nreview")
	if err != nil || !created || id == spoof {
		t.Fatalf("created=%v id=%d err=%v", created, id, err)
	}
}
