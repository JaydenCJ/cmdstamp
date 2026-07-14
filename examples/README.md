# cmdstamp examples

This directory is a complete miniature project: a fake CLI (`hello.sh`),
a document quoting its output (`GUIDE.md`) and the manifest binding them
(`cmdstamp.json`). Everything runs offline.

## 1. Stamp the guide

`GUIDE.md` ships pre-stamped, so verify passes as checked out:

```bash
cd examples
cmdstamp verify        # 3 regions: 3 ok, 0 stale
cmdstamp update        # idempotent: 0 stamped, 3 unchanged
```

The three stamps show the three main manifest shapes: an argv command
(`hello-help`), a second argv command reusing the same file
(`greet-example`), and a shell pipeline rendered as raw Markdown
(`langs-table` — command output becomes a bullet list, not a code block).

## 2. Watch drift get caught

Edit `hello.sh` — add a language to the `langs` output, or reword the
help text — then:

```bash
cmdstamp verify        # exit 1, unified diff of the stale region
cmdstamp update        # restamp; verify is clean again
git diff GUIDE.md      # the doc change is now reviewable
```

This `verify`-in-a-pre-push-hook loop is the intended workflow: docs
drift is caught by the same gate as failing tests.

## 3. Inspect regions

```bash
cmdstamp list          # declared stamps: command, format, files
cmdstamp scan          # every marked region with line spans and status
```

`scan` also flags regions that exist in files but not in the manifest
(`undeclared`) and stamps whose markers are missing (`missing:` lines) —
the two ways a rename typically goes wrong.
