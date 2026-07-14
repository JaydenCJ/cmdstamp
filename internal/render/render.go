// Package render turns raw command output into the text that goes between
// a region's markers.
//
// Two formats exist. "code" (the default) wraps output in a fenced code
// block — the fence is sized to be longer than any backtick run inside the
// output, so a command that itself prints ``` can never break out of its
// block. "raw" inserts the output verbatim, for commands that emit
// Markdown (tables, lists) meant to be part of the document itself.
package render

import (
	"strings"
)

// Options controls how command output is rendered into a region body.
type Options struct {
	// Format is "code" or "raw". Validation happens in the manifest
	// package; render treats anything other than "raw" as "code".
	Format string
	// Lang is the fence info string for code format (e.g. "text",
	// "console"). Ignored for raw format.
	Lang string
	// Trim, when true, drops trailing whitespace-only lines from the
	// output before rendering. Most CLIs end --help with a blank line
	// or two; keeping them just churns diffs.
	Trim bool
}

// Body renders output into region content. The result is either empty or
// ends with "\n" — the invariant marker.Splice enforces — so the two
// packages compose without newline surprises.
func Body(output string, opts Options) string {
	out := normalize(output, opts.Trim)
	if opts.Format == "raw" {
		return out
	}
	fence := fenceFor(out)
	var b strings.Builder
	b.Grow(len(out) + 2*len(fence) + len(opts.Lang) + 2)
	b.WriteString(fence)
	b.WriteString(opts.Lang)
	b.WriteString("\n")
	b.WriteString(out)
	b.WriteString(fence)
	b.WriteString("\n")
	return b.String()
}

// normalize guarantees the output is empty or newline-terminated, and
// optionally trims trailing whitespace-only lines. It never touches
// interior lines: stamped output should stay honest, not prettified.
func normalize(output string, trim bool) string {
	if trim {
		output = strings.TrimRight(output, " \t\r\n")
	}
	if output == "" {
		return ""
	}
	if !strings.HasSuffix(output, "\n") {
		output += "\n"
	}
	return output
}

// fenceFor returns a backtick fence strictly longer than the longest
// backtick run found at the start of any line in out (CommonMark only
// recognizes fences at a line start, after up to three spaces of
// indentation — inline runs cannot close a block).
func fenceFor(out string) string {
	longest := 0
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		if len(line)-len(trimmed) > 3 {
			continue // indented ≥4 spaces: a code line, not a fence
		}
		run := 0
		for _, r := range trimmed {
			if r != '`' {
				break
			}
			run++
		}
		if run > longest {
			longest = run
		}
	}
	n := longest + 1
	if n < 3 {
		n = 3
	}
	return strings.Repeat("`", n)
}
