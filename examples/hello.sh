#!/bin/sh
# hello.sh — a stand-in for "your CLI": the tool whose output the docs
# quote. Its --help text and subcommand output are what cmdstamp keeps
# fresh in GUIDE.md. Everything is static and offline.
case "${1:-}" in
  --help)
    echo "usage: hello [--help] [greet NAME] [langs]"
    echo ""
    echo "commands:"
    echo "  greet NAME   print a greeting for NAME"
    echo "  langs        list supported languages, one per line"
    ;;
  greet)
    echo "hello, ${2:-world}!"
    ;;
  langs)
    printf 'en\nja\nzh\n'
    ;;
  *)
    echo "hello: unknown command '${1:-}' (try --help)" >&2
    exit 2
    ;;
esac
