// Package llm sends a single review request to an LLM provider: the Anthropic
// Messages API, or the OpenAI Chat Completions API (also used for any
// OpenAI-compatible server such as Ollama, vLLM or OpenRouter).
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/ctrl-research/ego/internal/httpx"
)

// Provider names accepted by New.
const (
	Anthropic        = "anthropic"
	OpenAI           = "openai"
	OpenAICompatible = "openai-compatible"
)

// Request is one review: a system prompt and a single user message.
type Request struct {
	Model     string
	System    string
	Prompt    string
	MaxTokens int
	// Extra is merged into the JSON request body for provider-specific
	// parameters. Objects merge key by key; a JSON null value removes a key.
	Extra map[string]json.RawMessage
	// Schema, when set, is a JSON Schema the response should follow. It is
	// enforced natively on Anthropic models that support structured outputs
	// (see StructuredOutputSupported); elsewhere the prompt carries it.
	Schema json.RawMessage
}

// Result is the generated review text.
type Result struct {
	Text string
	// Truncated reports that generation stopped at MaxTokens.
	Truncated bool
	// Model is the model ID the API reports serving (e.g. a dated
	// snapshot); empty if the response doesn't say.
	Model string
	// InputTokens and OutputTokens are the billed token counts, when the
	// response reports usage.
	InputTokens  int
	OutputTokens int
}

// ErrEmpty is returned when the provider responds without any review text.
var ErrEmpty = errors.New("LLM response contained no review text")

// outOfTokens explains an empty response that stopped at the token limit:
// with reasoning models, the reasoning usually used up the whole budget.
func outOfTokens(outputTokens int) error {
	used := ""
	if outputTokens > 0 {
		used = fmt.Sprintf(" (%d output tokens)", outputTokens)
	}
	return fmt.Errorf("%w: the model reached max-tokens%s before writing the review, most likely spending them on reasoning; raise max-tokens", ErrEmpty, used)
}

// Client calls one provider.
type Client struct {
	Provider string
	URL      string // full endpoint URL
	APIKey   string
	HTTP     *http.Client
	Retry    httpx.Retry
}

// New returns a Client for provider. baseURL overrides the provider's default
// base URL and is required for openai-compatible.
func New(provider, baseURL, apiKey string) (*Client, error) {
	c := &Client{
		Provider: provider,
		APIKey:   apiKey,
		// Non-streaming generations of up to ~16k tokens can take minutes.
		HTTP:  httpx.NewClient(10 * time.Minute),
		Retry: httpx.Retry{Attempts: 3, Delay: 5 * time.Second},
	}
	switch provider {
	case Anthropic:
		c.URL = strings.TrimRight(or(baseURL, "https://api.anthropic.com"), "/") + "/v1/messages"
	case OpenAI:
		c.URL = strings.TrimRight(or(baseURL, "https://api.openai.com/v1"), "/") + "/chat/completions"
	case OpenAICompatible:
		if baseURL == "" {
			return nil, fmt.Errorf("base URL is required for provider %q", provider)
		}
		c.URL = strings.TrimRight(baseURL, "/") + "/chat/completions"
	default:
		return nil, fmt.Errorf("unknown provider %q", provider)
	}
	return c, nil
}

// maxResponseBody bounds the LLM response read into memory.
const maxResponseBody = 32 << 20

// Review sends req and returns the generated text.
func (c *Client) Review(ctx context.Context, req Request) (*Result, error) {
	var payload any
	headers := map[string]string{"Content-Type": "application/json"}
	if c.Provider == Anthropic {
		ar := anthropicRequest{
			Model:     req.Model,
			MaxTokens: req.MaxTokens,
			System:    req.System,
			Messages:  []message{{Role: "user", Content: req.Prompt}},
		}
		if req.Schema != nil && StructuredOutputSupported(req.Model) {
			ar.OutputConfig = &outputConfig{Format: &outputFormat{Type: "json_schema", Schema: req.Schema}}
		}
		payload = ar
		headers["x-api-key"] = c.APIKey
		headers["anthropic-version"] = "2023-06-01"
	} else {
		payload = openAIRequest{
			Model:     req.Model,
			MaxTokens: req.MaxTokens,
			Messages: []message{
				{Role: "system", Content: req.System},
				{Role: "user", Content: req.Prompt},
			},
		}
		if c.APIKey != "" {
			headers["Authorization"] = "Bearer " + c.APIKey
		}
	}
	body, err := withExtra(payload, req.Extra)
	if err != nil {
		return nil, err
	}

	resp, _, err := httpx.Do(ctx, c.HTTP, c.Retry, func(ctx context.Context) (*http.Request, error) {
		r, err := http.NewRequestWithContext(ctx, http.MethodPost, c.URL, bytes.NewReader(body))
		if err != nil {
			return nil, err
		}
		for k, v := range headers {
			r.Header.Set(k, v)
		}
		return r, nil
	}, maxResponseBody, http.StatusOK)
	if err != nil {
		return nil, fmt.Errorf("LLM request failed: %w", err)
	}

	if c.Provider == Anthropic {
		return parseAnthropic(resp)
	}
	return parseOpenAI(resp)
}

// withExtra marshals payload and merges extra into it.
func withExtra(payload any, extra map[string]json.RawMessage) ([]byte, error) {
	body, err := json.Marshal(payload)
	if err != nil || len(extra) == 0 {
		return body, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return nil, err
	}
	if err := merge(fields, extra); err != nil {
		return nil, err
	}
	return json.Marshal(fields)
}

// merge applies extra onto fields: null deletes a key, two objects merge
// recursively, and anything else replaces the existing value. Merging
// objects lets e.g. extra-body set output_config.effort without dropping
// the output_config.format ego sets.
func merge(fields, extra map[string]json.RawMessage) error {
	for k, v := range extra {
		if string(v) == "null" {
			delete(fields, k)
			continue
		}
		var dst, src map[string]json.RawMessage
		if json.Unmarshal(fields[k], &dst) == nil && dst != nil && json.Unmarshal(v, &src) == nil && src != nil {
			if err := merge(dst, src); err != nil {
				return err
			}
			merged, err := json.Marshal(dst)
			if err != nil {
				return err
			}
			fields[k] = merged
			continue
		}
		fields[k] = v
	}
	return nil
}

// structuredOutputModels lists Anthropic models that accept
// output_config.format (structured outputs). Opus 4.7/4.6 and Sonnet 4.6 do
// not, and reject the parameter.
var structuredOutputModels = map[string]bool{
	"claude-fable-5-1": true, "claude-mythos-5-1": true,
	"claude-fable-5": true, "claude-mythos-5": true,
	"claude-opus-5-5": true, "claude-opus-5": true, "claude-opus-4-8": true,
	"claude-sonnet-5-5": true, "claude-sonnet-5": true,
	"claude-haiku-4-5": true,
	"claude-opus-4-5":  true, "claude-opus-4-1": true,
}

var dateSuffix = regexp.MustCompile(`-\d{8}$`)

// StructuredOutputSupported reports whether model accepts Anthropic
// structured outputs. Dated snapshots and Bedrock/Vertex spellings match
// their base model.
func StructuredOutputSupported(model string) bool {
	m := strings.TrimPrefix(strings.ToLower(strings.TrimSpace(model)), "anthropic.")
	if i := strings.IndexByte(m, '@'); i >= 0 {
		m = m[:i]
	}
	return structuredOutputModels[dateSuffix.ReplaceAllString(m, "")]
}

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model        string        `json:"model"`
	MaxTokens    int           `json:"max_tokens"`
	System       string        `json:"system"`
	Messages     []message     `json:"messages"`
	OutputConfig *outputConfig `json:"output_config,omitempty"`
}

type outputConfig struct {
	Format *outputFormat `json:"format,omitempty"`
}

type outputFormat struct {
	Type   string          `json:"type"`
	Schema json.RawMessage `json:"schema"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Model      string `json:"model"`
	StopReason string `json:"stop_reason"`
	Usage      struct {
		InputTokens              int `json:"input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		OutputTokens             int `json:"output_tokens"`
	} `json:"usage"`
	StopDetails *struct {
		Category    *string `json:"category"`
		Explanation string  `json:"explanation"`
	} `json:"stop_details"`
}

func parseAnthropic(body []byte) (*Result, error) {
	var r anthropicResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("decode Anthropic response: %w", err)
	}
	if r.StopReason == "refusal" {
		detail := ""
		if d := r.StopDetails; d != nil {
			if d.Category != nil {
				detail = " (" + *d.Category + ")"
			}
			if d.Explanation != "" {
				detail += ": " + d.Explanation
			}
		}
		return nil, fmt.Errorf("model declined to review%s", detail)
	}
	var parts []string
	for _, block := range r.Content {
		if block.Type == "text" {
			parts = append(parts, block.Text)
		}
	}
	text := strings.Join(parts, "\n")
	u := r.Usage
	if strings.TrimSpace(text) == "" {
		if r.StopReason == "max_tokens" {
			return nil, outOfTokens(u.OutputTokens)
		}
		return nil, ErrEmpty
	}
	return &Result{
		Text:         text,
		Truncated:    r.StopReason == "max_tokens",
		Model:        r.Model,
		InputTokens:  u.InputTokens + u.CacheCreationInputTokens + u.CacheReadInputTokens,
		OutputTokens: u.OutputTokens,
	}, nil
}

type openAIRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	Messages  []message `json:"messages"`
}

type openAIResponse struct {
	Model string `json:"model"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
	Choices []struct {
		Message struct {
			Content *string `json:"content"`
			Refusal *string `json:"refusal"`
			// Reasoning returned separately (e.g. MiniMax with reasoning_split).
			ReasoningContent *string `json:"reasoning_content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
}

func parseOpenAI(body []byte) (*Result, error) {
	var r openAIResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return nil, fmt.Errorf("decode chat completions response: %w", err)
	}
	if len(r.Choices) == 0 {
		return nil, ErrEmpty
	}
	choice := r.Choices[0]
	if ref := choice.Message.Refusal; ref != nil && *ref != "" {
		return nil, fmt.Errorf("model declined to review: %s", *ref)
	}
	truncated := choice.FinishReason == "length"
	content := ""
	if choice.Message.Content != nil {
		content = *choice.Message.Content
	}
	text, finished := stripThinking(content)
	if !finished || strings.TrimSpace(text) == "" {
		if truncated {
			return nil, outOfTokens(r.Usage.CompletionTokens)
		}
		if rc := choice.Message.ReasoningContent; rc != nil && strings.TrimSpace(*rc) != "" {
			return nil, fmt.Errorf("%w: the model returned reasoning but no answer", ErrEmpty)
		}
		return nil, ErrEmpty
	}
	return &Result{
		Text:         text,
		Truncated:    truncated,
		Model:        r.Model,
		InputTokens:  r.Usage.PromptTokens,
		OutputTokens: r.Usage.CompletionTokens,
	}, nil
}

// stripThinking removes a leading <think>…</think> block from s. Some
// OpenAI-compatible servers (MiniMax M2, DeepSeek-R1 and Qwen via Ollama)
// return the model's reasoning inline in content this way. finished is false
// when the block was opened but never closed.
func stripThinking(s string) (text string, finished bool) {
	t := strings.TrimLeft(s, " \t\r\n")
	if !strings.HasPrefix(t, "<think>") {
		return s, true
	}
	end := strings.Index(t, "</think>")
	if end < 0 {
		return "", false
	}
	return strings.TrimLeft(t[end+len("</think>"):], " \t\r\n"), true
}

func or(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}
