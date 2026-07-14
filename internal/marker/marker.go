// Package marker locates and rewrites cmdstamp regions in text files.
//
// A region is a named span delimited by two marker lines:
//
//	<!-- cmdstamp:begin hello-help -->
//	...stamped content...
//	<!-- cmdstamp:end hello-help -->
//
// Three comment styles are recognized, so the same tool covers Markdown,
// shell/YAML/TOML and C-family sources without any per-file configuration:
//
//	html   <!-- cmdstamp:begin NAME -->   <!-- cmdstamp:end NAME -->
//	hash   # cmdstamp:begin NAME          # cmdstamp:end NAME
//	slash  // cmdstamp:begin NAME         // cmdstamp:end NAME
//
// Markers are matched on whole lines only (leading/trailing whitespace and
// a trailing CR are ignored), and the original marker lines are preserved
// byte-for-byte on splice — cmdstamp only ever rewrites the lines strictly
// between them. Everything outside regions is untouchable by construction.
package marker

import (
	"fmt"
	"regexp"
	"strings"
)

// Style identifies the comment syntax a marker line was written in.
type Style string

// The recognized marker styles.
const (
	StyleHTML  Style = "html"
	StyleHash  Style = "hash"
	StyleSlash Style = "slash"
)

// Region is one named marked span inside a file.
type Region struct {
	Name  string
	Style Style
	// BeginLine and EndLine are 1-based line numbers of the marker lines.
	BeginLine int
	EndLine   int
	// Content is the exact text between the markers (line terminators
	// included). Empty for an empty region.
	Content string
}

// nameRe validates region names: they travel through manifests, CLI args
// and shell one-liners, so the alphabet is kept deliberately boring.
var nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)

// ValidName reports whether s is a legal region name.
func ValidName(s string) bool { return nameRe.MatchString(s) }

// markerRe matches one marker line in any style. Group 1 = kind
// (begin|end), group 2 = name. Anchored to the whole line so that prose
// merely mentioning "cmdstamp:begin" mid-sentence is never a marker.
var markerRe = regexp.MustCompile(
	`^[ \t]*(?:<!--[ \t]*cmdstamp:(begin|end)[ \t]+([A-Za-z0-9][A-Za-z0-9_.-]*)[ \t]*-->|#[ \t]*cmdstamp:(begin|end)[ \t]+([A-Za-z0-9][A-Za-z0-9_.-]*)|//[ \t]*cmdstamp:(begin|end)[ \t]+([A-Za-z0-9][A-Za-z0-9_.-]*))[ \t]*\r?$`)

// parseLine classifies a single line. ok is false for non-marker lines.
func parseLine(line string) (kind, name string, style Style, ok bool) {
	m := markerRe.FindStringSubmatch(line)
	if m == nil {
		return "", "", "", false
	}
	switch {
	case m[1] != "":
		return m[1], m[2], StyleHTML, true
	case m[3] != "":
		return m[3], m[4], StyleHash, true
	default:
		return m[5], m[6], StyleSlash, true
	}
}

// splitLines splits text into lines, each keeping its trailing "\n" if it
// had one. A final line without a newline is returned as-is, so joining
// the slice reproduces the input byte-for-byte.
func splitLines(text string) []string {
	if text == "" {
		return nil
	}
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

// ParseError is a positioned marker-structure error.
type ParseError struct {
	Line int    // 1-based line number
	Msg  string // human-readable problem
}

func (e *ParseError) Error() string { return fmt.Sprintf("line %d: %s", e.Line, e.Msg) }

// Scan finds every well-formed region in text, in file order. It returns
// a *ParseError on the first structural problem: an end without a begin,
// a begin inside an open region (nesting), a name mismatch, an unclosed
// region at EOF, or the same region name completed twice (splicing would
// be ambiguous).
func Scan(text string) ([]Region, error) {
	lines := splitLines(text)
	var regions []Region
	seen := make(map[string]int) // name -> begin line of completed region
	openName := ""
	openStyle := Style("")
	openLine := 0
	var body strings.Builder

	for i, raw := range lines {
		n := i + 1
		kind, name, style, ok := parseLine(strings.TrimSuffix(raw, "\n"))
		if !ok {
			if openName != "" {
				body.WriteString(raw)
			}
			continue
		}
		switch kind {
		case "begin":
			if openName != "" {
				return nil, &ParseError{n, fmt.Sprintf(
					"begin %q while region %q (line %d) is still open; regions cannot nest", name, openName, openLine)}
			}
			if prev, dup := seen[name]; dup {
				return nil, &ParseError{n, fmt.Sprintf(
					"duplicate region %q (already defined at line %d)", name, prev)}
			}
			openName, openStyle, openLine = name, style, n
			body.Reset()
		case "end":
			if openName == "" {
				return nil, &ParseError{n, fmt.Sprintf("end %q without a matching begin", name)}
			}
			if name != openName {
				return nil, &ParseError{n, fmt.Sprintf(
					"end %q does not match open region %q (line %d)", name, openName, openLine)}
			}
			regions = append(regions, Region{
				Name:      name,
				Style:     openStyle,
				BeginLine: openLine,
				EndLine:   n,
				Content:   body.String(),
			})
			seen[name] = openLine
			openName = ""
		}
	}
	if openName != "" {
		return nil, &ParseError{openLine, fmt.Sprintf("region %q is never closed", openName)}
	}
	return regions, nil
}

// Find returns the region with the given name, scanning text first.
// A missing region is reported distinctly from a structural error so
// callers can suggest adding markers rather than fixing them.
func Find(text, name string) (Region, error) {
	regions, err := Scan(text)
	if err != nil {
		return Region{}, err
	}
	for _, r := range regions {
		if r.Name == name {
			return r, nil
		}
	}
	return Region{}, &NotFoundError{Name: name}
}

// NotFoundError reports that a named region does not exist in a file.
type NotFoundError struct{ Name string }

func (e *NotFoundError) Error() string {
	return fmt.Sprintf("region %q not found (add begin/end markers first)", e.Name)
}

// Splice returns text with the content of the named region replaced by
// content. The marker lines themselves — including their indentation and
// comment style — are preserved byte-for-byte, as is everything outside
// the region. content must be empty or end with "\n" so the end marker
// stays on its own line; Splice enforces this rather than guessing.
func Splice(text, name, content string) (string, error) {
	if content != "" && !strings.HasSuffix(content, "\n") {
		return "", fmt.Errorf("internal: spliced content must end with a newline")
	}
	r, err := Find(text, name)
	if err != nil {
		return "", err
	}
	lines := splitLines(text)
	var b strings.Builder
	b.Grow(len(text) + len(content))
	for i, raw := range lines {
		n := i + 1
		if n > r.BeginLine && n < r.EndLine {
			continue // old region body
		}
		b.WriteString(raw)
		if n == r.BeginLine {
			b.WriteString(content)
		}
	}
	return b.String(), nil
}

// BeginMarker and EndMarker render marker lines in the given style; they
// are used by `cmdstamp init` output and the docs, and keep the exact
// spelling in one place.
func BeginMarker(style Style, name string) string { return renderMarker(style, "begin", name) }

// EndMarker renders the closing marker line for a region.
func EndMarker(style Style, name string) string { return renderMarker(style, "end", name) }

func renderMarker(style Style, kind, name string) string {
	switch style {
	case StyleHash:
		return fmt.Sprintf("# cmdstamp:%s %s", kind, name)
	case StyleSlash:
		return fmt.Sprintf("// cmdstamp:%s %s", kind, name)
	default:
		return fmt.Sprintf("<!-- cmdstamp:%s %s -->", kind, name)
	}
}
