package report

import (
	"regexp"
	"strings"
)

// Price is a model's list price in USD per million tokens.
type Price struct {
	Input, Output float64
}

// knownPrices holds Anthropic first-party list prices (as of 2026-09-25).
// Only prices that could be verified are listed; other providers show token
// counts unless the caller sets input-price/output-price. Prompt caching and
// batch discounts are ignored, so costs are estimates.
var knownPrices = map[string]Price{
	"claude-fable-5-1":  {10, 50},
	"claude-mythos-5-1": {10, 50},
	"claude-fable-5":    {10, 50},
	"claude-opus-5-5":   {4, 20},
	"claude-opus-5":     {5, 25},
	"claude-opus-4-8":   {5, 25},
	"claude-opus-4-7":   {5, 25},
	"claude-opus-4-6":   {5, 25},
	"claude-sonnet-5-5": {2, 10},
	"claude-sonnet-5":   {2, 10},
	"claude-sonnet-4-6": {3, 15},
	"claude-haiku-4-5":  {1, 5},
}

var dateSuffix = regexp.MustCompile(`-\d{8}$`)

// LookupPrice returns the built-in price for model. It accepts bare IDs,
// dated snapshots (claude-haiku-4-5-20251001), Bedrock-style
// "anthropic." prefixes and Vertex-style "@version" suffixes. Matching is
// exact after that normalization, so claude-opus-5 never matches
// claude-opus-5-5.
func LookupPrice(model string) (Price, bool) {
	m := strings.ToLower(strings.TrimSpace(model))
	m = strings.TrimPrefix(m, "anthropic.")
	if i := strings.IndexByte(m, '@'); i >= 0 {
		m = m[:i]
	}
	m = dateSuffix.ReplaceAllString(m, "")
	p, ok := knownPrices[m]
	return p, ok
}

// Cost returns the cost in USD of the given token counts at p.
func (p Price) Cost(inputTokens, outputTokens int) float64 {
	return (float64(inputTokens)*p.Input + float64(outputTokens)*p.Output) / 1e6
}
