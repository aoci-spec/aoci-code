#!/usr/bin/env python3
"""Real-repository track: a fresh local clone of a real third-party repository
-> init -> scan -> Maintain/update_entry rounds with realistic template Entries
(C8/C9 with long S for larger files) -> aligned -> update wave (touch N authored
files, re-align) -> one cognition_optimization batch beside the held sources
(#90) -> PreToolUse hook probe on an indexed file (#85). Every Maintain
response is measured in bytes and checked against the transport budget. The
template Entries carry no semantics, so this track proves transport, batching,
alignment and timing on real trees, never cognition quality.

It is a run-by-name contract (scripts/blackbox/ladder.json), not a CI gate: it
needs local clones of the repositories (RuoYi and excalidraw are the two the
release record compares) and takes a few minutes per repository.

usage: realrepo.py <name-or-path> [<binary>] [--wave=N] [--label=L] [--ignored-tree=N]
                   [--sources=DIR] [--work=DIR] [--parallel=N]
  <name-or-path>   a directory, or a name under --sources (default
                   $AOCI_REALREPO_SOURCES or ~/aoci-demo-cand)
  <binary>         defaults to $AOCI_BIN, then <repo>/build/aoci
  --work=DIR       where clones and reports go (default $AOCI_REALREPO_WORK,
                   then $TMPDIR/aoci-realrepo); the report is <work>/<name>-<label>.json
  --parallel=N     set code_cognition_parallel_batches=N on the clone and author
                   every sibling a round lists (delivered by batch_id) before the
                   next Maintain: one agent playing N, which proves the siblings
                   apply and the plan completes; the wall clock of N agents it cannot show
Options take only the --opt=value form; a space-separated value is read as a positional argument."""
import hashlib, json, os, shutil, subprocess, sys, tempfile, time

# The suites print fixture output that may carry Chinese text; on a Windows
# runner the console codec is cp1252, so print the report as UTF-8 regardless.
for _stream in (sys.stdout, sys.stderr):
    if hasattr(_stream, "reconfigure"):
        try:
            _stream.reconfigure(encoding="utf-8", errors="replace")
        except (ValueError, OSError):
            pass

HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, HERE)
args = [a for a in sys.argv[1:] if not a.startswith("--")]
opts = {a.split("=")[0]: (a.split("=")[1] if "=" in a else "1") for a in sys.argv[1:] if a.startswith("--")}
if not args:
    sys.exit(__doc__)
name_or_path = args[0]
REPO_ROOT = os.path.dirname(os.path.dirname(HERE))
BIN = args[1] if len(args) > 1 else os.environ.get("AOCI_BIN") or os.path.join(REPO_ROOT, "build", "aoci")
if not os.path.isfile(BIN):
    sys.exit(f"binary not found: {BIN}")
WAVE = int(opts.get("--wave", "60"))
LABEL = opts.get("--label", os.path.basename(BIN))
BUDGET = 24 * 1024
os.environ["AOCI_BIN"] = BIN
import mcp_upgrade as U
if os.path.isdir(os.path.join(name_or_path, ".git")):
    src = os.path.abspath(name_or_path)
    name = os.path.basename(src.rstrip("/"))
else:
    name = name_or_path
    sources = opts.get("--sources") or os.environ.get("AOCI_REALREPO_SOURCES") or os.path.expanduser("~/aoci-demo-cand")
    src = os.path.join(sources, name)
if not os.path.isdir(os.path.join(src, ".git")):
    sys.exit(f"not a git repository: {src}")
work_root = opts.get("--work") or os.environ.get("AOCI_REALREPO_WORK") or os.path.join(tempfile.gettempdir(), "aoci-realrepo")
os.makedirs(work_root, exist_ok=True)
work = os.path.join(work_root, f"{name}-{LABEL}")
shutil.rmtree(work, ignore_errors=True)
subprocess.run(["git", "clone", "-q", "--local", "--no-hardlinks", src, work], check=True)
subprocess.run(["git", "-C", work, "config", "user.email", "probe@example.invalid"], check=True)
subprocess.run(["git", "-C", work, "config", "user.name", "probe"], check=True)
for junk in (".aoci", "aoci.txt", "aoci.meta.txt", "aoci.code.txt"):
    p = os.path.join(work, junk)
    if os.path.isdir(p): shutil.rmtree(p)
    elif os.path.exists(p): os.remove(p)
report = {"repo": name, "source": src, "binary": BIN, "label": LABEL, "started": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())}
try:
    report["binary_version"] = subprocess.run([BIN, "--version"], capture_output=True, text=True, encoding="utf-8", errors="replace", timeout=30).stdout.strip()
except Exception as exc:  # noqa: BLE001 - recorded, not fatal
    report["binary_version"] = f"unavailable: {exc}"
IGNORED_TREE = int(opts.get("--ignored-tree", "3000"))
PARALLEL = int(opts.get("--parallel", "1"))
if IGNORED_TREE > 0:
    # #97 stress: a git-ignored node_modules tree with a symlink back into
    # itself. git must account for it as one entry, so scan time and the
    # inventory's ignored count must not scale with IGNORED_TREE.
    for i in range(IGNORED_TREE):
        d = os.path.join(work, "node_modules", f"pkg{i:04d}")
        os.makedirs(d, exist_ok=True)
        with open(os.path.join(d, "index.js"), "w", encoding="utf-8") as fh:
            fh.write("module.exports = %d;\n" % i)
    try:
        os.symlink(os.path.join("..", "node_modules"), os.path.join(work, "node_modules", "loop"))
    except OSError:
        pass
    # Ask git whether the planted tree is ignored instead of searching .gitignore
    # for the word: a rule for a nested web/node_modules/ only would pass a
    # substring test and leave the root tree to be authored file by file.
    def planted_tree_ignored():
        probe = subprocess.run(["git", "-C", work, "check-ignore", "-q", "node_modules/pkg0000/index.js"])
        if probe.returncode not in (0, 1):
            sys.exit(f"git check-ignore failed in {work} (exit {probe.returncode})")
        return probe.returncode == 0
    if not planted_tree_ignored():
        gi = os.path.join(work, ".gitignore")
        existing = open(gi, encoding="utf-8").read() if os.path.exists(gi) else ""
        with open(gi, "a", encoding="utf-8") as fh:
            fh.write(("" if not existing or existing.endswith("\n") else "\n") + "node_modules/\n")
        subprocess.run(["git", "-C", work, "add", ".gitignore"], check=True)
        subprocess.run(["git", "-C", work, "commit", "-q", "-m", "ignore node_modules"], check=True)
        if not planted_tree_ignored():
            sys.exit(f"the planted node_modules tree is still not ignored in {work}; a negation rule overrides node_modules/")
t0 = time.time()
p = U.run(BIN, work, "init", "--locale", "en-US", check=True)
if PARALLEL > 1:
    U.run(BIN, work, "config", "set", "code_cognition_parallel_batches", str(PARALLEL), check=True)
p = U.run(BIN, work, "scan", "--json", check=True)
scan = json.loads(p.stdout)
report["scan"] = {"fingerprints": scan.get("fingerprint_count"), "skipped_sources": scan.get("skipped_sources"),
                  "authoring_estimate": scan.get("authoring_estimate"), "seconds": round(time.time() - t0, 1)}
p = U.run(BIN, work, "scope", "status", "--json", check=False)
try:
    st = json.loads(p.stdout); ev = st.get("evaluation") or {}; summ = ev.get("safe_inventory") or {}
    paths = [e.get("path") for k in ("index", "observe", "exclude") for e in (ev.get(k) or [])]
    report["scan"]["ignored_tree"] = {"planted": IGNORED_TREE, "inventory_ignored": summ.get("ignored"),
                                      "collapsed": "node_modules/" in paths,
                                      "descended": any(x.startswith("node_modules/pkg") for x in paths if x)}
except ValueError:
    report["scan"]["ignored_tree"] = {"planted": IGNORED_TREE, "error": p.stdout[:200]}
print("scan:", json.dumps(report["scan"]), flush=True)

def escale(lines):
    return "L" if lines > 400 else "M" if lines >= 200 else "S" if lines >= 100 else "T"

LONG_S = ("Callers depend on the externally visible behavior of this file staying stable across refactors; "
          "keep its public names, error texts, and ordering as they are and change them only with the tests "
          "that pin them.")

def template_entry(rel, data):
    base = os.path.basename(rel)
    lines = data.count(b"\n") + (1 if data and not data.endswith(b"\n") else 0)
    scale = escale(lines)
    if scale == "L":
        return f"{base}[CG9{scale}]: F:Provides {base}, a large unit of the {name} repository with its own public surface | R:- | A:- | S:{LONG_S} {LONG_S[:220]}"
    if scale == "M":
        return f"{base}[CG8{scale}]: F:Provides {base}, a mid-sized unit of the {name} repository | R:- | A:- | S:{LONG_S}"
    return f"{base}[CG5{scale}]: F:Provides {base} for the {name} repository | R:- | A:- | S:-"

def timed_cli(*args):
    """Seconds one CLI command takes against the work tree; the exit code rides along."""
    t = time.time()
    proc = U.run(BIN, work, *args)
    return {"seconds": round(time.time() - t, 2), "exit": proc.returncode}

def author_rounds(session, phase, max_rounds=400):
    rounds, written, stopped, sizes, included, existing, deliveries = 0, 0, None, [], [], 0, 0
    # Per-tool wall time: how much of a round AOCI itself spends, apart from
    # the model's authoring time this harness does not have. The split between
    # Maintain and update_entry is what the performance work reads.
    calls = {}
    def timed(tool, args=None):
        t = time.time()
        text = session.call(tool, args)
        spent = time.time() - t
        c = calls.setdefault(tool, [0, 0.0, 0.0])
        c[0] += 1; c[1] += spent; c[2] = max(c[2], spent)
        return text
    while rounds < max_rounds:
        text = timed("aoci_maintain")
        m = json.loads(text)
        rounds += 1
        sizes.append(len(text.encode("utf-8")))
        status = m.get("status")
        if status == "applied" and m.get("aligned") is True:
            rounds -= 1; sizes.pop()
            break
        if status != "repair_required":
            stopped = {"round": rounds, "status": status, "result": m.get("result"), "next_action": m.get("next_action"),
                       "findings": [f.get("code") for f in ((m.get("governance") or {}).get("findings") or [])][:8]}
            break
        plan = m.get("code_plan") or {}
        # Under parallel batches the round also carries the siblings issued
        # ahead; one agent plays them all, each delivered whole by batch_id.
        batches = [m]
        for sibling in plan.get("siblings") or []:
            if sibling.get("state") == "applied":
                continue
            text = timed("aoci_maintain", {"batch_id": sibling.get("batch_id")})
            sizes.append(len(text.encode("utf-8")))
            deliveries += 1
            batches.append(json.loads(text))
        for batch in batches:
            cands = batch.get("candidates") or []
            batch_plan = batch.get("code_plan") or {}
            included.append(batch_plan.get("included"))
            existing += sum(1 for c in cands if c.get("existing_entry"))
            entries = []
            for c in cands:
                rel = c["path"]
                with open(os.path.join(work, rel), "rb") as fh:
                    data = fh.read()
                entries.append({"path": rel, "source_sha256": c["source_sha256"], "candidate_id": c["candidate_id"],
                                "new_entry": template_entry(rel, data)})
            r = json.loads(timed("aoci_update_entry", {"code_batch_id": batch_plan.get("batch_id"), "entries": entries}))
            if r.get("status") != "applied":
                stopped = {"round": rounds, "update_status": r.get("status"), "findings": (r.get("findings") or [])[:5], "message": str(r)[:300]}
                break
            written += r.get("applied", 0)
        if stopped:
            break
        if rounds % 10 == 0:
            print(f"{phase} round {rounds}: applied {written}, remaining {plan.get('remaining')}, max bytes {max(sizes)}", flush=True)
    return {"rounds": rounds, "entries_written": written, "stopped": stopped, "parallel": PARALLEL, "sibling_deliveries": deliveries,
            "maintain_bytes_max": max(sizes) if sizes else 0, "maintain_bytes_avg": (sum(sizes) // len(sizes)) if sizes else 0,
            "included_min": min(i for i in included if i) if included else 0, "included_max": max(i for i in included if i) if included else 0,
            "existing_entries_seen": existing, "over_budget_rounds": sum(1 for s in sizes if s > BUDGET),
            "call_seconds": {tool: {"count": c[0], "total": round(c[1], 2), "max": round(c[2], 2), "avg": round(c[1] / c[0], 3)}
                             for tool, c in sorted(calls.items())}}

# phase 1: first build
t1 = time.time()
with U.Session(BIN, work) as s:
    report["first_build"] = author_rounds(s, "build")
report["first_build"]["seconds"] = round(time.time() - t1, 1)
# The terminal proof every closure pays after the last Apply, timed per command.
report["first_build"]["proof_seconds"] = {"verify": timed_cli("verify", "--json"), "check": timed_cli("check", "--json"),
                                          "guide": timed_cli("index", "agent", "guide", "--agent", "claude", "--json")}
v = U.verify_facts(BIN, work); report["first_build"]["verify_aligned"] = bool(v and v.get("aligned")); report["first_build"]["code_objects"] = v and v.get("code_object_count")
print("first_build:", json.dumps(report["first_build"]), flush=True)

# phase 2: update wave over the first WAVE authored files (sorted path order == batch order)
authored = []
with open(os.path.join(work, "aoci.code.txt"), encoding="utf-8") as fh:
    section = ""
    for line in fh:
        line = line.rstrip("\n")
        if line.startswith("===") and line.endswith("==="):
            section = line.strip("=")
        elif "[" in line and "]: F:" in line:
            authored.append((section, line.split("[", 1)[0]))
root_norm = work.replace("\\", "/")
NEUTRAL_ROOT = "/<repository>"
wave_paths = []
for section, base in authored:
    # A section root is a historical coordinate: an index written since the
    # neutral root records /<repository>, an older one the authoring machine's
    # path. The wave silently touched nothing on the first neutral-root
    # binaries because only the machine path was stripped.
    if section.startswith(root_norm):
        rel_dir = section[len(root_norm):].strip("/")
    elif section.startswith(NEUTRAL_ROOT):
        rel_dir = section[len(NEUTRAL_ROOT):].strip("/")
    else:
        rel_dir = section.strip("/")
    rel = f"{rel_dir}/{base}" if rel_dir else base
    if os.path.isfile(os.path.join(work, rel)) and not rel.startswith("."):
        wave_paths.append(rel)
if authored and not wave_paths:
    sys.exit(f"no authored Entry maps onto a file under {work}: section roots {sorted({s for s, _ in authored})[:3]}")
wave_paths = sorted(set(wave_paths))[:WAVE]
for rel in wave_paths:
    with open(os.path.join(work, rel), "a", encoding="utf-8") as fh:
        fh.write("\n// harness update wave\n")
t2 = time.time()
with U.Session(BIN, work) as s:
    report["update_wave"] = author_rounds(s, "wave")
report["update_wave"].update({"touched": len(wave_paths), "seconds": round(time.time() - t2, 1)})
v = U.verify_facts(BIN, work); report["update_wave"]["verify_aligned"] = bool(v and v.get("aligned"))
print("update_wave:", json.dumps(report["update_wave"]), flush=True)

# phase 3: one cognition_optimization batch beside the held sources (#90)
opt = {"skipped": True}
if wave_paths:
    target = "code:" + wave_paths[0]
    with U.Session(BIN, work) as s:
        m = json.loads(s.call("aoci_maintain", {"intent": "cognition_optimization", "object_refs": [target]}))
        cands = m.get("candidates") or []
        if cands and m.get("code_plan"):
            entries = [{"path": c["path"], "source_sha256": c["source_sha256"], "candidate_id": c["candidate_id"], "new_entry": c["existing_entry"]} for c in cands]
            r = json.loads(s.call("aoci_update_entry", {"code_batch_id": m["code_plan"]["batch_id"], "entries": entries}))
            opt = {"skipped": False, "status": r.get("status"), "applied": r.get("applied"), "state": (r.get("optimization") or {}).get("state"),
                   "findings": (r.get("findings") or [])[:3], "next_action": str(r.get("next_action"))[:80]}
        else:
            opt = {"skipped": False, "status": m.get("status"), "note": "no candidates issued", "message": str(m)[:200]}
v = U.verify_facts(BIN, work); opt["verify_aligned_after"] = bool(v and v.get("aligned"))
report["optimization"] = opt
print("optimization:", json.dumps(opt), flush=True)

# phase 4: PreToolUse hook on an indexed file (#85)
hook = {"skipped": True}
if wave_paths:
    payload = json.dumps({"tool_name": "Edit", "tool_input": {"file_path": os.path.join(work, wave_paths[0])}})
    proc = subprocess.run([BIN, "--repo", work, "hook", "pretool", "--agent", "claude", "--stdin-json"], input=payload, capture_output=True, text=True, encoding="utf-8", errors="replace", timeout=120)
    hook = {"skipped": False, "exit": proc.returncode, "stdout_bytes": len(proc.stdout), "has_entry": "[" in proc.stdout and "F:" in proc.stdout}
report["hook"] = hook
print("hook:", json.dumps(hook), flush=True)

p = U.run(BIN, work, "verify", "--json")
try:
    doc = json.loads(p.stdout); gov = doc.get("governance") or {}
    drift = gov.get("code_drift") or {}
    report["drift"] = {k: len(drift.get(k) or []) for k in ("missing", "orphan", "stale", "unbaselined", "skipped", "curation_excluded")}
    report["findings_codes"] = sorted({f.get("code") for f in gov.get("findings") or []})
except Exception as e:
    report["drift_error"] = str(e)
report["total_seconds"] = round(time.time() - t0, 1)
report_path = os.path.join(work_root, f"{name}-{LABEL}.json")
with open(report_path, "w", encoding="utf-8") as fh:
    json.dump(report, fh, indent=1)
print("report:", report_path, flush=True)
print(json.dumps({k: report[k] for k in ("scan", "first_build", "update_wave", "optimization", "hook", "drift", "total_seconds") if k in report}, indent=1))
