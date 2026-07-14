// Integration tests driving the CLI in-process through Run with explicit
// streams and temp-dir projects — every subcommand, every exit code,
// no compiled binary and no global state.
package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/JaydenCJ/cmdstamp/internal/version"
)

// invoke runs the CLI and returns (exit code, stdout, stderr).
func invoke(t *testing.T, argv ...string) (int, string, string) {
	t.Helper()
	var out, errBuf strings.Builder
	code := Run(argv, &out, &errBuf)
	return code, out.String(), errBuf.String()
}

// fixture writes a manifest and project files into a temp dir and returns
// the dir plus the manifest path to pass via --manifest.
func fixture(t *testing.T, manifestJSON string, files map[string]string) (string, string) {
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
	if manifestJSON != "" {
		if err := os.WriteFile(mpath, []byte(manifestJSON), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, mpath
}

const demoManifest = `{
  "version": 1,
  "stamps": {
    "hello": {"command": ["echo", "hello world"], "files": ["doc.md"], "lang": "text"}
  }
}`

const demoDoc = "# doc\n\n<!-- cmdstamp:begin hello -->\n<!-- cmdstamp:end hello -->\n"

func TestVersionFlagAndSubcommand(t *testing.T) {
	want := "cmdstamp " + version.Version + "\n"
	for _, argv := range [][]string{{"--version"}, {"-V"}, {"version"}} {
		code, out, _ := invoke(t, argv...)
		if code != 0 || out != want {
			t.Errorf("%v: code=%d out=%q, want 0 %q", argv, code, out, want)
		}
	}
}

func TestHelpExitsZeroWithUsage(t *testing.T) {
	code, out, _ := invoke(t, "--help")
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	for _, needle := range []string{"update", "verify", "scan", "cmdstamp:begin NAME", "Exit codes"} {
		if !strings.Contains(out, needle) {
			t.Errorf("usage missing %q", needle)
		}
	}
}

func TestUsageErrorsExitTwo(t *testing.T) {
	cases := []struct {
		desc   string
		argv   []string
		substr string
	}{
		{"no args", nil, "Usage:"},
		{"unknown command", []string{"stamp"}, `unknown command "stamp"`},
		{"unknown global flag", []string{"--quiet", "update"}, `unknown global flag "--quiet"`},
	}
	for _, c := range cases {
		code, _, errOut := invoke(t, c.argv...)
		if code != 2 || !strings.Contains(errOut, c.substr) {
			t.Errorf("%s: code=%d stderr=%q, want 2 containing %q", c.desc, code, errOut, c.substr)
		}
	}
}

func TestFlagAfterSubcommandRejected(t *testing.T) {
	// `update --verify` must not be read as a stamp named "--verify".
	_, mpath := fixture(t, demoManifest, map[string]string{"doc.md": demoDoc})
	code, _, errOut := invoke(t, "--manifest", mpath, "update", "--", "--verify")
	if code != 2 || !strings.Contains(errOut, `unknown flag "--verify"`) {
		t.Errorf("code=%d stderr=%q", code, errOut)
	}
}

func TestInitWritesStarterAndRefusesOverwrite(t *testing.T) {
	dir := t.TempDir()
	mpath := filepath.Join(dir, "cmdstamp.json")
	code, out, _ := invoke(t, "--manifest", mpath, "init")
	if code != 0 || !strings.Contains(out, "wrote "+mpath) {
		t.Fatalf("code=%d out=%q", code, out)
	}
	if _, err := os.Stat(mpath); err != nil {
		t.Fatalf("manifest not written: %v", err)
	}
	code, _, errOut := invoke(t, "--manifest", mpath, "init")
	if code != 2 || !strings.Contains(errOut, "already exists") {
		t.Errorf("second init: code=%d stderr=%q", code, errOut)
	}
}

func TestUpdateStampsAndReports(t *testing.T) {
	dir, mpath := fixture(t, demoManifest, map[string]string{"doc.md": demoDoc})
	code, out, _ := invoke(t, "--manifest", mpath, "update")
	if code != 0 {
		t.Fatalf("code = %d, out=%q", code, out)
	}
	if !strings.Contains(out, "stamped    doc.md#hello") ||
		!strings.Contains(out, "1 region: 1 stamped, 0 unchanged") {
		t.Errorf("out = %q", out)
	}
	data, err := os.ReadFile(filepath.Join(dir, "doc.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "```text\nhello world\n```") {
		t.Errorf("doc.md = %q", data)
	}
}

func TestUpdateSecondRunReportsUnchanged(t *testing.T) {
	_, mpath := fixture(t, demoManifest, map[string]string{"doc.md": demoDoc})
	invoke(t, "--manifest", mpath, "update")
	code, out, _ := invoke(t, "--manifest", mpath, "update")
	if code != 0 || !strings.Contains(out, "unchanged  doc.md#hello") {
		t.Errorf("code=%d out=%q", code, out)
	}
}

func TestUpdateUnknownStampExitsTwo(t *testing.T) {
	_, mpath := fixture(t, demoManifest, map[string]string{"doc.md": demoDoc})
	code, _, errOut := invoke(t, "--manifest", mpath, "update", "nope")
	if code != 2 || !strings.Contains(errOut, `unknown stamp "nope"`) {
		t.Errorf("code=%d stderr=%q", code, errOut)
	}
	if !strings.Contains(errOut, "declared: hello") {
		t.Errorf("error should list declared stamps: %q", errOut)
	}
}

func TestUpdateMissingRegionExitsTwo(t *testing.T) {
	_, mpath := fixture(t, demoManifest, map[string]string{"doc.md": "# empty\n"})
	code, _, errOut := invoke(t, "--manifest", mpath, "update")
	if code != 2 || !strings.Contains(errOut, "doc.md") || !strings.Contains(errOut, "not found") {
		t.Errorf("code=%d stderr=%q", code, errOut)
	}
}

func TestUpdateCommandFailureShowsStderrHint(t *testing.T) {
	manifest := `{
	  "version": 1,
	  "stamps": {
	    "boom": {"shell": "echo tool exploded >&2; exit 7", "files": ["doc.md"]}
	  }
	}`
	doc := "<!-- cmdstamp:begin boom -->\n<!-- cmdstamp:end boom -->\n"
	_, mpath := fixture(t, manifest, map[string]string{"doc.md": doc})
	code, _, errOut := invoke(t, "--manifest", mpath, "update")
	if code != 2 {
		t.Fatalf("code = %d", code)
	}
	for _, needle := range []string{`stamp "boom"`, "exit 7 (expected 0)", "tool exploded"} {
		if !strings.Contains(errOut, needle) {
			t.Errorf("stderr %q missing %q", errOut, needle)
		}
	}
}

func TestVerifyFreshExitsZero(t *testing.T) {
	_, mpath := fixture(t, demoManifest, map[string]string{"doc.md": demoDoc})
	invoke(t, "--manifest", mpath, "update")
	code, out, _ := invoke(t, "--manifest", mpath, "verify")
	if code != 0 || !strings.Contains(out, "ok      doc.md#hello") ||
		!strings.Contains(out, "1 region: 1 ok, 0 stale") {
		t.Errorf("code=%d out=%q", code, out)
	}
}

func TestVerifyStaleExitsOneWithDiff(t *testing.T) {
	stale := "<!-- cmdstamp:begin hello -->\n```text\nold text\n```\n<!-- cmdstamp:end hello -->\n"
	_, mpath := fixture(t, demoManifest, map[string]string{"doc.md": stale})
	code, out, _ := invoke(t, "--manifest", mpath, "verify")
	if code != 1 {
		t.Fatalf("code = %d, want 1", code)
	}
	for _, needle := range []string{
		"stale   doc.md#hello",
		"-old text",
		"+hello world",
		"run `cmdstamp update` to restamp",
	} {
		if !strings.Contains(out, needle) {
			t.Errorf("out %q missing %q", out, needle)
		}
	}
}

func TestVerifyDoesNotModifyFiles(t *testing.T) {
	stale := "<!-- cmdstamp:begin hello -->\nanything\n<!-- cmdstamp:end hello -->\n"
	dir, mpath := fixture(t, demoManifest, map[string]string{"doc.md": stale})
	invoke(t, "--manifest", mpath, "verify")
	data, _ := os.ReadFile(filepath.Join(dir, "doc.md"))
	if string(data) != stale {
		t.Error("verify rewrote the file")
	}
}

func TestListShowsStamps(t *testing.T) {
	_, mpath := fixture(t, demoManifest, map[string]string{"doc.md": demoDoc})
	code, out, _ := invoke(t, "--manifest", mpath, "list")
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	for _, needle := range []string{"NAME", "hello", "echo hello world", "code:text", "doc.md"} {
		if !strings.Contains(out, needle) {
			t.Errorf("out %q missing %q", out, needle)
		}
	}
}

func TestScanReportsStatusAndMissing(t *testing.T) {
	manifest := `{
	  "version": 1,
	  "stamps": {
	    "hello": {"command": ["echo", "hi"], "files": ["doc.md"]},
	    "absent": {"command": ["echo", "x"], "files": ["doc.md"]}
	  }
	}`
	doc := "<!-- cmdstamp:begin hello -->\n<!-- cmdstamp:end hello -->\n" +
		"<!-- cmdstamp:begin extra -->\n<!-- cmdstamp:end extra -->\n"
	_, mpath := fixture(t, manifest, map[string]string{"doc.md": doc})
	code, out, _ := invoke(t, "--manifest", mpath, "scan")
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	for _, needle := range []string{
		"declared", "undeclared", "extra", "html",
		"missing: doc.md#absent",
	} {
		if !strings.Contains(out, needle) {
			t.Errorf("out %q missing %q", out, needle)
		}
	}
}

func TestMissingManifestExitsTwo(t *testing.T) {
	code, _, errOut := invoke(t, "--manifest", filepath.Join(t.TempDir(), "none.json"), "update")
	if code != 2 || !strings.Contains(errOut, "none.json") {
		t.Errorf("code=%d stderr=%q", code, errOut)
	}
}

func TestManifestEqualsFormWorks(t *testing.T) {
	_, mpath := fixture(t, demoManifest, map[string]string{"doc.md": demoDoc})
	code, out, _ := invoke(t, "--manifest="+mpath, "verify")
	if code != 1 && code != 0 { // fresh docs would be 0; unstamped fixture is stale
		t.Fatalf("code = %d out=%q", code, out)
	}
	if !strings.Contains(out, "doc.md#hello") {
		t.Errorf("out = %q", out)
	}
}

func TestVerifySubsetOnlyRunsNamedStamps(t *testing.T) {
	manifest := `{
	  "version": 1,
	  "stamps": {
	    "good": {"command": ["echo", "g"], "files": ["doc.md"]},
	    "broken": {"command": ["false"], "files": ["doc.md"]}
	  }
	}`
	doc := "<!-- cmdstamp:begin good -->\n```\ng\n```\n<!-- cmdstamp:end good -->\n" +
		"<!-- cmdstamp:begin broken -->\n<!-- cmdstamp:end broken -->\n"
	_, mpath := fixture(t, manifest, map[string]string{"doc.md": doc})
	// Selecting only "good" must not execute the broken command at all.
	code, out, _ := invoke(t, "--manifest", mpath, "verify", "good")
	if code != 0 || !strings.Contains(out, "1 region: 1 ok, 0 stale") {
		t.Errorf("code=%d out=%q", code, out)
	}
}
