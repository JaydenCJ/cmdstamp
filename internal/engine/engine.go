// Package engine orchestrates a cmdstamp run.
//
// One run means: select stamps from the manifest, execute each stamp's
// command exactly once (even when it targets five files), render the
// output, and splice it into every declared region. Files are read once
// into an in-memory set and written once at the end, so two stamps
// aimed at the same document never clobber each other and update stays
// atomic-ish: nothing is written until every command has succeeded.
package engine

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/JaydenCJ/cmdstamp/internal/diff"
	"github.com/JaydenCJ/cmdstamp/internal/manifest"
	"github.com/JaydenCJ/cmdstamp/internal/marker"
	"github.com/JaydenCJ/cmdstamp/internal/render"
	"github.com/JaydenCJ/cmdstamp/internal/runner"
)

// Runner executes one command spec; swapped for a fake in tests.
type Runner func(runner.Spec) (string, error)

// Result is the outcome for one (stamp, file) region.
type Result struct {
	Stamp string
	File  string // manifest-relative path, as declared
	// Fresh means the on-disk region already equals the rendered output.
	Fresh bool
	// Diff is the unified diff of stored vs fresh content (verify only,
	// empty when Fresh).
	Diff string
}

// Ref renders the region address used in all reports, e.g. "README.md#help".
func (r Result) Ref() string { return r.File + "#" + r.Stamp }

// Run holds the shared state of one update or verify pass.
type Run struct {
	Manifest *manifest.Manifest
	Runner   Runner

	files map[string]*fileState
}

type fileState struct {
	path    string // absolute-ish path on disk (manifest dir joined)
	content string // current in-memory content, updated by splices
	orig    string // content as read from disk
}

// New prepares a run over the given manifest with the real command runner.
func New(m *manifest.Manifest) *Run {
	return &Run{Manifest: m, Runner: runner.Run, files: map[string]*fileState{}}
}

// Update executes the named stamps (all stamps when names is empty),
// splices fresh output into every region, and writes changed files back.
// The returned results say which regions actually changed.
func (r *Run) Update(names []string) ([]Result, error) {
	results, err := r.execute(names)
	if err != nil {
		return nil, err
	}
	for _, path := range r.sortedFiles() {
		fs := r.files[path]
		if fs.content == fs.orig {
			continue
		}
		if err := writeFilePreserveMode(fs.path, fs.content); err != nil {
			return nil, fmt.Errorf("write %s: %w", path, err)
		}
	}
	return results, nil
}

// Verify executes the named stamps and compares fresh output against what
// the files currently hold, without writing anything. Stale regions carry
// a unified diff.
func (r *Run) Verify(names []string) ([]Result, error) {
	return r.execute(names)
}

// execute runs commands and splices in memory, producing one Result per
// (stamp, file). It is the common core of Update and Verify.
func (r *Run) execute(names []string) ([]Result, error) {
	selected, err := r.Manifest.Select(names)
	if err != nil {
		return nil, err
	}
	var results []Result
	for _, name := range selected {
		s := r.Manifest.Stamps[name]
		output, err := r.Runner(spec(r.Manifest, s))
		if err != nil {
			return nil, fmt.Errorf("stamp %q: %w", name, err)
		}
		body := render.Body(output, render.Options{
			Format: s.Format,
			Lang:   s.Lang,
			Trim:   s.TrimEnabled(),
		})
		for _, file := range s.Files {
			res, err := r.spliceOne(name, file, body)
			if err != nil {
				return nil, err
			}
			results = append(results, res)
		}
	}
	return results, nil
}

// spliceOne applies one region update to the in-memory file set.
func (r *Run) spliceOne(name, file, body string) (Result, error) {
	fs, err := r.load(file)
	if err != nil {
		return Result{}, fmt.Errorf("stamp %q: %w", name, err)
	}
	region, err := marker.Find(fs.content, name)
	if err != nil {
		return Result{}, fmt.Errorf("stamp %q: %s: %w", name, file, err)
	}
	if region.Content == body {
		return Result{Stamp: name, File: file, Fresh: true}, nil
	}
	next, err := marker.Splice(fs.content, name, body)
	if err != nil {
		return Result{}, fmt.Errorf("stamp %q: %s: %w", name, file, err)
	}
	fs.content = next
	ref := file + "#" + name
	return Result{
		Stamp: name,
		File:  file,
		Fresh: false,
		Diff:  diff.Unified(ref+" (stored)", ref+" (fresh)", region.Content, body, 3),
	}, nil
}

// load fetches a file into the run's cache on first use.
func (r *Run) load(file string) (*fileState, error) {
	if fs, ok := r.files[file]; ok {
		return fs, nil
	}
	path := filepath.Join(r.Manifest.Dir, filepath.FromSlash(file))
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	fs := &fileState{path: path, content: string(data), orig: string(data)}
	r.files[file] = fs
	return fs, nil
}

func (r *Run) sortedFiles() []string {
	paths := make([]string, 0, len(r.files))
	for p := range r.files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return paths
}

// spec assembles the runner spec for a stamp, resolving its working
// directory against the manifest location so runs work from anywhere.
func spec(m *manifest.Manifest, s *manifest.Stamp) runner.Spec {
	dir := m.Dir
	if s.Dir != "" {
		dir = filepath.Join(m.Dir, filepath.FromSlash(s.Dir))
	}
	return runner.Spec{
		Argv:   s.Command,
		Shell:  s.Shell,
		Dir:    dir,
		Env:    s.Env,
		Stream: s.Stream,
		Exit:   s.Exit,
	}
}

// writeFilePreserveMode rewrites path keeping its existing permission
// bits (docs are sometimes executable templates or read-only in odd
// setups; silently resetting to 0644 would be a surprise).
func writeFilePreserveMode(path, content string) error {
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	return os.WriteFile(path, []byte(content), mode)
}

// ScanReport describes what `cmdstamp scan` found in one file.
type ScanReport struct {
	File    string
	Regions []ScanRegion
}

// ScanRegion is one marked region with its manifest status.
type ScanRegion struct {
	marker.Region
	// Declared is true when the manifest has a stamp of this name
	// listing this file.
	Declared bool
}

// Scan inspects the given manifest-relative files (all files referenced
// by the manifest when the list is empty) and classifies every region
// found. It also returns the declared regions that are missing on disk,
// as "file#stamp" refs, sorted.
func (r *Run) Scan(files []string) ([]ScanReport, []string, error) {
	declared := map[string]map[string]bool{} // file -> stamp name -> true
	for name, s := range r.Manifest.Stamps {
		for _, f := range s.Files {
			if declared[f] == nil {
				declared[f] = map[string]bool{}
			}
			declared[f][name] = true
		}
	}
	if len(files) == 0 {
		for f := range declared {
			files = append(files, f)
		}
		sort.Strings(files)
	}

	var reports []ScanReport
	var missing []string
	for _, file := range files {
		fs, err := r.load(file)
		if err != nil {
			return nil, nil, err
		}
		regions, err := marker.Scan(fs.content)
		if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", file, err)
		}
		found := map[string]bool{}
		report := ScanReport{File: file}
		for _, reg := range regions {
			found[reg.Name] = true
			report.Regions = append(report.Regions, ScanRegion{
				Region:   reg,
				Declared: declared[file][reg.Name],
			})
		}
		for name := range declared[file] {
			if !found[name] {
				missing = append(missing, file+"#"+name)
			}
		}
		reports = append(reports, report)
	}
	sort.Strings(missing)
	return reports, missing, nil
}
