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
}

// Result is the generated review text.
type Result struct {
	Text string
	// Truncated reports that generation stopped at MaxTokens.
	Truncated bool
}

// ErrEmpty is returned when the provider responds without any review text.
var ErrEmpty = errors.New("LLM response contained no review text")

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
		payload = anthropicRequest{
			Model:     req.Model,
			MaxTokens: req.MaxTokens,
			System:    req.System,
			Messages:  []message{{Role: "user", Content: req.Prompt}},
		}
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
	body, err := json.Marshal(payload)
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

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	System    string    `json:"system"`
	Messages  []message `json:"messages"`
}

type anthropicResponse struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	StopReason  string `json:"stop_reason"`
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
	if strings.TrimSpace(text) == "" {
		return nil, ErrEmpty
	}
	return &Result{Text: text, Truncated: r.StopReason == "max_tokens"}, nil
}

type openAIRequest struct {
	Model     string    `json:"model"`
	MaxTokens int       `json:"max_tokens"`
	Messages  []message `json:"messages"`
}

type openAIResponse struct {
	Choices []struct {
		Message struct {
			Content *string `json:"content"`
			Refusal *string `json:"refusal"`
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
	if choice.Message.Content == nil || strings.TrimSpace(*choice.Message.Content) == "" {
		return nil, ErrEmpty
	}
	return &Result{Text: *choice.Message.Content, Truncated: choice.FinishReason == "length"}, nil
}

func or(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}
