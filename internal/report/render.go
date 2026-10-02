package report

import (
	"fmt"
	"strings"
	"unicode"
)

// Info is reviewer metadata known to ego (not to the model).
type Info struct {
	Provider     string
	Model        string // as requested
	ModelVersion string // as reported by the API; may be empty
	EgoVersion   string
	InputTokens  int
	OutputTokens int
	// Cost is the estimated cost in USD, or nil when no price is known.
	Cost *float64
}

// Options controls rendering.
type Options struct {
	Concise      bool
	ReviewerInfo bool
}

var verdictTitle = map[string]string{
	"approve":       "✅ Looks good",
	"needs_changes": "⚠️ Needs changes",
	"blocking":      "🛑 Blocking",
}

var severityIcon = map[string]string{
	"critical": "🛑 **Critical**",
	"high":     "🔴 **High**",
	"medium":   "🟠 **Medium**",
	"low":      "🟡 **Low**",
}

var severityRank = map[string]int{"critical": 0, "high": 1, "medium": 2, "low": 3}

var coverageLabel = map[string]string{
	"adequate":       "✅ Adequate",
	"partial":        "⚠️ Partial",
	"missing":        "❌ Missing",
	"not_applicable": "➖ Not applicable",
}

// Render formats r as the comment body (without the sticky marker).
// Model-written text is passed through Clean.
func Render(r *Review, info Info, opts Options) string {
	var b strings.Builder

	fmt.Fprintf(&b, "## %s · Risk: **%s**\n\n", verdictTitle[r.Verdict], r.Risk)
	if s := Clean(r.VerdictReason); s != "" {
		b.WriteString(s + "\n\n")
	}

	b.WriteString("### Summary\n\n")
	b.WriteString(Clean(r.Summary) + "\n\n")

	code, security := split(r.Findings)

	b.WriteString("### Code review\n\n")
	writeFindings(&b, code, opts.Concise, "No issues found.")

	if len(r.DependencyChanges) > 0 {
		b.WriteString("### Dependency changes\n\n")
		for _, d := range r.DependencyChanges {
			fmt.Fprintf(&b, "- `%s` %s → %s", inline(d.Name), inline(d.From), inline(d.To))
			if d.Breaking {
				b.WriteString(" · ⚠️ **Breaking**")
			}
			if s := Clean(d.Notes); s != "" {
				b.WriteString(": " + s)
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("### Security review\n\n")
	if s := Clean(r.SecuritySummary); s != "" {
		b.WriteString(s + "\n\n")
		if len(security) > 0 {
			writeFindings(&b, security, opts.Concise, "")
		}
	} else {
		writeFindings(&b, security, opts.Concise, "No security issues found.")
	}

	if r.Tests.Coverage != "" || r.Tests.Notes != "" || len(r.Tests.Gaps) > 0 {
		b.WriteString("### Tests\n\n")
		if l := coverageLabel[r.Tests.Coverage]; l != "" {
			fmt.Fprintf(&b, "**Coverage:** %s", l)
			if s := Clean(r.Tests.Notes); s != "" {
				b.WriteString(" · " + s)
			}
			b.WriteString("\n\n")
		} else if s := Clean(r.Tests.Notes); s != "" {
			b.WriteString(s + "\n\n")
		}
		for _, g := range r.Tests.Gaps {
			b.WriteString("- " + Clean(g) + "\n")
		}
		if len(r.Tests.Gaps) > 0 {
			b.WriteString("\n")
		}
	}

	if len(r.Questions) > 0 {
		var q strings.Builder
		for _, s := range r.Questions {
			q.WriteString("- " + Clean(s) + "\n")
		}
		if opts.Concise {
			fmt.Fprintf(&b, "<details><summary><b>Questions for the author</b> (%d)</summary>\n\n%s\n</details>\n\n", len(r.Questions), q.String())
		} else {
			b.WriteString("### Questions for the author\n\n" + q.String() + "\n")
		}
	}

	if opts.ReviewerInfo {
		b.WriteString(ReviewerInfo(info))
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// RenderRaw formats unstructured model output: used with a custom
// review-prompt, or when structured output couldn't be parsed (notice says
// why).
func RenderRaw(text string, info Info, opts Options, notice string) string {
	var b strings.Builder
	if notice != "" {
		b.WriteString("> [!WARNING]\n> " + notice + "\n\n")
	}
	b.WriteString(Clean(strings.TrimSpace(text)) + "\n\n")
	if opts.ReviewerInfo {
		b.WriteString(ReviewerInfo(info))
	}
	return strings.TrimRight(b.String(), "\n") + "\n"
}

// ReviewerInfo renders the collapsible reviewer-info block.
func ReviewerInfo(info Info) string {
	tokens := fmt.Sprintf("%s in / %s out", count(info.InputTokens), count(info.OutputTokens))
	summary := fmt.Sprintf("Reviewed by ego %s · %s · %s", info.EgoVersion, info.Model, tokens)
	cost := "unknown (set `input-price` / `output-price`)"
	if info.Cost != nil {
		cost = "≈ " + dollars(*info.Cost) + " (estimate from list prices)"
		summary += " · ≈ " + dollars(*info.Cost)
	}
	version := info.ModelVersion
	if version == "" {
		version = "not reported"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "---\n<details><summary><sub>%s</sub></summary>\n\n", summary)
	b.WriteString("| | |\n|---|---|\n")
	fmt.Fprintf(&b, "| Provider | `%s` |\n", inline(info.Provider))
	fmt.Fprintf(&b, "| Model | `%s` |\n", inline(info.Model))
	fmt.Fprintf(&b, "| Model version | `%s` |\n", inline(version))
	fmt.Fprintf(&b, "| ego version | `%s` |\n", inline(info.EgoVersion))
	fmt.Fprintf(&b, "| Tokens | %s |\n", tokens)
	fmt.Fprintf(&b, "| Cost | %s |\n", cost)
	b.WriteString("\n</details>\n")
	return b.String()
}

func split(findings []Finding) (code, security []Finding) {
	for _, f := range findings {
		if f.Category == "security" {
			security = append(security, f)
		} else {
			code = append(code, f)
		}
	}
	sortBySeverity(code)
	sortBySeverity(security)
	return code, security
}

func sortBySeverity(fs []Finding) {
	// Stable insertion sort keeps the model's order within a severity.
	for i := 1; i < len(fs); i++ {
		for j := i; j > 0 && severityRank[fs[j].Severity] < severityRank[fs[j-1].Severity]; j-- {
			fs[j], fs[j-1] = fs[j-1], fs[j]
		}
	}
}

// writeFindings writes findings, folding medium/low ones into a collapsed
// block in concise mode.
func writeFindings(b *strings.Builder, fs []Finding, concise bool, none string) {
	if len(fs) == 0 {
		b.WriteString(none + "\n\n")
		return
	}
	var shown, folded []Finding
	for _, f := range fs {
		if concise && severityRank[f.Severity] >= severityRank["medium"] {
			folded = append(folded, f)
		} else {
			shown = append(shown, f)
		}
	}
	for _, f := range shown {
		writeFinding(b, f)
	}
	if len(shown) > 0 {
		b.WriteString("\n")
	}
	if len(folded) > 0 {
		fmt.Fprintf(b, "<details><summary>%d medium/low finding%s</summary>\n\n", len(folded), plural(len(folded)))
		for _, f := range folded {
			writeFinding(b, f)
		}
		b.WriteString("\n</details>\n\n")
	}
}

func writeFinding(b *strings.Builder, f Finding) {
	fmt.Fprintf(b, "- %s · %s", severityIcon[f.Severity], Clean(f.Title))
	if loc := location(f); loc != "" {
		fmt.Fprintf(b, " · `%s`", loc)
	}
	if f.Uncertain {
		b.WriteString(" · _uncertain_")
	}
	b.WriteString("\n")
	if s := Clean(f.Detail); s != "" {
		b.WriteString(indent(s) + "\n")
	}
	if s := Clean(f.Suggestion); s != "" {
		b.WriteString(indent("**Fix:** "+s) + "\n")
	}
}

func location(f Finding) string {
	file := inline(f.File)
	if file == "" {
		return ""
	}
	if f.Line > 0 {
		return fmt.Sprintf("%s:%d", file, f.Line)
	}
	return file
}

// indent nests text under a list item, keeping paragraphs and code blocks
// inside it.
func indent(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = "  " + l
		}
	}
	return "\n" + strings.Join(lines, "\n")
}

// inline makes s safe inside a `code span` on one line.
func inline(s string) string {
	s = strings.NewReplacer("`", "'", "\n", " ", "\r", " ").Replace(s)
	return strings.TrimSpace(s)
}

// Clean neutralizes @mentions outside code so model output (which a PR can
// steer through prompt injection) can't ping users or teams. A zero-width
// space after the @ breaks the mention without changing how it looks.
func Clean(s string) string {
	var b strings.Builder
	inCode := false
	runes := []rune(strings.TrimSpace(s))
	for i, r := range runes {
		if r == '`' {
			inCode = !inCode
		}
		b.WriteRune(r)
		if r == '@' && !inCode && i+1 < len(runes) && isHandleRune(runes[i+1]) &&
			(i == 0 || !isHandleRune(runes[i-1])) {
			b.WriteRune('​')
		}
	}
	return b.String()
}

func isHandleRune(r rune) bool {
	return r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func count(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}

func dollars(v float64) string {
	if v < 0.1 {
		return fmt.Sprintf("$%.4f", v)
	}
	return fmt.Sprintf("$%.2f", v)
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
