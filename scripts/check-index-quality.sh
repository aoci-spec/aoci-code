#!/usr/bin/env bash
# Index quality floors for this repository's own index.
#
# scripts/index-quality-floors.txt names, per dimension of `aoci index score`,
# the most violations the committed index may carry, the most high-importance
# Entries without an S it may carry in percent, and the budget status it must
# report. The score is taken on the tree as it stands, which closure
# discipline keeps aligned when committed: coverage and freshness are
# governance state and read zero only after governance, so a dirty working
# tree fails here by design. The check runs in CI on the committed tree and
# by name after a closure's governance (ladder: index-quality).
#
# Usage:
#   scripts/check-index-quality.sh [binary]     default $AOCI_BIN, then build/aoci
#
# Exit status: 0 when every floor holds; 1 when one is broken or the floors
# file is missing (a missing gate is a broken gate, never a skipped one); 2
# when the score cannot be taken (no binary, or the score failed).
#
# Tightening a floor is an ordinary edit. Loosening one must say why in the
# commit message, because it means the index got worse and the gate let it.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."
FLOORS="scripts/index-quality-floors.txt"
BIN="${1:-${AOCI_BIN:-build/aoci}}"

if [ ! -f "$FLOORS" ]; then
  echo "check-index-quality: $FLOORS is missing; the gate is broken, not skipped" >&2
  exit 1
fi
if [ ! -x "$BIN" ]; then
  echo "check-index-quality: $BIN is not an executable; build one (make build BUILD_BIN=...) or pass its path" >&2
  exit 2
fi
score_file="$(mktemp "${TMPDIR:-/tmp}/index-quality.XXXXXX")"
trap 'rm -f "$score_file"' EXIT
if ! "$BIN" --json index score > "$score_file"; then
  echo "check-index-quality: $BIN --json index score failed" >&2
  exit 2
fi

# The program arrives on stdin, so the score travels by file.
python3 - "$FLOORS" "$score_file" <<'PY'
import json, sys

floors = {}
for line in open(sys.argv[1], encoding="utf-8"):
    line = line.split("#", 1)[0].strip()
    if line:
        key, value = line.split("=", 1)
        floors[key.strip()] = value.strip()
score = json.load(open(sys.argv[2], encoding="utf-8"))
dimensions = {d["name"]: d for d in score["dimensions"]}
budget = score.get("budget") or {}
broken = 0

def report(ok, key, actual, floor):
    global broken
    print(("ok     " if ok else "BROKEN ") + f"{key:<34} {actual} (floor {floor})")
    if not ok:
        broken += 1

for key, value in floors.items():
    if key in dimensions:
        bad = dimensions[key]["bad"]
        report(bad <= int(value), key, f"{bad}/{dimensions[key]['total']} violations", value)
    elif key == "high_importance_s_absent_percent":
        actual = budget.get(key)
        report(actual is not None and actual <= int(value), key, f"{actual}%", value + "%")
    elif key == "budget_status":
        actual = budget.get("status")
        report(actual == value, key, actual, value)
    else:
        report(False, key, "unknown floor key", value)
layout = score.get("layout", "legacy-monolithic")
print(f"index quality: layout {layout}, {score['entry_count']} Entries, {score['disk_count']} sources, {len(floors)} floors, {broken} broken")
sys.exit(1 if broken else 0)
PY
