// Package report defines the structured review the model returns, parses it
// leniently from model output, and renders it as the PR comment.
package report

import (
	"encoding/json"
	"errors"
	"strings"
)

// Review is the structured review requested from the model. Every field is
// filled in by the model; see Schema for the wire format.
type Review struct {
	Verdict           string             `json:"verdict"` // approve | needs_changes | blocking
	Risk              string             `json:"risk"`    // low | medium | high
	VerdictReason     string             `json:"verdict_reason"`
	Summary           string             `json:"summary"`
	Findings          []Finding          `json:"findings"`
	DependencyChanges []DependencyChange `json:"dependency_changes"`
	SecuritySummary   string             `json:"security_summary"`
	Tests             Tests              `json:"tests"`
	Questions         []string           `json:"questions"`
}

// Finding is one code or security issue.
type Finding struct {
	Category   string `json:"category"` // code | security
	Severity   string `json:"severity"` // critical | high | medium | low
	File       string `json:"file"`
	Line       int    `json:"line"` // 0 when not tied to a line
	Title      string `json:"title"`
	Detail     string `json:"detail"`
	Suggestion string `json:"suggestion"`
	Uncertain  bool   `json:"uncertain"`
}

// DependencyChange is a version bump found in the diff.
type DependencyChange struct {
	Name     string `json:"name"`
	From     string `json:"from"`
	To       string `json:"to"`
	Breaking bool   `json:"breaking"`
	Notes    string `json:"notes"`
}

// Tests is the model's assessment of test coverage for the change.
type Tests struct {
	Coverage string   `json:"coverage"` // adequate | partial | missing | not_applicable
	Notes    string   `json:"notes"`
	Gaps     []string `json:"gaps"`
}

// Schema is the JSON Schema for Review. It stays within the subset accepted
// by Anthropic structured outputs: every object sets additionalProperties
// false and lists all properties as required, with no numeric or length
// constraints.
var Schema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["verdict", "risk", "verdict_reason", "summary", "findings", "dependency_changes", "security_summary", "tests", "questions"],
  "properties": {
    "verdict": {"type": "string", "enum": ["approve", "needs_changes", "blocking"]},
    "risk": {"type": "string", "enum": ["low", "medium", "high"]},
    "verdict_reason": {"type": "string"},
    "summary": {"type": "string"},
    "findings": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["category", "severity", "file", "line", "title", "detail", "suggestion", "uncertain"],
        "properties": {
          "category": {"type": "string", "enum": ["code", "security"]},
          "severity": {"type": "string", "enum": ["critical", "high", "medium", "low"]},
          "file": {"type": "string"},
          "line": {"type": "integer"},
          "title": {"type": "string"},
          "detail": {"type": "string"},
          "suggestion": {"type": "string"},
          "uncertain": {"type": "boolean"}
        }
      }
    },
    "dependency_changes": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["name", "from", "to", "breaking", "notes"],
        "properties": {
          "name": {"type": "string"},
          "from": {"type": "string"},
          "to": {"type": "string"},
          "breaking": {"type": "boolean"},
          "notes": {"type": "string"}
        }
      }
    },
    "security_summary": {"type": "string"},
    "tests": {
      "type": "object",
      "additionalProperties": false,
      "required": ["coverage", "notes", "gaps"],
      "properties": {
        "coverage": {"type": "string", "enum": ["adequate", "partial", "missing", "not_applicable"]},
        "notes": {"type": "string"},
        "gaps": {"type": "array", "items": {"type": "string"}}
      }
    },
    "questions": {"type": "array", "items": {"type": "string"}}
  }
}`)

// ErrNoReview is returned when no usable structured review is found.
var ErrNoReview = errors.New("model output did not contain a valid structured review")

// Parse extracts a Review from model output. Models without native
// structured output may wrap the JSON in prose or a ```json fence, so Parse
// tries the whole text, then the last fenced block, then the outermost
// braces. Enum values are normalized; a review needs at least a recognized
// verdict and a summary.
func Parse(text string) (*Review, error) {
	for _, candidate := range candidates(text) {
		var r Review
		if err := json.Unmarshal([]byte(candidate), &r); err != nil {
			continue
		}
		if r.normalize() {
			return &r, nil
		}
	}
	return nil, ErrNoReview
}

func candidates(text string) []string {
	text = strings.TrimSpace(text)
	out := []string{text}
	if i := strings.LastIndex(text, "```json"); i >= 0 {
		rest := text[i+len("```json"):]
		if j := strings.Index(rest, "```"); j >= 0 {
			out = append(out, strings.TrimSpace(rest[:j]))
		}
	}
	if i, j := strings.Index(text, "{"), strings.LastIndex(text, "}"); i >= 0 && j > i {
		out = append(out, text[i:j+1])
	}
	return out
}

// normalize lowercases enums, maps unknown values to safe defaults, and
// reports whether the review is usable.
func (r *Review) normalize() bool {
	r.Verdict = norm(r.Verdict)
	switch r.Verdict {
	case "approve", "needs_changes", "blocking":
	default:
		return false
	}
	if strings.TrimSpace(r.Summary) == "" {
		return false
	}
	r.Risk = oneOf(norm(r.Risk), "medium", "low", "medium", "high")
	for i := range r.Findings {
		f := &r.Findings[i]
		f.Category = oneOf(norm(f.Category), "code", "code", "security")
		f.Severity = oneOf(norm(f.Severity), "medium", "critical", "high", "medium", "low")
	}
	r.Tests.Coverage = oneOf(norm(r.Tests.Coverage), "", "adequate", "partial", "missing", "not_applicable")
	return true
}

func norm(s string) string {
	return strings.ReplaceAll(strings.ToLower(strings.TrimSpace(s)), " ", "_")
}

func oneOf(v, fallback string, allowed ...string) string {
	for _, a := range allowed {
		if v == a {
			return v
		}
	}
	return fallback
}
