// Package httpx holds the HTTP plumbing shared by the forge and LLM clients:
// a client that never follows redirects, retries for transient failures, and
// bounded error bodies.
package httpx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// NewClient returns an HTTP client that does not follow redirects. Requests
// carry credentials (forge token, LLM API key) in headers, and Go only strips
// Authorization/Cookie on cross-host redirects, not custom headers such as
// x-api-key, so a redirect could leak a secret to another host. Redirect
// responses are returned to the caller and treated as errors.
func NewClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// Retry configures retries for transient failures: connection errors and
// 408, 409, 429 and 5xx responses.
type Retry struct {
	Attempts int           // total attempts, including the first; <1 means 1
	Delay    time.Duration // wait between attempts unless Retry-After says otherwise
	MaxDelay time.Duration // cap on a server-supplied Retry-After; 0 means 60s
}

// StatusError is returned when the final response has an unexpected status.
type StatusError struct {
	Method string
	URL    string
	Status int
	Body   string // truncated response body, for diagnostics
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("%s %s: HTTP %d", e.Method, e.URL, e.Status)
}

const maxErrorBody = 4096

// Do sends the request built by newReq, retrying per r, and returns the
// response body when the status is one of ok. newReq is called once per
// attempt so request bodies are fresh each time. maxBody caps how much of a
// successful body is read; the bool result reports whether the body was cut
// off at that cap.
func Do(ctx context.Context, c *http.Client, r Retry, newReq func(context.Context) (*http.Request, error), maxBody int64, ok ...int) ([]byte, bool, error) {
	attempts := max(r.Attempts, 1)
	var lastErr error
	for attempt := 1; ; attempt++ {
		req, err := newReq(ctx)
		if err != nil {
			return nil, false, err
		}
		body, truncated, wait, err := once(c, req, maxBody, ok)
		if err == nil {
			return body, truncated, nil
		}
		lastErr = err
		if wait < 0 || attempt >= attempts {
			return nil, false, lastErr
		}
		if wait == 0 {
			wait = r.Delay
		}
		if limit := orDefault(r.MaxDelay, 60*time.Second); wait > limit {
			wait = limit
		}
		select {
		case <-ctx.Done():
			return nil, false, errors.Join(lastErr, ctx.Err())
		case <-time.After(wait):
		}
	}
}

// once performs a single attempt. wait is -1 when the failure is not
// retryable, 0 for "use the default delay", or the server's Retry-After.
func once(c *http.Client, req *http.Request, maxBody int64, ok []int) (body []byte, truncated bool, wait time.Duration, err error) {
	resp, err := c.Do(req)
	if err != nil {
		if req.Context().Err() != nil {
			return nil, false, -1, err
		}
		return nil, false, 0, err // connection error or timeout: retry
	}
	defer resp.Body.Close()

	for _, code := range ok {
		if resp.StatusCode == code {
			body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
			if err != nil {
				return nil, false, 0, fmt.Errorf("read %s response: %w", req.URL.Redacted(), err)
			}
			if int64(len(body)) > maxBody {
				return body[:maxBody], true, 0, nil
			}
			return body, false, 0, nil
		}
	}

	errBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
	serr := &StatusError{
		Method: req.Method,
		URL:    req.URL.Redacted(),
		Status: resp.StatusCode,
		Body:   string(bytes.ToValidUTF8(errBody, []byte("?"))),
	}
	if !retryable(resp.StatusCode) {
		return nil, false, -1, serr
	}
	return nil, false, retryAfter(resp.Header.Get("Retry-After")), serr
}

func retryable(code int) bool {
	return code == http.StatusRequestTimeout || code == http.StatusConflict ||
		code == http.StatusTooManyRequests || code >= 500
}

// retryAfter parses a Retry-After header given in seconds; anything else
// falls back to the default delay.
func retryAfter(v string) time.Duration {
	if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	return 0
}

func orDefault(v, fallback time.Duration) time.Duration {
	if v > 0 {
		return v
	}
	return fallback
}
