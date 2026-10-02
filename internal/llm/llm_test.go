package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ctrl-research/ego/internal/httpx"
)

type captured struct {
	path    string
	headers http.Header
	body    map[string]any
}

// serve starts a fake provider that records the request and replies with
// status and response.
func serve(t *testing.T, status int, response string) (*captured, string) {
	t.Helper()
	got := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.path = r.URL.Path
		got.headers = r.Header.Clone()
		raw, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(raw, &got.body); err != nil {
			t.Errorf("request body is not JSON: %v", err)
		}
		w.WriteHeader(status)
		io.WriteString(w, response)
	}))
	t.Cleanup(srv.Close)
	return got, srv.URL
}

func newTestClient(t *testing.T, provider, baseURL, key string) *Client {
	t.Helper()
	c, err := New(provider, baseURL, key)
	if err != nil {
		t.Fatal(err)
	}
	c.HTTP = httpx.NewClient(5 * time.Second)
	c.Retry = httpx.Retry{Attempts: 1}
	return c
}

var req = Request{Model: "m", System: "sys", Prompt: "diff", MaxTokens: 123}

func TestAnthropicRequestAndResponse(t *testing.T) {
	got, url := serve(t, 200, `{"content":[{"type":"thinking","thinking":""},{"type":"text","text":"part 1"},{"type":"text","text":"part 2"}],"stop_reason":"end_turn"}`)
	res, err := newTestClient(t, Anthropic, url+"/", "key").Review(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "part 1\npart 2" || res.Truncated {
		t.Errorf("result = %+v", res)
	}
	if got.path != "/v1/messages" {
		t.Errorf("path = %s", got.path)
	}
	if got.headers.Get("x-api-key") != "key" || got.headers.Get("anthropic-version") != "2023-06-01" {
		t.Errorf("headers = %v", got.headers)
	}
	if got.body["system"] != "sys" || got.body["model"] != "m" || got.body["max_tokens"] != float64(123) {
		t.Errorf("body = %v", got.body)
	}
	msgs := got.body["messages"].([]any)
	if len(msgs) != 1 || msgs[0].(map[string]any)["content"] != "diff" {
		t.Errorf("messages = %v", msgs)
	}
}

func TestAnthropicStopReasons(t *testing.T) {
	_, url := serve(t, 200, `{"content":[{"type":"text","text":"partial"}],"stop_reason":"max_tokens"}`)
	res, err := newTestClient(t, Anthropic, url, "key").Review(context.Background(), req)
	if err != nil || !res.Truncated {
		t.Errorf("max_tokens: res=%+v err=%v", res, err)
	}

	_, url = serve(t, 200, `{"content":[],"stop_reason":"refusal","stop_details":{"type":"refusal","category":"cyber","explanation":"nope"}}`)
	_, err = newTestClient(t, Anthropic, url, "key").Review(context.Background(), req)
	if err == nil || !strings.Contains(err.Error(), "declined") || !strings.Contains(err.Error(), "cyber") {
		t.Errorf("refusal: err = %v", err)
	}

	_, url = serve(t, 200, `{"content":[],"stop_reason":"end_turn"}`)
	_, err = newTestClient(t, Anthropic, url, "key").Review(context.Background(), req)
	if !errors.Is(err, ErrEmpty) {
		t.Errorf("empty: err = %v", err)
	}
}

func TestOpenAIRequestAndResponse(t *testing.T) {
	got, url := serve(t, 200, `{"choices":[{"message":{"content":"looks good"},"finish_reason":"length"}]}`)
	res, err := newTestClient(t, OpenAI, url, "key").Review(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "looks good" || !res.Truncated {
		t.Errorf("result = %+v", res)
	}
	if got.path != "/chat/completions" || got.headers.Get("Authorization") != "Bearer key" {
		t.Errorf("path=%s auth=%q", got.path, got.headers.Get("Authorization"))
	}
	msgs := got.body["messages"].([]any)
	if len(msgs) != 2 || msgs[0].(map[string]any)["role"] != "system" {
		t.Errorf("messages = %v", msgs)
	}
}

func TestOpenAICompatibleWithoutKey(t *testing.T) {
	got, url := serve(t, 200, `{"choices":[{"message":{"content":"ok"}}]}`)
	if _, err := newTestClient(t, OpenAICompatible, url+"/v1", "").Review(context.Background(), req); err != nil {
		t.Fatal(err)
	}
	if got.path != "/v1/chat/completions" {
		t.Errorf("path = %s", got.path)
	}
	if _, ok := got.headers["Authorization"]; ok {
		t.Error("Authorization header sent without an API key")
	}
}

func TestOpenAIRefusalAndEmpty(t *testing.T) {
	_, url := serve(t, 200, `{"choices":[{"message":{"content":null,"refusal":"can't help"}}]}`)
	if _, err := newTestClient(t, OpenAI, url, "k").Review(context.Background(), req); err == nil || !strings.Contains(err.Error(), "can't help") {
		t.Errorf("refusal: err = %v", err)
	}
	_, url = serve(t, 200, `{"choices":[]}`)
	if _, err := newTestClient(t, OpenAI, url, "k").Review(context.Background(), req); !errors.Is(err, ErrEmpty) {
		t.Errorf("empty: err = %v", err)
	}
}

func TestHTTPErrorIncludesBody(t *testing.T) {
	_, url := serve(t, 401, `{"error":"invalid key"}`)
	_, err := newTestClient(t, Anthropic, url, "bad").Review(context.Background(), req)
	var serr *httpx.StatusError
	if !errors.As(err, &serr) || serr.Status != 401 || !strings.Contains(serr.Body, "invalid key") {
		t.Errorf("err = %v", err)
	}
}

func TestNewValidatesProvider(t *testing.T) {
	if _, err := New(OpenAICompatible, "", ""); err == nil {
		t.Error("openai-compatible without base URL: want error")
	}
	if _, err := New("bogus", "", ""); err == nil {
		t.Error("unknown provider: want error")
	}
	c, err := New(OpenAI, "", "k")
	if err != nil || c.URL != "https://api.openai.com/v1/chat/completions" {
		t.Errorf("default OpenAI URL = %q err=%v", c.URL, err)
	}
}

func TestOpenAIStripsInlineThinking(t *testing.T) {
	for _, tc := range []struct {
		name, content, finish, want, wantErr string
	}{
		{"think block", `<think>\nlet me look\n</think>\n\n## Summary\nok`, "stop", "## Summary\nok", ""},
		{"no think block", "## Summary\nmentions <think> later", "stop", "## Summary\nmentions <think> later", ""},
		{"only thinking", "<think>hmm</think>", "stop", "", "no review text"},
		{"cut off mid-thought", "<think>still going", "length", "", "reached max-tokens before writing the review"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{
				"message":       map[string]any{"content": strings.ReplaceAll(tc.content, `\n`, "\n")},
				"finish_reason": tc.finish,
			}}})
			_, url := serve(t, 200, string(resp))
			res, err := newTestClient(t, OpenAICompatible, url, "").Review(context.Background(), req)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("err = %v, want %q", err, tc.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if res.Text != tc.want {
				t.Errorf("text = %q, want %q", res.Text, tc.want)
			}
		})
	}
}

func TestExtraBodyMergesAndRemoves(t *testing.T) {
	got, url := serve(t, 200, `{"choices":[{"message":{"content":"ok"}}]}`)
	r := req
	r.Extra = map[string]json.RawMessage{
		"reasoning_split":       json.RawMessage(`true`),
		"max_tokens":            json.RawMessage(`null`),
		"max_completion_tokens": json.RawMessage(`500`),
	}
	if _, err := newTestClient(t, OpenAICompatible, url, "").Review(context.Background(), r); err != nil {
		t.Fatal(err)
	}
	if got.body["reasoning_split"] != true || got.body["max_completion_tokens"] != float64(500) {
		t.Errorf("extra fields missing: %v", got.body)
	}
	if _, ok := got.body["max_tokens"]; ok {
		t.Errorf("max_tokens not removed: %v", got.body)
	}
	if got.body["model"] != "m" {
		t.Errorf("model = %v", got.body["model"])
	}
}

func TestAnthropicStructuredOutputAndUsage(t *testing.T) {
	schema := json.RawMessage(`{"type":"object"}`)
	for model, wantFormat := range map[string]bool{
		"claude-opus-4-8":           true,
		"claude-haiku-4-5-20251001": true,
		"claude-opus-4-7":           false, // rejects output_config.format
		"claude-sonnet-4-6":         false,
	} {
		t.Run(model, func(t *testing.T) {
			got, url := serve(t, 200, `{"model":"`+model+`-20260801","content":[{"type":"text","text":"{}"}],"stop_reason":"end_turn",
				"usage":{"input_tokens":100,"cache_creation_input_tokens":20,"cache_read_input_tokens":5,"output_tokens":42}}`)
			r := req
			r.Model, r.Schema = model, schema
			r.Extra = map[string]json.RawMessage{"output_config": json.RawMessage(`{"effort":"high"}`)}
			res, err := newTestClient(t, Anthropic, url, "k").Review(context.Background(), r)
			if err != nil {
				t.Fatal(err)
			}
			if res.Model != model+"-20260801" || res.InputTokens != 125 || res.OutputTokens != 42 {
				t.Errorf("result = %+v", res)
			}
			oc, _ := got.body["output_config"].(map[string]any)
			if oc["effort"] != "high" {
				t.Errorf("extra-body effort lost: %v", got.body["output_config"])
			}
			_, hasFormat := oc["format"]
			if hasFormat != wantFormat {
				t.Errorf("format sent = %v, want %v (%v)", hasFormat, wantFormat, oc)
			}
		})
	}
}

func TestOpenAIUsage(t *testing.T) {
	_, url := serve(t, 200, `{"model":"gpt-x-2026","choices":[{"message":{"content":"ok"}}],"usage":{"prompt_tokens":7,"completion_tokens":3}}`)
	res, err := newTestClient(t, OpenAI, url, "k").Review(context.Background(), Request{Model: "gpt-x", Schema: json.RawMessage(`{}`)})
	if err != nil {
		t.Fatal(err)
	}
	if res.Model != "gpt-x-2026" || res.InputTokens != 7 || res.OutputTokens != 3 {
		t.Errorf("result = %+v", res)
	}
}

func TestMergeNullRemovesNestedKey(t *testing.T) {
	fields := map[string]json.RawMessage{"a": json.RawMessage(`{"x":1,"y":2}`), "b": json.RawMessage(`1`)}
	if err := merge(fields, map[string]json.RawMessage{"a": json.RawMessage(`{"y":null,"z":3}`), "b": json.RawMessage(`{"q":1}`)}); err != nil {
		t.Fatal(err)
	}
	if string(fields["a"]) != `{"x":1,"z":3}` || string(fields["b"]) != `{"q":1}` {
		t.Errorf("fields = %s %s", fields["a"], fields["b"])
	}
}

func TestEmptyResponseExplainsTokenLimit(t *testing.T) {
	for name, tc := range map[string]struct {
		provider, body, want string
	}{
		"openai reasoning split, out of tokens": {OpenAICompatible,
			`{"choices":[{"message":{"content":"","reasoning_content":"long thoughts"},"finish_reason":"length"}],"usage":{"completion_tokens":16000}}`,
			"reached max-tokens (16000 output tokens) before writing the review"},
		"openai null content, out of tokens": {OpenAICompatible,
			`{"choices":[{"message":{"content":null},"finish_reason":"length"}]}`,
			"reached max-tokens before writing the review"},
		"openai reasoning only, finished": {OpenAICompatible,
			`{"choices":[{"message":{"content":"","reasoning_content":"thoughts"},"finish_reason":"stop"}]}`,
			"returned reasoning but no answer"},
		"anthropic thinking only, out of tokens": {Anthropic,
			`{"content":[{"type":"thinking","thinking":""}],"stop_reason":"max_tokens","usage":{"output_tokens":16000}}`,
			"reached max-tokens (16000 output tokens) before writing the review"},
	} {
		t.Run(name, func(t *testing.T) {
			_, url := serve(t, 200, tc.body)
			_, err := newTestClient(t, tc.provider, url, "k").Review(context.Background(), req)
			if !errors.Is(err, ErrEmpty) || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want ErrEmpty containing %q", err, tc.want)
			}
		})
	}
}
