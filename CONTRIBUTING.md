# Contributing to cmdstamp

Issues, discussions and pull requests are all welcome.

## Getting started

You need Go 1.22 or newer; there are no other dependencies of any kind.

```bash
git clone https://github.com/JaydenCJ/cmdstamp.git
cd cmdstamp
go build ./...
go test ./...
bash scripts/smoke.sh
```

`scripts/smoke.sh` builds the binary and drives the full update → verify →
drift → restamp lifecycle against a fake CLI project; it must finish by
printing `SMOKE OK`.

## Before you open a pull request

1. `gofmt -l .` reports nothing (formatting is enforced).
2. `go vet ./...` passes with no findings.
3. `go test ./...` passes (all 88 tests).
4. `bash scripts/smoke.sh` prints `SMOKE OK`.
5. Add tests for behavior changes; keep logic in pure, unit-testable
   packages (`marker`, `render`, `manifest`, `diff`) rather than in the
   CLI layer.

## Ground rules

- Zero runtime dependencies is a core feature: the `go.mod` require list
  stays empty. Adding a dependency needs strong justification in the PR.
- No network calls, ever — cmdstamp runs the commands you declared and
  reads the files you declared, nothing else. No telemetry.
- Splice safety is non-negotiable: bytes outside a marked region are
  never modified, and marker lines are preserved verbatim. Any change to
  splicing needs byte-exact tests.
- The manifest is strict by design: new fields must be validated, and
  unknown keys stay hard errors.
- Code comments and doc comments are written in English.

## Reporting bugs

Please include the output of `cmdstamp --version`, your `cmdstamp.json`,
the marker block from the affected file, and the exact command line —
`cmdstamp scan` output is usually the fastest way to show region state.

## Security

Please do not open public issues for security problems; use GitHub's
private vulnerability reporting on this repository instead.
