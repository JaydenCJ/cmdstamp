# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.1.0] - 2026-07-12

### Added

- `cmdstamp update [name...]`: run every declared command once, render its
  output, and splice it into all marked regions across all target files;
  reports stamped vs unchanged regions.
- `cmdstamp verify [name...]`: re-run declared commands and exit 1 with an
  indented unified diff for every region whose stored content no longer
  matches — the drift gate for pre-push hooks and release checklists.
- Language-neutral markers in three auto-detected comment styles
  (`<!-- cmdstamp:begin NAME -->`, `# cmdstamp:begin NAME`,
  `// cmdstamp:begin NAME`); marker lines are preserved byte-for-byte and
  structural mistakes fail with positioned line errors.
- Strict JSON manifest (`cmdstamp.json`, unknown keys rejected) declaring
  argv or shell commands with working directory, extra env vars, stream
  selection (stdout/stderr/combined) and an expected exit code.
- `code` rendering with overflow-proof fence sizing (a command printing
  ``` can never escape its block) and `raw` rendering for commands that
  emit Markdown; trailing-blank-line trimming on by default.
- One command execution per stamp regardless of target-file count; files
  are spliced in memory and written only after every command succeeds, so
  a failing stamp never leaves half-updated docs.
- Manifest paths are confined to the project (no absolute paths, no `..`
  escapes), and file permission bits survive rewrites.
- `cmdstamp list`, `cmdstamp scan` (region inventory with
  declared/undeclared/missing status) and `cmdstamp init`.
- 88 deterministic offline tests (`go test ./...`) and an end-to-end
  `scripts/smoke.sh` that prints `SMOKE OK`.

[0.1.0]: https://github.com/JaydenCJ/cmdstamp/releases/tag/v0.1.0
