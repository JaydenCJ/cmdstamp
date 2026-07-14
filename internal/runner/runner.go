// Package runner executes one declared command and captures its output.
//
// Commands come in two shapes: an argv array (executed directly, no shell
// involved, no quoting pitfalls) or a shell string (run via "sh -c" for
// pipelines and redirection). Both are executed with an explicit working
// directory and optional extra environment variables, and the exit code is
// part of the contract: anything other than the expected code is an error
// that carries the command's stderr, because a half-failed command must
// never be stamped into documentation.
package runner

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// Spec describes one command execution.
type Spec struct {
	// Argv is the command and its arguments, executed without a shell.
	// Mutually exclusive with Shell (the manifest enforces this).
	Argv []string
	// Shell is a command line for "sh -c".
	Shell string
	// Dir is the working directory. Empty means the current directory.
	Dir string
	// Env holds extra environment variables layered over the parent
	// process environment.
	Env map[string]string
	// Stream selects what to capture: "stdout" (default), "stderr", or
	// "combined" (both, interleaved in arrival order).
	Stream string
	// Exit is the expected exit code. Help text famously exits 0 on
	// some tools and 2 on others; declaring it keeps both honest.
	Exit int
}

// Run executes the spec and returns the captured output.
func Run(spec Spec) (string, error) {
	var cmd *exec.Cmd
	switch {
	case spec.Shell != "":
		cmd = exec.Command("sh", "-c", spec.Shell)
	case len(spec.Argv) > 0:
		cmd = exec.Command(spec.Argv[0], spec.Argv[1:]...)
	default:
		return "", errors.New("runner: empty command")
	}
	cmd.Dir = spec.Dir
	cmd.Env = mergedEnv(spec.Env)
	cmd.Stdin = nil // stamped commands must not wait on a terminal

	var out, errBuf bytes.Buffer
	switch spec.Stream {
	case "stderr":
		cmd.Stderr = &out
	case "combined":
		cmd.Stdout = &out
		cmd.Stderr = &out
	default: // "stdout"
		cmd.Stdout = &out
		cmd.Stderr = &errBuf
	}

	err := cmd.Run()
	code, runErr := exitCode(err)
	if runErr != nil {
		return "", fmt.Errorf("%s: %w", describe(spec), runErr)
	}
	if code != spec.Exit {
		msg := fmt.Sprintf("%s: exit %d (expected %d)", describe(spec), code, spec.Exit)
		if hint := strings.TrimSpace(errBuf.String()); hint != "" {
			msg += "\n  stderr: " + firstLines(hint, 5)
		}
		return "", errors.New(msg)
	}
	return out.String(), nil
}

// exitCode maps exec.Cmd.Run's error into (code, fatal-error). A nonzero
// exit is data, not failure — the caller compares it to the expectation.
func exitCode(err error) (int, error) {
	if err == nil {
		return 0, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode(), nil
	}
	return 0, err // not found, not executable, dir missing, ...
}

// mergedEnv layers extra over the parent environment, deterministically.
func mergedEnv(extra map[string]string) []string {
	if len(extra) == 0 {
		return os.Environ()
	}
	env := os.Environ()
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		env = append(env, k+"="+extra[k])
	}
	return env
}

// describe renders the command for error messages.
func describe(spec Spec) string {
	if spec.Shell != "" {
		return fmt.Sprintf("sh -c %q", spec.Shell)
	}
	return strings.Join(spec.Argv, " ")
}

// firstLines keeps at most n lines of s, marking any truncation.
func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) <= n {
		return strings.Join(lines, "\n  ")
	}
	more := len(lines) - n
	unit := "lines"
	if more == 1 {
		unit = "line"
	}
	return strings.Join(lines[:n], "\n  ") + fmt.Sprintf("\n  ... (%d more %s)", more, unit)
}
