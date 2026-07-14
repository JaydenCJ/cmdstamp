// Tests for run orchestration: one command execution per stamp, an
// in-memory file set so stamps sharing a document compose, no writes
// until every command succeeded, and verify never writing at all.
package engine

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JaydenCJ/cmdstamp/internal/manifest"
	"github.com/JaydenCJ/cmdstamp/internal/runner"
)

// project builds a manifest-on-disk fixture and returns the loaded run.
func project(t *testing.T, manifestJSON string, files map[string]string) (*Run, string) {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mpath := filepath.Join(dir, "cmdstamp.json")
	if err := os.WriteFile(mpath, []byte(manifestJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load(mpath)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return New(m), dir
}

func read(t *testing.T, dir, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(name)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

const helloManifest = `{
  "version": 1,
  "stamps": {
    "greet": {"command": ["echo", "hello"], "files": ["doc.md"], "lang": "text"}
  }
}`

const helloDoc = "# doc\n<!-- cmdstamp:begin greet -->\n<!-- cmdstamp:end greet -->\n"

func TestUpdateStampsRenderedOutput(t *testing.T) {
	run, dir := project(t, helloManifest, map[string]string{"doc.md": helloDoc})
	results, err := run.Update(nil)
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(results) != 1 || results[0].Fresh {
		t.Fatalf("results = %+v", results)
	}
	want := "# doc\n<!-- cmdstamp:begin greet -->\n```text\nhello\n```\n<!-- cmdstamp:end greet -->\n"
	if got := read(t, dir, "doc.md"); got != want {
		t.Errorf("doc.md = %q, want %q", got, want)
	}
}

func TestUpdateIsIdempotent(t *testing.T) {
	run, dir := project(t, helloManifest, map[string]string{"doc.md": helloDoc})
	if _, err := run.Update(nil); err != nil {
		t.Fatal(err)
	}
	first := read(t, dir, "doc.md")

	m, err := manifest.Load(filepath.Join(dir, "cmdstamp.json"))
	if err != nil {
		t.Fatal(err)
	}
	results, err := New(m).Update(nil)
	if err != nil {
		t.Fatalf("second Update: %v", err)
	}
	if !results[0].Fresh {
		t.Error("second update must report the region fresh")
	}
	if read(t, dir, "doc.md") != first {
		t.Error("second update changed the file")
	}
}

func TestCommandRunsOncePerStampAcrossFiles(t *testing.T) {
	// A stamp fanned out to three documents must execute its command a
	// single time — commands can be slow or (worse) not perfectly
	// idempotent between calls.
	run, _ := project(t, `{
	  "version": 1,
	  "stamps": {
	    "multi": {"command": ["echo", "x"], "files": ["a.md", "b.md", "c.md"]}
	  }
	}`, map[string]string{
		"a.md": "<!-- cmdstamp:begin multi -->\n<!-- cmdstamp:end multi -->\n",
		"b.md": "<!-- cmdstamp:begin multi -->\n<!-- cmdstamp:end multi -->\n",
		"c.md": "<!-- cmdstamp:begin multi -->\n<!-- cmdstamp:end multi -->\n",
	})
	calls := 0
	run.Runner = func(s runner.Spec) (string, error) {
		calls++
		return "x\n", nil
	}
	results, err := run.Update(nil)
	if err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Errorf("command ran %d times, want 1", calls)
	}
	if len(results) != 3 {
		t.Errorf("got %d results, want 3", len(results))
	}
}

func TestStampsSharingAFileCompose(t *testing.T) {
	// Both regions live in one document; the second splice must build
	// on the first, not on the stale on-disk copy.
	doc := "<!-- cmdstamp:begin one -->\n<!-- cmdstamp:end one -->\n" +
		"<!-- cmdstamp:begin two -->\n<!-- cmdstamp:end two -->\n"
	run, dir := project(t, `{
	  "version": 1,
	  "stamps": {
	    "one": {"command": ["echo", "first"], "files": ["doc.md"], "format": "raw"},
	    "two": {"command": ["echo", "second"], "files": ["doc.md"], "format": "raw"}
	  }
	}`, map[string]string{"doc.md": doc})
	if _, err := run.Update(nil); err != nil {
		t.Fatal(err)
	}
	got := read(t, dir, "doc.md")
	if !strings.Contains(got, "first\n") || !strings.Contains(got, "second\n") {
		t.Errorf("one of the sibling stamps was lost: %q", got)
	}
}

func TestUpdateWritesNothingWhenALaterCommandFails(t *testing.T) {
	// Stamps run in name order; "aaa" succeeds, "zzz" fails. The file
	// must remain byte-identical: half-updated docs are worse than
	// stale docs because they look regenerated.
	doc := "<!-- cmdstamp:begin aaa -->\n<!-- cmdstamp:end aaa -->\n" +
		"<!-- cmdstamp:begin zzz -->\n<!-- cmdstamp:end zzz -->\n"
	run, dir := project(t, `{
	  "version": 1,
	  "stamps": {
	    "aaa": {"command": ["echo", "fine"], "files": ["doc.md"]},
	    "zzz": {"command": ["false"], "files": ["doc.md"]}
	  }
	}`, map[string]string{"doc.md": doc})
	_, err := run.Update(nil)
	if err == nil || !strings.Contains(err.Error(), `stamp "zzz"`) {
		t.Fatalf("err = %v, want zzz failure", err)
	}
	if read(t, dir, "doc.md") != doc {
		t.Error("file was partially written despite a failed stamp")
	}
}

func TestVerifyReportsStaleWithDiffAndNeverWrites(t *testing.T) {
	stale := "<!-- cmdstamp:begin greet -->\n```text\nold greeting\n```\n<!-- cmdstamp:end greet -->\n"
	run, dir := project(t, helloManifest, map[string]string{"doc.md": stale})
	results, err := run.Verify(nil)
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	r := results[0]
	if r.Fresh {
		t.Fatal("stale region reported fresh")
	}
	if !strings.Contains(r.Diff, "-old greeting") || !strings.Contains(r.Diff, "+hello") {
		t.Errorf("diff not actionable:\n%s", r.Diff)
	}
	if !strings.Contains(r.Diff, "doc.md#greet (stored)") {
		t.Errorf("diff labels missing region ref:\n%s", r.Diff)
	}
	if read(t, dir, "doc.md") != stale {
		t.Error("verify modified a file")
	}
}

func TestVerifyFreshAfterUpdate(t *testing.T) {
	run, dir := project(t, helloManifest, map[string]string{"doc.md": helloDoc})
	if _, err := run.Update(nil); err != nil {
		t.Fatal(err)
	}
	m, err := manifest.Load(filepath.Join(dir, "cmdstamp.json"))
	if err != nil {
		t.Fatal(err)
	}
	results, err := New(m).Verify(nil)
	if err != nil {
		t.Fatal(err)
	}
	if !results[0].Fresh || results[0].Diff != "" {
		t.Errorf("results = %+v, want fresh with no diff", results[0])
	}
}

func TestMissingRegionErrorNamesFileAndStamp(t *testing.T) {
	run, _ := project(t, helloManifest, map[string]string{"doc.md": "# no markers\n"})
	_, err := run.Update(nil)
	if err == nil {
		t.Fatal("want error for missing region")
	}
	for _, needle := range []string{`stamp "greet"`, "doc.md", "not found"} {
		if !strings.Contains(err.Error(), needle) {
			t.Errorf("error %q missing %q", err, needle)
		}
	}
}

func TestStampDirResolvesAgainstManifest(t *testing.T) {
	run, dir := project(t, `{
	  "version": 1,
	  "stamps": {
	    "here": {"shell": "basename \"$(pwd)\"", "files": ["doc.md"], "dir": "sub", "format": "raw"}
	  }
	}`, map[string]string{
		"doc.md":       "<!-- cmdstamp:begin here -->\n<!-- cmdstamp:end here -->\n",
		"sub/.keep":    "",
		"sub/note.txt": "n",
	})
	if _, err := run.Update(nil); err != nil {
		t.Fatal(err)
	}
	if got := read(t, dir, "doc.md"); !strings.Contains(got, "sub\n") {
		t.Errorf("command did not run in sub/: %q", got)
	}
}

func TestUpdatePreservesFileMode(t *testing.T) {
	run, dir := project(t, helloManifest, map[string]string{"doc.md": helloDoc})
	path := filepath.Join(dir, "doc.md")
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := run.Update(nil); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("mode = %v, want 0600 preserved", info.Mode().Perm())
	}
}

func TestScanClassifiesRegions(t *testing.T) {
	run, _ := project(t, `{
	  "version": 1,
	  "stamps": {
	    "known": {"command": ["echo", "k"], "files": ["doc.md"]},
	    "ghost": {"command": ["echo", "g"], "files": ["doc.md"]}
	  }
	}`, map[string]string{
		"doc.md": "<!-- cmdstamp:begin known -->\n<!-- cmdstamp:end known -->\n" +
			"<!-- cmdstamp:begin stray -->\n<!-- cmdstamp:end stray -->\n",
	})
	reports, missing, err := run.Scan(nil)
	if err != nil {
		t.Fatalf("Scan: %v", err)
	}
	if len(reports) != 1 || len(reports[0].Regions) != 2 {
		t.Fatalf("reports = %+v", reports)
	}
	byName := map[string]bool{}
	for _, r := range reports[0].Regions {
		byName[r.Name] = r.Declared
	}
	if !byName["known"] || byName["stray"] {
		t.Errorf("declared flags wrong: %v", byName)
	}
	if len(missing) != 1 || missing[0] != "doc.md#ghost" {
		t.Errorf("missing = %v, want [doc.md#ghost]", missing)
	}
}

func TestRunnerErrorsPropagateWithStampName(t *testing.T) {
	run, _ := project(t, helloManifest, map[string]string{"doc.md": helloDoc})
	run.Runner = func(runner.Spec) (string, error) {
		return "", errors.New("boom")
	}
	_, err := run.Verify(nil)
	if err == nil || !strings.Contains(err.Error(), `stamp "greet": boom`) {
		t.Fatalf("err = %v", err)
	}
}
