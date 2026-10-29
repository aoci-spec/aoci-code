#!/usr/bin/env python3
"""Per-call latency track: how long each governance call takes on a real tree,
as a function of its file count, before any Entry exists. It clones each named
repository, initializes it, and times the calls a user waits on: scan, then
three Maintains in one server process (cold, warm, steady: the first creates
.aoci/drafts, which git lists as a new ignored path, so only the third sees
the inventory the second one built; the cold-to-steady gap is what the
in-process memos save), an Overview check, then verify, check, the Guide and
status. Nothing is authored; the rounds of a first build are timed per tool by
realrepo.py (call_seconds). One JSON line per call, a report per repository.
It is a run-by-name contract (scripts/blackbox/ladder.json), not a CI gate:
it needs local clones and the largest leg takes a few minutes.
usage: perf_calls.py <name-or-path>... [--binary=BIN] [--label=L] [--sources=DIR] [--work=DIR]
  <name-or-path>   a directory, or a name under --sources (default
                   $AOCI_REALREPO_SOURCES or ~/aoci-demo-cand)
  --binary=BIN     defaults to $AOCI_BIN, then <repo>/build/aoci
  --work=DIR       where clones and reports go (default $AOCI_REALREPO_WORK,
                   then $TMPDIR/aoci-realrepo); the report is <work>/<name>-<label>.perf.json
Options take only the --opt=value form."""
import json, os, shutil, subprocess, sys, tempfile, time
for _stream in (sys.stdout, sys.stderr):
    if hasattr(_stream, "reconfigure"):
        try:
            _stream.reconfigure(encoding="utf-8", errors="replace")
        except (ValueError, OSError):
            pass
HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
names = [a for a in sys.argv[1:] if not a.startswith("--")]
opts = {a.split("=")[0]: (a.split("=")[1] if "=" in a else "1") for a in sys.argv[1:] if a.startswith("--")}
if not names:
    sys.exit(__doc__)
REPO_ROOT = os.path.dirname(os.path.dirname(HERE))
BIN = opts.get("--binary") or os.environ.get("AOCI_BIN") or os.path.join(REPO_ROOT, "build", "aoci")
if not os.path.isfile(BIN):
    sys.exit(f"binary not found: {BIN}")
LABEL = opts.get("--label", os.path.basename(BIN))
os.environ["AOCI_BIN"] = BIN
import mcp_upgrade as U
sources = opts.get("--sources") or os.environ.get("AOCI_REALREPO_SOURCES") or os.path.expanduser("~/aoci-demo-cand")
work_root = opts.get("--work") or os.environ.get("AOCI_REALREPO_WORK") or os.path.join(tempfile.gettempdir(), "aoci-realrepo")
os.makedirs(work_root, exist_ok=True)


def clone(name_or_path):
    if os.path.isdir(os.path.join(name_or_path, ".git")):
        src = os.path.abspath(name_or_path)
        name = os.path.basename(src.rstrip("/"))
    else:
        name, src = name_or_path, os.path.join(sources, name_or_path)
    if not os.path.isdir(os.path.join(src, ".git")):
        sys.exit(f"not a git repository: {src}")
    work = os.path.join(work_root, f"{name}-{LABEL}-perf")
    shutil.rmtree(work, ignore_errors=True)
    subprocess.run(["git", "clone", "-q", "--local", "--no-hardlinks", src, work], check=True)
    for junk in (".aoci", "aoci.txt", "aoci.meta.txt", "aoci.code.txt"):
        p = os.path.join(work, junk)
        if os.path.isdir(p): shutil.rmtree(p)
        elif os.path.exists(p): os.remove(p)
    return name, work


def timed(label, fn):
    t = time.time()
    out = fn()
    return round(time.time() - t, 2), out


exit_code = 0
for name_or_path in names:
    name, work = clone(name_or_path)
    tracked = int(subprocess.run(["git", "-C", work, "ls-files"], capture_output=True, text=True, encoding="utf-8", errors="replace").stdout.count("\n"))
    report = {"repo": name, "label": LABEL, "binary": BIN, "tracked_files": tracked, "calls": [], "started": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())}

    def rec(call, seconds, size, note=""):
        row = {"repo": name, "files": tracked, "call": call, "seconds": seconds, "bytes": size}
        if note:
            row["note"] = note
        report["calls"].append(row)
        print(json.dumps(row), flush=True)

    U.run(BIN, work, "init", "--locale", "en-US", check=True)
    seconds, proc = timed("scan", lambda: U.run(BIN, work, "scan", "--json"))
    try:
        scan = json.loads(proc.stdout)
        fingerprints = scan.get("fingerprint_count") or 0
    except ValueError:
        scan, fingerprints = {}, 0
    rec("scan", seconds, len(proc.stdout), f"fingerprints={fingerprints} exit={proc.returncode}")
    report["fingerprints"] = fingerprints
    with U.Session(BIN, work) as s:
        for call in ("maintain.cold", "maintain.warm", "maintain.steady"):
            seconds, text = timed(call, lambda: s.call("aoci_maintain"))
            try:
                m = json.loads(text)
                note = f"status={m.get('status')} included={(m.get('code_plan') or {}).get('included')}"
            except ValueError:
                note = "non-json"
            rec(call, seconds, len(text.encode("utf-8")), note)
        seconds, text = timed("overview.check_only", lambda: s.call("aoci_overview", {"check_only": True}))
        rec("overview.check_only", seconds, len(text.encode("utf-8")))
    for call, args in (("verify", ("verify", "--json")), ("check", ("check", "--json")),
                       ("guide", ("index", "agent", "guide", "--agent", "claude", "--json")),
                       ("status", ("status", "--json")), ("scope.status", ("scope", "status", "--json"))):
        seconds, proc = timed(call, lambda a=args: U.run(BIN, work, *a))
        rec(call, seconds, len(proc.stdout), f"exit={proc.returncode}")
    report["total_seconds"] = round(sum(c["seconds"] for c in report["calls"]), 2)
    path = os.path.join(work_root, f"{name}-{LABEL}.perf.json")
    with open(path, "w", encoding="utf-8") as fh:
        json.dump(report, fh, indent=1)
    print(f"perf: {name} files={tracked} fingerprints={fingerprints} total={report['total_seconds']}s report={path}", flush=True)
    if any(c["call"] == "scan" and "exit=0" not in c.get("note", "") for c in report["calls"]):
        exit_code = 1
sys.exit(exit_code)
