// Package cli implements the cmdstamp command-line interface.
//
// The entry point is Run, which takes argv and explicit streams and
// returns a process exit code. Keeping the CLI a pure function of its
// inputs (no os.Exit, no global state beyond the filesystem it is asked
// to touch) is what lets the integration tests drive every subcommand
// in-process, deterministically.
//
// Exit codes:
//
//	0  success — regions stamped or everything verified fresh
//	1  stale — verify found at least one region that no longer matches
//	   its command's output
//	2  usage, manifest, file or command error
package cli

import (
	"flag"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/JaydenCJ/cmdstamp/internal/engine"
	"github.com/JaydenCJ/cmdstamp/internal/manifest"
	"github.com/JaydenCJ/cmdstamp/internal/version"
)

const (
	exitOK    = 0
	exitStale = 1
	exitError = 2

	defaultManifest = "cmdstamp.json"
)

const usageText = `cmdstamp %s — stamp live command output into marked doc regions

Usage:
  cmdstamp [--manifest FILE] <command> [args]

Commands:
  update [name...]
        run declared commands and rewrite their marked regions
        (no names = every stamp in the manifest)
  verify [name...]
        run declared commands and fail with a diff if any region is stale
  list  list declared stamps with commands and target files
  scan [file...]
        inspect files for marked regions and their manifest status
        (no files = every file the manifest references)
  init  write a starter %s in the current directory
  version
        print the cmdstamp version

Global flags (before the command):
  --manifest FILE   manifest path (default %q); files and working
                    directories in it resolve relative to its location
  --version, -V     print the cmdstamp version and exit
  --help, -h        show this help and exit

Markers (three styles, auto-detected; NAME matches a stamp name):
  <!-- cmdstamp:begin NAME -->   ...   <!-- cmdstamp:end NAME -->
  # cmdstamp:begin NAME          ...   # cmdstamp:end NAME
  // cmdstamp:begin NAME         ...   // cmdstamp:end NAME

Exit codes: 0 ok, 1 stale region (verify), 2 usage/manifest/command error.
`

// Run executes one cmdstamp invocation and returns its exit code.
func Run(argv []string, stdout, stderr io.Writer) int {
	manifestPath := defaultManifest

	i := 0
	for i < len(argv) {
		arg := argv[i]
		switch {
		case arg == "--manifest" && i+1 < len(argv):
			manifestPath = argv[i+1]
			i += 2
		case strings.HasPrefix(arg, "--manifest="):
			manifestPath = strings.TrimPrefix(arg, "--manifest=")
			i++
		case arg == "--version" || arg == "-V":
			fmt.Fprintf(stdout, "cmdstamp %s\n", version.Version)
			return exitOK
		case arg == "--help" || arg == "-h":
			printUsage(stdout)
			return exitOK
		case strings.HasPrefix(arg, "-"):
			fmt.Fprintf(stderr, "cmdstamp: unknown global flag %q\n", arg)
			printUsage(stderr)
			return exitError
		default:
			return dispatch(arg, argv[i+1:], manifestPath, stdout, stderr)
		}
	}
	printUsage(stderr)
	return exitError
}

func dispatch(cmd string, args []string, manifestPath string, stdout, stderr io.Writer) int {
	switch cmd {
	case "update":
		return cmdUpdate(args, manifestPath, stdout, stderr)
	case "verify":
		return cmdVerify(args, manifestPath, stdout, stderr)
	case "list":
		return cmdList(args, manifestPath, stdout, stderr)
	case "scan":
		return cmdScan(args, manifestPath, stdout, stderr)
	case "init":
		return cmdInit(args, manifestPath, stdout, stderr)
	case "version":
		fmt.Fprintf(stdout, "cmdstamp %s\n", version.Version)
		return exitOK
	case "help":
		printUsage(stdout)
		return exitOK
	default:
		fmt.Fprintf(stderr, "cmdstamp: unknown command %q\n", cmd)
		printUsage(stderr)
		return exitError
	}
}

func printUsage(w io.Writer) {
	fmt.Fprintf(w, usageText, version.Version, defaultManifest, defaultManifest)
}

// fail prints an error in the one canonical shape and returns exit 2.
func fail(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "cmdstamp: %v\n", err)
	return exitError
}

// newRun loads the manifest and prepares an engine run.
func newRun(manifestPath string) (*engine.Run, error) {
	m, err := manifest.Load(manifestPath)
	if err != nil {
		return nil, err
	}
	return engine.New(m), nil
}

// parseArgs rejects stray flags after the subcommand so that a typo like
// `update --verify` errors instead of being read as a stamp name.
func parseArgs(cmd string, args []string) ([]string, error) {
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return nil, fmt.Errorf("%s: %w", cmd, err)
	}
	for _, a := range fs.Args() {
		if strings.HasPrefix(a, "-") {
			return nil, fmt.Errorf("%s: unknown flag %q", cmd, a)
		}
	}
	return fs.Args(), nil
}

func cmdUpdate(args []string, manifestPath string, stdout, stderr io.Writer) int {
	names, err := parseArgs("update", args)
	if err != nil {
		return fail(stderr, err)
	}
	run, err := newRun(manifestPath)
	if err != nil {
		return fail(stderr, err)
	}
	results, err := run.Update(names)
	if err != nil {
		return fail(stderr, err)
	}
	stamped := 0
	for _, r := range results {
		if r.Fresh {
			fmt.Fprintf(stdout, "unchanged  %s\n", r.Ref())
		} else {
			stamped++
			fmt.Fprintf(stdout, "stamped    %s\n", r.Ref())
		}
	}
	fmt.Fprintf(stdout, "%s: %d stamped, %d unchanged\n",
		regionCount(len(results)), stamped, len(results)-stamped)
	return exitOK
}

func cmdVerify(args []string, manifestPath string, stdout, stderr io.Writer) int {
	names, err := parseArgs("verify", args)
	if err != nil {
		return fail(stderr, err)
	}
	run, err := newRun(manifestPath)
	if err != nil {
		return fail(stderr, err)
	}
	results, err := run.Verify(names)
	if err != nil {
		return fail(stderr, err)
	}
	stale := 0
	for _, r := range results {
		if r.Fresh {
			fmt.Fprintf(stdout, "ok      %s\n", r.Ref())
			continue
		}
		stale++
		fmt.Fprintf(stdout, "stale   %s\n", r.Ref())
		fmt.Fprint(stdout, indent(r.Diff, "        "))
	}
	fmt.Fprintf(stdout, "%s: %d ok, %d stale\n", regionCount(len(results)), len(results)-stale, stale)
	if stale > 0 {
		fmt.Fprintf(stdout, "run `cmdstamp update` to restamp\n")
		return exitStale
	}
	return exitOK
}

func cmdList(args []string, manifestPath string, stdout, stderr io.Writer) int {
	if _, err := parseArgs("list", args); err != nil {
		return fail(stderr, err)
	}
	m, err := manifest.Load(manifestPath)
	if err != nil {
		return fail(stderr, err)
	}
	tw := tabwriter.NewWriter(stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "NAME\tCOMMAND\tFORMAT\tFILES\n")
	for _, name := range m.Names() {
		s := m.Stamps[name]
		cmd := s.Shell
		if cmd == "" {
			cmd = strings.Join(s.Command, " ")
		}
		format := s.Format
		if format == "" {
			format = "code"
		}
		if format == "code" && s.Lang != "" {
			format += ":" + s.Lang
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", name, cmd, format, strings.Join(s.Files, ", "))
	}
	tw.Flush()
	return exitOK
}

func cmdScan(args []string, manifestPath string, stdout, stderr io.Writer) int {
	files, err := parseArgs("scan", args)
	if err != nil {
		return fail(stderr, err)
	}
	run, err := newRun(manifestPath)
	if err != nil {
		return fail(stderr, err)
	}
	reports, missing, err := run.Scan(files)
	if err != nil {
		return fail(stderr, err)
	}
	tw := tabwriter.NewWriter(stdout, 2, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "FILE\tLINES\tNAME\tSTYLE\tSTATUS\n")
	for _, rep := range reports {
		if len(rep.Regions) == 0 {
			fmt.Fprintf(tw, "%s\t-\t-\t-\tno regions\n", rep.File)
			continue
		}
		for _, reg := range rep.Regions {
			status := "declared"
			if !reg.Declared {
				status = "undeclared"
			}
			fmt.Fprintf(tw, "%s\t%d-%d\t%s\t%s\t%s\n",
				rep.File, reg.BeginLine, reg.EndLine, reg.Name, reg.Style, status)
		}
	}
	tw.Flush()
	for _, ref := range missing {
		fmt.Fprintf(stdout, "missing: %s (declared in manifest, markers not found)\n", ref)
	}
	return exitOK
}

func cmdInit(args []string, manifestPath string, stdout, stderr io.Writer) int {
	if _, err := parseArgs("init", args); err != nil {
		return fail(stderr, err)
	}
	if err := manifest.WriteStarter(manifestPath); err != nil {
		return fail(stderr, err)
	}
	fmt.Fprintf(stdout, "wrote %s\n", manifestPath)
	fmt.Fprintf(stdout, "next: add markers to README.md, then run `cmdstamp update`\n")
	return exitOK
}

// regionCount renders "1 region" / "3 regions" for the summary lines.
func regionCount(n int) string {
	if n == 1 {
		return "1 region"
	}
	return fmt.Sprintf("%d regions", n)
}

// indent prefixes every non-empty line of s.
func indent(s, prefix string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(strings.TrimSuffix(s, "\n"), "\n")
	for i, l := range lines {
		if l != "" {
			lines[i] = prefix + l
		}
	}
	return strings.Join(lines, "\n") + "\n"
}
