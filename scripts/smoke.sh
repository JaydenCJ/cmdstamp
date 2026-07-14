#!/usr/bin/env bash
# End-to-end smoke test for cmdstamp. No network, idempotent, runs from a
# clean tree. This script plus 'go test ./...' is the whole verification
# story — the repository intentionally ships no CI.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORKDIR="$(mktemp -d)"
trap 'rm -rf "$WORKDIR"' EXIT

fail() {
  echo "SMOKE FAIL: $*" >&2
  exit 1
}

# expect HAYSTACK NEEDLE MESSAGE — substring assert without piping the
# tool into grep -q (early-exiting grep + pipefail is a SIGPIPE race).
expect() {
  case "$1" in
    *"$2"*) ;;
    *) fail "$3 (missing: $2)" ;;
  esac
}

BIN="$WORKDIR/cmdstamp"

echo "[1/9] build"
(cd "$ROOT" && go build -o "$BIN" ./cmd/cmdstamp) || fail "build failed"

echo "[2/9] --version matches the manifest version"
VERSION_OUT="$("$BIN" --version)"
[ "$VERSION_OUT" = "cmdstamp 0.1.0" ] || fail "unexpected version output: $VERSION_OUT"

echo "[3/9] set up a fake CLI project with marked docs"
cd "$WORKDIR"
cat > tool.sh <<'EOF'
#!/bin/sh
if [ "${1:-}" = "--help" ]; then
  echo "usage: tool [--help] [--list]"
  echo "  --list   print known items"
  exit 0
fi
printf 'alpha\nbeta\n'
EOF
chmod +x tool.sh
cat > cmdstamp.json <<'EOF'
{
  "version": 1,
  "stamps": {
    "tool-help": {"command": ["./tool.sh", "--help"], "files": ["GUIDE.md"], "lang": "text"},
    "items": {"shell": "./tool.sh --list | sort", "files": ["GUIDE.md"], "format": "raw"}
  }
}
EOF
cat > GUIDE.md <<'EOF'
# guide

<!-- cmdstamp:begin tool-help -->
<!-- cmdstamp:end tool-help -->

Items:

<!-- cmdstamp:begin items -->
<!-- cmdstamp:end items -->
EOF

echo "[4/9] update stamps both regions"
UPDATE_OUT="$("$BIN" update)"
expect "$UPDATE_OUT" "stamped    GUIDE.md#tool-help" "help region not stamped"
expect "$UPDATE_OUT" "stamped    GUIDE.md#items" "items region not stamped"
grep -q 'usage: tool \[--help\] \[--list\]' GUIDE.md || fail "help text missing from doc"
grep -q '```text' GUIDE.md || fail "code fence missing"
grep -q '^alpha$' GUIDE.md || fail "raw region missing"

echo "[5/9] verify passes and update is idempotent"
VERIFY_OUT="$("$BIN" verify)"
expect "$VERIFY_OUT" "2 regions: 2 ok, 0 stale" "verify not clean after update"
UPDATE_OUT="$("$BIN" update)"
expect "$UPDATE_OUT" "2 regions: 0 stamped, 2 unchanged" "update not idempotent"

echo "[6/9] verify fails with a diff when the tool's output changes"
sed -i.bak 's/alpha/gamma/' tool.sh
set +e
VERIFY_OUT="$("$BIN" verify 2>&1)"
VERIFY_CODE=$?
set -e
[ "$VERIFY_CODE" -eq 1 ] || fail "expected exit 1 on stale docs, got $VERIFY_CODE"
expect "$VERIFY_OUT" "stale   GUIDE.md#items" "stale region not reported"
expect "$VERIFY_OUT" "-alpha" "diff missing old line"
expect "$VERIFY_OUT" "+gamma" "diff missing new line"

echo "[7/9] update restamps, then verify is clean again"
UPDATE_OUT="$("$BIN" update)"
expect "$UPDATE_OUT" "stamped    GUIDE.md#items" "restamp did not happen"
VERIFY_OUT="$("$BIN" verify)"
expect "$VERIFY_OUT" "2 regions: 2 ok, 0 stale" "verify still stale after restamp"

echo "[8/9] list and scan see the project"
LIST_OUT="$("$BIN" list)"
expect "$LIST_OUT" "tool-help" "list missing stamp"
SCAN_OUT="$("$BIN" scan)"
expect "$SCAN_OUT" "declared" "scan missing declared region"

echo "[9/9] init writes a starter manifest and refuses to overwrite"
mkdir fresh && cd fresh
INIT_OUT="$("$BIN" init)"
expect "$INIT_OUT" "wrote cmdstamp.json" "init did not write"
[ -f cmdstamp.json ] || fail "starter manifest missing"
set +e
REINIT_OUT="$("$BIN" init 2>&1)"
REINIT_CODE=$?
set -e
[ "$REINIT_CODE" -eq 2 ] || fail "second init should exit 2, got $REINIT_CODE"
expect "$REINIT_OUT" "already exists" "second init did not refuse"

echo "SMOKE OK"
