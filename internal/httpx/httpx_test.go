package httpx

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func get(url string) func(context.Context) (*http.Request, error) {
	return func(ctx context.Context) (*http.Request, error) {
		return http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	}
}

func TestDoRetriesTransientStatus(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	body, _, err := Do(context.Background(), NewClient(time.Second), Retry{Attempts: 3, Delay: time.Millisecond}, get(srv.URL), 1024, http.StatusOK)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "ok" || calls.Load() != 3 {
		t.Errorf("body=%q calls=%d, want ok after 3 calls", body, calls.Load())
	}
}

func TestDoDoesNotRetryClientErrors(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, "bad request detail", http.StatusBadRequest)
	}))
	defer srv.Close()

	_, _, err := Do(context.Background(), NewClient(time.Second), Retry{Attempts: 3, Delay: time.Millisecond}, get(srv.URL), 1024, http.StatusOK)
	var serr *StatusError
	if !errors.As(err, &serr) || serr.Status != http.StatusBadRequest {
		t.Fatalf("err = %v, want StatusError 400", err)
	}
	if serr.Body != "bad request detail\n" {
		t.Errorf("body = %q", serr.Body)
	}
	if calls.Load() != 1 {
		t.Errorf("calls = %d, want 1", calls.Load())
	}
}

func TestDoGivesUpAfterAttempts(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	_, _, err := Do(context.Background(), NewClient(time.Second), Retry{Attempts: 2, Delay: time.Millisecond}, get(srv.URL), 1024, http.StatusOK)
	if err == nil || calls.Load() != 2 {
		t.Errorf("err=%v calls=%d, want error after 2 calls", err, calls.Load())
	}
}

func TestDoDoesNotFollowRedirects(t *testing.T) {
	var leaked atomic.Bool
	other := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		leaked.Store(true)
	}))
	defer other.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, other.URL, http.StatusFound)
	}))
	defer srv.Close()

	newReq := func(ctx context.Context) (*http.Request, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
		if err == nil {
			req.Header.Set("x-api-key", "secret")
		}
		return req, err
	}
	_, _, err := Do(context.Background(), NewClient(time.Second), Retry{}, newReq, 1024, http.StatusOK)
	var serr *StatusError
	if !errors.As(err, &serr) || serr.Status != http.StatusFound {
		t.Fatalf("err = %v, want StatusError 302", err)
	}
	if leaked.Load() {
		t.Error("redirect was followed")
	}
}

func TestDoTruncatesAtMaxBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("0123456789"))
	}))
	defer srv.Close()

	body, truncated, err := Do(context.Background(), NewClient(time.Second), Retry{}, get(srv.URL), 4, http.StatusOK)
	if err != nil || string(body) != "0123" || !truncated {
		t.Errorf("body=%q truncated=%v err=%v", body, truncated, err)
	}
	body, truncated, err = Do(context.Background(), NewClient(time.Second), Retry{}, get(srv.URL), 10, http.StatusOK)
	if err != nil || string(body) != "0123456789" || truncated {
		t.Errorf("exact fit: body=%q truncated=%v err=%v", body, truncated, err)
	}
}

func TestDoNoTimeoutRetry(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()

	for _, noRetry := range []bool{true, false} {
		calls.Store(0)
		_, _, err := Do(context.Background(), NewClient(50*time.Millisecond),
			Retry{Attempts: 3, Delay: time.Millisecond, NoTimeoutRetry: noRetry}, get(srv.URL), 1024, http.StatusOK)
		want := int32(3)
		if noRetry {
			want = 1
		}
		if err == nil || !isTimeout(err) || calls.Load() != want {
			t.Errorf("NoTimeoutRetry=%v: err=%v calls=%d, want timeout after %d calls", noRetry, err, calls.Load(), want)
		}
	}
}
