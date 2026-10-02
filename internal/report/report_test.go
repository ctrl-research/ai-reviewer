package report

import (
	"encoding/json"
	"strings"
	"testing"
)

const sample = `{"verdict":"Needs Changes","risk":"HIGH","verdict_reason":"A bug.","summary":"Adds a cache.",
"findings":[
 {"category":"code","severity":"low","file":"a.go","line":0,"title":"Naming","detail":"d1","suggestion":"","uncertain":false},
 {"category":"security","severity":"critical","file":"auth.go","line":12,"title":"Token logged","detail":"d2","suggestion":"Mask it.","uncertain":true},
 {"category":"code","severity":"high","file":"cache.go","line":40,"title":"Race","detail":"d3","suggestion":"Lock.","uncertain":false},
 {"category":"weird","severity":"bogus","file":"","line":0,"title":"Odd","detail":"","suggestion":"","uncertain":false}],
"dependency_changes":[{"name":"actions/checkout","from":"v4","to":"v7","breaking":true,"notes":"Node 24."}],
"security_summary":"Logs a token.",
"tests":{"coverage":"partial","notes":"Some.","gaps":["race test"]},
"questions":["Why 5s TTL?"]}`

func TestParseVariants(t *testing.T) {
	for name, text := range map[string]string{
		"bare":   sample,
		"fenced": "Here you go:\n```json\n" + sample + "\n```\nThanks!",
		"prose":  "Sure. " + sample + " Done.",
	} {
		t.Run(name, func(t *testing.T) {
			r, err := Parse(text)
			if err != nil {
				t.Fatal(err)
			}
			if r.Verdict != "needs_changes" || r.Risk != "high" {
				t.Errorf("enums not normalized: %q %q", r.Verdict, r.Risk)
			}
			odd := r.Findings[3]
			if odd.Category != "code" || odd.Severity != "medium" {
				t.Errorf("unknown enums not defaulted: %+v", odd)
			}
		})
	}
}

func TestParseRejects(t *testing.T) {
	for name, text := range map[string]string{
		"not json":     "LGTM",
		"bad verdict":  `{"verdict":"maybe","summary":"x"}`,
		"no summary":   `{"verdict":"approve","summary":"  "}`,
		"truncated":    sample[:200],
		"wrong shape":  `["approve"]`,
		"empty object": `{}`,
	} {
		if _, err := Parse(text); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
}

func render(t *testing.T, concise bool) string {
	t.Helper()
	r, err := Parse(sample)
	if err != nil {
		t.Fatal(err)
	}
	cost := 0.0123
	info := Info{Provider: "anthropic", Model: "claude-opus-4-8", ModelVersion: "claude-opus-4-8-20260801",
		EgoVersion: "1.2.3", InputTokens: 12345, OutputTokens: 678, Cost: &cost}
	return Render(r, info, Options{Concise: concise, ReviewerInfo: true})
}

func TestRenderConcise(t *testing.T) {
	out := render(t, true)
	for _, want := range []string{
		"## ⚠️ Needs changes · Risk: **high**",
		"### Summary\n\nAdds a cache.",
		"- 🔴 **High** · Race · `cache.go:40`",
		"<details><summary>2 medium/low findings</summary>",
		"### Dependency changes\n\n- `actions/checkout` v4 → v7 · ⚠️ **Breaking**: Node 24.",
		"### Security review\n\nLogs a token.",
		"- 🛑 **Critical** · Token logged · `auth.go:12` · _uncertain_",
		"**Fix:** Mask it.",
		"**Coverage:** ⚠️ Partial · Some.",
		"<details><summary><b>Questions for the author</b> (1)</summary>",
		"Reviewed by ego 1.2.3 · claude-opus-4-8 · 12.3k in / 678 out · ≈ $0.0123",
		"| Model version | `claude-opus-4-8-20260801` |",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	// High before low within code review; critical security finding shown.
	if strings.Index(out, "Race") > strings.Index(out, "Naming") {
		t.Error("findings not sorted by severity")
	}
}

func TestRenderDetailed(t *testing.T) {
	out := render(t, false)
	if strings.Contains(out, "medium/low finding") {
		t.Error("detailed mode should not fold findings")
	}
	if !strings.Contains(out, "### Questions for the author\n\n- Why 5s TTL?") {
		t.Errorf("questions should be expanded:\n%s", out)
	}
}

func TestRenderEmptySectionsAndNoInfo(t *testing.T) {
	r := &Review{Verdict: "approve", Risk: "low", Summary: "Docs."} // no security summary
	out := Render(r, Info{}, Options{Concise: true})
	for _, want := range []string{"## ✅ Looks good · Risk: **low**", "No issues found.", "No security issues found."} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q", want)
		}
	}
	for _, unwanted := range []string{"Dependency changes", "### Tests", "Questions", "Reviewed by"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("unexpected %q in:\n%s", unwanted, out)
		}
	}
}

func TestRenderRaw(t *testing.T) {
	out := RenderRaw("free text", Info{Model: "m", EgoVersion: "dev"}, Options{ReviewerInfo: true}, "couldn't parse")
	if !strings.HasPrefix(out, "> [!WARNING]\n> couldn't parse\n\nfree text\n\n---") {
		t.Errorf("out = %q", out)
	}
	if !strings.Contains(out, "unknown (set `input-price` / `output-price`)") || !strings.Contains(out, "`not reported`") {
		t.Errorf("missing unknown cost/version:\n%s", out)
	}
}

func TestCleanNeutralizesMentions(t *testing.T) {
	for in, want := range map[string]string{
		"ping @octocat now":       "ping @​octocat now",
		"@org/team please":        "@​org/team please",
		"mail me@example.com":     "mail me@example.com",
		"code `@Override` stays":  "code `@Override` stays",
		"trailing @":              "trailing @",
		"```\n@decorator\n``` @x": "```\n@decorator\n``` @​x",
	} {
		if got := Clean(in); got != want {
			t.Errorf("Clean(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestInlineStripsBackticksAndNewlines(t *testing.T) {
	if got := inline("a`b\nc"); got != "a'b c" {
		t.Errorf("inline = %q", got)
	}
}

func TestLookupPrice(t *testing.T) {
	for model, want := range map[string]float64{
		"claude-opus-4-8":                  5,
		"claude-haiku-4-5-20251001":        1,
		"anthropic.claude-sonnet-5-5":      2,
		"claude-opus-4-5@20251101":         -1, // not in the table
		"claude-opus-5":                    5,
		"claude-opus-5-5":                  4,
		"MiniMax-M3":                       -1,
		"gpt-4o":                           -1,
		"claude-opus-4-8-preview-20260101": -1,
	} {
		p, ok := LookupPrice(model)
		if want < 0 {
			if ok {
				t.Errorf("%s: unexpected price %+v", model, p)
			}
			continue
		}
		if !ok || p.Input != want {
			t.Errorf("%s: price = %+v ok=%v, want input %v", model, p, ok, want)
		}
	}
	if c := (Price{Input: 5, Output: 25}).Cost(1_000_000, 100_000); c != 7.5 {
		t.Errorf("cost = %v", c)
	}
}

func TestSchemaIsValidJSONAndStrict(t *testing.T) {
	var s map[string]any
	if err := json.Unmarshal(Schema, &s); err != nil {
		t.Fatal(err)
	}
	// Every object must set additionalProperties false and require all of
	// its properties (Anthropic structured outputs).
	var check func(path string, node map[string]any)
	check = func(path string, node map[string]any) {
		if node["type"] == "object" {
			if node["additionalProperties"] != false {
				t.Errorf("%s: additionalProperties must be false", path)
			}
			props, _ := node["properties"].(map[string]any)
			req, _ := node["required"].([]any)
			if len(req) != len(props) {
				t.Errorf("%s: %d required vs %d properties", path, len(req), len(props))
			}
			for k, v := range props {
				check(path+"."+k, v.(map[string]any))
			}
		}
		if items, ok := node["items"].(map[string]any); ok {
			check(path+"[]", items)
		}
	}
	check("$", s)
}

func TestSecuritySummaryReplacesNoIssuesLine(t *testing.T) {
	r := &Review{Verdict: "approve", Risk: "low", Summary: "x", SecuritySummary: "No security impact."}
	out := Render(r, Info{}, Options{})
	if !strings.Contains(out, "### Security review\n\nNo security impact.") || strings.Contains(out, "No security issues found.") {
		t.Errorf("out:\n%s", out)
	}
}
