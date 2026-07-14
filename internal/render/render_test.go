// Tests for output rendering. The fence-sizing cases matter most: a
// stamped command that prints Markdown fences must never be able to
// terminate its own code block and leak into the document.
package render

import (
	"strings"
	"testing"
)

func TestCodeFormatWrapsInFence(t *testing.T) {
	got := Body("usage: tool [flags]\n", Options{Format: "code", Lang: "text", Trim: true})
	want := "```text\nusage: tool [flags]\n```\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestCodeFormatWithoutLang(t *testing.T) {
	got := Body("x\n", Options{Format: "code", Trim: true})
	if got != "```\nx\n```\n" {
		t.Errorf("got %q", got)
	}
}

func TestRawFormatPassesThrough(t *testing.T) {
	md := "| a | b |\n| - | - |\n| 1 | 2 |\n"
	if got := Body(md, Options{Format: "raw", Trim: true}); got != md {
		t.Errorf("raw format altered output: %q", got)
	}
}

func TestFenceOutgrowsBacktickRunsInOutput(t *testing.T) {
	cases := []struct {
		desc   string
		output string
		prefix string
	}{
		{"inner ``` forces a 4-fence", "```sh\necho hi\n```\n", "````\n"},
		{"inner 5-run forces a 6-fence", "`````\nnested\n`````\n", "``````\n"},
		// CommonMark cannot close a fence indented four or more
		// spaces, so such runs must not inflate the fence.
		{"indented run is ignored", "    ```` deep in a code sample\n", "```\n"},
	}
	for _, c := range cases {
		got := Body(c.output, Options{Format: "code", Trim: true})
		if !strings.HasPrefix(got, c.prefix) {
			t.Errorf("%s: got %q, want prefix %q", c.desc, got, c.prefix)
		}
	}
}

func TestTrimDropsTrailingBlankLines(t *testing.T) {
	got := Body("line\n\n \t\n\n", Options{Format: "raw", Trim: true})
	if got != "line\n" {
		t.Errorf("got %q, want %q", got, "line\n")
	}
}

func TestTrimOffPreservesTrailingBlankLines(t *testing.T) {
	got := Body("line\n\n\n", Options{Format: "raw", Trim: false})
	if got != "line\n\n\n" {
		t.Errorf("got %q, want %q", got, "line\n\n\n")
	}
}

func TestTrimNeverTouchesInteriorWhitespace(t *testing.T) {
	// Alignment inside help output is meaningful; only the tail is fair game.
	out := "  -v   verbose\n  -q   quiet\n"
	got := Body(out, Options{Format: "raw", Trim: true})
	if got != out {
		t.Errorf("interior whitespace changed: %q", got)
	}
}

func TestMissingFinalNewlineIsAdded(t *testing.T) {
	// printf-style commands often omit the final newline; the region
	// body must still end with one so the end marker keeps its own line.
	got := Body("no newline", Options{Format: "raw", Trim: false})
	if got != "no newline\n" {
		t.Errorf("got %q", got)
	}
}

func TestEmptyOutputBothFormats(t *testing.T) {
	if got := Body("", Options{Format: "code", Lang: "text", Trim: true}); got != "```text\n```\n" {
		t.Errorf("code: got %q", got)
	}
	if got := Body("", Options{Format: "raw", Trim: true}); got != "" {
		t.Errorf("raw: got %q, want empty region body", got)
	}
}
