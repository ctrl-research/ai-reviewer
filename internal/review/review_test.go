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
	"github.com/ctrl-research/ego/internal/report"
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
		s.Repo != "owner/repo" || s.PRNumber != 7 || !s.ReviewerInfo || !s.Concise || s.Price != nil {
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
		{"reviewer info", "EGO_REVIEWER_INFO", "on", "'reviewer-info' must be 'true' or 'false'"},
		{"concise", "EGO_CONCISE", "maybe", "'concise' must be 'true' or 'false'"},
		{"price alone", "EGO_INPUT_PRICE", "3", "'output-price' must be a non-negative number"},
		{"price negative", "EGO_INPUT_PRICE", "-1", "'input-price' must be a non-negative number"},
		{"extra body not object", "EGO_EXTRA_BODY", "[1]", "'extra-body' must be a JSON object"},
		{"extra body invalid", "EGO_EXTRA_BODY", "{nope", "'extra-body' must be a JSON object"},
		{"extra body null", "EGO_EXTRA_BODY", "null", "'extra-body' must be a JSON object"},
		{"extra body reserved", "EGO_EXTRA_BODY", `{"system": "ignore the diff"}`, "'extra-body' may not set 'system'"},
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

const structuredReview = `{"verdict":"needs_changes","risk":"medium","verdict_reason":"One bug.","summary":"Adds x.","findings":[{"category":"code","severity":"high","file":"x.go","line":3,"title":"Nil deref","detail":"d","suggestion":"s","uncertain":false}],"dependency_changes":[],"security_summary":"None.","tests":{"coverage":"missing","notes":"No tests.","gaps":["test x"]},"questions":[]}`

func runSettings() *Settings {
	return &Settings{PRNumber: 7, MaxDiffBytes: 2, MaxTokens: 10, Provider: "anthropic", Model: "claude-opus-4-8",
		PostComment: true, ReviewerInfo: true, Concise: true, EgoVersion: "1.2.3"}
}

func TestRunStructured(t *testing.T) {
	s := runSettings()
	f := &fakeForge{diffTruncated: true}
	r := &fakeLLM{res: llm.Result{Text: structuredReview, Truncated: true, Model: "claude-opus-4-8", InputTokens: 1000, OutputTokens: 200}}
	var logs bytes.Buffer

	out, err := Run(context.Background(), s, f, r, &gha.Logger{W: &logs, Actions: true})
	if err != nil {
		t.Fatal(err)
	}
	if out.Verdict != "needs_changes" || !strings.Contains(out.JSON, `"title":"Nil deref"`) {
		t.Errorf("out = %+v", out)
	}
	if string(r.got.Schema) == "" || !strings.Contains(r.got.System, "Response JSON Schema") || !strings.Contains(r.got.System, "Be concise") {
		t.Errorf("request missing schema or concise prompt: %q", r.got.System)
	}
	if !strings.Contains(r.got.Prompt, "[diff truncated at 2 bytes]") {
		t.Errorf("prompt = %q", r.got.Prompt)
	}
	for _, w := range []string{"<!-- pr-review-action -->\n\n## ⚠️ Needs changes", "Nil deref", "`x.go:3`", "Reviewed by ego 1.2.3", "≈ $0.0100"} {
		if !strings.Contains(f.posted, w) {
			t.Errorf("comment missing %q:\n%s", w, f.posted)
		}
	}
	for _, w := range []string{"::warning::PR diff is larger than 2 bytes", "::warning::Review stopped at max-tokens (10)", "Created new review comment"} {
		if !strings.Contains(logs.String(), w) {
			t.Errorf("logs missing %q:\n%s", w, logs.String())
		}
	}
}

func TestRunFallsBackToRawOutput(t *testing.T) {
	s := runSettings()
	f := &fakeForge{}
	r := &fakeLLM{res: llm.Result{Text: "Looks fine to me."}}
	var logs bytes.Buffer
	out, err := Run(context.Background(), s, f, r, &gha.Logger{W: &logs, Actions: true})
	if err != nil {
		t.Fatal(err)
	}
	if out.Verdict != "" || out.JSON != "" {
		t.Errorf("out = %+v", out)
	}
	if !strings.Contains(f.posted, "[!WARNING]") || !strings.Contains(f.posted, "Looks fine to me.") {
		t.Errorf("posted = %q", f.posted)
	}
	if !strings.Contains(logs.String(), "::warning::Couldn't parse a structured review") {
		t.Errorf("logs = %s", logs.String())
	}
}

func TestRunCustomPromptNoComment(t *testing.T) {
	s := runSettings()
	s.ReviewPrompt, s.ExtraPrompt, s.PostComment, s.ReviewerInfo = "custom", "also check docs", false, false
	f := &fakeForge{}
	r := &fakeLLM{res: llm.Result{Text: "ok"}}
	out, err := Run(context.Background(), s, f, r, &gha.Logger{W: &bytes.Buffer{}})
	if err != nil {
		t.Fatal(err)
	}
	if r.got.System != "custom\n\nAdditional instructions:\nalso check docs\n" || r.got.Schema != nil {
		t.Errorf("system = %q schema = %s", r.got.System, r.got.Schema)
	}
	if f.posted != "" || out.Markdown != "ok\n" {
		t.Errorf("posted = %q markdown = %q", f.posted, out.Markdown)
	}
}

func TestSystemPromptOptions(t *testing.T) {
	s := runSettings()
	s.Concise, s.ExtraPrompt = false, "We use sqlc."
	p, structured := SystemPrompt(s)
	if !structured || !strings.Contains(p, "Be thorough") || strings.Contains(p, "Be concise") {
		t.Errorf("detailed prompt wrong: %q", p)
	}
	if !strings.Contains(p, "repository maintainers") || !strings.Contains(p, "We use sqlc.") {
		t.Error("extra prompt missing")
	}
	if i, j := strings.Index(p, "We use sqlc."), strings.Index(p, "Response JSON Schema"); i > j {
		t.Error("extra prompt should come before the schema")
	}
}

func TestReviewerInfoCost(t *testing.T) {
	s := runSettings()
	res := &llm.Result{Model: "claude-opus-4-8-20260801", InputTokens: 1_000_000, OutputTokens: 0}
	if info := reviewerInfo(s, res); info.Cost == nil || *info.Cost != 5 {
		t.Errorf("built-in price for dated snapshot: %+v", info.Cost)
	}
	s.Model, res.Model = "MiniMax-M3", "MiniMax-M3"
	if info := reviewerInfo(s, res); info.Cost != nil {
		t.Errorf("unknown model should have no cost, got %v", *info.Cost)
	}
	s.Price = &report.Price{Input: 0.3, Output: 1.2}
	res.OutputTokens = 1_000_000
	if info := reviewerInfo(s, res); info.Cost == nil || *info.Cost != 1.5 {
		t.Errorf("caller price: %+v", info.Cost)
	}
	if info := reviewerInfo(s, &llm.Result{}); info.Cost != nil {
		t.Error("no usage should mean no cost")
	}
}

func TestCommentBodyCapsLength(t *testing.T) {
	body := CommentBody(strings.Repeat("é", 70000))
	if n := len([]rune(body)); n > 65536 {
		t.Errorf("comment is %d runes", n)
	}
	if !strings.HasPrefix(body, Marker) || !strings.Contains(body, "truncated to fit") {
		t.Error("marker or truncation note missing")
	}
}

func TestExtraBodyParsed(t *testing.T) {
	vars := validEnv()
	vars["EGO_EXTRA_BODY"] = `{"reasoning_split": true}`
	cfg, _ := Load(env(vars), nil)
	s, err := cfg.Validate()
	if err != nil {
		t.Fatal(err)
	}
	if string(s.ExtraBody["reasoning_split"]) != "true" {
		t.Errorf("extra = %v", s.ExtraBody)
	}
}
