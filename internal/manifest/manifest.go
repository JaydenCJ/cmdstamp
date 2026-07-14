// Package manifest loads and validates cmdstamp.json.
//
// The manifest is the whole configuration surface: files carry only inert
// name markers, and everything about *how* a region gets its content —
// the command, its working directory, the output format — lives here, in
// one reviewable place. Parsing is strict: unknown keys are errors, not
// warnings, because a typoed "commmand" that silently does nothing is how
// documentation quietly stops being regenerated.
package manifest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/JaydenCJ/cmdstamp/internal/marker"
)

// FormatVersion is the manifest schema version this build reads.
const FormatVersion = 1

// Stamp is one declared command → regions mapping.
type Stamp struct {
	// Command is the argv form (no shell). Exactly one of Command and
	// Shell must be set.
	Command []string `json:"command,omitempty"`
	// Shell is a "sh -c" command line for pipelines.
	Shell string `json:"shell,omitempty"`
	// Files lists the documents containing a region named after this
	// stamp. Paths are relative to the manifest's directory.
	Files []string `json:"files"`
	// Format is "code" (fenced block, default) or "raw" (verbatim).
	Format string `json:"format,omitempty"`
	// Lang is the code-fence info string ("text", "console", ...).
	Lang string `json:"lang,omitempty"`
	// Dir is the command's working directory, relative to the manifest.
	Dir string `json:"dir,omitempty"`
	// Env holds extra environment variables for the command.
	Env map[string]string `json:"env,omitempty"`
	// Stream is "stdout" (default), "stderr" or "combined".
	Stream string `json:"stream,omitempty"`
	// Exit is the exit code the command is expected to return.
	Exit int `json:"exit,omitempty"`
	// Trim drops trailing blank lines from output. Defaults to true;
	// spelled as a pointer so "absent" and "false" are distinguishable.
	Trim *bool `json:"trim,omitempty"`
}

// Manifest is the parsed, validated cmdstamp.json.
type Manifest struct {
	Version int               `json:"version"`
	Stamps  map[string]*Stamp `json:"stamps"`

	// Dir is the directory containing the manifest file; Files and Dir
	// fields resolve against it so invocations work from anywhere.
	Dir string `json:"-"`
}

// Load reads and validates the manifest at path.
func Load(path string) (*Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("manifest: %w", err)
	}
	m, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("manifest %s: %w", path, err)
	}
	m.Dir = filepath.Dir(path)
	return m, nil
}

// Parse decodes and validates manifest JSON. DisallowUnknownFields makes
// every typo loud.
func Parse(data []byte) (*Manifest, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var m Manifest
	if err := dec.Decode(&m); err != nil {
		return nil, err
	}
	// Reject trailing garbage after the top-level object.
	if dec.More() {
		return nil, fmt.Errorf("unexpected content after the top-level object")
	}
	if err := m.validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

func (m *Manifest) validate() error {
	if m.Version != FormatVersion {
		return fmt.Errorf("unsupported version %d (this build reads version %d)", m.Version, FormatVersion)
	}
	if len(m.Stamps) == 0 {
		return fmt.Errorf("no stamps declared")
	}
	for name, s := range m.Stamps {
		if err := validateStamp(name, s); err != nil {
			return err
		}
	}
	return nil
}

func validateStamp(name string, s *Stamp) error {
	bad := func(format string, args ...any) error {
		return fmt.Errorf("stamp %q: %s", name, fmt.Sprintf(format, args...))
	}
	if !marker.ValidName(name) {
		return fmt.Errorf("stamp name %q: must match [A-Za-z0-9][A-Za-z0-9_.-]*", name)
	}
	if s == nil {
		return bad("must be an object")
	}
	hasArgv := len(s.Command) > 0
	hasShell := s.Shell != ""
	switch {
	case hasArgv && hasShell:
		return bad(`"command" and "shell" are mutually exclusive`)
	case !hasArgv && !hasShell:
		return bad(`needs "command" (argv array) or "shell" (string)`)
	}
	if hasArgv && s.Command[0] == "" {
		return bad("command[0] is empty")
	}
	if len(s.Files) == 0 {
		return bad(`"files" must list at least one target document`)
	}
	seen := make(map[string]bool, len(s.Files))
	for _, f := range s.Files {
		if err := checkRelPath(f); err != nil {
			return bad("file %q: %v", f, err)
		}
		if seen[f] {
			return bad("file %q listed twice", f)
		}
		seen[f] = true
	}
	switch s.Format {
	case "", "code", "raw":
	default:
		return bad(`format %q: must be "code" or "raw"`, s.Format)
	}
	if s.Lang != "" && s.Format == "raw" {
		return bad(`"lang" only applies to format "code"`)
	}
	if strings.ContainsAny(s.Lang, "`\n") {
		return bad(`lang %q: backticks and newlines are not allowed in a fence info string`, s.Lang)
	}
	if s.Dir != "" {
		if err := checkRelPath(s.Dir); err != nil {
			return bad("dir %q: %v", s.Dir, err)
		}
	}
	switch s.Stream {
	case "", "stdout", "stderr", "combined":
	default:
		return bad(`stream %q: must be "stdout", "stderr" or "combined"`, s.Stream)
	}
	if s.Exit < 0 || s.Exit > 255 {
		return bad("exit %d: expected exit codes are 0-255", s.Exit)
	}
	for k := range s.Env {
		if k == "" || strings.ContainsAny(k, "= \t\n") {
			return bad("env key %q: invalid variable name", k)
		}
	}
	return nil
}

// checkRelPath keeps manifest paths inside the project: relative, slash
// separated, no upward traversal. A manifest is often committed and run
// by other people's machines and CI — it should not be able to point a
// write at /etc or at a sibling checkout.
func checkRelPath(p string) error {
	if p == "" {
		return fmt.Errorf("empty path")
	}
	if strings.Contains(p, "\\") {
		return fmt.Errorf("use forward slashes")
	}
	if filepath.IsAbs(p) {
		return fmt.Errorf("must be relative to the manifest")
	}
	clean := filepath.ToSlash(filepath.Clean(p))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("must not escape the manifest directory")
	}
	return nil
}

// Names returns all stamp names in sorted order, the canonical iteration
// order everywhere (JSON maps are unordered; output must not be).
func (m *Manifest) Names() []string {
	names := make([]string, 0, len(m.Stamps))
	for n := range m.Stamps {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Select resolves the user's stamp-name arguments. Empty args mean "all".
func (m *Manifest) Select(args []string) ([]string, error) {
	if len(args) == 0 {
		return m.Names(), nil
	}
	seen := make(map[string]bool, len(args))
	var names []string
	for _, a := range args {
		if _, ok := m.Stamps[a]; !ok {
			return nil, fmt.Errorf("unknown stamp %q (declared: %s)", a, strings.Join(m.Names(), ", "))
		}
		if !seen[a] {
			seen[a] = true
			names = append(names, a)
		}
	}
	sort.Strings(names)
	return names, nil
}

// TrimEnabled reports the effective trim setting (default true).
func (s *Stamp) TrimEnabled() bool { return s.Trim == nil || *s.Trim }

// WriteStarter writes the starter manifest at path, refusing to clobber
// an existing file — init must never eat a configured project.
func WriteStarter(path string) error {
	if _, err := os.Stat(path); err == nil {
		return fmt.Errorf("%s already exists; not overwriting", path)
	}
	return os.WriteFile(path, []byte(Starter), 0o644)
}

// Starter is the manifest written by `cmdstamp init`: a real, runnable
// example (echo is everywhere) that documents the common fields by use.
const Starter = `{
  "version": 1,
  "stamps": {
    "example": {
      "command": ["echo", "hello from cmdstamp"],
      "files": ["README.md"],
      "format": "code",
      "lang": "text"
    }
  }
}
`
