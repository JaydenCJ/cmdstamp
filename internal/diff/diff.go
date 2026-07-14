// Package diff renders unified diffs for verify mode.
//
// The implementation is a plain longest-common-subsequence differ over
// lines with the standard prefix/suffix fast path. Regions stamped into
// docs are small (help screens, example output), so an O(N*M) LCS table
// on the trimmed middle is simple, exact and instant; there is no need
// for the full Myers machinery here.
package diff

import (
	"fmt"
	"strings"
)

// op is one scripted edit step. aIdx/bIdx are 0-based indexes of the line
// on each side, or -1 when the op does not touch that side.
type op struct {
	kind       byte // ' ' keep, '-' delete (from a), '+' insert (from b)
	line       string
	aIdx, bIdx int
}

// Unified returns a unified diff of a against b with the given number of
// context lines, or "" when they are equal. aLabel and bLabel become the
// ---/+++ header names. Inputs are split on "\n"; a missing final newline
// is flagged with the git-style "\ No newline at end of file" marker so
// that even an EOL-only difference is visible.
func Unified(aLabel, bLabel, a, b string, context int) string {
	if a == b {
		return ""
	}
	aLines, aNoEOL := splitLines(a)
	bLines, bNoEOL := splitLines(b)
	ops := script(aLines, bLines)

	// An EOL-only difference leaves the last op a ' ' keep; expand it to
	// a -/+ pair so the change shows up as a hunk at all.
	if aNoEOL != bNoEOL && len(ops) > 0 && ops[len(ops)-1].kind == ' ' {
		last := ops[len(ops)-1]
		ops = append(ops[:len(ops)-1],
			op{'-', last.line, last.aIdx, -1},
			op{'+', last.line, -1, last.bIdx})
	}

	var out strings.Builder
	fmt.Fprintf(&out, "--- %s\n+++ %s\n", aLabel, bLabel)
	for _, h := range hunks(ops, context) {
		fmt.Fprintf(&out, "@@ -%s +%s @@\n", span(h.aStart, h.aCount), span(h.bStart, h.bCount))
		for _, o := range h.ops {
			out.WriteByte(o.kind)
			out.WriteString(o.line)
			out.WriteByte('\n')
			if aNoEOL && o.aIdx == len(aLines)-1 && o.kind != '+' {
				out.WriteString("\\ No newline at end of file\n")
			} else if bNoEOL && o.bIdx == len(bLines)-1 && o.kind != '-' {
				out.WriteString("\\ No newline at end of file\n")
			}
		}
	}
	return out.String()
}

// splitLines splits s into newline-free lines and reports whether the
// final line lacked a terminating newline.
func splitLines(s string) ([]string, bool) {
	if s == "" {
		return nil, false
	}
	noEOL := !strings.HasSuffix(s, "\n")
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n"), noEOL
}

// script computes an edit script from a to b: common prefix/suffix are
// peeled off, the middle goes through an LCS table.
func script(a, b []string) []op {
	pre := 0
	for pre < len(a) && pre < len(b) && a[pre] == b[pre] {
		pre++
	}
	post := 0
	for post < len(a)-pre && post < len(b)-pre && a[len(a)-1-post] == b[len(b)-1-post] {
		post++
	}
	var ops []op
	for i, l := range a[:pre] {
		ops = append(ops, op{' ', l, i, i})
	}
	ops = append(ops, lcsOps(a, b, pre, len(a)-post, len(b)-post)...)
	for k := 0; k < post; k++ {
		ai := len(a) - post + k
		bi := len(b) - post + k
		ops = append(ops, op{' ', a[ai], ai, bi})
	}
	return ops
}

// lcsOps emits delete/insert/keep ops for a[aLo:aHi] vs b[aLo:bHi], with
// indexes expressed against the full slices.
func lcsOps(a, b []string, lo, aHi, bHi int) []op {
	n, m := aHi-lo, bHi-lo
	table := make([][]int, n+1)
	for i := range table {
		table[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[lo+i] == b[lo+j] {
				table[i][j] = table[i+1][j+1] + 1
			} else if table[i+1][j] >= table[i][j+1] {
				table[i][j] = table[i+1][j]
			} else {
				table[i][j] = table[i][j+1]
			}
		}
	}
	var ops []op
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[lo+i] == b[lo+j]:
			ops = append(ops, op{' ', a[lo+i], lo + i, lo + j})
			i++
			j++
		case table[i+1][j] >= table[i][j+1]:
			ops = append(ops, op{'-', a[lo+i], lo + i, -1})
			i++
		default:
			ops = append(ops, op{'+', b[lo+j], -1, lo + j})
			j++
		}
	}
	for ; i < n; i++ {
		ops = append(ops, op{'-', a[lo+i], lo + i, -1})
	}
	for ; j < m; j++ {
		ops = append(ops, op{'+', b[lo+j], -1, lo + j})
	}
	return ops
}

// hunk is a run of ops with its 1-based start lines and side lengths.
type hunk struct {
	aStart, aCount int
	bStart, bCount int
	ops            []op
}

// hunks groups the edit script into unified-diff hunks: every changed op
// plus up to `context` unchanged lines on each side, with adjacent groups
// merged when their context overlaps.
func hunks(ops []op, context int) []hunk {
	keep := make([]bool, len(ops))
	for i, o := range ops {
		if o.kind == ' ' {
			continue
		}
		lo := i - context
		if lo < 0 {
			lo = 0
		}
		hi := i + context
		if hi > len(ops)-1 {
			hi = len(ops) - 1
		}
		for j := lo; j <= hi; j++ {
			keep[j] = true
		}
	}
	var result []hunk
	aLine, bLine := 1, 1
	i := 0
	for i < len(ops) {
		if !keep[i] {
			aLine++
			bLine++ // unkept ops are always ' ' and advance both sides
			i++
			continue
		}
		h := hunk{aStart: aLine, bStart: bLine}
		for i < len(ops) && keep[i] {
			o := ops[i]
			h.ops = append(h.ops, o)
			if o.kind != '+' {
				h.aCount++
				aLine++
			}
			if o.kind != '-' {
				h.bCount++
				bLine++
			}
			i++
		}
		// Zero-length sides start one line earlier, per the format.
		if h.aCount == 0 {
			h.aStart--
		}
		if h.bCount == 0 {
			h.bStart--
		}
		result = append(result, h)
	}
	return result
}

// span renders "start,count", eliding ",1" as diff tools do.
func span(start, count int) string {
	if count == 1 {
		return fmt.Sprintf("%d", start)
	}
	return fmt.Sprintf("%d,%d", start, count)
}
