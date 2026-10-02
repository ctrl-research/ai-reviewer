// Package review runs the review pipeline: fetch the PR, build the prompts,
// ask the LLM, and publish the result.
package review

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/ctrl-research/ego/internal/forge"
	"github.com/ctrl-research/ego/internal/gha"
	"github.com/ctrl-research/ego/internal/llm"
	"github.com/ctrl-research/ego/internal/report"
)

// Marker identifies the sticky review comment. It is shared with the earlier
// bash implementation so existing comments keep being updated in place; do
// not change it.
const Marker = "<!-- pr-review-action -->"

// Forge is the subset of forge.Client used by Run.
type Forge interface {
	PullRequest(ctx context.Context, number int) (*forge.PullRequest, error)
	Diff(ctx context.Context, number int, maxBytes int64) ([]byte, bool, error)
	UpsertComment(ctx context.Context, number int, marker, body string) (bool, int64, error)
}

// Reviewer is the subset of llm.Client used by Run.
type Reviewer interface {
	Review(ctx context.Context, req llm.Request) (*llm.Result, error)
}

// Output is the result of a review.
type Output struct {
	Markdown string // the rendered review, without the sticky marker
	JSON     string // the structured review; empty if unavailable
	Verdict  string // approve | needs_changes | blocking; empty if unavailable
}

// Run executes one review.
func Run(ctx context.Context, s *Settings, f Forge, r Reviewer, log *gha.Logger) (*Output, error) {
	pr, err := f.PullRequest(ctx, s.PRNumber)
	if err != nil {
		return nil, err
	}
	diff, truncated, err := f.Diff(ctx, s.PRNumber, s.MaxDiffBytes)
	if err != nil {
		return nil, err
	}
	if truncated {
		log.Warningf("PR diff is larger than %d bytes; truncating.", s.MaxDiffBytes)
	}

	system, structured := SystemPrompt(s)
	req := llm.Request{
		Model:     s.Model,
		System:    system,
		Prompt:    UserPrompt(pr, diff, truncated, s.MaxDiffBytes),
		MaxTokens: s.MaxTokens,
		Extra:     s.ExtraBody,
	}
	if structured {
		req.Schema = report.Schema
	}
	res, err := r.Review(ctx, req)
	if err != nil {
		return nil, err
	}
	if res.Truncated {
		log.Warningf("Review stopped at max-tokens (%d) and may be incomplete.", s.MaxTokens)
	}

	info := reviewerInfo(s, res)
	opts := report.Options{Concise: s.Concise, ReviewerInfo: s.ReviewerInfo}
	out := &Output{}
	if structured {
		if rev, err := report.Parse(res.Text); err == nil {
			out.Markdown = report.Render(rev, info, opts)
			raw, _ := json.Marshal(rev)
			out.JSON, out.Verdict = string(raw), rev.Verdict
		} else {
			log.Warningf("Couldn't parse a structured review from the model's response; posting its raw output.")
			out.Markdown = report.RenderRaw(res.Text, info, opts,
				"ego couldn't parse a structured review from the model's response, so this is its raw output.")
		}
	} else {
		out.Markdown = report.RenderRaw(res.Text, info, opts, "")
	}

	if s.PostComment {
		created, id, err := f.UpsertComment(ctx, s.PRNumber, Marker, CommentBody(out.Markdown))
		if err != nil {
			return nil, err
		}
		if created {
			log.Infof("Created new review comment")
		} else {
			log.Infof("Updated existing review comment %d", id)
		}
	}
	return out, nil
}

// reviewerInfo collects what ego knows about the run. Cost uses the
// caller's prices if set, else the built-in table for the served model,
// then for the requested one.
func reviewerInfo(s *Settings, res *llm.Result) report.Info {
	info := report.Info{
		Provider:     s.Provider,
		Model:        s.Model,
		ModelVersion: res.Model,
		EgoVersion:   s.EgoVersion,
		InputTokens:  res.InputTokens,
		OutputTokens: res.OutputTokens,
	}
	if res.InputTokens == 0 && res.OutputTokens == 0 {
		return info
	}
	price, ok := report.Price{}, false
	if s.Price != nil {
		price, ok = *s.Price, true
	} else if price, ok = report.LookupPrice(res.Model); !ok {
		price, ok = report.LookupPrice(s.Model)
	}
	if ok {
		cost := price.Cost(res.InputTokens, res.OutputTokens)
		info.Cost = &cost
	}
	return info
}

// UserPrompt renders the PR description and diff as the user message.
func UserPrompt(pr *forge.PullRequest, diff []byte, truncated bool, maxBytes int64) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n%s\n", pr.Title, pr.Body)
	b.WriteString("\n\n## Diff\n\n```diff\n")
	if truncated {
		diff = trimPartialRune(diff)
	}
	b.Write(diff)
	if truncated {
		fmt.Fprintf(&b, "\n\n[diff truncated at %d bytes]\n", maxBytes)
	}
	b.WriteString("\n```\n")
	return b.String()
}

// maxCommentRunes keeps the comment under GitHub's 65,536-character limit,
// leaving room for the marker and truncation note.
const maxCommentRunes = 65000

// CommentBody prefixes the sticky marker and caps the length.
func CommentBody(markdown string) string {
	body := Marker + "\n\n" + markdown
	if r := []rune(body); len(r) > maxCommentRunes {
		body = string(r[:maxCommentRunes]) + "\n\n_…review truncated to fit GitHub's comment size limit._\n"
	}
	return body
}

// trimPartialRune drops an incomplete UTF-8 sequence left at the end of b by
// byte-level truncation.
func trimPartialRune(b []byte) []byte {
	for i := 1; i < utf8.UTFMax && i <= len(b); i++ {
		c := b[len(b)-i]
		if c < utf8.RuneSelf {
			return b
		}
		if utf8.RuneStart(c) {
			if !utf8.FullRune(b[len(b)-i:]) {
				return b[:len(b)-i]
			}
			return b
		}
	}
	return b
}
