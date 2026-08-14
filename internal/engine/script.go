package engine

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/sanketsaurav/bastion/internal/shellquote"
)

// scriptWriter builds the generated bash programs. Every dynamic value passes
// through q() (shell quoting) or b64() (opaque payloads) — nothing
// user-influenced is ever spliced into shell syntax raw.
type scriptWriter struct {
	b strings.Builder
}

func q(s string) string   { return shellquote.Quote(s) }
func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func (w *scriptWriter) linef(format string, args ...any) {
	fmt.Fprintf(&w.b, format+"\n", args...)
}

func (w *scriptWriter) raw(s string) { w.b.WriteString(s) }

func (w *scriptWriter) bytes() []byte { return []byte(w.b.String()) }

// header emits the shared prelude. set -u only: steps handle their own errors
// so a failure is reported as an event, not a silent death.
func (w *scriptWriter) header() {
	w.raw(`#!/usr/bin/env bash
set -u
export LC_ALL=C
b64() { base64 -w0 2>/dev/null; }
eb() { printf %s "$1" | b64; }
f() { printf '@f %s\n' "$*"; }
`)
}

// targetExpr renders a config target path ("~/x" or "/abs") as a quoted bash
// expression that expands ~ via $HOME.
func targetExpr(target string) string {
	if target == "~" {
		return `"$HOME"`
	}
	if strings.HasPrefix(target, "~/") {
		return `"$HOME"/` + q(target[2:])
	}
	return q(target)
}

// runFn emits the apply-step wrapper: streams step output as @l lines, emits
// @e start/ok/fail, and stops the script on the first failure so earlier
// successes stand and the run is resumable (SPEC.md §10).
func (w *scriptWriter) runFn() {
	w.raw(`e() { printf '@e %s %s\n' "$1" "$2"; }
run() {
  local id="$1"; shift
  e "$id" start
  ( "$@" 2>&1 | while IFS= read -r ln; do printf '@l %s %s\n' "$id" "$(printf %s "$ln" | b64)"; done
    exit "${PIPESTATUS[0]}" )
  local rc=$?
  if [ "$rc" -eq 0 ]; then e "$id" ok; else e "$id" fail; exit 20; fi
}
dk() { sudo -n docker "$@"; }
`)
}

// remoteLock emits the remote apply lock: mkdir-based, stale after an hour,
// recoverable without touching any state directory (SPEC.md §10).
func (w *scriptWriter) remoteLock(boxID string) {
	w.linef(`LOCKD="$HOME/.cache/bastion/locks"; mkdir -p "$LOCKD"; LOCK="$LOCKD/%s"`, q(boxID))
	w.raw(`if ! mkdir "$LOCK" 2>/dev/null; then
  age=$(( $(date +%s) - $(stat -c %Y "$LOCK" 2>/dev/null || echo 0) ))
  if [ "$age" -gt 3600 ]; then
    rmdir "$LOCK" 2>/dev/null || true
    mkdir "$LOCK" 2>/dev/null || { e lock fail; exit 21; }
    e lock takeover
  else
    e lock fail; exit 21
  fi
fi
trap 'rmdir "$LOCK" 2>/dev/null' EXIT
`)
}
