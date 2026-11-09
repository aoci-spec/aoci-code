#!/usr/bin/env python3
"""Summarize how long each governance call took on a repository, read from
its own .aoci/ledger.jsonl: no clone, no new call, nothing written. Every
event the binary appends carries duration_ms, so a user's Ledger already
holds the per-call timings a real host produced; this prints them per
operation (count, median, p90, max) with the largest source count any event
recorded (check, scan, and index score carry one), as one JSON line, ready
to paste into an issue.

usage: ledger_timings.py [<repository-root>] [--since=YYYY-MM-DD] [--ops=a,b,c]
  <repository-root>  defaults to the current directory
  --since=DATE       keep events on or after that UTC date
  --ops=LIST         restrict to these operations (default: the governance
                     calls a user waits on)
Options take only the --opt=value form. The Ledger never records source,
Entry text, or credentials, so the summary carries none either."""
import json
import os
import sys

DEFAULT_OPS = ("scan", "maintain", "update_entries_batch", "overview", "cognition_check",
               "verify", "check", "auto_finalize", "observe_review")

args = [a for a in sys.argv[1:] if not a.startswith("--")]
opts = {a.split("=")[0]: (a.split("=")[1] if "=" in a else "1") for a in sys.argv[1:] if a.startswith("--")}
if "--help" in opts or "-h" in args:
    print(__doc__)
    sys.exit(0)
root = os.path.abspath(args[0] if args else ".")
path = os.path.join(root, ".aoci", "ledger.jsonl")
if not os.path.isfile(path):
    sys.exit(f"no Ledger at {path}")
since = opts.get("--since", "")
ops = tuple(o for o in opts.get("--ops", "").split(",") if o) or DEFAULT_OPS

durations = {}
files = 0
first = last = ""
skipped = 0
with open(path, encoding="utf-8", errors="replace") as fh:
    for line in fh:
        try:
            event = json.loads(line)
        except ValueError:
            skipped += 1
            continue
        ts = event.get("ts") or ""
        if since and ts[:10] < since:
            continue
        op = event.get("op")
        if isinstance(event.get("paths_count"), int):
            files = max(files, event["paths_count"])
        ms = event.get("duration_ms")
        if op not in ops or not isinstance(ms, int):
            continue
        durations.setdefault(op, []).append(ms)
        first = first or ts
        last = ts


def percentile(values, share):
    ordered = sorted(values)
    return ordered[min(len(ordered) - 1, int(len(ordered) * share))]


summary = {
    "version": "ledger-timings/v1",
    "platform": sys.platform,
    "events_from": first[:19],
    "events_to": last[:19],
    "index_sources": files,
    "calls": {op: {"n": len(v), "median_ms": percentile(v, 0.5), "p90_ms": percentile(v, 0.9), "max_ms": max(v)}
              for op, v in sorted(durations.items())},
}
if skipped:
    summary["unreadable_lines"] = skipped
print(json.dumps(summary, ensure_ascii=False))
