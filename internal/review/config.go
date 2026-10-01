package review

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/ctrl-research/ego/internal/forge"
	"github.com/ctrl-research/ego/internal/llm"
)

// Config is the full set of review settings. In the action every field comes
// from an EGO_* environment variable set by action.yaml; locally the
// non-secret fields can also be given as flags.
type Config struct {
	Platform     string
	APIURL       string
	Repo         string
	Token        string
	Provider     string
	BaseURL      string
	Model        string
	APIKey       string
	PRNumber     string
	MaxTokens    string
	MaxDiffBytes string
	ReviewPrompt string
	ExtraBody    string
	PostComment  string
}

// Load builds a Config from the environment (via getenv) and command-line
// flags. Flags override environment variables. Secrets (the forge token and
// LLM API key) are environment-only so they never appear in the process list.
func Load(getenv func(string) string, args []string) (*Config, error) {
	env := func(name, fallback string) string {
		if v := getenv("EGO_" + name); v != "" {
			return v
		}
		return fallback
	}
	c := &Config{
		Token:  env("TOKEN", getenv("GITHUB_TOKEN")),
		APIKey: env("API_KEY", ""),
		// Free text: an empty value means "use the built-in prompt", so it is
		// read verbatim rather than through the fallback helper.
		ReviewPrompt: getenv("EGO_REVIEW_PROMPT"),
	}

	fs := flag.NewFlagSet("ego", flag.ContinueOnError)
	fs.StringVar(&c.Platform, "platform", env("PLATFORM", "github"), "forge hosting the PR: github or forgejo (also covers gitea)")
	fs.StringVar(&c.APIURL, "api-url", env("API_URL", or(getenv("GITHUB_API_URL"), "https://api.github.com")), "forge REST API base URL")
	fs.StringVar(&c.Repo, "repo", env("REPO", getenv("GITHUB_REPOSITORY")), "repository as owner/name")
	fs.StringVar(&c.PRNumber, "pr", env("PR_NUMBER", ""), "pull request number")
	fs.StringVar(&c.Provider, "provider", env("PROVIDER", llm.Anthropic), "LLM provider: anthropic, openai or openai-compatible")
	fs.StringVar(&c.BaseURL, "base-url", env("BASE_URL", ""), "LLM API base URL override")
	fs.StringVar(&c.Model, "model", env("MODEL", "claude-opus-4-8"), "model ID")
	fs.StringVar(&c.MaxTokens, "max-tokens", env("MAX_TOKENS", "16000"), "maximum output tokens")
	fs.StringVar(&c.MaxDiffBytes, "max-diff-bytes", env("MAX_DIFF_BYTES", "300000"), "truncate the diff beyond this many bytes")
	fs.StringVar(&c.ExtraBody, "extra-body", env("EXTRA_BODY", ""), "JSON object merged into the LLM request body (null removes a key)")
	fs.StringVar(&c.PostComment, "post-comment", env("POST_COMMENT", "true"), "post the review as a sticky PR comment")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "Usage: ego [flags]\n\nSecrets are read from the environment only:\n"+
			"  EGO_TOKEN    forge token (falls back to GITHUB_TOKEN)\n"+
			"  EGO_API_KEY  LLM provider API key\n\nFlags:\n")
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if fs.NArg() > 0 {
		return nil, fmt.Errorf("unexpected arguments: %s", strings.Join(fs.Args(), " "))
	}
	return c, nil
}

// Settings is a validated Config.
type Settings struct {
	Platform     forge.Platform
	APIURL       string
	Repo         string
	Token        string
	Provider     string
	BaseURL      string
	Model        string
	APIKey       string
	PRNumber     int
	MaxTokens    int
	MaxDiffBytes int64
	ReviewPrompt string
	ExtraBody    map[string]json.RawMessage
	PostComment  bool
}

var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// Validate checks c and converts it to Settings. Everything that ends up in
// an API path or a number is validated strictly so that untrusted or
// misconfigured values cannot reach those contexts.
func (c *Config) Validate() (*Settings, error) {
	s := &Settings{
		APIURL:       c.APIURL,
		Repo:         c.Repo,
		Token:        c.Token,
		Provider:     c.Provider,
		BaseURL:      c.BaseURL,
		Model:        c.Model,
		APIKey:       c.APIKey,
		ReviewPrompt: c.ReviewPrompt,
	}

	switch p := forge.Platform(c.Platform); p {
	case forge.GitHub, forge.Forgejo, forge.Gitea:
		s.Platform = p
	default:
		return nil, fmt.Errorf("unknown platform '%s'. Use 'github' or 'forgejo'", c.Platform)
	}

	if c.PRNumber == "" {
		return nil, errors.New("no pull request number found. Run on a pull_request event or set the 'pr-number' input")
	}
	var err error
	if s.PRNumber, err = positiveInt("pr-number", c.PRNumber); err != nil {
		return nil, err
	}
	if s.MaxTokens, err = nonNegativeInt("max-tokens", c.MaxTokens); err != nil {
		return nil, err
	}
	maxDiff, err := nonNegativeInt("max-diff-bytes", c.MaxDiffBytes)
	if err != nil {
		return nil, err
	}
	s.MaxDiffBytes = int64(maxDiff)

	if c.APIURL == "" {
		return nil, errors.New("'api-url' could not be determined; set the 'api-url' input")
	}
	if !repoPattern.MatchString(c.Repo) {
		return nil, fmt.Errorf("repository must be 'owner/name' (got '%s')", c.Repo)
	}
	if c.Token == "" {
		return nil, errors.New("'github-token' is required to read the PR and post the comment")
	}

	switch c.Provider {
	case llm.Anthropic, llm.OpenAI:
		if c.APIKey == "" {
			return nil, fmt.Errorf("'api-key' is required for provider '%s'", c.Provider)
		}
	case llm.OpenAICompatible:
		if c.BaseURL == "" {
			return nil, fmt.Errorf("'base-url' is required for provider '%s' (e.g. http://localhost:11434/v1)", c.Provider)
		}
	default:
		return nil, fmt.Errorf("unknown provider '%s'. Use 'anthropic', 'openai', or 'openai-compatible'", c.Provider)
	}

	if s.ExtraBody, err = parseExtraBody(c.ExtraBody); err != nil {
		return nil, err
	}

	if s.PostComment, err = strconv.ParseBool(c.PostComment); err != nil {
		return nil, fmt.Errorf("'post-comment' must be 'true' or 'false' (got '%s')", c.PostComment)
	}
	return s, nil
}

// reservedBodyKeys carry the prompt and model; extra-body may not replace them.
var reservedBodyKeys = []string{"model", "messages", "system"}

func parseExtraBody(v string) (map[string]json.RawMessage, error) {
	if strings.TrimSpace(v) == "" {
		return nil, nil
	}
	var extra map[string]json.RawMessage
	if err := json.Unmarshal([]byte(v), &extra); err != nil || extra == nil {
		return nil, errors.New("'extra-body' must be a JSON object, e.g. '{\"reasoning_split\": true}'")
	}
	for _, k := range reservedBodyKeys {
		if _, ok := extra[k]; ok {
			return nil, fmt.Errorf("'extra-body' may not set '%s'; use the matching input instead", k)
		}
	}
	return extra, nil
}

func positiveInt(name, v string) (int, error) {
	n, err := nonNegativeInt(name, v)
	if err != nil || n == 0 {
		return 0, fmt.Errorf("'%s' must be a positive integer (got '%s')", name, v)
	}
	return n, nil
}

func nonNegativeInt(name, v string) (int, error) {
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 || strings.ContainsAny(v, "+-") {
		return 0, fmt.Errorf("'%s' must be a non-negative integer (got '%s')", name, v)
	}
	return n, nil
}

func or(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}
