# Markers and manifest — the cmdstamp format

This document is the normative reference for the two artifacts cmdstamp
reads: marker lines inside your files and the `cmdstamp.json` manifest.

## Marker grammar

A region is a named span delimited by a begin and an end marker line.
Three comment styles are recognized, auto-detected per line:

```
<!-- cmdstamp:begin NAME -->     <!-- cmdstamp:end NAME -->     (html)
# cmdstamp:begin NAME            # cmdstamp:end NAME            (hash)
// cmdstamp:begin NAME           // cmdstamp:end NAME           (slash)
```

- `NAME` matches `[A-Za-z0-9][A-Za-z0-9_.-]*` and must equal a stamp name
  declared in the manifest for `update`/`verify` to touch it.
- A marker must occupy its own line. Leading/trailing whitespace and a
  trailing CR are ignored for recognition, but the line itself is
  preserved byte-for-byte on splice.
- Prose that merely mentions `cmdstamp:begin` mid-sentence is never a
  marker; recognition is anchored to the whole line.

### Structural rules

Scanning fails with a positioned error (`line N: ...`) when markers are
malformed. All of these are hard errors, never warnings:

| Problem | Why it is fatal |
| --- | --- |
| `end` without an open `begin` | the span is undefined |
| `begin` while another region is open | regions cannot nest |
| `end NAME` not matching the open region | almost always a typo'd rename |
| region never closed at EOF | a splice would eat the rest of the file |
| same name completed twice in one file | the splice target is ambiguous |

### Splice semantics

`update` replaces only the lines strictly between the two markers. The
marker lines — indentation, spacing, comment style — and every byte
outside the region are preserved exactly. Spliced content always ends
with a newline so the end marker keeps its own line; a file that ends
without a final newline keeps that property.

One caveat: recognition is line-based and does not parse Markdown, so a
marker-shaped line inside a fenced code block still counts. Indent such
demonstration lines or break them up (the trick used in the table above).

## Manifest schema (`cmdstamp.json`)

```json
{
  "version": 1,
  "stamps": {
    "cli-help": {
      "command": ["./mytool", "--help"],
      "files": ["README.md", "docs/usage.md"],
      "format": "code",
      "lang": "text"
    }
  }
}
```

Top level: `version` (must be `1`) and `stamps` (name → stamp object,
at least one). Unknown keys anywhere are errors — a typoed field that
silently no-ops is precisely the failure mode cmdstamp exists to remove.

Per stamp:

| Key | Type | Default | Effect |
| --- | --- | --- | --- |
| `command` | string[] | — | argv, executed directly (no shell) |
| `shell` | string | — | command line for `sh -c`; mutually exclusive with `command` |
| `files` | string[] | required | documents containing a region with this stamp's name |
| `format` | string | `"code"` | `"code"` fences the output; `"raw"` inserts it verbatim |
| `lang` | string | `""` | fence info string; only valid with `format: "code"` |
| `dir` | string | manifest dir | working directory for the command |
| `env` | object | `{}` | extra environment variables |
| `stream` | string | `"stdout"` | `"stdout"`, `"stderr"` or `"combined"` |
| `exit` | int | `0` | expected exit code (0–255); any other code aborts the run |
| `trim` | bool | `true` | drop trailing blank lines from the output |

Path rules: `files` and `dir` are relative to the manifest's directory,
use forward slashes, and may not be absolute or escape via `..` — a
committed manifest must not be able to aim a write outside the project.

## Execution model

- Each selected stamp's command runs **exactly once**, even when `files`
  lists many documents.
- Stamps run in sorted name order; files are modified in memory and
  written only after **every** command has succeeded, so a failing
  command never leaves half-updated docs.
- `verify` runs the same commands but writes nothing; it exits `1` and
  prints a unified diff per stale region.
- Exit codes: `0` ok, `1` stale region (verify), `2` usage, manifest,
  marker or command error.

## Rendering

`code` format wraps output in a backtick fence sized strictly longer
than any backtick run at a line start in the output — a stamped command
that prints ``` cannot escape its block. `raw` format inserts output
verbatim for commands that emit Markdown. Both guarantee the region body
is empty or newline-terminated; with `trim` (the default) trailing
whitespace-only lines are dropped first. Interior lines are never edited.
