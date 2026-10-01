package review

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/ctrl-research/ego/internal/forge"
	"github.com/ctrl-research/ego/internal/gha"
	"github.com/ctrl-research/ego/internal/llm"
)

func env(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func validEnv() map[string]string {
	return map[string]string{
		"GITHUB_REPOSITORY": "owner/repo",
		"EGO_TOKEN":         "tok",
		"EGO_API_KEY":       "key",
		"EGO_PR_NUMBER":     "7",
	}
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(env(validEnv()), nil)
	if err != nil {
		t.Fatal(err)
	}
	s, err := cfg.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if s.Platform != forge.GitHub || s.APIURL != "https://api.github.com" || s.Provider != "anthropic" ||
		s.Model != "claude-opus-4-8" || s.MaxTokens != 16000 || s.MaxDiffBytes != 300000 || !s.PostComment ||
		s.Repo != "owner/repo" || s.PRNumber != 7 {
		t.Errorf("settings = %+v", s)
	}
}

func TestFlagsOverrideEnv(t *testing.T) {
	vars := validEnv()
	vars["EGO_MODEL"] = "from-env"
	cfg, err := Load(env(vars), []string{"--model", "from-flag", "--pr", "9"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Model != "from-flag" || cfg.PRNumber != "9" {
		t.Errorf("cfg = %+v", cfg)
	}
}

func TestValidate(t *testing.T) {
	for _, tc := range []struct {
		name, key, value, want string
	}{
		{"bad platform", "EGO_PLATFORM", "gitlab", "unknown platform"},
		{"missing pr", "EGO_PR_NUMBER", "", "no pull request number"},
		{"pr not int", "EGO_PR_NUMBER", "7; rm -rf", "'pr-number' must be a positive integer"},
		{"pr zero", "EGO_PR_NUMBER", "0", "'pr-number' must be a positive integer"},
		{"pr signed", "EGO_PR_NUMBER", "+7", "'pr-number' must be a positive integer"},
		{"max tokens", "EGO_MAX_TOKENS", "-1", "'max-tokens' must be a non-negative integer"},
		{"max diff", "EGO_MAX_DIFF_BYTES", "1e6", "'max-diff-bytes' must be a non-negative integer"},
		{"repo path", "EGO_REPO", "owner/repo/../../x", "repository must be 'owner/name'"},
		{"no key", "EGO_API_KEY", "", "'api-key' is required for provider 'anthropic'"},
		{"bad provider", "EGO_PROVIDER", "gemini", "unknown provider"},
		{"compatible needs base", "EGO_PROVIDER", "openai-compatible", "'base-url' is required"},
		{"post comment", "EGO_POST_COMMENT", "yes", "'post-comment' must be 'true' or 'false'"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vars := validEnv()
			vars[tc.key] = tc.value
			cfg, err := Load(env(vars), nil)
			if err != nil {
				t.Fatal(err)
			}
			_, err = cfg.Validate()
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want %q", err, tc.want)
			}
		})
	}

	// An empty token after env fallback is rejected too.
	vars := validEnv()
	delete(vars, "EGO_TOKEN")
	cfg, _ := Load(env(vars), nil)
	if _, err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "'github-token' is required") {
		t.Errorf("missing token: err = %v", err)
	}
}

func TestOpenAICompatibleNeedsNoKey(t *testing.T) {
	vars := validEnv()
	delete(vars, "EGO_API_KEY")
	vars["EGO_PROVIDER"] = "openai-compatible"
	vars["EGO_BASE_URL"] = "http://localhost:11434/v1"
	cfg, _ := Load(env(vars), nil)
	if _, err := cfg.Validate(); err != nil {
		t.Error(err)
	}
}

func TestUserPrompt(t *testing.T) {
	pr := &forge.PullRequest{Title: "Add x", Body: "Does x."}
	got := UserPrompt(pr, []byte("+x\n"), false, 100)
	want := "# Add x\n\nDoes x.\n\n\n## Diff\n\n```diff\n+x\n\n```\n"
	if got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}

	got = UserPrompt(pr, []byte("+héllo"[:3]), true, 3) // cuts 'é' in half
	if !strings.Contains(got, "```diff\n+h\n\n[diff truncated at 3 bytes]\n\n```\n") {
		t.Errorf("truncated prompt = %q", got)
	}
}

func TestTrimPartialRune(t *testing.T) {
	for in, want := range map[string]string{
		"abc":           "abc",
		"ab\xc3":        "ab",
		"a€":            "a€",
		"a\xe2\x82":     "a",
		"a\xf0\x9f\x98": "a",
		"":              "",
	} {
		if got := string(trimPartialRune([]byte(in))); got != want {
			t.Errorf("trimPartialRune(%q) = %q, want %q", in, got, want)
		}
	}
}

type fakeForge struct {
	diffTruncated bool
	posted        string
}

func (f *fakeForge) PullRequest(context.Context, int) (*forge.PullRequest, error) {
	return &forge.PullRequest{Title: "T", Body: "B"}, nil
}

func (f *fakeForge) Diff(context.Context, int, int64) ([]byte, bool, error) {
	return []byte("+x"), f.diffTruncated, nil
}

func (f *fakeForge) UpsertComment(_ context.Context, _ int, marker, body string) (bool, int64, error) {
	if !strings.HasPrefix(body, marker) {
		return false, 0, errors.New("body does not start with marker")
	}
	f.posted = body
	return true, 1, nil
}

type fakeLLM struct {
	got llm.Request
	res llm.Result
}

func (f *fakeLLM) Review(_ context.Context, req llm.Request) (*llm.Result, error) {
	f.got = req
	return &f.res, nil
}

func TestRun(t *testing.T) {
	s := &Settings{PRNumber: 7, MaxDiffBytes: 2, MaxTokens: 10, Provider: "anthropic", Model: "m", PostComment: true}
	f := &fakeForge{diffTruncated: true}
	r := &fakeLLM{res: llm.Result{Text: "review text\n", Truncated: true}}
	var logs bytes.Buffer

	text, err := Run(context.Background(), s, f, r, &gha.Logger{W: &logs, Actions: true})
	if err != nil {
		t.Fatal(err)
	}
	if text != "review text\n" {
		t.Errorf("text = %q", text)
	}
	if r.got.System != DefaultSystemPrompt || !strings.Contains(r.got.Prompt, "[diff truncated at 2 bytes]") {
		t.Errorf("request = %+v", r.got)
	}
	want := "<!-- pr-review-action -->\n\nreview text\n\n---\n_Reviewed by `ego` (anthropic / m)_\n"
	if f.posted != want {
		t.Errorf("posted %q\nwant   %q", f.posted, want)
	}
	for _, w := range []string{"::warning::PR diff is larger than 2 bytes", "::warning::Review stopped at max-tokens (10)", "Created new review comment"} {
		if !strings.Contains(logs.String(), w) {
			t.Errorf("logs missing %q:\n%s", w, logs.String())
		}
	}
}

func TestRunCustomPromptNoComment(t *testing.T) {
	s := &Settings{PRNumber: 7, MaxDiffBytes: 100, ReviewPrompt: "custom", PostComment: false}
	f := &fakeForge{}
	r := &fakeLLM{res: llm.Result{Text: "ok"}}
	if _, err := Run(context.Background(), s, f, r, &gha.Logger{W: &bytes.Buffer{}}); err != nil {
		t.Fatal(err)
	}
	if r.got.System != "custom" || f.posted != "" {
		t.Errorf("system=%q posted=%q", r.got.System, f.posted)
	}
}
