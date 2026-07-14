// Tests for the unified differ. Verify mode's whole value is a diff a
// human can act on, so these assert exact hunk text, headers and the
// no-newline markers — not just "some output appeared".
package diff

import (
	"strings"
	"testing"
)

func TestEqualInputsProduceEmptyDiff(t *testing.T) {
	if got := Unified("a", "b", "same\nlines\n", "same\nlines\n", 3); got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestSimpleLineChange(t *testing.T) {
	got := Unified("stored", "fresh", "one\ntwo\nthree\n", "one\n2\nthree\n", 3)
	want := "--- stored\n+++ fresh\n@@ -1,3 +1,3 @@\n one\n-two\n+2\n three\n"
	if got != want {
		t.Errorf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestContextIsTrimmed(t *testing.T) {
	// A change deep in a long block must not drag the whole block in.
	var a, b []string
	for i := 0; i < 20; i++ {
		a = append(a, "line")
		b = append(b, "line")
	}
	b[10] = "changed"
	got := Unified("a", "b", strings.Join(a, "\n")+"\n", strings.Join(b, "\n")+"\n", 2)
	if !strings.Contains(got, "@@ -9,5 +9,5 @@") {
		t.Errorf("wrong hunk header:\n%s", got)
	}
	if lines := strings.Count(got, "\n"); lines != 9 { // 2 headers + 1 @@ + 6 body
		t.Errorf("diff has %d lines, want 9:\n%s", lines, got)
	}
}

func TestDistantChangesBecomeSeparateHunks(t *testing.T) {
	var a []string
	for i := 0; i < 30; i++ {
		a = append(a, "ctx")
	}
	b := append([]string(nil), a...)
	b[2] = "first"
	b[27] = "second"
	got := Unified("a", "b", strings.Join(a, "\n")+"\n", strings.Join(b, "\n")+"\n", 3)
	if strings.Count(got, "@@") != 4 { // two hunks, "@@" twice per header
		t.Errorf("want 2 hunks:\n%s", got)
	}
}

func TestInsertionsDeletionsAndEmptySides(t *testing.T) {
	// Pure insertions/deletions and empty sides exercise the zero-length
	// hunk-header convention ("-0,0" style) that trips up naive differs.
	cases := []struct{ desc, a, b, want string }{
		{"insertion only",
			"one\nthree\n", "one\ntwo\nthree\n",
			"--- a\n+++ b\n@@ -1,2 +1,3 @@\n one\n+two\n three\n"},
		{"deletion only",
			"one\ntwo\nthree\n", "one\nthree\n",
			"--- a\n+++ b\n@@ -1,3 +1,2 @@\n one\n-two\n three\n"},
		{"empty to content",
			"", "new\n",
			"--- a\n+++ b\n@@ -0,0 +1 @@\n+new\n"},
		{"content to empty",
			"old\n", "",
			"--- a\n+++ b\n@@ -1 +0,0 @@\n-old\n"},
	}
	for _, c := range cases {
		if got := Unified("a", "b", c.a, c.b, 3); got != c.want {
			t.Errorf("%s:\ngot  %q\nwant %q", c.desc, got, c.want)
		}
	}
}

func TestMissingFinalNewlineIsMarked(t *testing.T) {
	got := Unified("a", "b", "x\nend", "x\nend\n", 3)
	if !strings.Contains(got, "\\ No newline at end of file\n") {
		t.Errorf("missing no-newline marker:\n%s", got)
	}
	// The EOL-only difference must surface as a real -/+ pair.
	if !strings.Contains(got, "-end\n") || !strings.Contains(got, "+end\n") {
		t.Errorf("EOL-only change not visible:\n%s", got)
	}
}

func TestNoNewlineOnBothSidesOfRealChange(t *testing.T) {
	got := Unified("a", "b", "old", "new", 3)
	if strings.Count(got, "\\ No newline at end of file\n") != 2 {
		t.Errorf("want the marker after both -old and +new:\n%s", got)
	}
}
