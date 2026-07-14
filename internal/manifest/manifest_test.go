// Tests for manifest parsing and validation. Strictness is the feature
// under test: a manifest mistake must be a loud, positioned error at load
// time, never a stamp that silently stops updating documentation.
package manifest

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func parse(t *testing.T, src string) (*Manifest, error) {
	t.Helper()
	return Parse([]byte(src))
}

func mustParse(t *testing.T, src string) *Manifest {
	t.Helper()
	m, err := parse(t, src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return m
}

func assertErr(t *testing.T, src, substr string) {
	t.Helper()
	_, err := parse(t, src)
	if err == nil || !strings.Contains(err.Error(), substr) {
		t.Fatalf("want error containing %q, got %v", substr, err)
	}
}

const minimal = `{
  "version": 1,
  "stamps": {
    "help": {"command": ["tool", "--help"], "files": ["README.md"]}
  }
}`

func TestParseMinimalManifest(t *testing.T) {
	m := mustParse(t, minimal)
	s := m.Stamps["help"]
	if s == nil || len(s.Command) != 2 || s.Files[0] != "README.md" {
		t.Fatalf("unexpected stamp: %+v", s)
	}
	if !s.TrimEnabled() {
		t.Error("trim must default to true")
	}
}

func TestParseRejectsUnknownKeys(t *testing.T) {
	// Top-level and stamp-level typos alike must fail loudly, not
	// no-op: "commmand" silently doing nothing is the failure mode this
	// tool exists to prevent.
	assertErr(t, `{"version": 1, "stamp": {}}`, "stamp")
	assertErr(t, `{"version": 1, "stamps": {"x": {"commmand": ["a"], "files": ["f"]}}}`, "commmand")
}

func TestParseRejectsWrongOrMissingVersion(t *testing.T) {
	assertErr(t, `{"version": 2, "stamps": {"x": {"command": ["a"], "files": ["f"]}}}`, "unsupported version 2")
	assertErr(t, `{"stamps": {"x": {"command": ["a"], "files": ["f"]}}}`, "unsupported version 0")
}

func TestParseRejectsEmptyStamps(t *testing.T) {
	assertErr(t, `{"version": 1, "stamps": {}}`, "no stamps")
}

func TestParseRejectsTrailingGarbage(t *testing.T) {
	assertErr(t, minimal+`{"version": 1}`, "after the top-level object")
}

func TestParseRejectsCommandShellMisuse(t *testing.T) {
	assertErr(t, `{"version": 1, "stamps": {"x": {"command": ["a"], "shell": "a | b", "files": ["f"]}}}`,
		"mutually exclusive")
	assertErr(t, `{"version": 1, "stamps": {"x": {"files": ["f"]}}}`, `needs "command"`)
}

func TestParseValidatesFilesList(t *testing.T) {
	assertErr(t, `{"version": 1, "stamps": {"x": {"command": ["a"], "files": []}}}`, "at least one")
	assertErr(t, `{"version": 1, "stamps": {"x": {"command": ["a"], "files": ["f.md", "f.md"]}}}`,
		"listed twice")
}

func TestParseRejectsBadStampName(t *testing.T) {
	assertErr(t, `{"version": 1, "stamps": {"bad name!": {"command": ["a"], "files": ["f"]}}}`,
		"must match")
}

func TestParseValidatesFormatAndLang(t *testing.T) {
	assertErr(t, `{"version": 1, "stamps": {"x": {"command": ["a"], "files": ["f"], "format": "fenced"}}}`,
		`"code" or "raw"`)
	assertErr(t, `{"version": 1, "stamps": {"x": {"command": ["a"], "files": ["f"], "format": "raw", "lang": "text"}}}`,
		`only applies to format "code"`)
	// A backtick in the fence info string would corrupt the fence line.
	assertErr(t, "{\"version\": 1, \"stamps\": {\"x\": {\"command\": [\"a\"], \"files\": [\"f\"], \"lang\": \"te`xt\"}}}",
		"backticks")
}

func TestParseValidatesExecutionFields(t *testing.T) {
	assertErr(t, `{"version": 1, "stamps": {"x": {"command": ["a"], "files": ["f"], "stream": "both"}}}`,
		`"stdout", "stderr" or "combined"`)
	assertErr(t, `{"version": 1, "stamps": {"x": {"command": ["a"], "files": ["f"], "exit": 300}}}`,
		"0-255")
	assertErr(t, `{"version": 1, "stamps": {"x": {"command": ["a"], "files": ["f"], "env": {"A=B": "v"}}}}`,
		"invalid variable name")
}

func TestParseKeepsPathsInsideProject(t *testing.T) {
	// A committed manifest must not be able to aim a write at /etc or
	// at a sibling checkout.
	assertErr(t, `{"version": 1, "stamps": {"x": {"command": ["a"], "files": ["/etc/motd"]}}}`,
		"must be relative")
	assertErr(t, `{"version": 1, "stamps": {"x": {"command": ["a"], "files": ["../outside.md"]}}}`,
		"must not escape")
	assertErr(t, `{"version": 1, "stamps": {"x": {"command": ["a"], "files": ["f"], "dir": "../.."}}}`,
		"must not escape")
}

func TestNamesAreSorted(t *testing.T) {
	m := mustParse(t, `{
	  "version": 1,
	  "stamps": {
	    "zeta": {"command": ["z"], "files": ["f"]},
	    "alpha": {"command": ["a"], "files": ["f"]},
	    "mid": {"shell": "m", "files": ["f"]}
	  }
	}`)
	got := m.Names()
	want := []string{"alpha", "mid", "zeta"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Names() = %v, want %v", got, want)
		}
	}
}

func TestSelectAllAndSubsetAndUnknown(t *testing.T) {
	m := mustParse(t, `{
	  "version": 1,
	  "stamps": {
	    "b": {"command": ["b"], "files": ["f"]},
	    "a": {"command": ["a"], "files": ["f"]}
	  }
	}`)
	all, err := m.Select(nil)
	if err != nil || len(all) != 2 || all[0] != "a" {
		t.Fatalf("Select(nil) = %v, %v", all, err)
	}
	sub, err := m.Select([]string{"b", "b"})
	if err != nil || len(sub) != 1 || sub[0] != "b" {
		t.Fatalf("Select(b,b) = %v, %v (duplicates must collapse)", sub, err)
	}
	if _, err := m.Select([]string{"nope"}); err == nil || !strings.Contains(err.Error(), `unknown stamp "nope"`) {
		t.Fatalf("Select(nope) err = %v", err)
	}
}

func TestLoadResolvesManifestDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cmdstamp.json")
	if err := os.WriteFile(path, []byte(minimal), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.Dir != dir {
		t.Errorf("Dir = %q, want %q", m.Dir, dir)
	}
	if _, err := Load(filepath.Join(dir, "absent.json")); err == nil ||
		!strings.Contains(err.Error(), "absent.json") {
		t.Errorf("missing-file error should mention the path, got %v", err)
	}
}

func TestStarterManifestParses(t *testing.T) {
	// `cmdstamp init` must never emit something its own parser rejects.
	if _, err := Parse([]byte(Starter)); err != nil {
		t.Fatalf("starter manifest invalid: %v", err)
	}
}

func TestWriteStarterRefusesToOverwrite(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cmdstamp.json")
	if err := WriteStarter(path); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := WriteStarter(path); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second write err = %v, want refusal", err)
	}
}
