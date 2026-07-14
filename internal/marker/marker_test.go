// Tests for region scanning and splicing. The splice invariant — bytes
// outside a region are never touched, marker lines survive verbatim — is
// the property that makes cmdstamp safe to run on a real README, so most
// cases here assert byte-exact results, not just "contains".
package marker

import (
	"strings"
	"testing"
)

func mustScan(t *testing.T, text string) []Region {
	t.Helper()
	regions, err := Scan(text)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	return regions
}

func TestScanFindsHTMLRegion(t *testing.T) {
	text := "intro\n<!-- cmdstamp:begin help -->\nold\n<!-- cmdstamp:end help -->\ntail\n"
	regions := mustScan(t, text)
	if len(regions) != 1 {
		t.Fatalf("got %d regions, want 1", len(regions))
	}
	r := regions[0]
	if r.Name != "help" || r.Style != StyleHTML || r.BeginLine != 2 || r.EndLine != 4 {
		t.Errorf("unexpected region: %+v", r)
	}
	if r.Content != "old\n" {
		t.Errorf("content = %q, want %q", r.Content, "old\n")
	}
}

func TestScanFindsHashAndSlashRegions(t *testing.T) {
	cases := []struct {
		text  string
		style Style
	}{
		{"# cmdstamp:begin cfg\nport: 8080\n# cmdstamp:end cfg\n", StyleHash},
		{"// cmdstamp:begin cfg\nconst x = 1\n// cmdstamp:end cfg\n", StyleSlash},
	}
	for _, c := range cases {
		regions := mustScan(t, c.text)
		if len(regions) != 1 || regions[0].Style != c.style {
			t.Fatalf("%s region not recognized: %+v", c.style, regions)
		}
	}
}

func TestScanAllowsIndentedAndSpacedMarkers(t *testing.T) {
	// Markers inside a list or with editor-added trailing spaces must
	// still count; requiring column 0 would make regions vanish after
	// an innocent reformat.
	text := "  <!--  cmdstamp:begin a  -->  \nbody\n\t<!-- cmdstamp:end a -->\n"
	regions := mustScan(t, text)
	if len(regions) != 1 || regions[0].Content != "body\n" {
		t.Fatalf("indented markers not recognized: %+v", regions)
	}
}

func TestScanRecognizesCRLFMarkerLines(t *testing.T) {
	// Files with Windows line endings must at least locate their
	// regions; the trailing CR belongs to the line ending, not the name.
	text := "<!-- cmdstamp:begin w -->\r\nbody\r\n<!-- cmdstamp:end w -->\r\n"
	regions := mustScan(t, text)
	if len(regions) != 1 || regions[0].Name != "w" {
		t.Fatalf("CRLF markers not recognized: %+v", regions)
	}
}

func TestScanIgnoresProseMentions(t *testing.T) {
	// Mid-sentence mentions and code spans must not open regions —
	// otherwise this project's own documentation would break the tool.
	text := "use `<!-- cmdstamp:begin NAME -->` markers like cmdstamp:begin here\n"
	if regions := mustScan(t, text); len(regions) != 0 {
		t.Fatalf("prose mention treated as marker: %+v", regions)
	}
}

func TestScanMultipleRegionsInFileOrder(t *testing.T) {
	text := strings.Join([]string{
		"<!-- cmdstamp:begin b -->", "x", "<!-- cmdstamp:end b -->",
		"<!-- cmdstamp:begin a -->", "<!-- cmdstamp:end a -->", "",
	}, "\n")
	regions := mustScan(t, text)
	if len(regions) != 2 || regions[0].Name != "b" || regions[1].Name != "a" {
		t.Fatalf("want file order [b a], got %+v", regions)
	}
	if regions[1].Content != "" {
		t.Errorf("empty region content = %q, want empty", regions[1].Content)
	}
}

func TestScanStructuralErrorsArePositioned(t *testing.T) {
	// Broken marker structure must point at the offending line — the
	// whole point of failing early is that the user can fix it fast.
	cases := []struct {
		desc   string
		text   string
		line   int
		substr string
	}{
		{"end without begin",
			"text\n<!-- cmdstamp:end x -->\n", 2, "without a matching begin"},
		{"unclosed region reported at its begin line",
			"<!-- cmdstamp:begin x -->\nbody\n", 1, "never closed"},
		{"nested begin",
			"<!-- cmdstamp:begin x -->\n<!-- cmdstamp:begin y -->\n", 2, "cannot nest"},
		{"mismatched end name",
			"<!-- cmdstamp:begin x -->\n<!-- cmdstamp:end y -->\n", 2, "does not match"},
		{"duplicate completed region (splice would be ambiguous)",
			"<!-- cmdstamp:begin x -->\n<!-- cmdstamp:end x -->\n" +
				"<!-- cmdstamp:begin x -->\n<!-- cmdstamp:end x -->\n", 3, "duplicate region"},
	}
	for _, c := range cases {
		_, err := Scan(c.text)
		pe, ok := err.(*ParseError)
		if !ok {
			t.Errorf("%s: want *ParseError, got %v", c.desc, err)
			continue
		}
		if pe.Line != c.line || !strings.Contains(pe.Msg, c.substr) {
			t.Errorf("%s: got line %d msg %q; want line %d containing %q",
				c.desc, pe.Line, pe.Msg, c.line, c.substr)
		}
	}
}

func TestSpliceReplacesOnlyTheRegionBody(t *testing.T) {
	text := "before\n<!-- cmdstamp:begin r -->\nold 1\nold 2\n<!-- cmdstamp:end r -->\nafter\n"
	got, err := Splice(text, "r", "new\n")
	if err != nil {
		t.Fatalf("Splice: %v", err)
	}
	want := "before\n<!-- cmdstamp:begin r -->\nnew\n<!-- cmdstamp:end r -->\nafter\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSplicePreservesMarkerBytesExactly(t *testing.T) {
	// Odd spacing and indentation on the marker lines is the user's
	// choice; a splice must not "fix" it.
	text := "  <!--  cmdstamp:begin r -->  \nold\n\t<!-- cmdstamp:end r -->\n"
	got, err := Splice(text, "r", "new\n")
	if err != nil {
		t.Fatalf("Splice: %v", err)
	}
	want := "  <!--  cmdstamp:begin r -->  \nnew\n\t<!-- cmdstamp:end r -->\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSpliceEmptyContentEmptiesRegion(t *testing.T) {
	text := "<!-- cmdstamp:begin r -->\nold\n<!-- cmdstamp:end r -->\n"
	got, err := Splice(text, "r", "")
	if err != nil {
		t.Fatalf("Splice: %v", err)
	}
	want := "<!-- cmdstamp:begin r -->\n<!-- cmdstamp:end r -->\n"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSpliceIsIdempotent(t *testing.T) {
	text := "a\n<!-- cmdstamp:begin r -->\nx\n<!-- cmdstamp:end r -->\nb\n"
	once, err := Splice(text, "r", "fresh\n")
	if err != nil {
		t.Fatalf("first splice: %v", err)
	}
	twice, err := Splice(once, "r", "fresh\n")
	if err != nil {
		t.Fatalf("second splice: %v", err)
	}
	if once != twice {
		t.Errorf("splice not idempotent:\n1st %q\n2nd %q", once, twice)
	}
}

func TestSplicePreservesFileWithoutFinalNewline(t *testing.T) {
	// The end marker being the file's last line, unterminated, is legal;
	// splicing must not invent a trailing newline the user never wrote.
	text := "<!-- cmdstamp:begin r -->\nold\n<!-- cmdstamp:end r -->"
	got, err := Splice(text, "r", "new\n")
	if err != nil {
		t.Fatalf("Splice: %v", err)
	}
	want := "<!-- cmdstamp:begin r -->\nnew\n<!-- cmdstamp:end r -->"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestSpliceRejectsContentWithoutFinalNewline(t *testing.T) {
	// Internal contract: content lacking "\n" would glue output onto
	// the end marker line and corrupt the file.
	text := "<!-- cmdstamp:begin r -->\n<!-- cmdstamp:end r -->\n"
	if _, err := Splice(text, "r", "no newline"); err == nil {
		t.Fatal("expected error for unterminated content")
	}
}

func TestSpliceUntouchedRegionsSurvive(t *testing.T) {
	text := "<!-- cmdstamp:begin a -->\nkeep a\n<!-- cmdstamp:end a -->\n" +
		"<!-- cmdstamp:begin b -->\nold b\n<!-- cmdstamp:end b -->\n"
	got, err := Splice(text, "b", "new b\n")
	if err != nil {
		t.Fatalf("Splice: %v", err)
	}
	if !strings.Contains(got, "keep a\n") {
		t.Errorf("sibling region was disturbed: %q", got)
	}
	if !strings.Contains(got, "new b\n") || strings.Contains(got, "old b") {
		t.Errorf("target region not replaced: %q", got)
	}
}

func TestFindMissingRegionIsNotFoundError(t *testing.T) {
	_, err := Find("no markers here\n", "ghost")
	if _, ok := err.(*NotFoundError); !ok {
		t.Fatalf("want *NotFoundError, got %v", err)
	}
}

func TestValidName(t *testing.T) {
	for _, good := range []string{"help", "a", "v2", "cli-help", "pkg.list", "a_b", "0start"} {
		if !ValidName(good) {
			t.Errorf("ValidName(%q) = false, want true", good)
		}
	}
	for _, bad := range []string{"", "-lead", ".lead", "sp ace", "semi;colon", "tab\tname", "日本語"} {
		if ValidName(bad) {
			t.Errorf("ValidName(%q) = true, want false", bad)
		}
	}
}

func TestMarkerRenderers(t *testing.T) {
	cases := []struct {
		style Style
		begin string
		end   string
	}{
		{StyleHTML, "<!-- cmdstamp:begin x -->", "<!-- cmdstamp:end x -->"},
		{StyleHash, "# cmdstamp:begin x", "# cmdstamp:end x"},
		{StyleSlash, "// cmdstamp:begin x", "// cmdstamp:end x"},
	}
	for _, c := range cases {
		if got := BeginMarker(c.style, "x"); got != c.begin {
			t.Errorf("BeginMarker(%s) = %q, want %q", c.style, got, c.begin)
		}
		if got := EndMarker(c.style, "x"); got != c.end {
			t.Errorf("EndMarker(%s) = %q, want %q", c.style, got, c.end)
		}
	}
}

func TestRenderedMarkersRoundTripThroughScan(t *testing.T) {
	// Whatever we tell users to paste must be exactly what Scan accepts.
	for _, style := range []Style{StyleHTML, StyleHash, StyleSlash} {
		text := BeginMarker(style, "rt") + "\nbody\n" + EndMarker(style, "rt") + "\n"
		regions, err := Scan(text)
		if err != nil || len(regions) != 1 || regions[0].Style != style {
			t.Errorf("style %s: round trip failed: %v %+v", style, err, regions)
		}
	}
}
