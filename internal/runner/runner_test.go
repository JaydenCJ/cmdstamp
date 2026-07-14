// Tests for command execution. Everything runs /bin/sh with tiny inline
// scripts — fully offline and deterministic, no PATH assumptions beyond
// a POSIX shell.
package runner

import (
	"strings"
	"testing"
)

func TestArgvCapturesStdout(t *testing.T) {
	out, err := Run(Spec{Argv: []string{"sh", "-c", "echo out; echo err >&2"}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "out\n" {
		t.Errorf("out = %q, want %q (stderr must stay out of stdout capture)", out, "out\n")
	}
}

func TestShellFormRunsPipelines(t *testing.T) {
	out, err := Run(Spec{Shell: "printf 'b\\na\\n' | sort"})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "a\nb\n" {
		t.Errorf("out = %q, want sorted", out)
	}
}

func TestStreamSelection(t *testing.T) {
	out, err := Run(Spec{Shell: "echo out; echo err >&2", Stream: "stderr"})
	if err != nil {
		t.Fatalf("stderr stream: %v", err)
	}
	if out != "err\n" {
		t.Errorf("stderr stream out = %q, want %q", out, "err\n")
	}
	// Sequential writes interleave deterministically when both streams
	// share one buffer.
	out, err = Run(Spec{Shell: "echo one; echo two >&2; echo three", Stream: "combined"})
	if err != nil {
		t.Fatalf("combined stream: %v", err)
	}
	if out != "one\ntwo\nthree\n" {
		t.Errorf("combined stream out = %q", out)
	}
}

func TestUnexpectedExitCodeIsErrorWithStderr(t *testing.T) {
	_, err := Run(Spec{Shell: "echo broken pipe detail >&2; exit 3"})
	if err == nil {
		t.Fatal("want error for exit 3 when expecting 0")
	}
	msg := err.Error()
	if !strings.Contains(msg, "exit 3 (expected 0)") {
		t.Errorf("missing exit explanation: %q", msg)
	}
	if !strings.Contains(msg, "broken pipe detail") {
		t.Errorf("stderr hint missing from error: %q", msg)
	}
	// The contract cuts both ways: a command that "recovers" to exit 0
	// when the manifest promises 2 is a behavior change worth failing on.
	_, err = Run(Spec{Shell: "true", Exit: 2})
	if err == nil || !strings.Contains(err.Error(), "exit 0 (expected 2)") {
		t.Fatalf("reverse direction err = %v", err)
	}
}

func TestExpectedNonZeroExitSucceeds(t *testing.T) {
	// Plenty of tools exit 2 from --help; that must be declarable.
	out, err := Run(Spec{Shell: "echo usage; exit 2", Exit: 2})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "usage\n" {
		t.Errorf("out = %q", out)
	}
}

func TestExtraEnvIsVisible(t *testing.T) {
	out, err := Run(Spec{
		Shell: `printf '%s' "$STAMP_GREETING"`,
		Env:   map[string]string{"STAMP_GREETING": "from-env"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if out != "from-env" {
		t.Errorf("out = %q", out)
	}
}

func TestDirSetsWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	out, err := Run(Spec{Argv: []string{"pwd"}, Dir: dir})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	// Compare with a shell-resolved pwd to tolerate symlinked tmp dirs.
	want, err := Run(Spec{Shell: "pwd", Dir: dir})
	if err != nil {
		t.Fatalf("Run(pwd): %v", err)
	}
	if out != want {
		t.Errorf("pwd = %q, want %q", out, want)
	}
}

func TestUnrunnableSpecsAreErrors(t *testing.T) {
	_, err := Run(Spec{Argv: []string{"cmdstamp-test-no-such-binary-4d2f"}})
	if err == nil || !strings.Contains(err.Error(), "cmdstamp-test-no-such-binary-4d2f") {
		t.Fatalf("missing executable: err = %v", err)
	}
	if _, err := Run(Spec{}); err == nil {
		t.Fatal("want error for empty spec")
	}
}
