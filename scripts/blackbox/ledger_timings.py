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
from datetime import date

DEFAULT_OPS = ("scan", "maintain", "update_entries_batch", "overview", "cognition_check",
               "verify", "check", "auto_finalize", "observe_review")

args = sys.argv[1:]
if "--help" in args or "-h" in args:
    print(__doc__)
    sys.exit(0)

roots = []
since = ""
ops = DEFAULT_OPS
for arg in args:
    if not arg.startswith("--"):
        roots.append(arg)
        continue
    option, separator, value = arg.partition("=")
    if option not in ("--since", "--ops"):
        sys.exit(f"unknown option: {option}")
    if not separator or not value:
        sys.exit(f"{option} requires a value in {option}=VALUE form")
    if option == "--since":
        try:
            canonical_date = date.fromisoformat(value).isoformat()
        except ValueError:
            canonical_date = ""
        if canonical_date != value:
            sys.exit("--since requires a valid date in YYYY-MM-DD form")
        since = value
    else:
        ops = tuple(op for op in value.split(",") if op)
        if not ops:
            sys.exit("--ops requires at least one operation")
if len(roots) > 1:
    sys.exit("expected at most one repository root")
root = os.path.abspath(roots[0] if roots else ".")
path = os.path.join(root, ".aoci", "ledger.jsonl")
if not os.path.isfile(path):
    sys.exit(f"no Ledger at {path}")


def valid_metric(value):
    return value is None or (type(value) is int and value >= 0)


durations = {}
files = 0
first = last = ""
skipped = 0
with open(path, encoding="utf-8", errors="replace") as fh:
    for line in fh:
        try:
            event = json.loads(line)
        except (ValueError, RecursionError):
            skipped += 1
            continue
        if not isinstance(event, dict):
            skipped += 1
            continue
        ts = event.get("ts")
        op = event.get("op")
        ms = event.get("duration_ms")
        source_count = event.get("paths_count")
        if ((ts is not None and not isinstance(ts, str))
                or (op is not None and not isinstance(op, str))
                or not valid_metric(ms) or not valid_metric(source_count)):
            skipped += 1
            continue
        ts = ts or ""
        if since and ts[:10] < since:
            continue
        if source_count is not None:
            files = max(files, source_count)
        if op not in ops or ms is None:
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
