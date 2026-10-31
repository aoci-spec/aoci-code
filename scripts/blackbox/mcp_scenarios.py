#!/usr/bin/env python3
"""AOCI MCP scenario suite — black-box end-to-end tests over the public MCP + CLI
surfaces only. Write scenarios run against disposable fixture repositories; the
real repository is used read-only (delivery fault injection exploits its 7-chunk
chain). Safety properties, not implementation choices, are asserted; where the
contract permits multiple outcomes, the scenario records a CHARACTERIZATION.

Groups:
  A  delivery fault matrix   (real repo, read-only + fixture for config-change)
  B  write lifecycle         (fixture: missing/stale/repair/binding/orphan)
  C  crash recovery          (fixture: kill server mid-operation, prove no wedge)
  D  concurrency             (fixture: racing writers, snapshot-change mid-chain)
  E  authoring batch size    (fixture: team batch config, bounded transport)
  F  cognition-layer visibility (fixture: git-hidden Volumes, line-ending rewrite)
  H  pre-write hook          (fixture: aoci hook pretool under the Claude stdin-JSON and
                              Codex flag shapes; fail-open, Entry injection, strict stale block)
  N  next commands           (fixture: returned commands run as returned; attested cognition
                              stays uncertain, not invalid, under tree drift)
  O  optimization            (fixture: direct updates survive a completed optimization)
  P  path names              (fixture: special directory and file names, unspellable directory)
  T  human confirmation      (fixture: real pty, prompt must precede the read)
  U  status page             (fixture: the local page never disturbs a live governance cycle)
  W  host window             (every non-Overview response fits an ordinary host)

The run also checks that the scenario count published in the public READMEs and
in scripts/blackbox/README.md matches this run, so those numbers cannot drift.

Usage:  python3 scripts/blackbox/mcp_scenarios.py                 # every group, published order
        python3 scripts/blackbox/mcp_scenarios.py --group H --group O   # only these letters
        python3 scripts/blackbox/mcp_scenarios.py --list          # letters and what each runs
Env:    AOCI_REPO / AOCI_BIN 覆盖仓库与二进制路径（默认取本脚本所在仓库）;
        AOCI_SCENARIO_WORK 指定夹具目录并保留（默认系统临时目录、跑完清理）;
        AOCI_SCENARIO_KEEP=1 保留默认临时夹具以便排查。
Requires: a built binary; group A additionally needs the host repository to hold
an established multi-chunk overview (chunk_tokens 8000). Fixtures set their own
git identity; the host repository is never written.
"""
import argparse, base64, hashlib, json, os, random, re, select, shutil, subprocess, sys, tempfile, time
import urllib.parse, urllib.request

from stdio_deadline import rpc_deadline
from stdio_capture import BoundedStderr, stderr_failure

# The suites print fixture output that may carry Chinese text; on a Windows
# runner the console codec is cp1252, so print the report as UTF-8 regardless.
for _stream in (sys.stdout, sys.stderr):
    if hasattr(_stream, "reconfigure"):
        try:
            _stream.reconfigure(encoding="utf-8", errors="replace")
        except (ValueError, OSError):
            pass


_REPO_DEFAULT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
REAL = os.environ.get("AOCI_REPO", _REPO_DEFAULT)
BIN  = os.environ.get("AOCI_BIN", os.path.join(REAL, "build", "aoci"))
WORK = os.environ.get("AOCI_SCENARIO_WORK") or tempfile.mkdtemp(prefix="aoci-scenarios-")
_KEEP_WORK = bool(os.environ.get("AOCI_SCENARIO_KEEP")) or bool(os.environ.get("AOCI_SCENARIO_WORK"))
MARK = "<<<AOCI_OVERVIEW_CHUNK_BODY/v1>>>"
random.seed(20260814)

RESULTS = []  # (group, name, status, detail)  status: PASS/FAIL/CHAR
def record(group, name, status, detail=""):
    RESULTS.append((group, name, status, detail))
    print(f"{status:4} [{group}] {name}" + (f" | {detail[:160]}" if detail else ""))

# ---------------------------------------------------------------- host window
# Every tool result any host displays inline must fit that host's tool-result
# window. The gate is 48 KiB because the flagship host's window was measured,
# not assumed: current Claude Code spills any tool response past ~50 KB to a
# file and hands the model a 2 KB preview (bisected live on 2026-09-01/03:
# 49,429-byte responses enter the model context, 51,916-byte responses spill).
# The previous 64 KiB assumption certified responses that die on that host —
# a 52-table repository answered one Maintain with 53 KB, "passed" here, and
# spilled in production, forcing the model to parse the file with a script.
# The ledger records the UTF-8 size of every tools/call result the suites
# receive; the gate at the end asserts no non-Overview response crossed
# HOST_WINDOW_BYTES. Overview is chunked by its own configured budget and is
# reported but not gated here.
HOST_WINDOW_BYTES = 48 * 1024
RESPONSE_SIZES = []  # (tool, utf8 bytes, gated)
# Repositories whose team configuration deliberately raised the Code batch
# above the machine default (the wire-ceiling scenarios). A team that asks for
# 200 candidates a call has opted out of the default window; their responses
# are still measured and reported, just not gated.
TEAM_RAISED_BATCH_REPOS = set()


def mark_team_raised_batch(repo):
    TEAM_RAISED_BATCH_REPOS.add(os.path.abspath(repo))


def record_response_size(tool, resp, repo=None):
    try:
        text = "".join(c.get("text", "") for c in (resp.get("result") or {}).get("content") or []
                       if isinstance(c, dict))
    except Exception:
        text = ""
    gated = repo is None or os.path.abspath(repo) not in TEAM_RAISED_BATCH_REPOS
    RESPONSE_SIZES.append((tool, len(text.encode("utf-8")), gated))


def host_window_summary(limit=HOST_WINDOW_BYTES):
    """Return (ok, detail): the largest response per tool under default team
    configuration and whether every non-Overview tool stayed under the host
    window; ceiling-configured repositories are reported separately."""
    peak, raised = {}, {}
    for tool, size, gated in RESPONSE_SIZES:
        target = peak if gated else raised
        target[tool] = max(target.get(tool, 0), size)
    gated = {t: b for t, b in peak.items() if t != "aoci_overview"}
    worst = max(gated.values()) if gated else 0
    detail = "limit=%d calls=%d default-config peak: %s" % (limit, len(RESPONSE_SIZES),
             " ".join(f"{t.replace('aoci_', '')}={b}" for t, b in sorted(peak.items())))
    if raised:
        detail += " | team-raised-batch peak (not gated): " + \
            " ".join(f"{t.replace('aoci_', '')}={b}" for t, b in sorted(raised.items()))
    return worst <= limit, detail


# ---------------------------------------------------------------- MCP client
class Session:
    def __init__(self, repo, capabilities=None, on_request=None):
        """`capabilities` is what this client declares at initialize; `on_request`
        answers a request the server sends while a call is in flight (the
        elicitation/create of a host dialog), returning its result or None for
        method-not-found."""
        self.repo = repo
        self.on_request = on_request
        self.p = subprocess.Popen([BIN, "--repo", repo, "mcp"],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            text=True, encoding="utf-8", bufsize=1)
        self.stderr_capture = BoundedStderr(self.p.stderr)
        self.next_id = 1
        self.rpc("initialize", {"protocolVersion": "2025-06-18", "capabilities": capabilities or {},
                                "clientInfo": {"name": "aoci-scenarios", "version": "1.0"}})
        self.notify("notifications/initialized")
    def send_raw(self, line):
        self.p.stdin.write(line + "\n"); self.p.stdin.flush()
    def rpc(self, method, params=None, timeout=120):
        with rpc_deadline(self.p, method, timeout, getattr(self, "stderr_capture", None)):
            rid = self.next_id; self.next_id += 1
            msg = {"jsonrpc": "2.0", "id": rid, "method": method}
            if params is not None: msg["params"] = params
            self.send_raw(json.dumps(msg))
            while True:
                line = self.p.stdout.readline()
                if not line: raise RuntimeError(stderr_failure(getattr(self, "stderr_capture", None), "server closed stdout"))
                line = line.rstrip("\n")
                if not line: continue
                try: obj = json.loads(line)
                except Exception: continue
                if "method" in obj and "id" in obj:
                    self.answer(obj)
                    continue
                if obj.get("id") == rid: return obj

    def answer(self, request):
        result = self.on_request(request.get("method"), request.get("params") or {}) if self.on_request else None
        if result is None:
            reply = {"jsonrpc": "2.0", "id": request["id"], "error": {"code": -32601, "message": "method not found"}}
        else:
            reply = {"jsonrpc": "2.0", "id": request["id"], "result": result}
        self.send_raw(json.dumps(reply))

    def notify(self, method, params=None):
        msg = {"jsonrpc": "2.0", "method": method}
        if params is not None: msg["params"] = params
        self.send_raw(json.dumps(msg))
    def call(self, tool, args=None, timeout=180):
        resp = self.rpc("tools/call", {"name": tool, "arguments": args or {}}, timeout)
        record_response_size(tool, resp, self.repo)
        return resp
    def send_call_noread(self, tool, args):
        """Fire a tools/call without reading the response (for crash injection)."""
        rid = self.next_id; self.next_id += 1
        self.send_raw(json.dumps({"jsonrpc": "2.0", "id": rid, "method": "tools/call",
                                  "params": {"name": tool, "arguments": args}}))
        return rid
    def kill(self):
        try: self.p.kill(); self.p.wait(timeout=5)
        except Exception: pass
        finally: self.stderr_capture.finish()
    def close(self):
        try: self.p.stdin.close(); self.p.wait(timeout=10)
        except Exception: self.kill()
        finally: self.stderr_capture.finish()

def text_of(resp):
    if resp.get("error") is not None:
        return json.dumps(resp["error"]), True
    r = resp.get("result") or {}
    txt = "\n".join(c.get("text", "") for c in (r.get("content") or []) if c.get("type") == "text")
    return txt, bool(r.get("isError"))

def jload(text):
    try: return json.loads(text[text.index("{"):text.rindex("}") + 1])
    except Exception: return {}

def parse_kv(text):
    out = {}
    for ln in text.splitlines():
        mm = re.match(r"^([a-z_0-9.]+):\s*(.*)$", ln.strip())
        if mm:
            k, v = mm.group(1), mm.group(2)
            if v in ("true", "false"): out[k] = (v == "true")
            elif re.fullmatch(r"-?\d+", v): out[k] = int(v)
            else: out[k] = v
    return out

def meta_and_body(text):
    if MARK in text:
        head, body = text.split(MARK + "\n", 1)
        return jload(head), body
    kv = parse_kv(text)
    if "request_mode" in kv or "completed" in kv or "delivery_mode" in kv:
        return kv, None
    return jload(text), None

def is_rejection(text, err):
    return err or '"error"' in text or "error" in text.lower() or "invalid" in text.lower() or "stopped" in text.lower()

# ---------------------------------------------------------------- CLI helper
def cli(repo, *args, expect_ok=True):
    r = subprocess.run([BIN, "--repo", repo, *args, "--json"],
                       capture_output=True, text=True, encoding="utf-8", errors="replace", timeout=120)
    body = {}
    try: body = json.loads(r.stdout)
    except Exception:
        try: body = jload(r.stdout)
        except Exception: pass
    return r.returncode, body, r.stdout, r.stderr

def sh(cwd, *args):
    r = subprocess.run(list(args), cwd=cwd, capture_output=True, text=True, encoding="utf-8", errors="replace", timeout=120)
    if r.returncode != 0:
        raise RuntimeError(f"{args}: {r.stderr[:300]}")
    return r.stdout

# ---------------------------------------------------------------- fixture
# init generates governed sources of its own: the AGENTS.md contract block and
# the .gitattributes that keeps a Windows checkout from rewriting every Volume.
# Both are ordinary authoring targets, so fixture expectations count them.
INIT_GENERATED_TARGETS = 2

def make_fixture(name, nfiles, batch_entries=None):
    """Fresh Volumes fixture with nfiles source files. `batch_entries` pins the
    team Code batch size (a scenario that wants one call to carry the whole
    fixture sets the 200 wire ceiling); None keeps the machine default."""
    d = os.path.join(WORK, name)
    shutil.rmtree(d, ignore_errors=True)
    os.makedirs(os.path.join(d, "pkg"))
    for i in range(1, nfiles + 1):
        with open(os.path.join(d, "pkg", f"f{i:03}.go"), "w") as f:
            f.write(f"package pkg\n\n// fixture unit {i}: independent constant provider\nfunc F{i:03}() int {{ return {i} }}\n")
    sh(d, "git", "init", "-q")
    sh(d, "git", "config", "user.email", "fixture@test.invalid")
    sh(d, "git", "config", "user.name", "fixture")
    sh(d, "git", "add", "-A")
    sh(d, "git", "commit", "-q", "-m", "fixture")
    rc, _, out, errs = cli(d, "init", "--locale", "en-US")
    if rc != 0: raise RuntimeError(f"init failed: {out[:200]} {errs[:200]}")
    if batch_entries is not None:
        rc, _, out, errs = cli(d, "config", "set", "code_cognition_batch_entries", str(batch_entries))
        if rc != 0: raise RuntimeError(f"config set code_cognition_batch_entries failed: {out[:200]} {errs[:200]}")
        if batch_entries > 50:
            # The transport budget is the operative bound; a fixture that wants
            # the wire ceiling per batch raises it to the maximum as well.
            rc, _, out, errs = cli(d, "config", "set", "maintain_transport_budget_bytes", str(192 * 1024))
            if rc != 0: raise RuntimeError(f"config set maintain_transport_budget_bytes failed: {out[:200]} {errs[:200]}")
        mark_team_raised_batch(d)
    rc, _, out, errs = cli(d, "scan")
    if rc != 0: raise RuntimeError(f"scan failed: {out[:200]} {errs[:200]}")
    return d

def maintain(s):
    t, err = text_of(s.call("aoci_maintain"))
    return jload(t), t, err

def entry_line(path, i=None, s_field="-"):
    base = os.path.basename(path)
    if base == "AGENTS.md":
        return f"{base}[CG5T]: F:Defines fixture repository integration guidance | R:- | A:- | S:{s_field}"
    n = i if i is not None else int(re.sub(r"\D", "", base) or 0)
    return f"{base}[CG5T]: F:Provides fixture constant unit {n} | R:- | A:- | S:{s_field}"

def submit_batch(s, m, mutate=None):
    """Build the complete update_entry request from a maintain result; `mutate`
    may edit the entries list (for fault injection) before sending."""
    cands = m.get("candidates") or []
    batch = (m.get("code_plan") or {}).get("batch_id") or (cands[0].get("batch_id") if cands else "")
    entries = [{"path": c["path"], "source_sha256": c["source_sha256"],
                "candidate_id": c["candidate_id"], "new_entry": entry_line(c["path"])}
               for c in cands]
    if mutate: mutate(entries)
    t, err = text_of(s.call("aoci_update_entry", {"code_batch_id": batch, "entries": entries}, timeout=300))
    return jload(t), t, err

def land_write(fx, fname, ftext):
    """Mutate a fixture file and apply its entry with the given F text."""
    with open(os.path.join(fx, "pkg", fname), "a") as f:
        f.write(f"\n// land_write mutation for {ftext}\n")
    sw = Session(fx)
    m, t, err = maintain(sw)
    c = (m.get("candidates") or [{}])[0]
    tw, _ = text_of(sw.call("aoci_update_entry", {"code_batch_id": (m.get("code_plan") or {}).get("batch_id"),
        "entries": [{"path": c.get("path"), "source_sha256": c.get("source_sha256"),
                     "candidate_id": c.get("candidate_id"),
                     "new_entry": f"{os.path.basename(c.get('path',''))}[CG5T]: F:{ftext} | R:- | A:- | S:-"}]}))
    sw.close()
    return jload(tw).get("status")

def fixture_aligned(repo):
    rc, v, _, _ = cli(repo, "verify")
    aligned = bool((v.get("governance") or {}).get("governance_aligned") or v.get("governance_aligned"))
    return rc == 0 and aligned, v

# ================================================================ GROUP A
def group_a():
    """Delivery fault matrix on the host repository's chunk chain: replayed,
    tampered, and cross-process cursors (read-only)."""
    g = "A"
    # -- A1 replay + A2 tamper + A3 sha tamper + A4 cross-session: real repo chain
    s = Session(REAL)
    t, err = text_of(s.call("aoci_overview"))
    m1, _ = meta_and_body(t)
    cur1 = m1.get("next_cursor")
    if not cur1:
        record(g, "chain-setup", "FAIL", "real repo overview did not chunk"); s.close(); return
    t, err = text_of(s.call("aoci_overview", {"cursor": cur1}))
    m2, _ = meta_and_body(t)
    cur2 = m2.get("next_cursor")

    # A1: replay an already-consumed cursor (cur1 again after advancing to cur2).
    # Documented: an exact replay of a genuine cursor idempotently re-serves the
    # identical Chunk bytes (spec/public/aoci-overview-delivery-v1.txt, cursor §).
    t, err = text_of(s.call("aoci_overview", {"cursor": cur1}))
    mrep, brep = meta_and_body(t)
    if brep is not None and hashlib.sha256(brep.encode()).hexdigest() == m2.get("chunk_sha256"):
        record(g, "A1.replayed-cursor", "PASS", "idempotent re-serve of identical chunk bytes")
    else:
        record(g, "A1.replayed-cursor", "FAIL", t[:200])

    # A2: tampered ordinal inside the cursor
    parts = cur2.split(":")
    # V2 wraps the legacy payload with a session id and MAC. Mutating the
    # payload must fail without repairing/re-signing its authentication.
    v2 = parts[0] == "v2"
    ordinal_offset, sha_offset = (4, 5) if v2 else (2, 3)
    ordinal_parts = parts.copy()
    ordinal_parts[ordinal_offset] = str(int(parts[ordinal_offset]) + 37)
    bad_ord = ":".join(ordinal_parts)
    t, err = text_of(s.call("aoci_overview", {"cursor": bad_ord}))
    record(g, "A2.tampered-ordinal", "PASS" if is_rejection(t, err) else "FAIL", t[:150])

    # A3: tampered previous-chunk sha
    sha_parts = parts.copy()
    sha_parts[sha_offset] = "0" * 64
    bad_sha = ":".join(sha_parts)
    t, err = text_of(s.call("aoci_overview", {"cursor": bad_sha}))
    record(g, "A3.tampered-prev-sha", "PASS" if is_rejection(t, err) else "FAIL", t[:150])

    # A4: v2 state is intentionally ephemeral and must reject after restart.
    # Legacy v1 remains reconstructable with live governance validation.
    s2 = Session(REAL)
    t, err = text_of(s2.call("aoci_overview", {"cursor": cur2}))
    mx, bx = meta_and_body(t)
    if v2:
        rejected = bx is None and is_rejection(t, err) and "overview_session_unavailable_restart_required" in t
        # Keep the old wire compatibility promise under the same safety scenario.
        legacy_cursor = ":".join(parts[2:6])
        legacy_text, legacy_err = text_of(s2.call("aoci_overview", {"cursor": legacy_cursor}))
        legacy_meta, legacy_body = meta_and_body(legacy_text)
        original_text, original_err = text_of(s.call("aoci_overview", {"cursor": cur2}))
        original_meta, original_body = meta_and_body(original_text)
        compatible = (not legacy_err and not original_err and legacy_body is not None
                      and legacy_body == original_body
                      and legacy_meta.get("chunk_sha256") == original_meta.get("chunk_sha256"))
        ok = rejected and compatible
        record(g, "A4.cross-session-cursor", "PASS" if ok else "FAIL",
               "v2 rejected safely after restart; v1 reconstructs identical chunk bytes"
               if ok else f"v2_rejected={rejected} v1_compatible={compatible}: {t[:100]}")
    elif bx is not None:
        ok = hashlib.sha256(bx.encode()).hexdigest() == mx.get("chunk_sha256")
        record(g, "A4.cross-session-cursor", "PASS" if ok else "FAIL",
               "deterministic re-derivation, chunk sha self-consistent" if ok else "served chunk failed its own sha")
    else:
        record(g, "A4.cross-session-cursor", "FAIL", t[:150])
    s.close(); s2.close()

    # A6: the #114 loop breaker. A compaction reload that is itself compacted
    # before it is proven makes the next reload a capacity stop (no body, the
    # generation consumed) instead of another chain; check_only shows the
    # plan and the verdict first; an explicit request with nothing pending
    # still delivers; the verdict dies with the server process.
    COMPACTION = ["context_compaction"]
    s = Session(REAL)
    t, err = text_of(s.call("aoci_overview", {"refresh_reasons": COMPACTION, "refresh_event_id": "a6-compact-1"}))
    first, _ = meta_and_body(t)
    stop_text, stop_err = text_of(s.call("aoci_overview", {"refresh_reasons": COMPACTION, "refresh_event_id": "a6-compact-2"}))
    stop = parse_kv(stop_text)
    check_text, _ = text_of(s.call("aoci_overview", {"check_only": True}))
    check = jload(check_text)
    again = parse_kv(text_of(s.call("aoci_overview", {"refresh_reasons": COMPACTION, "refresh_event_id": "a6-compact-3"}))[0])
    explicit, _ = meta_and_body(text_of(s.call("aoci_overview"))[0])
    s.close()
    s2 = Session(REAL)
    fresh, _ = meta_and_body(text_of(s2.call("aoci_overview", {"refresh_reasons": COMPACTION, "refresh_event_id": "a6-compact-4"}))[0])
    s2.close()
    capacity = check.get("host_capacity") or {}
    plan = check.get("delivery_plan") or {}
    checks = {
        "first_reload_chunked": first.get("continuation_required") is True and first.get("chunk_index") == 1,
        "stop_mode": stop.get("delivery_mode") == "capacity_stop" and stop.get("fallback_reason") == "host_context_capacity_exceeded",
        "stop_has_no_body": MARK not in stop_text and "<<<AOCI_OVERVIEW_BODY_BEGIN" not in stop_text and not stop_err,
        "stop_consumes": stop.get("completed") is False and stop.get("refresh_status") == "refresh_not_required"
                         and stop.get("delivery_guidance") == "full_system_claim_disabled_source_bound_task_continuation_allowed",
        "stop_names_interrupted_chunk": stop.get("interrupted_chunks_served") == 1 and str(stop.get("interrupted_delivery_chain_id", "")).startswith("chain-"),
        "check_only_plan_and_verdict": capacity.get("verdict") is True and capacity.get("compaction_reload") == "capacity_stop"
                                        and plan.get("chunk_count") == first.get("chunk_count"),
        "later_reload_stops": again.get("delivery_mode") == "capacity_stop" and again.get("capacity_interruptions") == 1,
        "explicit_request_delivers": explicit.get("chunk_index") == 1,
        "new_process_has_no_verdict": fresh.get("chunk_index") == 1,
    }
    failed = [name for name, ok in checks.items() if not ok]
    record(g, "A6.compaction-reload-capacity-stop", "PASS" if not failed else "FAIL",
           "interrupted reload stops; plan and verdict visible; explicit and new-process reloads deliver"
           if not failed else f"failed={failed} stop={stop_text[:160]}")

# ================================================================ GROUP B (+A5)
def group_b():
    """Write lifecycle on a 160-file fixture: establish, stale refresh, repair
    round trip, wrong binding, orphan removal (also A5: chunk_tokens change mid-chain)."""
    g = "B"
    NF = 160
    # One call establishing the whole fixture is deliberately a wire-ceiling
    # scenario; the machine default batch is 20 and is exercised by group E.
    fx = make_fixture("fx-life", NF, batch_entries=200)

    # B1: full establishment through MCP: maintain -> complete batch -> aligned
    s = Session(fx)
    m, t, err = maintain(s)
    n = len(m.get("candidates") or [])
    if n != NF + INIT_GENERATED_TARGETS:
        record(g, "B1.maintain-missing", "FAIL", f"expected {NF+INIT_GENERATED_TARGETS} candidates, got {n}: {t[:150]}"); s.close(); return None
    record(g, "B1.maintain-missing", "PASS", f"{n} create candidates (incl init-generated targets)")
    r, t, err = submit_batch(s, m)
    ok = r.get("status") == "applied" and r.get("applied") == NF + INIT_GENERATED_TARGETS and r.get("remaining") == 0
    record(g, "B1.apply-batch", "PASS" if ok else "FAIL", t[:200] if not ok else f"applied={NF+INIT_GENERATED_TARGETS}")
    al, v = fixture_aligned(fx)
    record(g, "B1.verify-aligned", "PASS" if al else "FAIL")

    # A5: chunk_tokens change mid-chain must stop the chain (needs the big fixture index)
    rc, _, out, _ = cli(fx, "config", "set", "overview_delivery.chunk_tokens", "4000")
    if rc != 0:
        record("A", "A5.config-key", "FAIL", out[:150])
    else:
        sc = Session(fx)
        t, err = text_of(sc.call("aoci_overview"))
        mm, bb = meta_and_body(t)
        if not mm.get("next_cursor"):
            record("A", "A5.chunk-tokens-change", "CHAR", f"fixture index fits one chunk at 4000 tokens (est={mm.get('estimated_tokens')}); scenario skipped")
        else:
            cur = mm["next_cursor"]
            cli(fx, "config", "set", "overview_delivery.chunk_tokens", "6000")
            t, err = text_of(sc.call("aoci_overview", {"cursor": cur}))
            record("A", "A5.chunk-tokens-change", "PASS" if is_rejection(t, err) else "FAIL", t[:150])
            cli(fx, "config", "set", "overview_delivery.chunk_tokens", "8000")
        sc.close()

    # B2: stale entry refresh with preserved semantics
    with open(os.path.join(fx, "pkg", "f001.go"), "a") as f:
        f.write("\nfunc F001b() int { return 1001 }\n")
    m, t, err = maintain(s)
    cands = m.get("candidates") or []
    ok = len(cands) == 1 and cands[0]["path"] == "pkg/f001.go" and cands[0].get("change") == "update"
    record(g, "B2.stale-detected", "PASS" if ok else "FAIL", t[:150] if not ok else "")
    if ok:
        r, t, err = submit_batch(s, m)
        record(g, "B2.stale-reapplied", "PASS" if r.get("status") == "applied" else "FAIL", t[:150])

    # B3: repair_required round trip (S over the C5 quota of 500 runes)
    with open(os.path.join(fx, "pkg", "f002.go"), "a") as f:
        f.write("\nfunc F002b() int { return 1002 }\n")
    m, t, err = maintain(s)
    long_s = "x" * 501
    r, t, err = submit_batch(s, m, mutate=lambda es: es.__setitem__(0, {**es[0], "new_entry": entry_line(es[0]["path"], s_field=long_s)}))
    findings = r.get("findings") or []
    ok = (r.get("status") == "repair_required" and r.get("formal_writes_started") is False
          and any(f.get("rule_code") == "fras_s_too_long" for f in findings))
    record(g, "B3.repair-required", "PASS" if ok else "FAIL", t[:200] if not ok else "fras_s_too_long, zero writes")
    r, t, err = submit_batch(s, m)  # resubmit clean same complete batch
    record(g, "B3.repair-resubmit", "PASS" if r.get("status") == "applied" else "FAIL", t[:150])

    # B4: wrong source binding inside an otherwise valid batch
    with open(os.path.join(fx, "pkg", "f003.go"), "a") as f:
        f.write("\nfunc F003b() int { return 1003 }\n")
    m, t, err = maintain(s)
    r, t, err = submit_batch(s, m, mutate=lambda es: es.__setitem__(0, {**es[0], "source_sha256": "0" * 64}))
    zero_write = r.get("formal_writes_started") in (False, None) and r.get("status") != "applied"
    # The finding must let the model recover from the response it has: rc16
    # took code_plan.candidates off the wire, and until this check the repair
    # action still sent the model there. expected is the issued binding, the
    # action names the candidates list and the finding's own expected value.
    issued = (m.get("candidates") or [{}])[0].get("source_sha256")
    mism = [f for f in (r.get("findings") or []) if f.get("rule_code") == "code_candidate_source_sha256_mismatch"]
    action = (mism[0].get("safe_repair_action") or "") if mism else ""
    named = bool(mism) and mism[0].get("expected") == issued and mism[0].get("actual") == "0" * 64 \
        and "candidates" in action and "expected" in action and "code_plan.candidates" not in action
    ok4 = zero_write and named
    record(g, "B4.wrong-source-sha", "PASS" if ok4 else "FAIL",
           (r.get("status") or t[:100]) + f" zero-write finding_names_issued_binding={named}" if ok4 else t[:200])
    r, t, err = submit_batch(s, m)  # correct bindings
    record(g, "B4.correct-resubmit", "PASS" if r.get("status") == "applied" else "FAIL", t[:150])

    # B5: orphan removal
    os.remove(os.path.join(fx, "pkg", "f004.go"))
    sh(fx, "git", "add", "-A"); sh(fx, "git", "commit", "-q", "-m", "drop f004")
    m, t, err = maintain(s)
    orphans = m.get("orphan_remove_candidates") or []
    ok = any("f004" in o for o in orphans)
    record(g, "B5.orphan-detected", "PASS" if ok else "FAIL", str(orphans)[:120])
    if ok:
        t, err = text_of(s.call("aoci_remove_entry", {"path": "code:pkg/f004.go"}))
        record(g, "B5.orphan-removed", "PASS" if not err and "Removed" in t else "FAIL", t[:150])
    al, v = fixture_aligned(fx)
    record(g, "B5.final-aligned", "PASS" if al else "FAIL")
    s.close()
    return fx

# ================================================================ GROUP C
def group_c():
    """Crash recovery: the server killed mid-chain and mid-Apply, six times, must
    never leave the fixture wedged for the next public-surface caller."""
    g = "C"
    fx = make_fixture("fx-crash", 12)
    s = Session(fx)
    m, t, err = maintain(s)
    r, t, err = submit_batch(s, m)
    if r.get("status") != "applied":
        record(g, "C0.setup", "FAIL", t[:150]); s.close(); return
    s.close()

    # C1: kill mid-overview-chain; fresh session must run a clean full chain
    cli(fx, "config", "set", "overview_delivery.chunk_tokens", "4000")
    s1 = Session(fx)
    t, _ = text_of(s1.call("aoci_overview"))
    s1.kill()
    s2 = Session(fx)
    t, err = text_of(s2.call("aoci_overview"))
    m2, b2 = meta_and_body(t)
    ok = not err and (m2.get("completed") or m2.get("next_cursor"))
    record(g, "C1.chain-survives-server-death", "PASS" if ok else "FAIL", t[:150])
    s2.close()

    # C2: crash-injection loop around update_entry; property: never wedged
    wedged = []
    for it in range(6):
        fpath = os.path.join(fx, "pkg", "f005.go")
        with open(fpath, "a") as f:
            f.write(f"\nfunc F005x{it}() int {{ return {9000+it} }}\n")
        sk = Session(fx)
        m, t, err = maintain(sk)
        cands = m.get("candidates") or []
        if not cands:
            wedged.append(f"it{it}: no candidate after mutation"); sk.close(); break
        batch = (m.get("code_plan") or {}).get("batch_id")
        entries = [{"path": c["path"], "source_sha256": c["source_sha256"],
                    "candidate_id": c["candidate_id"], "new_entry": entry_line(c["path"])} for c in cands]
        sk.send_call_noread("aoci_update_entry", {"code_batch_id": batch, "entries": entries})
        time.sleep(random.uniform(0.0, 0.12))
        sk.kill()
        # Recovery purely through public surfaces. A process killed while
        # holding .aoci/lock leaves a stale lock that is reclaimed only after
        # age > 60s AND the owner PID is dead (internal/fs/lock.go); until
        # then submits return stopped with a write_conflict finding after the
        # 10s acquire timeout. That window is self-healing by design, so the
        # loop must outlast it (~90s worst case) instead of judging early.
        recovered = False
        for attempt in range(10):
            rc, v, _, _ = cli(fx, "verify")
            sr = Session(fx)
            m2, t2, err2 = maintain(sr)
            st = m2.get("status")
            lock_window = False
            if st == "repair_required" and m2.get("candidates"):
                r2, t2b, _ = submit_batch(sr, m2)
                if r2.get("status") == "applied": recovered = True
                elif r2.get("status") == "stopped" and "write_conflict" in t2b: lock_window = True
            elif st in ("applied",) or m2.get("aligned") is True:
                recovered = True
            elif st == "stopped":
                # follow whatever the machine returns once more (documented recovery is internal)
                sr.close(); sr = Session(fx)
                m3, t3, _ = maintain(sr)
                if m3.get("aligned") is True: recovered = True
                elif m3.get("candidates"):
                    r3, t3b, _ = submit_batch(sr, m3)
                    recovered = r3.get("status") == "applied"
                    if not recovered and "write_conflict" in t3b: lock_window = True
            sr.close()
            al, _ = fixture_aligned(fx)
            if al and recovered:
                break
            if al:
                recovered = True; break
            time.sleep(8.0 if lock_window else 0.3)
        al, v = fixture_aligned(fx)
        if not (al and recovered):
            wedged.append(f"it{it}: aligned={al}")
            break
    record(g, "C2.kill-during-apply-x6", "PASS" if not wedged else "FAIL",
           "all iterations recovered to aligned via public surfaces" if not wedged else "; ".join(wedged))

# ================================================================ GROUP D
def group_d(bigfx=None):
    """Concurrency: racing writers over the same drift, and an index-byte change
    versus a Baseline-only change in the middle of a delivery chain."""
    g = "D"
    fx = make_fixture("fx-race", 12)
    s = Session(fx)
    m, t, err = maintain(s)
    r, t, err = submit_batch(s, m)
    if r.get("status") != "applied":
        record(g, "D0.setup", "FAIL", t[:150]); s.close(); return
    s.close()

    # D1: two sessions maintain the same drift, both submit
    with open(os.path.join(fx, "pkg", "f006.go"), "a") as f:
        f.write("\nfunc F006b() int { return 2006 }\n")
    sA, sB = Session(fx), Session(fx)
    mA, _, _ = maintain(sA)
    mB, _, _ = maintain(sB)
    rA, tA, _ = submit_batch(sA, mA)
    rB, tB, _ = submit_batch(sB, mB)
    a_ok = rA.get("status") == "applied"
    # contract-legal outcomes for the loser: idempotent duplicate (applied, Applied==0),
    # repair/stopped with zero writes — anything but silent corruption
    b_state = rB.get("status")
    b_legal = (b_state == "applied" and rB.get("applied") in (0, None)) or b_state in ("repair_required", "stopped")
    al, v = fixture_aligned(fx)
    ok = a_ok and b_legal and al
    record(g, "D1.racing-writers", "PASS" if ok else "FAIL",
           f"A={rA.get('status')}/{rA.get('applied')} B={b_state}/{rB.get('applied')} aligned={al}")
    sA.close(); sB.close()

    # D2: REAL index-byte change mid-chain must fail closed (overview_snapshot_changed)
    fx = bigfx or fx
    cli(fx, "config", "set", "overview_delivery.chunk_tokens", "4000")
    sC = Session(fx)
    t, _ = text_of(sC.call("aoci_overview"))
    mm, _ = meta_and_body(t)
    cur = mm.get("next_cursor")
    if not cur:
        record(g, "D2.index-change-mid-chain", "CHAR", "fixture fits one chunk; cannot open a chain")
    else:
        st = land_write(fx, "f011.go", "Provides revised fixture constant unit 11")
        t, err = text_of(sC.call("aoci_overview", {"cursor": cur}))
        mrej, brej = meta_and_body(t)
        ok = st == "applied" and brej is None and ("overview_snapshot_changed" in t or "bad_args" in t or err)
        record(g, "D2.index-change-mid-chain", "PASS" if ok else "FAIL", t[:150])
    sC.close()

    # D3: V2 binds Baseline bytes and must reject baseline-only drift.
    # V1 retains its old byte-compatible continuation with live governance.
    sC = Session(fx)
    t, _ = text_of(sC.call("aoci_overview"))
    mm, _ = meta_and_body(t)
    cur = mm.get("next_cursor")
    if not cur:
        record(g, "D3.baseline-only-mid-chain", "CHAR", "no chain")
    else:
        st = land_write(fx, "f012.go", "Provides fixture constant unit 12")  # unchanged text => index bytes stable
        t, err = text_of(sC.call("aoci_overview", {"cursor": cur}))
        mc, bc = meta_and_body(t)
        if cur.startswith("v2:"):
            stopped = bc is None and is_rejection(t, err) and "overview_snapshot_changed" in t
            legacy_cursor = ":".join(cur.split(":")[2:6])
            legacy_text, legacy_err = text_of(sC.call("aoci_overview", {"cursor": legacy_cursor}))
            legacy_meta, legacy_body = meta_and_body(legacy_text)
            compatible = (not legacy_err and legacy_body is not None
                          and legacy_meta.get("index_sha256") == mm.get("index_sha256"))
            ok = st == "applied" and stopped and compatible
            record(g, "D3.baseline-only-mid-chain", "PASS" if ok else "FAIL",
                   "v2 rejects Baseline drift; v1 preserves byte-compatible continuation"
                   if ok else f"v2_stopped={stopped} v1_compatible={compatible}: {t[:100]}")
        elif st == "applied" and not err and (mc.get("completed") or mc.get("chunk_index")):
            record(g, "D3.baseline-only-mid-chain", "PASS",
                   "v1 proceeds with unchanged Index bytes and live governance")
        else:
            record(g, "D3.baseline-only-mid-chain", "FAIL", t[:150])
    sC.close()

def group_e():
    """Large fresh repository, machine-default batch: the first Maintain must
    fit an ordinary Host tool-result window and stay actionable inline. This is
    the failure a real user hit — a ~1400-file repository answered its first
    Maintain with ~330 KB at 200 per batch, the Host spilled it to disk, and the
    model fell back to scripts that broke on encoding, quoting, and paths.
    Since v0.1.0-rc16 the batch is cut by a byte budget
    (maintain_transport_budget_bytes, default 24 KiB) under a count cap
    (code_cognition_batch_entries, default 50): a create batch carries more
    candidates than one of long existing Entries, the plan no longer repeats
    the candidates, and the response itself stays under the budget."""
    g = "E"
    NF = 260
    BUDGET = 24 * 1024
    fx = make_fixture("fx-large", NF)
    # Known line counts tighten L3's byte reservation once their fingerprints
    # are outside the racy window. Keep the Guide/first-Maintain comparison
    # from straddling that window without sleeping or disabling the cache.
    when = time.time() - 10
    for directory, dirs, files in os.walk(fx):
        dirs[:] = [d for d in dirs if d not in (".git", ".aoci")]
        for name in files:
            os.utime(os.path.join(directory, name), (when, when))
    s = Session(fx)
    m, t, err = maintain(s)
    plan = m.get("code_plan") or {}
    total = NF + INIT_GENERATED_TARGETS
    included = plan.get("included") or 0
    ok = (not err) and plan.get("max_entries") == 50 and 20 <= included <= 50 \
        and plan.get("remaining") == total - included and len(m.get("candidates") or []) == included \
        and plan.get("transport_budget_bytes") == BUDGET and plan.get("candidates") is None
    record(g, "E1.default-batch-is-budgeted", "PASS" if ok else "FAIL",
           f"max_entries={plan.get('max_entries')} included={included} remaining={plan.get('remaining')} "
           f"cands={len(m.get('candidates') or [])} budget={plan.get('transport_budget_bytes')} plan_cands={plan.get('candidates') is not None}")
    # The Guide shows the model the batch Maintain will issue, not the count cap.
    rc_g, guide, _, _ = cli(fx, "index", "agent", "guide", "--agent", "codex", expect_ok=False)
    gbatch = (guide.get("authoring_batch") or guide.get("batch") or {}) if isinstance(guide, dict) else {}
    ok = gbatch.get("included") == included and gbatch.get("remaining") == plan.get("remaining") \
        and gbatch.get("max_entries") == 50 and gbatch.get("transport_budget_bytes") == BUDGET
    record(g, "E1b.guide-projects-the-budgeted-batch", "PASS" if ok else "FAIL",
           f"guide.included={gbatch.get('included')} guide.remaining={gbatch.get('remaining')} maintain.included={included} budget={gbatch.get('transport_budget_bytes')}")
    size = len(t.encode("utf-8"))
    gov = m.get("governance") or {}
    trunc = gov.get("list_truncation") or {}
    totals = trunc.get("totals") or {}
    findings = gov.get("findings") or []
    missing = (gov.get("code_drift") or {}).get("missing") or []
    ok = size <= BUDGET and len(findings) == 5 and len(missing) == 5 \
        and totals.get("findings") == total and totals.get("code_drift.missing") == total and trunc.get("limit") == 5
    record(g, "E2.first-maintain-fits-transport-budget", "PASS" if ok else "FAIL",
           f"bytes={size} findings={len(findings)}/{totals.get('findings')} missing={len(missing)}/{totals.get('code_drift.missing')} limit={trunc.get('limit')}")
    instr = " ".join(m.get("instructions") or [])
    ok = "aoci_update_entry" in instr and "governance.budget" in instr and "aoci_maintain again" in instr and "parallel" in instr
    record(g, "E3.instructions-say-inline-parallel-remaintain", "PASS" if ok else "FAIL", instr[-200:])
    # candidates and plan stay complete and actionable: author the batch inline,
    # with S fields as long as a real C7 Entry carries, so the update batch
    # below meets long existing Entries.
    long_s = ("Keep the deterministic externally visible behavior of this fixture stable across refactors; " * 4).strip()
    def long_entries(entries):
        for e in entries:
            base = os.path.basename(e["path"])
            e["new_entry"] = f"{base}[CG8S]: F:Provides the {base} fixture behavior for the large repository | R:- | A:- | S:{long_s}"
    r, tw, err = submit_batch(s, m, mutate=long_entries)
    ok = r.get("status") == "applied" and r.get("applied") == included and r.get("remaining") == total - included \
        and "maintain" in (r.get("next_action") or "").lower()
    record(g, "E4.batch-applies-and-continues", "PASS" if ok else "FAIL",
           f"status={r.get('status')} applied={r.get('applied')} remaining={r.get('remaining')} next={str(r.get('next_action'))[:80]}")
    # the next Maintain issues the next budgeted batch against the new preimage, same shape
    m2, t2, _ = maintain(s)
    p2 = m2.get("code_plan") or {}
    included2 = p2.get("included") or 0
    ok = 20 <= included2 <= 50 and p2.get("remaining") == total - included - included2 and len(t2.encode("utf-8")) <= BUDGET
    record(g, "E4b.next-maintain-pages-next-batch", "PASS" if ok else "FAIL",
           f"included={included2} remaining={p2.get('remaining')} bytes={len(t2.encode('utf-8'))}")
    s.close()
    # Verify keeps the complete enumeration: the transport bound is Maintain-only.
    rc, v, _, _ = cli(fx, "verify", expect_ok=False)
    vmissing = ((v.get("governance") or {}).get("code_drift") or {}).get("missing") or []
    vtrunc = (v.get("governance") or {}).get("list_truncation")
    ok = len(vmissing) == total - included and vtrunc is None
    record(g, "E5.verify-lists-every-item", "PASS" if ok else "FAIL", f"verify.missing={len(vmissing)} truncation={vtrunc}")
    # An update batch carries every existing Entry once, so long Entries mean
    # fewer candidates per round, never a response past the budget: this is the
    # shape that overflowed Codex's tool-result cap at a fixed 20 per batch.
    authored = [c["path"] for c in (m.get("candidates") or [])]
    for rel in authored:
        with open(os.path.join(fx, rel), "a", encoding="utf-8") as fh:
            fh.write("\n// touched after the first batch\n")
    s = Session(fx)
    m3, t3, err3 = maintain(s)
    s.close()
    p3 = m3.get("code_plan") or {}
    cands3 = m3.get("candidates") or []
    included3 = p3.get("included") or 0
    ok = (not err3) and 5 <= included3 < included and all(c.get("existing_entry") for c in cands3) \
        and len(cands3) == included3 and len(t3.encode("utf-8")) <= BUDGET and p3.get("remaining") == total - included3
    record(g, "E7.long-entry-update-batch-fits-budget", "PASS" if ok else "FAIL",
           f"included={included3} (create batch was {included}) bytes={len(t3.encode('utf-8'))} existing={sum(1 for c in cands3 if c.get('existing_entry'))}/{len(cands3)} remaining={p3.get('remaining')}")
    # Team configuration moves the cap and the budget; out-of-range values are rejected.
    rc, _, out, errs = cli(fx, "config", "set", "code_cognition_batch_entries", "50")
    rcb, _, _, _ = cli(fx, "config", "set", "maintain_transport_budget_bytes", "65536")
    s = Session(fx)
    m4, t4, err4 = maintain(s)
    s.close()
    p4 = m4.get("code_plan") or {}
    rc0, _, _, e0 = cli(fx, "config", "set", "code_cognition_batch_entries", "0", expect_ok=False)
    rc201, _, _, e201 = cli(fx, "config", "set", "code_cognition_batch_entries", "201", expect_ok=False)
    rclow, _, _, _ = cli(fx, "config", "set", "maintain_transport_budget_bytes", "16383", expect_ok=False)
    rchigh, _, _, _ = cli(fx, "config", "set", "maintain_transport_budget_bytes", "196609", expect_ok=False)
    ok = rc == 0 and rcb == 0 and p4.get("max_entries") == 50 and p4.get("included") == 50 \
        and p4.get("transport_budget_bytes") == 65536 and len(t4.encode("utf-8")) <= 65536 \
        and rc0 != 0 and rc201 != 0 and rclow != 0 and rchigh != 0
    record(g, "E6.batch-and-budget-configurable-and-bounded", "PASS" if ok else "FAIL",
           f"set50 rc={rc} budget rc={rcb} max_entries={p4.get('max_entries')} included={p4.get('included')} "
           f"budget={p4.get('transport_budget_bytes')} bytes={len(t4.encode('utf-8'))} set0={rc0} set201={rc201} low={rclow} high={rchigh}")


# ================================================================ GROUP F
def group_f():
    """The cognition layer must stay visible to Git, and a checkout that only
    rewrites line endings must not read as drift.

    Both classes were reported from a real project. Hiding the formal assets from
    Git made scan publish a Baseline that could never govern the Volumes it left
    out; the failure surfaced much later as a blocked Guide naming neither the
    rule nor the file. The line-ending class needs no ignore rule at all: Git for
    Windows defaults to core.autocrlf=true, so an ordinary clone rewrote every
    Volume and hard-blocked a repository whose own tolerance policy calls that
    difference equivalent."""
    g = "F"
    d = os.path.join(WORK, "fx-visibility")
    shutil.rmtree(d, ignore_errors=True)
    os.makedirs(os.path.join(d, "pkg"))
    with open(os.path.join(d, "pkg", "a.go"), "w") as f:
        f.write("package pkg\n\nfunc A() int { return 1 }\n")
    sh(d, "git", "init", "-q")
    sh(d, "git", "config", "user.email", "fixture@test.invalid")
    sh(d, "git", "config", "user.name", "fixture")
    sh(d, "git", "add", "-A")
    sh(d, "git", "commit", "-q", "-m", "fixture")

    # F4: init must hand the repository the line-ending protection AOCI uses on
    # itself, because nothing else stops a Windows checkout from rewriting the
    # Volumes it governs.
    rc, _, out, errs = cli(d, "init", "--locale", "en-US")
    if rc != 0:
        record(g, "F1.scan-refuses-hidden-cognition", "FAIL", f"init failed: {out[:150]} {errs[:150]}")
        return
    attributes_path = os.path.join(d, ".gitattributes")
    has_normalization = False
    if os.path.exists(attributes_path):
        with open(attributes_path, encoding="utf-8") as fh:
            has_normalization = "text=auto eol=lf" in fh.read()
    record(g, "F4.init-writes-line-ending-protection",
           "PASS" if has_normalization else "FAIL",
           "init wrote .gitattributes normalizing to LF" if has_normalization
           else "init left the repository exposed to autocrlf rewrites")

    # F1/F2: a formal asset hidden from Git must stop scan, naming the asset and
    # the rule, and --dry-run must report the same thing rather than promising a
    # scan that would fail.
    with open(os.path.join(d, ".git", "info", "exclude"), "a", encoding="utf-8") as fh:
        fh.write("\naoci.txt\naoci.meta.txt\naoci.code.txt\n")
    rc, _, out, errs = cli(d, "scan", expect_ok=False)
    blob = (out or "") + (errs or "")
    named = "aoci.code.txt" in blob and "exclude" in blob
    record(g, "F1.scan-refuses-hidden-cognition", "PASS" if rc != 0 and named else "FAIL",
           f"rc={rc} names_asset_and_rule={named} | {blob[:160]}")
    baseline_path = os.path.join(d, ".aoci", "baseline.json")
    record(g, "F1b.refused-scan-writes-no-baseline",
           "PASS" if not os.path.exists(baseline_path) else "FAIL",
           "no Baseline published" if not os.path.exists(baseline_path)
           else "a refused scan still published a Baseline")
    rc_dry, _, out_dry, errs_dry = cli(d, "scan", "--dry-run", expect_ok=False)
    record(g, "F2.dry-run-reports-the-same-refusal", "PASS" if rc_dry != 0 else "FAIL",
           f"rc={rc_dry} | {((out_dry or '') + (errs_dry or ''))[:160]}")

    # F3: with the assets visible, scan succeeds; a pure line-ending rewrite of
    # every Volume afterwards must stay authorable and name its own repair.
    exclude_path = os.path.join(d, ".git", "info", "exclude")
    with open(exclude_path, encoding="utf-8") as fh:
        kept = [line for line in fh.read().splitlines()
                if line.strip() not in ("aoci.txt", "aoci.meta.txt", "aoci.code.txt")]
    with open(exclude_path, "w", encoding="utf-8") as fh:
        fh.write("\n".join(kept) + "\n")
    rc, _, out, errs = cli(d, "scan")
    if rc != 0:
        record(g, "F3.line-ending-rewrite-is-not-a-lockout", "FAIL",
               f"scan failed once assets were visible: {out[:150]} {errs[:150]}")
        return
    for rel in ("aoci.txt", "aoci.meta.txt", "aoci.code.txt"):
        target = os.path.join(d, rel)
        with open(target, "rb") as fh:
            body = fh.read()
        with open(target, "wb") as fh:
            fh.write(body.replace(b"\n", b"\r\n"))
    rc, guide, out, errs = cli(d, "index", "agent", "guide", "--agent", "scenario")
    findings = guide.get("findings") or []
    codes = {f.get("code") for f in findings}
    repairs = [f.get("safe_repair_action") or "" for f in findings
               if str(f.get("code", "")).endswith("_line_ending_only")]
    not_blocked = guide.get("stage") != "blocked" and "code_volume_unbaselined" not in codes
    reported = any(str(code).endswith("_line_ending_only") for code in codes)
    actionable = any("eol=lf" in text or "LF" in text for text in repairs)
    record(g, "F3.line-ending-rewrite-is-not-a-lockout",
           "PASS" if not_blocked and reported and actionable else "FAIL",
           f"stage={guide.get('stage')} reported={reported} actionable={actionable} codes={sorted(c for c in codes if 'volume' in str(c))}")

    # F5: one root cause must produce one blocker. A Managed Scope policy that no
    # longer matches its receipt already reports scope_change_required; a real
    # operator also received business_source_manifest_invalid, went to look at a
    # subsystem that was working, and found nothing there because nothing was
    # wrong with it.
    import json as _json
    for rel in ("aoci.txt", "aoci.meta.txt", "aoci.code.txt"):
        target = os.path.join(d, rel)
        with open(target, "rb") as fh:
            body = fh.read()
        with open(target, "wb") as fh:
            fh.write(body.replace(b"\r\n", b"\n"))
    baseline_path = os.path.join(d, ".aoci", "baseline.json")
    with open(baseline_path, encoding="utf-8") as fh:
        state = _json.load(fh)
    scope_receipt = state.get("managed_scope")
    if not isinstance(scope_receipt, dict):
        record(g, "F5.scope-drift-reports-one-blocker", "CHAR", "fixture carries no Managed Scope receipt")
        return
    scope_receipt["policy_identity"] = "a" * 64
    with open(baseline_path, "w", encoding="utf-8") as fh:
        _json.dump(state, fh, indent=2)
    rc, check, out, errs = cli(d, "check", expect_ok=False)
    codes = {f.get("code") for f in (check.get("findings") or [])}
    single = "scope_change_required" in codes and "business_source_manifest_invalid" not in codes
    record(g, "F5.scope-drift-reports-one-blocker", "PASS" if single else "FAIL",
           f"codes={sorted(str(c) for c in codes)}")

    # F6: the remediation the Guide hands back must be one this repository can
    # actually run. It reaches this instruction only after the baseline-missing
    # branch returned, so a Baseline exists and scan always refuses.
    rc, guide, out, errs = cli(d, "index", "agent", "guide", "--agent", "scenario", expect_ok=False)
    instructions = " ".join(guide.get("instructions") or [])
    offers_scan = "aoci scan" in instructions
    names_scope = "scope" in instructions
    record(g, "F6.scope-drift-remediation-is-runnable", "PASS" if names_scope and not offers_scan else "FAIL",
           f"offers_scan={offers_scan} names_scope={names_scope} | {instructions[:130]}")


def group_f_scope():
    """A line-ending rewrite must not lock a repository out of Scope Change either.

    313a3ab routed Volume comparison through baseline.EquivalentFingerprints and
    F3 pins that on the Maintain path. The same commit declared volumegovernance
    the one consumer that had bypassed the judgement, and nothing enforced that
    claim: internal/scopechange still compared raw SHA. So the very checkout F3
    proves harmless hard-failed a governed Scope Change with
    managed_scope_index_source_stale, while ordinary Verify, Check and Guide
    called the file aligned and offered no work - a block with no move that
    clears it. F3 never leaves the Maintain path, which is why it could not see
    this, and why the defect reached a real user on rc5. This scenario walks the
    Scope Change path instead, and pins both arms: tolerated and reported, and
    a genuinely changed source judged by the layout the fixture actually has
    (see F7b).
    """
    g = "F"
    first = "F7.line-ending-rewrite-does-not-lock-out-scope-change"
    d = os.path.join(WORK, "fx-scope-line-ending")
    shutil.rmtree(d, ignore_errors=True)
    os.makedirs(os.path.join(d, "pkg"))
    # Binary write: Python text mode translates \n to \r\n on Windows, which would
    # leave this fixture already CRLF and make the flip below a content change.
    with open(os.path.join(d, "pkg", "a.go"), "wb") as f:
        f.write(b"package pkg\n\nfunc A() int { return 1 }\n")
    sh(d, "git", "init", "-q")
    sh(d, "git", "config", "user.email", "fixture@test.invalid")
    sh(d, "git", "config", "user.name", "fixture")
    sh(d, "git", "add", "-A")
    sh(d, "git", "commit", "-q", "-m", "fixture")
    rc, _, out, errs = cli(d, "init", "--locale", "en-US")
    if rc != 0:
        record(g, first, "FAIL", f"init failed: {out[:150]} {errs[:150]}")
        return
    rc, _, out, errs = cli(d, "scan")
    if rc != 0:
        record(g, first, "FAIL", f"scan failed: {out[:150]} {errs[:150]}")
        return

    # A policy edit is what makes a Scope Change necessary at all. The rule is a
    # deliberate no-op matching a path that never exists, so the only thing under
    # test is how the source difference is judged.
    rc, _, out, errs = cli(d, "scope", "rule", "add", "no-op-future",
                           "--action", "exclude", "--pattern", "never-present.txt",
                           "--pattern-kind", "file", "--order", "100",
                           "--reason", "scenario no-op policy change")
    if rc != 0:
        record(g, first, "FAIL", f"scope rule add failed: {out[:150]} {errs[:150]}")
        return
    # The candidate set lives outside the repository: inside it, it would become
    # an untracked Index-role source of its own and change the very evaluation
    # under test.
    candidate = os.path.join(WORK, "fx-scope-line-ending-candidates.json")
    with open(candidate, "w", encoding="utf-8") as fh:
        fh.write('{"version":"managed-scope-candidate-set/v1","entries":[],"dispositions":[]}')

    source = os.path.join(d, "pkg", "a.go")
    with open(source, "rb") as fh:
        original = fh.read()
    # Flip to whichever form differs from what is on disk, so the rewrite is a
    # line-ending difference on every platform rather than only where the fixture
    # happened to land as LF. The precondition is then asserted, not assumed: a
    # naive one-way flip over CRLF yields \r\r\n, which is a real content change
    # and would quietly test the opposite of what this scenario claims.
    lf_body = original.replace(b"\r\n", b"\n")
    flipped = lf_body if original != lf_body else lf_body.replace(b"\n", b"\r\n")
    if flipped == original or flipped.replace(b"\r\n", b"\n") != lf_body:
        record(g, first, "FAIL", "fixture precondition: the rewrite is not line-ending-only")
        return
    with open(source, "wb") as fh:
        fh.write(flipped)
    rc, plan, out, errs = cli(d, "scope", "plan", "--prepared-at", "2026-08-29T00:00:00Z",
                              "--candidate-file", candidate, expect_ok=False)
    body = plan.get("plan") or plan
    tolerated = [item.get("path") for item in (body.get("source_line_ending_only") or [])]
    blob = (out or "") + (errs or "")
    ok = rc == 0 and "managed_scope_index_source_stale" not in blob and "pkg/a.go" in tolerated
    record(g, first, "PASS" if ok else "FAIL",
           f"rc={rc} tolerated={tolerated} | {blob[:130]}")

    # Tolerance is not blindness, and a genuinely changed source is judged by
    # the layout. Legacy: the candidate channel edits the index document, so a
    # stale source needs an Entry candidate and the plan fails closed with
    # managed_scope_index_source_stale (pinned in internal/scopechange). Volumes
    # v1, which is what init produces here: the only document the channel can
    # edit is the Root manifest, which holds no Entries, so no candidate could
    # clear that block and refusing it left the repository with no legal move
    # (#47: Maintain and update_entry stop with scope_change_required until the
    # policy is active, and the policy could not be activated). The plan now
    # goes through, keeps the old fingerprint in the postimage Baseline so
    # Maintain still sees the source as Stale, leaves the manifest byte-identical,
    # and reports the retention instead of hiding it.
    with open(source, "wb") as fh:
        fh.write(flipped + b"\nfunc B() int { return 2 }\n")
    with open(os.path.join(d, ".aoci", "baseline.json"), encoding="utf-8") as fh:
        old_sha = ((json.load(fh).get("files") or {}).get("pkg/a.go") or {}).get("sha256")
    rc, env, out, errs = cli(d, "scope", "preview", "--prepared-at", "2026-08-29T00:00:00Z",
                             "--candidate-file", candidate, expect_ok=False)
    body = env.get("plan") or {}
    retained = [item.get("path") for item in (body.get("source_stale_retained") or [])]
    kept = None
    try:
        raw = base64.b64decode((env.get("baseline_postimage") or {}).get("postimage_bytes") or "")
        kept = ((json.loads(raw).get("files") or {}).get("pkg/a.go") or {}).get("sha256") == old_sha
    except Exception as err:  # noqa: BLE001 - the verdict below reports it
        kept = f"undecodable postimage: {err}"
    image = env.get("index_postimage") or {}
    manifest_same = bool(image) and image.get("postimage_sha256") == image.get("preimage_sha256")
    blob = (out or "") + (errs or "")
    ok = rc == 0 and old_sha is not None and "pkg/a.go" in retained and kept is True and manifest_same
    record(g, "F7b.genuinely-changed-source-is-retained-not-blocked-in-volumes",
           "PASS" if ok else "FAIL",
           f"rc={rc} retained={retained} old_digest_kept={kept} manifest_same={manifest_same} | {blob[:120] if rc else ''}")

    # The candidate that used to be the only way past the stale guard is
    # refused before projection: under Volumes it would have been inserted into
    # the Root manifest and applied under auto authorization as low risk.
    with open(source, "rb") as fh:
        live_sha = hashlib.sha256(fh.read()).hexdigest()
    carrying = os.path.join(WORK, "fx-scope-line-ending-entry-candidates.json")
    with open(carrying, "w", encoding="utf-8") as fh:
        json.dump({"version": "managed-scope-candidate-set/v1",
                   "entries": [{"candidate_id": "c1", "path": "pkg/a.go", "source_sha256": live_sha,
                                "new_entry": "a.go[CG5T]: F:Provides fixture unit A | R:- | A:- | S:-",
                                "review_status": "reviewed"}],
                   "dispositions": []}, fh)
    rc, _, out, errs = cli(d, "scope", "preview", "--prepared-at", "2026-08-29T00:00:00Z",
                           "--candidate-file", carrying, expect_ok=False)
    blob = (out or "") + (errs or "")
    ok = rc != 0 and "managed_scope_volumes_entry_candidates_unsupported" in blob and "aoci_maintain" in blob
    record(g, "F7c.volumes-refuses-entry-candidates-before-projection",
           "PASS" if ok else "FAIL", f"rc={rc} | {blob[:160]}")


def group_f_activate():
    """A pending policy and changed source can reach ordinary maintenance in one command."""
    g, name = "F", "F10.scope-activate-preserves-drift-and-pauses-for-approval"
    d = make_fixture("scope-activate", 1)
    s = Session(d)
    try:
        m, _, _ = maintain(s)
        applied, text, _ = submit_batch(s, m)
        if applied.get("status") != "applied":
            record(g, name, "FAIL", f"fixture authoring failed: {text[:160]}")
            return
        rc, _, out, errs = cli(d, "scope", "rule", "add", "future",
                               "--action", "exclude", "--pattern", "future.txt",
                               "--pattern-kind", "file", "--reason", "future artifact")
        if rc != 0:
            record(g, name, "FAIL", f"rule add failed: {(out + errs)[:160]}")
            return
        before = repo_digest(d)
        with open(os.path.join(d, "pkg", "f001.go"), "a", encoding="utf-8") as fh:
            fh.write("\nfunc Changed() int { return 2 }\n")
        rc, result, out, errs = cli(d, "scope", "activate", expect_ok=False)
        after = repo_digest(d)
        unchanged = all(before[p] == after.get(p) for p in
                        (".aoci/config.json", "aoci.txt", "aoci.meta.txt", "aoci.code.txt"))
        m, _, _ = maintain(s)
        retained = [c.get("path") for c in m.get("candidates", [])] == ["pkg/f001.go"]
        if rc != 0 or result.get("status") != "applied" or not unchanged or not retained:
            record(g, name, "FAIL", f"rc={rc} unchanged={unchanged} retained={retained} | {(out + errs)[:160]}")
            return
        submit_batch(s, m)
        aligned, _ = fixture_aligned(d)
        cli(d, "scope", "budget", "set", "--max-tokens", "500000")
        before = repo_digest(d)
        rc, result, out, errs = cli(d, "scope", "activate", expect_ok=False)
        details = result.get("details") or {}
        preview = details.get("preview_file", "")
        after = repo_digest(d)
        unchanged = all(before[p] == after.get(p) for p in
                        (".aoci/config.json", ".aoci/baseline.json", "aoci.txt", "aoci.meta.txt", "aoci.code.txt"))
        saved = (os.path.isfile(preview) and
                 os.path.commonpath([os.path.abspath(preview), os.path.join(d, ".aoci", "scope-change")])
                 == os.path.join(d, ".aoci", "scope-change"))
        commands = all(token in details.get(field, "") for field, token in
                       (("approve_command", "--out-file"), ("apply_command", "--approval-file")))
        ok = (aligned and rc == 2 and result.get("error_code") == "managed_scope_human_approval_required"
              and details.get("interaction_required") is True and details.get("formal_writes_started") is False
              and unchanged and saved and commands)
        record(g, name, "PASS" if ok else "FAIL",
               f"aligned={aligned} approval_rc={rc} unchanged={unchanged} saved={saved} commands={commands}")
    finally:
        s.close()


def group_f_host_dialog():
    """A person approves or rejects a paused Scope Change in the host's dialog.

    The terminal was the only way to approve, and people did not know to open
    one and type a 64-character phrase. aoci_maintain intent=human_approval asks
    through MCP form elicitation instead. Driven here by a client that declares
    elicitation and answers it, against the real binary: a host without dialogs
    and a person who rejects leave every formal byte as it was and say what
    happens next; a person who approves records an approval that scope apply
    takes to aligned.
    """
    g = "F"
    approve_name = "F13.host-dialog-approval-is-recorded-and-applies-to-aligned"
    reject_name = "F14.host-dialog-rejection-or-no-dialog-changes-nothing"
    d = make_fixture("scope-host-dialog", 1)
    s = Session(d)
    try:
        m, _, _ = maintain(s)
        applied, text, _ = submit_batch(s, m)
    finally:
        s.close()
    if applied.get("status") != "applied":
        for name in (reject_name, approve_name):
            record(g, name, "FAIL", f"fixture authoring failed: {text[:160]}")
        return
    rc, _, out, errs = cli(d, "scope", "approval-mode", "review")
    rc2, result, out2, errs2 = cli(d, "scope", "activate", expect_ok=False)
    details = result.get("details") or {}
    request = details.get("host_dialog_request") or {}
    arguments = request.get("arguments") or {}
    preview = details.get("preview_file", "")
    approval = os.path.join(os.path.dirname(preview), "approval.json") if preview else ""
    if rc != 0 or rc2 != 2 or request.get("tool") != "aoci_maintain" or not arguments or not preview:
        for name in (reject_name, approve_name):
            record(g, name, "FAIL", f"activation did not offer a dialog route: rc={rc}/{rc2} | {(out2 + errs2)[:160]}")
        return
    watched = (".aoci/config.json", ".aoci/baseline.json", "aoci.txt", "aoci.meta.txt", "aoci.code.txt")
    before = repo_digest(d)

    plain = Session(d)
    try:
        t1, err1 = text_of(plain.call("aoci_maintain", arguments))
    finally:
        plain.close()
    unavailable = jload(t1)
    shown = []
    def reject(method, params):
        if method != "elicitation/create":
            return None
        shown.append(params.get("message", ""))
        return {"action": "accept", "content": {"decision": "reject", "note": "not this week"}}
    rejecting = Session(d, capabilities={"elicitation": {}}, on_request=reject)
    try:
        t2, err2 = text_of(rejecting.call("aoci_maintain", arguments))
    finally:
        rejecting.close()
    rejected = jload(t2)
    after = repo_digest(d)
    unchanged = all(before.get(p) == after.get(p) for p in watched)
    absent = not os.path.exists(approval)
    ok = (not err1 and unavailable.get("status") == "host_dialog_unavailable"
          and len(unavailable.get("next_commands") or []) == 2
          and not err2 and rejected.get("status") == "rejected" and rejected.get("human_note") == "not this week"
          and len(shown) == 1 and "scope rule remove" in (rejected.get("message") or "") and absent and unchanged)
    record(g, reject_name, "PASS" if ok else "FAIL",
           f"no_dialog={unavailable.get('status')} rejected={rejected.get('status')} shown={len(shown)} "
           f"approval_absent={absent} unchanged={unchanged}" + ("" if ok else f" | {t1[:120]} | {t2[:160]}"))

    shown = []
    def approve(method, params):
        if method != "elicitation/create":
            return None
        shown.append(params.get("message", ""))
        return {"action": "accept", "content": {"decision": "approve"}}
    approving = Session(d, capabilities={"elicitation": {}}, on_request=approve)
    try:
        t3, err3 = text_of(approving.call("aoci_maintain", arguments))
    finally:
        approving.close()
    approved = jload(t3)
    recorded = {}
    try:
        with open(approval, encoding="utf-8") as fh:
            recorded = json.load(fh)
    except (OSError, ValueError):
        pass
    untouched = all(before.get(p) == repo_digest(d).get(p) for p in watched)
    applies = any("--approval-file" in command for command in approved.get("next_commands") or [])
    rc4, _, out4, errs4 = cli(d, "scope", "apply", "--preview-file", preview, "--approval-file", approval, expect_ok=False)
    aligned, _ = fixture_aligned(d)
    ok = (not err3 and approved.get("status") == "approved" and len(shown) == 1 and applies
          and recorded.get("approval_mechanism") == "host_elicitation_confirmation"
          and untouched and rc4 == 0 and aligned)
    record(g, approve_name, "PASS" if ok else "FAIL",
           f"approved={approved.get('status')} shown={len(shown)} mechanism={recorded.get('approval_mechanism')} "
           f"untouched_before_apply={untouched} apply_rc={rc4} aligned={aligned}"
           + ("" if ok else f" | {t3[:160]} | {(out4 + errs4)[:160]}"))


class SessionlessSession(Session):
    """A client on the sessionless 2026-07-28 revision: it never calls
    initialize and sends its revision, client information, and capabilities
    in every request's _meta."""
    def __init__(self, repo, capabilities=None):
        self.repo = repo
        self.on_request = None
        self.meta = {"io.modelcontextprotocol/protocolVersion": "2026-07-28",
                     "io.modelcontextprotocol/clientInfo": {"name": "aoci-scenarios-sessionless", "version": "1.0"},
                     "io.modelcontextprotocol/clientCapabilities": capabilities or {}}
        self.p = subprocess.Popen([BIN, "--repo", repo, "mcp"],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            text=True, encoding="utf-8", bufsize=1)
        self.stderr_capture = BoundedStderr(self.p.stderr)
        self.next_id = 1
    def rpc(self, method, params=None, timeout=120):
        params = dict(params or {})
        params["_meta"] = dict(self.meta)
        return super().rpc(method, params, timeout)


def group_f_sessionless_dialog():
    """The host dialog on the sessionless 2026-07-28 revision.

    That revision forbids a server's own requests, so the dialog travels as an
    input request: the first call returns the form and a request state and
    writes nothing, and the client calls again with the person's answer beside
    that state. Driven by a raw sessionless client against the real binary: an
    answer beside a forged state records nothing, and the issued state with an
    approval records one that scope apply takes to aligned.
    """
    g, name = "F", "F16.sessionless-dialog-round-binds-the-answer-and-applies-to-aligned"
    d = make_fixture("scope-sessionless-dialog", 1)
    s = Session(d)
    try:
        m, _, _ = maintain(s)
        applied, text, _ = submit_batch(s, m)
    finally:
        s.close()
    if applied.get("status") != "applied":
        record(g, name, "FAIL", f"fixture authoring failed: {text[:160]}")
        return
    rc, _, _, _ = cli(d, "scope", "approval-mode", "review")
    rc2, result, out2, errs2 = cli(d, "scope", "activate", expect_ok=False)
    details = result.get("details") or {}
    arguments = (details.get("host_dialog_request") or {}).get("arguments") or {}
    preview = details.get("preview_file", "")
    approval = os.path.join(os.path.dirname(preview), "approval.json") if preview else ""
    if rc != 0 or rc2 != 2 or not arguments or not preview:
        record(g, name, "FAIL", f"activation did not offer a dialog route: rc={rc}/{rc2} | {(out2 + errs2)[:160]}")
        return
    watched = (".aoci/config.json", ".aoci/baseline.json", "aoci.txt", "aoci.meta.txt", "aoci.code.txt")
    before = repo_digest(d)
    s = SessionlessSession(d, capabilities={"elicitation": {"form": {}}})
    try:
        first = s.rpc("tools/call", {"name": "aoci_maintain", "arguments": arguments}).get("result") or {}
        form = (first.get("inputRequests") or {}).get("human_approval") or {}
        state = first.get("requestState") or ""
        asked = (first.get("resultType") == "input_required" and form.get("method") == "elicitation/create"
                 and (form.get("params") or {}).get("mode") == "form" and state.startswith("v1:") and not os.path.exists(approval))
        answer = {"human_approval": {"action": "accept", "content": {"decision": "approve"}}}
        t2, err2 = text_of(s.rpc("tools/call", {"name": "aoci_maintain", "arguments": arguments,
                                                 "inputResponses": answer, "requestState": "v1:" + "0" * 64}))
        forged = jload(t2)
        forged_ok = not err2 and forged.get("status") == "stale" and not os.path.exists(approval)
        t3, err3 = text_of(s.rpc("tools/call", {"name": "aoci_maintain", "arguments": arguments,
                                                 "inputResponses": answer, "requestState": state}))
        approved = jload(t3)
    finally:
        s.close()
    recorded = {}
    try:
        with open(approval, encoding="utf-8") as fh:
            recorded = json.load(fh)
    except (OSError, ValueError):
        pass
    untouched = all(before.get(path) == repo_digest(d).get(path) for path in watched)
    rc4, _, out4, errs4 = cli(d, "scope", "apply", "--preview-file", preview, "--approval-file", approval, expect_ok=False)
    aligned, _ = fixture_aligned(d)
    ok = (asked and forged_ok and not err3 and approved.get("status") == "approved"
          and recorded.get("approval_mechanism") == "host_elicitation_confirmation"
          and untouched and rc4 == 0 and aligned)
    record(g, name, "PASS" if ok else "FAIL",
           f"asked={asked} forged={forged.get('status')} approved={approved.get('status')} "
           f"mechanism={recorded.get('approval_mechanism')} untouched_before_apply={untouched} apply_rc={rc4} aligned={aligned}"
           + ("" if ok else f" | {str(first)[:160]} | {t2[:120]} | {t3[:160]} | {(out4 + errs4)[:120]}"))


def group_f_observe_review():
    """A Host reviews changed Observe evidence without leaving MCP (#151).

    The acceptance the reporter proposed: in an aligned repository change only
    one test file the production profile observes, then list the evidence,
    review it, confirm it, and reach maintenance from MCP alone; and once the
    bytes move again, the confirmation issued for the old listing must not be
    usable.
    """
    g, name = "F", "F15.observe-review-confirms-the-listed-bytes-and-refuses-moved-ones"
    d = make_fixture("observe-review", 1)
    s = Session(d)
    try:
        m, _, _ = maintain(s)
        applied, text, _ = submit_batch(s, m)
        if applied.get("status") != "applied":
            record(g, name, "FAIL", f"fixture authoring failed: {text[:160]}")
            return
        test_file = os.path.join(d, "pkg", "f001_test.go")
        with open(test_file, "w", encoding="utf-8") as fh:
            fh.write('package pkg\n\nimport "testing"\n\nfunc TestF001(t *testing.T) {}\n')
        rc, _, out, errs = cli(d, "scope", "acknowledge", "--reviewed-by", "fixture")
        if rc != 0:
            record(g, name, "FAIL", f"baselining the observed test failed: {(out + errs)[:160]}")
            return
        with open(test_file, "a", encoding="utf-8") as fh:
            fh.write("\n// reviewed change\n")
        blocked, t1, _ = maintain(s)
        listing = blocked.get("observe_review") or {}
        listed = [item.get("path") for item in listing.get("evidence") or []]
        before = repo_digest(d)
        with open(test_file, "a", encoding="utf-8") as fh:
            fh.write("// moved after the listing\n")
        t2, err2 = text_of(s.call("aoci_maintain", {"intent": "observe_review", "observe_review": {
            "review_digest": listing.get("review_digest", ""), "reviewed_by": "scenario-host"}}))
        stale = jload(t2)
        unchanged = before.get(".aoci/baseline.json") == repo_digest(d).get(".aoci/baseline.json")
        fresh = (stale.get("observe_review") or {}).get("review_digest", "")
        t3, err3 = text_of(s.call("aoci_maintain", {"intent": "observe_review", "observe_review": {
            "review_digest": fresh, "reviewed_by": "scenario-host"}}))
        confirmed = jload(t3)
        after, t4, _ = maintain(s)
    finally:
        s.close()
    aligned, _ = fixture_aligned(d)
    ok = (blocked.get("next_action") == "review_observed_evidence" and listed == ["pkg/f001_test.go"]
          and listing.get("confirm_request") is not None
          and not err2 and stale.get("status") == "stale" and unchanged and fresh and fresh != listing.get("review_digest")
          and not err3 and confirmed.get("status") == "acknowledged" and confirmed.get("formal_writes_started") is True
          and confirmed.get("review_digest") == fresh and confirmed.get("acknowledged_paths") == ["pkg/f001_test.go"]
          and after.get("observe_review") is None and after.get("next_action") != "review_observed_evidence" and aligned)
    record(g, name, "PASS" if ok else "FAIL",
           f"listed={listed} stale={stale.get('status')} baseline_unchanged={unchanged} confirmed={confirmed.get('status')} "
           f"after={after.get('next_action')} aligned={aligned}" + ("" if ok else f" | {t1[:120]} | {t2[:120]} | {t3[:120]}"))


def group_f_ignored_tool_file():
    """A file another tool writes into a Git-ignored directory between preview
    and Apply is not part of the change (#101).

    Git-ignored files are policy candidates so that a rule may pull one in, and
    until rc19 they stayed in the Scope Change envelope with the inventory
    counts that include them. CodeGraph's watcher rewriting its lock file while
    a human reviewed a preview then made Apply fail with a replay mismatch, an
    error that reads as the operator's fault when no sequence of correct steps
    could pass. The ignore rule lives in .git/info/exclude so that the rule
    itself is not a new candidate, and the directory name is one no built-in
    rule knows, so what is under test is the envelope, not a safety category.
    """
    g, name = "F", "F12.ignored-tool-file-between-preview-and-apply-keeps-replay-stable"
    d = make_fixture("scope-ignored-tool-file", 1)
    # Author the fixture first so that the only thing between the repository
    # and aligned afterwards is the policy change itself.
    s = Session(d)
    try:
        m, _, _ = maintain(s)
        applied, text, _ = submit_batch(s, m)
    finally:
        s.close()
    if applied.get("status") != "applied":
        record(g, name, "FAIL", f"fixture authoring failed: {text[:160]}")
        return
    with open(os.path.join(d, ".git", "info", "exclude"), "a", encoding="utf-8") as fh:
        fh.write(".toolstate/\n")
    rc, _, out, errs = cli(d, "scope", "rule", "add", "no-op-future",
                           "--action", "exclude", "--pattern", "never-present.txt",
                           "--pattern-kind", "file", "--order", "100",
                           "--reason", "scenario no-op policy change")
    if rc != 0:
        record(g, name, "FAIL", f"scope rule add failed: {(out + errs)[:160]}")
        return
    candidate = os.path.join(WORK, "fx-scope-ignored-tool-candidates.json")
    with open(candidate, "w", encoding="utf-8") as fh:
        fh.write('{"version":"managed-scope-candidate-set/v1","entries":[],"dispositions":[]}')
    rc, _, out, errs = cli(d, "scope", "preview", "--candidate-file", candidate, expect_ok=False)
    if rc != 0:
        record(g, name, "FAIL", f"scope preview failed: {(out + errs)[:160]}")
        return
    preview = os.path.join(WORK, "fx-scope-ignored-tool-preview.json")
    with open(preview, "w", encoding="utf-8") as fh:
        fh.write(out)
    os.makedirs(os.path.join(d, ".toolstate"), exist_ok=True)
    with open(os.path.join(d, ".toolstate", "tool.lock"), "w", encoding="utf-8") as fh:
        fh.write("pid 4242\n")
    rc, result, out, errs = cli(d, "scope", "apply", "--preview-file", preview, expect_ok=False)
    aligned, _ = fixture_aligned(d)
    ok = rc == 0 and result.get("status") == "applied" and aligned
    record(g, name, "PASS" if ok else "FAIL",
           f"rc={rc} status={result.get('status')} aligned={aligned} | {(out + errs)[:160] if rc else ''}")


def group_f_held_sources():
    """A first scan over an image, an empty file, and a file above the read limit
    reaches aligned in auto mode: the three are held out as code_skipped with their
    cause, never dressed as orphans, alignment does not move when another image
    lands, and a held file can still be authored directly (it then leaves skipped).
    Releases up to v0.1.0-rc14 stopped the first Maintain here with
    pending_curation: markers that Volumes v1 had no decision path to clear."""
    g, name = "F", "F11.held-sources-reach-aligned-without-a-decision"
    d = os.path.join(WORK, "held-sources")
    shutil.rmtree(d, ignore_errors=True)
    for sub in ("pkg", "assets", "data"):
        os.makedirs(os.path.join(d, sub))
    for i in (1, 2):
        with open(os.path.join(d, "pkg", f"f{i:03}.go"), "w") as f:
            f.write(f"package pkg\n\n// fixture unit {i}: independent constant provider\nfunc F{i:03}() int {{ return {i} }}\n")
    png = b"\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00"
    with open(os.path.join(d, "assets", "logo.png"), "wb") as f:
        f.write(png)
    open(os.path.join(d, "data", "empty.txt"), "wb").close()
    with open(os.path.join(d, "data", "dump.txt"), "w") as f:
        f.write("row\n" * ((1 << 20) // 4 + 1))
    sh(d, "git", "init", "-q")
    sh(d, "git", "config", "user.email", "fixture@test.invalid")
    sh(d, "git", "config", "user.name", "fixture")
    sh(d, "git", "add", "-A")
    sh(d, "git", "commit", "-q", "-m", "fixture")
    rc, _, out, errs = cli(d, "init", "--locale", "en-US")
    if rc != 0:
        record(g, name, "FAIL", f"init failed: {(out + errs)[:160]}")
        return
    rc, scan, out, errs = cli(d, "scan")
    counts = scan.get("skipped_sources") or {}
    announced = rc == 0 and counts.get("total") == 3 and counts.get("binary") == 1 \
        and counts.get("empty") == 1 and counts.get("oversize") == 1 and "unreadable" not in counts
    probes = {"assets/logo.png", "data/empty.txt", "data/dump.txt"}
    s = Session(d)
    try:
        m, text, _ = maintain(s)
        gov = m.get("governance") or {}
        markers = [str(o) for o in (m.get("orphan_remove_candidates") or [])]
        cands = {c.get("path") for c in (m.get("candidates") or [])}
        drift = gov.get("code_drift") or {}
        # Maintain samples its findings while it carries candidates (five since
        # rc16); the complete held-source causes come from Verify.
        _, vdoc, _, _ = cli(d, "verify", expect_ok=False)
        causes = {f.get("target"): f.get("cause") for f in ((vdoc.get("governance") or {}).get("findings") or []) if f.get("code") == "code_skipped"}
        first_ok = (m.get("status") == "repair_required" and not markers and bool(m.get("authoring_meta"))
                    and not (cands & probes) and set(drift.get("skipped") or []) == probes
                    and causes == {"assets/logo.png": "binary", "data/empty.txt": "empty", "data/dump.txt": "oversize"})
        if not first_ok:
            record(g, name, "FAIL", f"announced={announced} status={m.get('status')} markers={markers[:3]} "
                                    f"skipped={drift.get('skipped')} causes={causes} | {text[:120]}")
            return
        applied, atext, _ = submit_batch(s, m)
        aligned, _ = fixture_aligned(d)
        if applied.get("status") != "applied" or not aligned:
            record(g, name, "FAIL", f"auto authoring did not align: status={applied.get('status')} aligned={aligned} | {atext[:160]}")
            return
        with open(os.path.join(d, "assets", "new.png"), "wb") as f:
            f.write(png + b"new")
        still, v = fixture_aligned(d)
        skipped = set(((v.get("governance") or {}).get("code_drift") or {}).get("skipped") or [])
        with open(os.path.join(d, "assets", "logo.png"), "rb") as f:
            sha = hashlib.sha256(f.read()).hexdigest()
        r = jload(text_of(s.call("aoci_update_entry", {"path": "assets/logo.png", "source_sha256": sha,
                  "new_entry": "logo.png[CG1T]: F:Brand mark shown by the fixture | R:- | A:- | S:-"}))[0]) or {}
        after, v2 = fixture_aligned(d)
        skipped2 = set(((v2.get("governance") or {}).get("code_drift") or {}).get("skipped") or [])
        ok = (announced and still and skipped == probes | {"assets/new.png"} and r.get("status") == "applied"
              and after and skipped2 == {"assets/new.png", "data/empty.txt", "data/dump.txt"})
        record(g, name, "PASS" if ok else "FAIL",
               f"announced={announced} new_png_holds={still} direct_author={r.get('status')} "
               f"aligned_after={after} skipped={sorted(skipped2)}")
    finally:
        s.close()


def group_f_deleted_observe():
    """Deleting a tracked observe source must not wedge acknowledgement.

    scope status keys an observe removal on the source snapshot; the Scope Change
    plan keyed it on the role map. A file deleted from the worktree but still
    tracked by Git is evaluated as an unsafe filesystem object, so it stayed in
    the role map and read as "reclassified by policy" rather than "gone". status
    listed it, the plan did not, and the review set scope acknowledge submits
    could never match - the refusal named neither the extra nor the missing path,
    and nothing the operator could run cleared it.
    """
    g = "F"
    name = "F8.deleted-observe-source-can-be-acknowledged"
    d = os.path.join(WORK, "fx-deleted-observe")
    shutil.rmtree(d, ignore_errors=True)
    os.makedirs(os.path.join(d, "pkg"))
    with open(os.path.join(d, "pkg", "a.go"), "wb") as f:
        f.write(b"package pkg\n\nfunc A() int { return 1 }\n")
    with open(os.path.join(d, "pkg", "a_test.go"), "wb") as f:
        f.write(b"package pkg\n\nimport \"testing\"\n\nfunc TestA(t *testing.T) { _ = A() }\n")
    sh(d, "git", "init", "-q")
    sh(d, "git", "config", "user.email", "fixture@test.invalid")
    sh(d, "git", "config", "user.name", "fixture")
    sh(d, "git", "add", "-A")
    sh(d, "git", "commit", "-q", "-m", "fixture")
    for step in ("init", "scan"):
        args = [step] if step == "scan" else [step, "--locale", "en-US"]
        rc, _, out, errs = cli(d, *args)
        if rc != 0:
            record(g, name, "FAIL", f"{step} failed: {out[:120]} {errs[:120]}")
            return

    # Delete one observe source while Git still tracks it -- what an ordinary
    # "I removed this test" looks like -- and add another, so more than one
    # observe change is pending. The second change is what makes this scenario
    # able to fail at all: with the deletion alone the plan computed an empty
    # change set, skipped the review gate entirely, and acknowledged happily
    # while still not seeing the removal.
    os.remove(os.path.join(d, "pkg", "a_test.go"))
    with open(os.path.join(d, "pkg", "b_test.go"), "wb") as f:
        f.write(b"package pkg\n\nimport \"testing\"\n\nfunc TestB(t *testing.T) { _ = A() }\n")
    rc, status, out, errs = cli(d, "scope", "status", expect_ok=False)
    drift = status.get("drift") or {}
    removed = drift.get("observed_removed") or []
    pending = status.get("observed_pending_review") or 0
    if "pkg/a_test.go" not in removed or pending < 2:
        record(g, name, "CHAR",
               f"fixture precondition: need the removal plus another pending change, got removed={removed} pending={pending}")
        return

    rc, result, out, errs = cli(d, "scope", "acknowledge", "--reviewed-by", "scenario", expect_ok=False)
    blob = (result.get("message") or "") + out
    ok = rc == 0 and result.get("status") == "applied"
    record(g, name, "PASS" if ok else "FAIL",
           f"rc={rc} status={result.get('status')} | {blob[:120]}")

    # And the acknowledgement must have actually retired the fingerprint, not
    # merely returned success.
    rc, status, out, errs = cli(d, "scope", "status", expect_ok=False)
    pending = status.get("observed_pending_review")
    record(g, "F8b.acknowledgement-retires-the-vanished-fingerprint",
           "PASS" if pending == 0 else "FAIL", f"observed_pending_review={pending}")


def group_f_excluded_tracked():
    """A repository that tracks an excluded file must still initialize.

    Initial-scope approval keyed on a counter that incremented for every tracked
    path a built-in rule EXCLUDED, so one tracked file under vendor/, dist/ or
    any other built-in generated directory refused init outright. An excluded
    path is never opened, so it cannot make initialization unsafe - and every
    remediation the refusal offered was wrong for that category: removing the
    file or gitignoring it both mean deleting it from the repository, gitignore
    does not untrack an already tracked file, and the opt-in it named accepts
    only sensitive paths and reads the config file that only init creates.
    A project vendoring its own framework source could not be onboarded at all.
    """
    g = "F"
    d = os.path.join(WORK, "fx-excluded-tracked")
    shutil.rmtree(d, ignore_errors=True)
    os.makedirs(os.path.join(d, "pkg"))
    os.makedirs(os.path.join(d, "vendor", "framework", "src"))
    with open(os.path.join(d, "pkg", "a.go"), "wb") as f:
        f.write(b"package pkg\n\nfunc A() int { return 1 }\n")
    # Hand-maintained framework source that happens to live under vendor/.
    with open(os.path.join(d, "vendor", "framework", "src", "core.ts"), "wb") as f:
        f.write(b"export const SECRETLESS_MARKER = 'vendored-source'\n")
    sh(d, "git", "init", "-q")
    sh(d, "git", "config", "user.email", "fixture@test.invalid")
    sh(d, "git", "config", "user.name", "fixture")
    sh(d, "git", "add", "-A")
    sh(d, "git", "commit", "-q", "-m", "fixture")

    rc, _, out, errs = cli(d, "init", "--locale", "en-US")
    blob = (out or "") + (errs or "")
    record(g, "F9.tracked-excluded-file-does-not-refuse-init",
           "PASS" if rc == 0 else "FAIL",
           f"rc={rc}" if rc == 0 else f"rc={rc} | {blob[:200]}")
    if rc != 0:
        return

    # The layout must be the current one. A repository that reaches init through
    # a fallback path gets a Legacy monolithic index instead, which is a quieter
    # failure than a refusal.
    root_marker = ""
    root_path = os.path.join(d, "aoci.txt")
    if os.path.exists(root_path):
        with open(root_path, encoding="utf-8") as fh:
            root_marker = fh.readline().strip()
    record(g, "F9b.init-still-produces-the-volumes-layout",
           "PASS" if root_marker.startswith("#AOCI-ROOT-MANIFEST") else "FAIL",
           f"root marker: {root_marker[:60]}")

    rc, _, out, errs = cli(d, "scan")
    if rc != 0:
        record(g, "F9c.excluded-file-stays-excluded-and-unread", "FAIL",
               f"scan failed: {(out + errs)[:160]}")
        return
    rc, body, _, _ = cli(d, "scope", "explain", "vendor/framework/src/core.ts")
    role = (body or {}).get("role") or ""
    # And its content never reaches formal cognition.
    leaked = False
    for asset in ("aoci.txt", "aoci.meta.txt", "aoci.code.txt"):
        path = os.path.join(d, asset)
        if not os.path.exists(path):
            continue
        with open(path, encoding="utf-8") as fh:
            if "SECRETLESS_MARKER" in fh.read():
                leaked = True
    record(g, "F9c.excluded-file-stays-excluded-and-unread",
           "PASS" if role == "exclude" and not leaked else "FAIL",
           f"role={role or '(none)'} content_reached_cognition={leaked}")


# ================================================================ GROUP N
# next_commands 与认知状态语义: 终态/阻断指引必须指向真实可调用面, 且这些命令
# 必须真的能跑; 已交付+已认证的认知在树漂移时是 uncertain, 不是 invalid。
# 两个宿主在两个仓库上独立撞过这两处: 一个按 "Verify, Aggregate Check, Guide"
# 在 MCP 工具面上找不到任何东西, 一个在 10/10 认证后因功能分支在途文件读到
# cognition_state=invalid 而被诱导重新交付整个索引。

def run_returned_command(fx, command):
    """Run a next_commands string exactly as returned (agent placeholder filled)."""
    filled = command.replace("{agent}", "claude")
    proc = subprocess.run(filled, shell=True, cwd=fx, capture_output=True, text=True, encoding="utf-8", errors="replace", timeout=120)
    return proc.returncode, (proc.stdout or "") + (proc.stderr or "")

def overview_meta_and_full_body(text):
    """meta_and_body, but a single-shot full delivery keeps its body: a small
    index answers with kv metadata plus the body inline and no chunk marker,
    and the shared helper's kv branch returns body=None for that shape."""
    if MARK in text:
        return meta_and_body(text)
    head, sep, rest = text.partition("<<<AOCI_OVERVIEW_BODY_BEGIN")
    return parse_kv(head), (sep + rest) if sep else None

NEUTRAL_ROOT = "/<repository>"


def parse_body_entries(body, fx):
    """Ordinal-ordered (rel_path, tag, core_f) parsed from a delivered body.
    Section roots are historical coordinates: a new index records the neutral
    root, an older one the fixture path. On Windows the fixture path is
    backslashed while the index writes forward slashes, so both sides are
    normalized before the prefix strip — os.path.relpath is unreliable across
    mixed separators."""
    entries, section = [], ""
    fx_norm = fx.replace("\\", "/").rstrip("/")
    for line in (body or "").splitlines():
        line = line.rstrip("\r")
        if line.startswith("===") and line.endswith("==="):
            raw = line.strip("=").replace("\\", "/")
            if raw.startswith(NEUTRAL_ROOT):
                rel = raw[len(NEUTRAL_ROOT):].strip("/")
            else:
                rel = raw[len(fx_norm):].strip("/") if raw.startswith(fx_norm) else raw.strip("/")
            rel = "" if rel in (".", "") else rel
            section = rel + "/" if rel else ""
            continue
        m = re.match(r"^([^\s#=<│─][^\[]*)\[([A-Z0-9]+)\]: F:(.*?) \| R:", line)
        if m:
            entries.append((section + m.group(1), m.group(2), m.group(3)))
    return entries


# ---------------------------------------------------------------- group M
def current_entries(fx):
    """Ordinal-ordered (rel_path, tag, core_f) of the fixture's Code Volume as
    it stands on disk: the canonical sequence a delta's ordinals point into."""
    with open(os.path.join(fx, "aoci.code.txt"), encoding="utf-8") as fh:
        return parse_body_entries(fh.read(), fx)

def attest_delivery(s, fx, t, wrong=False):
    """Confirm and attest one full-form delivery (complete or delta) from the
    current on-disk sequence; `wrong` answers every core F incorrectly."""
    meta, body = overview_meta_and_full_body(t)
    ordinals = meta.get("challenge_ordinals") or []
    if isinstance(ordinals, int):
        ordinals = [ordinals]
    if isinstance(ordinals, str):
        ordinals = [int(x) for x in ordinals.split(",") if x.strip()]
    entries = current_entries(fx)
    answers = []
    for o in ordinals:
        path, tag, core_f = entries[o - 1]
        answers.append({"ordinal": o, "object_identity": path, "tag": tag,
                        "core_f": "nothing like the delivered responsibility" if wrong else core_f})
    att = {"version": "model-cognition-attestation/v1",
           "index_sha256": meta.get("challenge_index_sha256"),
           "entry_sequence_sha256": meta.get("challenge_entry_sequence_sha256"),
           "entry_count": meta.get("challenge_entry_count"),
           "challenge_digest": meta.get("challenge_digest"),
           "reported_entry_count": meta.get("entry_count"),
           "reported_estimated_tokens": meta.get("estimated_tokens"),
           "coverage_percent": 100, "system_mastery_percent": 85, "confidence_percent": 90,
           "truncation_detected": False, "unseen_sections": [], "uncertainty_reasons": [],
           "challenge_answers": answers}
    confirm = {"version": "overview-delivery-receipt/v1",
               "body_sha256": meta.get("body_sha256"),
               "body_bytes": meta.get("body_utf8_bytes"),
               "end_marker_observed": True}
    t2, err = text_of(s.call("aoci_overview", {"host_delivery_confirmation": confirm, "model_cognition_attestation": att}))
    return parse_kv(t2), t2

def truthy(value):
    """parse_kv reads true/false into booleans; a comparison that accepts both."""
    return value is True or value == "true"

def pass_probe(s, fx):
    """Take the two-question probe and answer it from the on-disk sequence."""
    t, err = text_of(s.call("aoci_overview", {"check_only": True, "probe": True}))
    probe = jload(t).get("cognition_probe") or {}
    entries = current_entries(fx)
    answers = []
    for o in probe.get("ordinals") or []:
        path, tag, core_f = entries[o - 1]
        answers.append({"ordinal": o, "object_identity": path, "tag": tag, "core_f": core_f})
    t2, err = text_of(s.call("aoci_overview", {"check_only": True,
        "probe_answers": {"version": "cognition-probe/v1", "digest": probe.get("digest"), "answers": answers}}))
    return (jload(t2).get("probe_result") or {}).get("result") == "pass"

def add_source_and_align(s, fx, fname):
    """Create one new fixture source and author its Entry."""
    with open(os.path.join(fx, "pkg", fname), "w") as f:
        f.write("package pkg\n\n// added unit\nfunc Added() int { return 99 }\n")
    m, t, err = maintain(s)
    a, tw, err = submit_batch(s, m)
    return a.get("aligned") is True

def group_m_delta():
    """Incremental delivery (the L5 section of the delivery contract): after a
    passing complete Attestation and a probe pass, delta_since delivers exactly
    the changed and added Entries; a passing delta Attestation re-anchors the
    base; no probe, a restart, a partial delta Attestation, and an unchanged
    base each answer as the contract says."""
    g = "M"
    fx = make_fixture("fx-delta", 8)
    s = Session(fx)
    m, t, err = maintain(s)
    a, tw, err = submit_batch(s, m)
    t, err = text_of(s.call("aoci_overview"))
    meta, body = overview_meta_and_full_body(t)
    base = meta.get("index_sha256")
    proof, pt = attest_delivery(s, fx, t)
    attested = a.get("aligned") is True and meta.get("delivery_mode") == "full" and proof.get("model_attestation") == "pass"
    # -- M1: one Entry revised, one added: the delta is exactly those two
    changed = land_write(fx, "f001.go", "revised constant provider one") == "applied"
    added = add_source_and_align(s, fx, "f009.go")
    probed = pass_probe(s, fx)
    t, err = text_of(s.call("aoci_overview", {"delta_since": base}))
    meta, body = overview_meta_and_full_body(t)
    delivered = parse_body_entries(body, fx) if body else []
    entries = current_entries(fx)
    paths = [p for p, _, _ in entries]
    want_changed = str(paths.index("pkg/f001.go") + 1) if "pkg/f001.go" in paths else "?"
    want_added = str(paths.index("pkg/f009.go") + 1) if "pkg/f009.go" in paths else "?"
    shaped = (attested and changed and added and probed and meta.get("delivery_mode") == "delta"
              and str(meta.get("delta_object_count")) == "2" and len(delivered) == 2
              and str(meta.get("delta_changed_ordinals")) == want_changed
              and str(meta.get("delta_added_ordinals")) == want_added
              and str(meta.get("scope_object_count")) == str(len(entries))
              and meta.get("delta_base_scope_identity") == base
              and {p for p, _, _ in delivered} == {"pkg/f001.go", "pkg/f009.go"})
    record(g, "M1.delta-carries-exactly-the-changed-and-added-entries", "PASS" if shaped else "FAIL",
           f"attested={attested} changed={changed} added={added} probed={probed} mode={meta.get('delivery_mode')} "
           f"count={meta.get('delta_object_count')} delivered={[p for p, _, _ in delivered]}"[:200])
    # -- M2: the delta Attestation passes against the complete identity facts and re-anchors
    proof, pt = attest_delivery(s, fx, t)
    after_first = meta.get("index_sha256")
    second_changed = land_write(fx, "f002.go", "revised constant provider two") == "applied"
    probed = pass_probe(s, fx)
    t2, err = text_of(s.call("aoci_overview", {"delta_since": after_first}))
    meta2, body2 = overview_meta_and_full_body(t2)
    delivered2 = parse_body_entries(body2, fx) if body2 else []
    ok2 = (proof.get("model_attestation") == "pass" and truthy(proof.get("model_full_cognition_reliable"))
           and second_changed and probed and meta2.get("delivery_mode") == "delta"
           and str(meta2.get("delta_object_count")) == "1" and [p for p, _, _ in delivered2] == ["pkg/f002.go"])
    record(g, "M2.delta-attestation-passes-and-the-next-delta-covers-only-newer-writes", "PASS" if ok2 else "FAIL",
           f"attestation={proof.get('model_attestation')} reliable={proof.get('model_full_cognition_reliable')} "
           f"mode={meta2.get('delivery_mode')} count={meta2.get('delta_object_count')} delivered={[p for p, _, _ in delivered2]}"[:200])
    proof2, _ = attest_delivery(s, fx, t2)
    after_second = meta2.get("index_sha256")
    # -- M3: without a probe pass the request answers the complete scope
    third_changed = land_write(fx, "f003.go", "revised constant provider three") == "applied"
    t3, err = text_of(s.call("aoci_overview", {"delta_since": after_second}))
    meta3, _ = overview_meta_and_full_body(t3)
    ok3 = (proof2.get("model_attestation") == "pass" and third_changed
           and meta3.get("delivery_mode") == "full" and meta3.get("fallback_reason") == "delta_probe_required")
    record(g, "M3.no-probe-pass-answers-the-complete-scope", "PASS" if ok3 else "FAIL",
           f"mode={meta3.get('delivery_mode')} reason={meta3.get('fallback_reason')}"[:200])
    # -- M4: a partial delta Attestation drops the base
    probed = pass_probe(s, fx)
    t4, err = text_of(s.call("aoci_overview", {"delta_since": after_second}))
    meta4, _ = overview_meta_and_full_body(t4)
    wrong, _ = attest_delivery(s, fx, t4, wrong=True)
    probed2 = pass_probe(s, fx)
    t5, err = text_of(s.call("aoci_overview", {"delta_since": after_second}))
    meta5, _ = overview_meta_and_full_body(t5)
    ok4 = (probed and meta4.get("delivery_mode") == "delta" and wrong.get("model_attestation") in ("partial", "fail")
           and probed2 and meta5.get("delivery_mode") == "full" and meta5.get("fallback_reason") == "delta_base_unknown")
    record(g, "M4.partial-delta-attestation-drops-the-base", "PASS" if ok4 else "FAIL",
           f"delta={meta4.get('delivery_mode')} wrong={wrong.get('model_attestation')} next={meta5.get('delivery_mode')} reason={meta5.get('fallback_reason')}"[:200])
    proof5, _ = attest_delivery(s, fx, t5)
    current = meta5.get("index_sha256")
    # -- M5: an unchanged base answers an empty delta without a proof
    probed = pass_probe(s, fx)
    t6, err = text_of(s.call("aoci_overview", {"delta_since": current}))
    meta6, body6 = overview_meta_and_full_body(t6)
    ok5 = (proof5.get("model_attestation") == "pass" and probed and meta6.get("delivery_mode") == "delta"
           and str(meta6.get("delta_object_count")) == "0" and truthy(meta6.get("completed"))
           and not body6 and "challenge_digest" not in t6 and truthy(meta6.get("model_full_cognition_reliable")))
    record(g, "M5.unchanged-base-answers-an-empty-delta", "PASS" if ok5 else "FAIL",
           f"mode={meta6.get('delivery_mode')} count={meta6.get('delta_object_count')} completed={meta6.get('completed')} body={bool(body6)}"[:200])
    s.close()
    # -- M6: a new Host session has no base: the request answers the complete scope
    s2 = Session(fx)
    probed = pass_probe(s2, fx)
    t7, err = text_of(s2.call("aoci_overview", {"delta_since": current}))
    meta7, _ = overview_meta_and_full_body(t7)
    s2.close()
    ok6 = probed and meta7.get("delivery_mode") == "full" and meta7.get("fallback_reason") == "delta_base_unknown"
    record(g, "M6.restart-has-no-base", "PASS" if ok6 else "FAIL",
           f"mode={meta7.get('delivery_mode')} reason={meta7.get('fallback_reason')}"[:200])

def group_n():
    """next_commands are complete, runnable commands that clear what they name;
    attested cognition under tree drift reads uncertain, never invalid."""
    g = "N"
    # -- N1: 终态证明命令是完整可执行命令, 且逐条真的执行通过
    fx = make_fixture("nextcmd", 6)
    s = Session(fx)
    m, t, err = maintain(s)
    a, tw, err = submit_batch(s, m)
    s.close()
    cmds = a.get("next_commands") or []
    shaped = (a.get("aligned") is True and len(cmds) == 3
              and " verify --json" in cmds[0] and " check --json" in cmds[1]
              and "index agent guide" in cmds[2]
              and all(c.startswith(("'", '"')) for c in cmds))
    record(g, "N1.final-apply-carries-proof-commands", "PASS" if shaped else "FAIL",
           f"aligned={a.get('aligned')} cmds={cmds[:3]}"[:200])
    ran = shaped
    for cmd in cmds if shaped else []:
        rc, out = run_returned_command(fx, cmd)
        if rc != 0:
            ran = False
            record(g, "N1.returned-command-failed", "FAIL", f"{cmd} rc={rc} {out[:120]}")
            break
    if shaped and ran:
        record(g, "N1b.every-returned-command-runs", "PASS",
               "verify, aggregate check, and guide all exited 0 as returned")
    elif shaped:
        record(g, "N1b.every-returned-command-runs", "FAIL", "see N1.returned-command-failed")
    else:
        record(g, "N1b.every-returned-command-runs", "FAIL", "no commands to run")

    # -- N2: observed_pending 阻断携带 acknowledge 命令, 执行它即解除阻断
    d = os.path.join(WORK, "nextcmd-observe")
    shutil.rmtree(d, ignore_errors=True)
    os.makedirs(os.path.join(d, "pkg"))
    for i in range(1, 5):
        with open(os.path.join(d, "pkg", f"f{i:03}.go"), "w") as f:
            f.write(f"package pkg\n\nfunc F{i:03}() int {{ return {i} }}\n")
    with open(os.path.join(d, "pkg", "f001_test.go"), "w") as f:
        f.write("package pkg\n\nimport \"testing\"\n\nfunc TestF001(t *testing.T) { _ = F001() }\n")
    sh(d, "git", "init", "-q")
    sh(d, "git", "config", "user.email", "fixture@test.invalid")
    sh(d, "git", "config", "user.name", "fixture")
    sh(d, "git", "add", "-A")
    sh(d, "git", "commit", "-q", "-m", "fixture")
    rc, _, out, errs = cli(d, "init", "--locale", "en-US")
    rc, _, out, errs = cli(d, "scan")
    s = Session(d)
    m, t, err = maintain(s)
    a, tw, err = submit_batch(s, m)
    with open(os.path.join(d, "pkg", "f001_test.go"), "a") as f:
        f.write("\n// observed drift\n")
    with open(os.path.join(d, "pkg", "added.go"), "w") as f:
        f.write("package pkg\n\nfunc Added() int { return 5 }\n")
    m2, t2, err = maintain(s)
    s.close()
    cmds = m2.get("next_commands") or []
    ack = next((c for c in cmds if "scope acknowledge" in c), "")
    shaped = (m2.get("result") == "blocked"
              and m2.get("next_action") == "review_observed_evidence"
              and m2.get("authoring_batch", {}).get("next_action") == m2.get("next_action")
              and len(m2.get("instructions", [])) == 4
              and 0 <= t2.find('"instructions"') < t2.find('"governance"')
              and 0 <= t2.find('"next_commands"') < t2.find('"governance"')
              and any("index agent guide" in c for c in cmds)
              and ack != "" and "{agent}" in ack)
    record(g, "N2.blocked-carries-acknowledge-command", "PASS" if shaped else "FAIL",
           f"result={m2.get('result')} cmds={cmds}"[:200])
    if shaped:
        # Review the synthetic observe change and its production dependency before
        # acknowledging. The added source must still need authoring afterwards.
        rc_status, status, _, _ = cli(d, "scope", "status", "--json")
        rc_guide, guide, guide_wire, _ = cli(d, "index", "agent", "guide", "--agent", "codex", "--json")
        reviewed = (rc_status == 0 and status.get("observed_pending_review") == 1
                    and rc_guide == 0 and guide.get("next_action") == "review_observed_evidence"
                    and guide.get("instructions")[:4] == m2.get("instructions")
                    and 0 <= guide_wire.find('"instructions"') < guide_wire.find('"findings"')
                    and 0 <= guide_wire.find('"stop"') < guide_wire.find('"findings"'))
        rc, out = run_returned_command(d, ack)
        rc_after, after_guide, _, _ = cli(d, "index", "agent", "guide", "--agent", "codex", "--json")
        s = Session(d)
        m3, _, _ = maintain(s)
        authored = False
        if m3.get("candidates"):
            applied, _, apply_err = submit_batch(s, m3)
            authored = not apply_err and applied.get("aligned") is True
        s.close()
        proof = all(cli(d, *args)[0] == 0 for args in (
            ("verify", "--json"), ("check", "--json"),
            ("index", "agent", "guide", "--agent", "codex", "--json")))
        cleared = (reviewed and rc == 0 and rc_after == 0
                   and after_guide.get("stage") == "authoring_required"
                   and m3.get("result") == "authoring_required" and authored and proof)
        record(g, "N2b.acknowledge-command-clears-the-block", "PASS" if cleared else "FAIL",
               f"rc={rc} reviewed={reviewed} after={m3.get('result')} authored={authored} proof={proof}")
    else:
        record(g, "N2b.acknowledge-command-clears-the-block", "FAIL", "blocked shape missing")

    # -- N3: 已认证的认知在树漂移下保持 uncertain, 不再被降级为 invalid
    s = Session(fx)
    t, err = text_of(s.call("aoci_overview"))
    meta, body = overview_meta_and_full_body(t)
    guard = 0
    while meta.get("continuation_required") is True and guard < 8:
        guard += 1
        t, err = text_of(s.call("aoci_overview", {"cursor": meta.get("next_cursor")}))
        m2, b2 = overview_meta_and_full_body(t)
        meta = {**meta, **m2}
        body = (body or "") + (b2 or "")
    ordinals = meta.get("challenge_ordinals") or []
    if isinstance(ordinals, str):
        ordinals = [int(x) for x in ordinals.split(",") if x.strip()]
    entries = parse_body_entries(body, fx)
    answers = []
    for o in ordinals:
        path, tag, core_f = entries[o - 1]
        answers.append({"ordinal": o, "object_identity": path, "tag": tag, "core_f": core_f})
    att = {"version": "model-cognition-attestation/v1",
           "index_sha256": meta.get("challenge_index_sha256"),
           "entry_sequence_sha256": meta.get("challenge_entry_sequence_sha256"),
           "entry_count": meta.get("challenge_entry_count"),
           "challenge_digest": meta.get("challenge_digest"),
           "reported_entry_count": meta.get("entry_count"),
           "reported_estimated_tokens": meta.get("estimated_tokens"),
           "coverage_percent": 100, "system_mastery_percent": 80, "confidence_percent": 90,
           "truncation_detected": False, "unseen_sections": [], "uncertainty_reasons": [],
           "challenge_answers": answers}
    confirm = {"version": "overview-delivery-receipt/v1",
               "body_sha256": meta.get("body_sha256"),
               "body_bytes": meta.get("body_utf8_bytes"),
               "end_marker_observed": True}
    t, err = text_of(s.call("aoci_overview", {
        "host_delivery_confirmation": confirm, "model_cognition_attestation": att}))
    attested = "model_attestation: pass" in t and "cognition_state: valid" in t
    record(g, "N3.harness-attestation-passes", "PASS" if attested else "FAIL", t[:200])
    # 一个漂移文件低于语义阈值(30), 不构成 refresh 待决 —— 要让刷新真正挂起,
    # 宿主声明 phase_transition 且树不稳定, 状态机走到 deferred_until_stable。
    # 修复前这里回 invalid: 身份匹配+认证通过的回执被"未settled"降级成"无认知"。
    with open(os.path.join(fx, "pkg", "f001.go"), "a") as f:
        f.write("\n// semantic drift after attestation\n")
    t, err = text_of(s.call("aoci_overview", {"check_only": True,
        "refresh_reasons": ["phase_transition"], "refresh_event_id": "n3-drift-1"}))
    s.close()
    res = jload(t)
    assessment = res.get("assessment") or {}
    receipt = res.get("cognition_receipt") or assessment.get("cognition_receipt") or {}
    if not receipt:
        receipt = (res.get("assessment") or {}).get("receipt") or {}
    drifted = assessment.get("refresh_status") == "refresh_deferred_until_stable"
    state = assessment.get("state") or receipt.get("cognition_state")
    ok = attested and drifted and state == "uncertain"
    record(g, "N3b.drift-keeps-attested-cognition-uncertain-not-invalid",
           "PASS" if ok else "FAIL",
           "attested receipt reads uncertain under deferred refresh" if ok else
           f"attested={attested} refresh_status={assessment.get('refresh_status')} state={state}")

def group_t():
    """A TTY confirmation must be readable before the operator has to answer it.

    The prompt carries the exact phrase they are required to type, and the
    library entry point buffers both cobra writers until Execute returns. That
    held the prompt until after the command had already failed, so operators
    typed blind or gave up. Unit tests inject the prompt writer, which proves the
    prompt reaches *a* writer; only a real process on a real terminal proves it
    reaches the operator before the read blocks, which is why this lives here.
    """
    g = "T"
    if not hasattr(os, "openpty"):
        record(g, "T1.confirmation-prompt-precedes-the-read", "CHAR", "no pty on this platform")
        return
    import pty

    d = os.path.join(WORK, "fx-tty")
    shutil.rmtree(d, ignore_errors=True)
    os.makedirs(os.path.join(d, "pkg"))
    with open(os.path.join(d, "pkg", "a.go"), "w") as f:
        f.write("package pkg\n\nfunc A() int { return 1 }\n")
    sh(d, "git", "init", "-q")
    sh(d, "git", "config", "user.email", "fixture@test.invalid")
    sh(d, "git", "config", "user.name", "fixture")
    sh(d, "git", "add", "-A")
    sh(d, "git", "commit", "-q", "-m", "fixture")
    for args in (("init", "--locale", "en-US"), ("scan",),
                 ("config", "set", "automation_mode", "review")):
        rc, _, out, errs = cli(d, *args)
        if rc != 0:
            record(g, "T1.confirmation-prompt-precedes-the-read", "FAIL",
                   f"fixture setup failed at {args[0]}: {(out + errs)[:150]}")
            return

    # Under review every plan demands a human, so the smallest candidate set
    # produces a preview that reaches the prompt.
    candidate_path = os.path.join(d, "candidates.json")
    with open(candidate_path, "w", encoding="utf-8") as fh:
        json.dump({"version": "managed-scope-candidate-set/v1", "entries": [], "dispositions": []}, fh)
    rc, preview, out, errs = cli(d, "scope", "preview", "--candidate-file", candidate_path)
    plan = (preview or {}).get("plan") or {}
    phrase = plan.get("confirmation_phrase") or ""
    if rc != 0 or not plan.get("interaction_required") or not phrase:
        record(g, "T1.confirmation-prompt-precedes-the-read", "FAIL",
               f"fixture did not produce a preview demanding a human: rc={rc} {(out + errs)[:130]}")
        return
    preview_path = os.path.join(d, "preview.json")
    with open(preview_path, "w", encoding="utf-8") as fh:
        json.dump(preview, fh)

    master, slave = pty.openpty()
    process = subprocess.Popen(
        [BIN, "scope", "approve", "--preview-file", preview_path, "--actor", "scenario"],
        cwd=d, stdin=slave, stdout=slave, stderr=slave, close_fds=True)
    os.close(slave)
    # Read for a bounded window WITHOUT writing anything. Whatever arrives here is
    # exactly what a human would have on screen when deciding what to type.
    deadline, before = time.time() + 5.0, b""
    while time.time() < deadline:
        readable, _, _ = select.select([master], [], [], 0.2)
        if not readable:
            continue
        try:
            chunk = os.read(master, 65536)
        except OSError:
            break
        if not chunk:
            break
        before += chunk
        if phrase.encode() in before:
            break
    visible = phrase in before.decode("utf-8", errors="replace")
    try:
        os.write(master, b"WRONG PHRASE\n")
    except OSError:
        pass
    try:
        process.wait(timeout=15)
    except Exception:
        process.kill()
    try:
        os.close(master)
    except OSError:
        pass
    record(g, "T1.confirmation-prompt-precedes-the-read", "PASS" if visible else "FAIL",
           f"bytes_before_input={len(before)} phrase_visible={visible}")


# ---------------------------------------------------------------- documentation binding
def group_p_unspellable_directory():
    """A directory the index cannot record is stopped before anyone authors it.

    A directory whose name ends with a space has no section header that reads back
    to the same path, and no edit to an Entry can change that. The update path used
    to answer repair_required with a retry scope, and Maintain kept issuing the same
    batch, so a host that follows next_action re-authored it forever. Maintain now
    withholds the batch and answers stopped with stop facts that quote the directory
    and give the operator's way out; a caller that submits the path anyway gets the
    same answer; and renaming the directory, the first way out named, clears it.
    """
    g = "P"
    name = "P2.unspellable-directory-is-withheld-before-authoring"
    d = os.path.join(WORK, "fx-unspellable")
    shutil.rmtree(d, ignore_errors=True)
    try:
        os.makedirs(os.path.join(d, "trail "))
        os.makedirs(os.path.join(d, "good"))
        held = os.path.isdir(os.path.join(d, "trail ")) and "trail " in os.listdir(d)
    except OSError:
        held = False
    if not held:
        record(g, name, "CHAR", "this filesystem cannot hold a directory whose name ends with a space")
        return
    for rel in ("trail /x001.go", "good/y002.go"):
        with open(os.path.join(d, *rel.split("/")), "w") as fh:
            fh.write("package fixture\n\nfunc F() int { return 1 }\n")
    sh(d, "git", "init", "-q")
    sh(d, "git", "config", "user.email", "fixture@test.invalid")
    sh(d, "git", "config", "user.name", "fixture")
    sh(d, "git", "add", "-A")
    sh(d, "git", "commit", "-q", "-m", "fixture")
    rc, _, out, errs = cli(d, "init", "--locale", "en-US")
    if rc == 0:
        rc, _, out, errs = cli(d, "scan")
    if rc != 0:
        record(g, name, "FAIL", f"init/scan failed: {(out + errs)[:160]}")
        return
    with open(os.path.join(d, "aoci.code.txt"), "rb") as fh:
        before = fh.read()
    s = Session(d)
    try:
        def withheld(m):
            stop = m.get("stop") or {}
            return (m.get("status") == "stopped" and m.get("next_action") == "resolve_unspellable_directory"
                    and not m.get("candidates") and not m.get("code_plan")
                    and stop.get("rule_code") == "code_directory_unspellable" and '"trail "' in (stop.get("actual") or "")
                    and "aoci_maintain" in (stop.get("safe_next_action") or ""))
        m1, t1, _ = maintain(s)
        m2, _, _ = maintain(s)  # a host that simply asks again gets the same stop, not a batch
        with open(os.path.join(d, "trail ", "x001.go"), "rb") as fh:
            sha = hashlib.sha256(fh.read()).hexdigest()
        direct, _ = text_of(s.call("aoci_update_entry", {"entries": [
            {"path": "trail /x001.go", "source_sha256": sha, "new_entry": entry_line("trail /x001.go")}]}))
        refused = jload(direct)
        with open(os.path.join(d, "aoci.code.txt"), "rb") as fh:
            untouched = fh.read() == before
        os.rename(os.path.join(d, "trail "), os.path.join(d, "trail"))
        aligned, last = False, ""
        for _ in range(4):
            m, t, _ = maintain(s)
            if m.get("aligned"):
                aligned = True
                break
            if m.get("status") != "repair_required":
                last = t[:200]
                break
            res, t2, _ = submit_batch(s, m)
            last = t2[:200]
            if res.get("status") != "applied":
                break
    finally:
        s.close()
    stopped = (refused.get("status") == "stopped" and refused.get("retry_scope") == [] and refused.get("applied") == 0
               and (refused.get("stop") or {}).get("rule_code") == "code_directory_unspellable")
    ok = withheld(m1) and withheld(m2) and stopped and untouched and aligned
    record(g, name, "PASS" if ok else "FAIL",
           f"maintain_withheld={withheld(m1)} again={withheld(m2)} direct_update_stopped={stopped} "
           f"volume_untouched={untouched} aligned_after_rename={aligned}" + ("" if ok else f" | {t1[:160]} | {last}"))


def verify_published_scenario_count(total):
    """Return a list of drift descriptions; empty means every document agrees."""
    patterns = {
        "README.md": r"\*\*Fault-injection scenarios\*\* — (\d+) scenarios",
        "README.zh-CN.md": r"\*\*故障注入场景\*\* —— (\d+) 个场景",
        os.path.join("scripts", "blackbox", "README.md"): r"Fault-injection scenarios \((\d+) scenarios",
    }
    drift = []
    for rel, pattern in patterns.items():
        try:
            with open(os.path.join(REAL, rel), encoding="utf-8") as fh:
                match = re.search(pattern, fh.read())
        except OSError as err:
            drift.append(f"{rel}: unreadable ({err})")
            continue
        if match is None:
            drift.append(f"{rel}: no documented scenario count found")
        elif int(match.group(1)) != total:
            drift.append(f"{rel}: documents {match.group(1)}, this run has {total}")
    return drift




# ---------------------------------------------------------------- group O
def group_o_optimization():
    """A completed cognition-optimization checkpoint must not close the direct update path.

    Optimization clears the checkpoint's current batch id when it completes and
    leaves the file in place for good. Until #57 the update classifier matched
    a batch-less item against that empty id, so every later direct update
    (path + source_sha256, the documented compatibility path; the same for an
    existing Database object) was refused as cognition_optimization_batch_mixed
    with zero writes. Maintain-issued batches carry ids and never noticed, which
    is why the suites never noticed either. This walks the exact sequence on
    the binary under test: direct update works, optimization completes, the
    same direct update still works.
    """
    g = "O"
    name = "O1.direct-update-after-completed-optimization-still-applies"
    d = make_fixture("fx-optimization", 3)
    s = Session(d)
    try:
        m, t, err = maintain(s)
        rounds = 0
        while m.get("status") == "repair_required" and rounds < 5:
            res, t2, err2 = submit_batch(s, m)
            if res.get("status") != "applied":
                record(g, name, "FAIL", f"authoring failed: {t2[:160]}")
                return
            if res.get("aligned"):
                break
            m, t, err = maintain(s)
            rounds += 1
        target = "pkg/f001.go"
        with open(os.path.join(d, target), "rb") as fh:
            source_sha = hashlib.sha256(fh.read()).hexdigest()

        def direct(text):
            body, err3 = text_of(s.call("aoci_update_entry", {"entries": [
                {"path": target, "source_sha256": source_sha, "new_entry": text}]}))
            return jload(body) or {}, body

        before, tb = direct(entry_line(target).replace("unit 1", "unit one"))
        if before.get("status") != "applied":
            record(g, name, "FAIL", f"direct update must work before optimization: {tb[:160]}")
            return
        opt_text, _ = text_of(s.call("aoci_maintain", {"intent": "cognition_optimization"}))
        opt = jload(opt_text) or {}
        cands = opt.get("candidates") or []
        plan = opt.get("code_plan") or {}
        if not cands or not plan.get("batch_id"):
            record(g, name, "FAIL", f"optimization planned nothing: {opt_text[:160]}")
            return
        done_text, _ = text_of(s.call("aoci_update_entry", {"code_batch_id": plan["batch_id"], "entries": [
            {"path": c["path"], "source_sha256": c["source_sha256"], "candidate_id": c["candidate_id"],
             "new_entry": c.get("existing_entry")} for c in cands]}, timeout=300))
        done = jload(done_text) or {}
        state = (done.get("optimization") or {}).get("state")
        checkpoint = {}
        try:
            with open(os.path.join(d, ".aoci", "drafts", "code-cognition", "optimization-active.json"), encoding="utf-8") as fh:
                checkpoint = json.load(fh)
        except Exception:  # noqa: BLE001 - reported through the verdict below
            pass
        if state != "complete" or not checkpoint.get("completed") or checkpoint.get("current_batch_id"):
            record(g, name, "FAIL", f"optimization did not complete: state={state} "
                   f"checkpoint={ {k: checkpoint.get(k) for k in ('completed', 'current_batch_id')} } | {done_text[:120]}")
            return
        with open(os.path.join(d, "aoci.code.txt"), "rb") as fh:
            volume_before = fh.read()
        after, ta = direct(entry_line(target).replace("unit 1", "unit one, revised"))
        with open(os.path.join(d, "aoci.code.txt"), "rb") as fh:
            volume_after = fh.read()
        ok = (after.get("status") == "applied" and after.get("applied") == 1
              and volume_after != volume_before and "batch_mixed" not in ta)
        record(g, name, "PASS" if ok else "FAIL",
               f"status={after.get('status')} applied={after.get('applied')} volume_changed={volume_after != volume_before}"
               + ("" if ok else f" | {ta[:150]}"))
    finally:
        s.close()



def group_o_s_absent_priority():
    """A high-importance Entry with S:- is reviewed first, with its reason named.

    A RuoYi build under Codex left S:- on two thirds of its C7-C9 Entries and
    nothing surfaced them: the selector ordered by budget pressure and C only,
    so an empty S at C8 sat behind every filled C9 Entry. Budget pressure still
    comes first, then C>=7 Entries without S, then everything else by C; the
    candidate names the reason and the instructions say that returning the
    Entry unchanged is a valid outcome. Since rc20 an Entry whose E scale
    letter its line count contradicts (#103) ranks right after those, still
    ahead of C order, with reason e_scale_mismatch. This authors a 3-file
    fixture, rewrites one Entry to C8 with S:-, one to C9 with S filled, and
    one to M at three lines through the direct path, then checks the
    optimization batch order, reasons, instruction, and that the unchanged
    batch still completes.
    """
    g = "O"
    name = "O2.high-importance-entry-without-s-is-reviewed-first"
    d = make_fixture("fx-optimization-s", 3)
    s = Session(d)
    try:
        m, t, err = maintain(s)
        rounds = 0
        while m.get("status") == "repair_required" and rounds < 5:
            res, t2, err2 = submit_batch(s, m)
            if res.get("status") != "applied":
                record(g, name, "FAIL", f"authoring failed: {t2[:160]}")
                return
            if res.get("aligned"):
                break
            m, t, err = maintain(s)
            rounds += 1

        def sha(rel):
            with open(os.path.join(d, rel), "rb") as fh:
                return hashlib.sha256(fh.read()).hexdigest()

        def direct(rel, text):
            body, _ = text_of(s.call("aoci_update_entry", {"entries": [
                {"path": rel, "source_sha256": sha(rel), "new_entry": text}]}))
            return jload(body) or {}, body

        absent, ta = direct("pkg/f001.go", "f001.go[CG8T]: F:Provides fixture constant unit 1 | R:- | A:- | S:-")
        filled, tf = direct("pkg/f002.go", "f002.go[CG9T]: F:Provides fixture constant unit 2 | R:- | A:- | S:Callers must not cache the value across restarts")
        wrong, tw = direct("pkg/f003.go", "f003.go[CG5M]: F:Provides fixture constant unit 3 | R:- | A:- | S:-")
        if absent.get("status") != "applied" or filled.get("status") != "applied" or wrong.get("status") != "applied":
            record(g, name, "FAIL", f"direct rewrites failed: {ta[:120]} | {tf[:120]} | {tw[:120]}")
            return
        opt_text, _ = text_of(s.call("aoci_maintain", {"intent": "cognition_optimization"}))
        opt = jload(opt_text) or {}
        cands = opt.get("candidates") or []
        plan = opt.get("code_plan") or {}
        order = [c.get("path") for c in cands]
        reasons = {c.get("path"): c.get("selection_reason") for c in cands}
        instructed = "s_absent_high_importance" in " ".join(opt.get("instructions") or [])
        # The fixture also carries the init-generated AGENTS.md and .gitattributes
        # Entries at C5, so only the structure of the order is pinned: the C8
        # Entry without S first; then exactly the Entries whose E letter the
        # line count contradicts (f003 tagged M at three lines, and the template
        # tags the init-generated AGENTS.md T); then the filled C9 one; then
        # everything else by C and size.
        others_flagged = [pth for pth, why in reasons.items() if pth != "pkg/f001.go" and why == "s_absent_high_importance"]
        scale_flagged = sorted(pth for pth, why in reasons.items() if why == "e_scale_mismatch")
        f002_at = order.index("pkg/f002.go") if "pkg/f002.go" in order else -1
        between = sorted(order[1:f002_at]) if f002_at > 0 else None
        if (order[:1] != ["pkg/f001.go"] or reasons.get("pkg/f001.go") != "s_absent_high_importance"
                or "pkg/f003.go" not in scale_flagged or between != scale_flagged
                or others_flagged or not instructed or not plan.get("batch_id")):
            record(g, name, "FAIL", f"order={order} reasons={reasons} instructed={instructed} | {opt_text[:160]}")
            return
        done_text, _ = text_of(s.call("aoci_update_entry", {"code_batch_id": plan["batch_id"], "entries": [
            {"path": c["path"], "source_sha256": c["source_sha256"], "candidate_id": c["candidate_id"],
             "new_entry": c.get("existing_entry")} for c in cands]}, timeout=300))
        done = jload(done_text) or {}
        state = (done.get("optimization") or {}).get("state")
        ok = state == "complete"
        record(g, name, "PASS" if ok else "FAIL",
               f"order={order} reason(f001)={reasons.get('pkg/f001.go')} instructed={instructed} "
               f"unchanged_batch_status={done.get('status')} state={state}" + ("" if ok else f" | {done_text[:150]}"))
    finally:
        s.close()


# ---------------------------------------------------------------- group P
def group_p_special_names():
    """Directory and file names the section grammar could not spell (#58, #60).

    The directory path in a section header stopped at the first whitespace, "=",
    or "(", and a file name could not hold "[". A repository under a path with a
    space got truncated roots in every later section; a directory with a space
    made its Entry resolve somewhere else, so the same file was reported orphan
    and missing for good; and one Next.js-style [...id] file failed its whole
    atomic batch forever. This fixture lives under a root with a space and
    carries one source of each kind, and must author to alignment through
    ordinary batches with natural names in the Volume; a copy of it at another
    path must then verify aligned too.
    """
    g = "P"
    name = "P1.special-directory-and-file-names-author-to-alignment"
    d = os.path.join(WORK, "fx special names")
    shutil.rmtree(d, ignore_errors=True)
    sources = ["src/deep dir/b001.go", "app/(home)/page002.go", "pkg/max=/c003.go", "pages/docs/[...id]004.go"]
    for rel in sources:
        target = os.path.join(d, *rel.split("/"))
        os.makedirs(os.path.dirname(target), exist_ok=True)
        with open(target, "w") as fh:
            fh.write("package fixture\n\nfunc F() int { return 1 }\n")
    sh(d, "git", "init", "-q")
    sh(d, "git", "config", "user.email", "fixture@test.invalid")
    sh(d, "git", "config", "user.name", "fixture")
    sh(d, "git", "add", "-A")
    sh(d, "git", "commit", "-q", "-m", "fixture")
    rc, _, out, errs = cli(d, "init", "--locale", "en-US")
    if rc == 0:
        rc, _, out, errs = cli(d, "scan")
    if rc != 0:
        record(g, name, "FAIL", f"init/scan failed: {(out + errs)[:160]}")
        return
    s = Session(d)
    try:
        aligned, last = False, ""
        for _ in range(5):
            m, t, err = maintain(s)
            if m.get("aligned"):
                aligned = True
                break
            if m.get("status") != "repair_required":
                last = t[:200]
                break
            res, t2, err2 = submit_batch(s, m)
            last = t2[:200]
            if res.get("status") != "applied":
                break
        body, _ = text_of(s.call("aoci_get_entries", {"paths": sources}))
    finally:
        s.close()
    rc, v, _, _ = cli(d, "verify", expect_ok=False)
    verified = rc == 0 and bool((v.get("governance") or {}).get("governance_aligned") or v.get("governance_aligned"))
    with open(os.path.join(d, "aoci.code.txt"), encoding="utf-8") as fh:
        volume = fh.read()
    # Directory names are written as they are, under the neutral root a new index
    # records: the root section first, every later section continuing it, and the
    # work directory, whose name holds a space, appearing nowhere in the file.
    root = d.replace("\\", "/")
    headers = (f"==={NEUTRAL_ROOT}/===" in volume and root not in volume
               and all(f"==={NEUTRAL_ROOT}/{sub}/===" in volume for sub in ("src/deep dir", "app/(home)", "pkg/max=", "pages/docs")))
    resolved = all(f"object_ref=code:{rel}]" in body for rel in sources) and "Not indexed" not in body
    # A clone or a CI checkout has the same bytes under another absolute root, and
    # every header above records this one. The copy must verify aligned as well.
    moved = os.path.join(WORK, "fx-special-names-checkout")
    shutil.rmtree(moved, ignore_errors=True)
    shutil.copytree(d, moved)
    rc2, v2, _, _ = cli(moved, "verify", expect_ok=False)
    relocated = rc2 == 0 and bool((v2.get("governance") or {}).get("governance_aligned") or v2.get("governance_aligned"))
    ok = aligned and verified and headers and resolved and relocated
    record(g, name, "PASS" if ok else "FAIL",
           f"aligned={aligned} verify={verified} natural_headers={headers} entries_resolve={resolved} "
           f"checkout_verify={relocated}" + ("" if ok else f" | {last}"))


# ---------------------------------------------------------------- group U
# The local status page runs in its own process and reads the same repository a
# live MCP server is governing. The claim it makes about itself is that it can
# never disturb that: no lock, no formal write, no audit event. A claim like
# that is only worth what a cross-process run proves, so this group runs the
# page against a fixture while an MCP session drives a complete governance
# cycle through it, and compares the repository byte for byte.
def repo_digest(root):
    """Every repository-relative path mapped to its content digest."""
    digests = {}
    for base, dirs, files in os.walk(root):
        for name in files:
            path = os.path.join(base, name)
            try:
                with open(path, "rb") as fh:
                    digests[os.path.relpath(path, root).replace("\\", "/")] = hashlib.sha256(fh.read()).hexdigest()
            except OSError:
                digests[os.path.relpath(path, root).replace("\\", "/")] = "unreadable"
    return digests

def start_ui(repo):
    """Start a page and return (process, base_url).

    The port is chosen by the kernel and read back from the page's readiness
    line rather than guessed: a guessed port sits inside the ephemeral range
    (44620-48715 on a typical Linux runner), where an unrelated outbound
    connection can take it between the moment the test picks it and the moment
    the page binds it. Streams go to files, not pipes, because nothing here
    drains a pipe and a full one blocks the page.
    """
    stem = os.path.join(WORK, "ui-%d-%d" % (os.getpid(), random.randrange(1 << 30)))
    out_path, err_path = stem + ".out", stem + ".err"
    out_file, err_file = open(out_path, "w", encoding="utf-8"), open(err_path, "w", encoding="utf-8")
    try:
        process = subprocess.Popen([BIN, "ui", "--repo", repo, "--port", "0", "--discover=false", "--json"],
                                   stdout=out_file, stderr=err_file, text=True, encoding="utf-8", errors="replace")
    finally:
        out_file.close()
        err_file.close()
    process.aoci_stdout, process.aoci_stderr = out_path, err_path
    deadline, url = time.time() + 30, ""
    while time.time() < deadline:
        try:
            with open(out_path, encoding="utf-8") as handle:
                first = handle.readline()
        except OSError:
            first = ""
        if first.strip().startswith("{"):
            try:
                url = json.loads(first).get("url", "")
            except ValueError:
                url = ""
            if url:
                break
        if process.poll() is not None:
            break
        time.sleep(0.1)
    return process, url

def ui_failure(process):
    """Why a page did not come up: its own stderr, else its stdout envelope."""
    for path in (getattr(process, "aoci_stderr", ""), getattr(process, "aoci_stdout", "")):
        try:
            text = open(path, encoding="utf-8", errors="replace").read().strip()
        except OSError:
            text = ""
        if text:
            return " ".join(text.split())[:200]
    return "no output; exit=%s" % process.poll()

def stop_ui(process):
    process.terminate()
    try:
        process.wait(timeout=10)
    except Exception:
        process.kill()

def urlopen_retry(target, timeout, attempts=3):
    """A page that is up can still miss one connection on a loaded runner (a
    urlopen timeout crashed group U once on the 2026-10-29 merge run): a timed
    out or refused connection is retried, an HTTP status is an answer and is
    raised at once."""
    for attempt in range(attempts):
        try:
            return urllib.request.urlopen(target, timeout=timeout)
        except urllib.error.HTTPError:
            raise
        except (urllib.error.URLError, TimeoutError, OSError):
            if attempt == attempts - 1:
                raise
            time.sleep(1)

def ui_get(base, path):
    address, fragment = urllib.parse.urldefrag(base)
    token = urllib.parse.parse_qs(fragment).get("access_token", [""])[0]
    request = urllib.request.Request(address.rstrip("/") + path,
                                     headers={"Authorization": "Bearer " + token})
    with urlopen_retry(request, 10) as response:
        return response.status, response.read().decode("utf-8")

def group_u():
    """The local status page serves verify's facts, never disturbs a live
    governance cycle, and changes no byte of the repository it reads."""
    g = "U"
    fx = make_fixture("ui-nointerfere", 5)
    page, base = start_ui(fx)
    if not base:
        for name in ("U1.page-serves-the-same-facts-as-verify",
                     "U2.governance-cycle-is-unaffected-by-the-page",
                     "U3.page-traffic-changes-no-byte"):
            record(g, name, "FAIL", "page did not start: " + ui_failure(page))
        stop_ui(page)
        return
    try:
        # -- U1: the page reports the assessment verify reports. verify
        # publishes it under "governance"; agreement must be field for field,
        # because a second implementation would drift silently.
        _, state = ui_get(base, "/api/state?repo=" + urllib.parse.quote(fx))
        snapshot = json.loads(state)
        _, report, _, _ = cli(fx, "verify", expect_ok=False)
        facts = snapshot.get("facts") or {}
        governance = report.get("governance") or {}
        compared = ("result", "governance_aligned", "structure_valid", "composite_identity",
                    "code_entry_count", "code_source_count", "next_required_action")
        differing = [k for k in compared if facts.get(k) != governance.get(k)]
        same = bool(governance) and not differing and snapshot.get("composite_identity") == report.get("composite_identity")
        # The readiness fragment authorises this client, never an anonymous
        # request from another local process. Keep the normal facts check too.
        try:
            with urlopen_retry(urllib.parse.urldefrag(base)[0] + "api/repos", 3):
                same = False
        except urllib.error.HTTPError as error:
            same = same and error.code == 401
        record(g, "U1.page-serves-the-same-facts-as-verify", "PASS" if same else "FAIL",
               f"agree on {len(compared)} fields, result={facts.get('result')}" if same
               else f"differ: {differing} page={facts.get('result')} verify={governance.get('result')}"[:200])

        # -- U2: a complete Maintain/Apply cycle through MCP, with the page
        # polling throughout, still reaches aligned with every candidate applied.
        session = Session(fx)
        maintained, _, _ = maintain(session)
        for _ in range(3):
            ui_get(base, "/api/state?repo=" + urllib.parse.quote(fx))
            ui_get(base, "/api/entries?repo=" + urllib.parse.quote(fx))
        applied, _, _ = submit_batch(session, maintained)
        for _ in range(3):
            ui_get(base, "/api/state?repo=" + urllib.parse.quote(fx))
        session.close()
        clean = (applied.get("status") == "applied" and applied.get("aligned") is True
                 and applied.get("finding_count") == 0
                 and applied.get("applied") == applied.get("attempted") and applied.get("attempted") > 0)
        record(g, "U2.governance-cycle-is-unaffected-by-the-page", "PASS" if clean else "FAIL",
               f"status={applied.get('status')} aligned={applied.get('aligned')} applied={applied.get('applied')}/{applied.get('attempted')}"[:200])
    finally:
        stop_ui(page)

    # -- U3: page traffic alone changes nothing. The baseline digest is taken on
    # a repository the page has never been pointed at, because a write with
    # constant content is idempotent: sampling after the page has already run
    # would compare the damage against itself and pass. Verified against a
    # binary whose state handler writes one fixed file.
    quiet = make_fixture("ui-readonly", 4)
    session = Session(quiet)
    submit_batch(session, maintain(session)[0])
    session.close()
    before = repo_digest(quiet)
    quiet_page, quiet_base = start_ui(quiet)
    if not quiet_base:
        record(g, "U3.page-traffic-changes-no-byte", "FAIL", "page did not start: " + ui_failure(quiet_page))
        stop_ui(quiet_page)
        return
    try:
        for _ in range(8):
            for path in ("/", "/api/repos", "/api/state?repo=" + urllib.parse.quote(quiet),
                         "/api/entries?repo=" + urllib.parse.quote(quiet),
                         "/api/raw?repo=" + urllib.parse.quote(quiet) + "&asset=root",
                         "/api/raw?repo=" + urllib.parse.quote(quiet) + "&asset=meta"):
                ui_get(quiet_base, path)
    finally:
        stop_ui(quiet_page)
    after = repo_digest(quiet)
    changed = sorted(set(before) ^ set(after)) + sorted(
        k for k in set(before) & set(after) if before[k] != after[k])
    record(g, "U3.page-traffic-changes-no-byte", "PASS" if not changed else "FAIL",
           "no file changed while only serving the page" if not changed
           else "changed: " + ", ".join(changed[:6]))


def group_u_detach():
    """U4: a page started with --detach outlives the shell that started it,
    a second --detach reuses it instead of starting another, and --stop ends
    it. The registry is pointed at the work dir so a developer's own panels
    are never touched. This is the property the README's onboarding prompt
    relies on: an agent's tool shell ends when its command does."""
    g = "U"
    fx = make_fixture("ui-detach", 5)
    env = dict(os.environ, AOCI_UI_REGISTRY_DIR=os.path.join(WORK, "ui-registry"))
    want = os.path.normcase(os.path.normpath(fx))

    def run(*extra):
        p = subprocess.run([BIN, "ui", "--repo", fx, "--discover=false", "--json", *extra],
                           capture_output=True, text=True, encoding="utf-8", errors="replace", timeout=60, env=env)
        out = p.stdout.strip()
        try:
            payload = json.loads(out) if out.startswith("{") else {}
        except ValueError:
            payload = {}
        return p.returncode, payload, (p.stderr.strip() or out)[:200]

    def answers(url):
        try:
            _, body = ui_get(url, "/api/repos")
            return any(os.path.normcase(os.path.normpath(item.get("root", ""))) == want
                       for item in json.loads(body))
        except Exception:
            return False

    pid, gone = 0, False
    try:
        rc, first, err = run("--detach")
        url, pid = first.get("url", ""), int(first.get("pid", 0) or 0)
        started = rc == 0 and first.get("detached") is True and pid > 0 and bool(url)
        alive = started and answers(url)        # the starting shell has already returned
        rc2, second, _ = run("--detach")
        reused = rc2 == 0 and second.get("reused") is True and second.get("url") == url
        rc3, third, err3 = run("--stop")
        stopped = rc3 == 0 and third.get("stopped") is True
        for _ in range(50):
            if not answers(url):
                gone = True
                break
            time.sleep(0.1)
        ok = started and alive and reused and stopped and gone
        record(g, "U4.detached-page-outlives-shell-reuses-and-stops", "PASS" if ok else "FAIL",
               f"started={started} alive={alive} reused={reused} stopped={stopped} gone={gone}"
               + ("" if ok else f" | {err} {err3}"))
    finally:
        if pid and not gone:
            try:
                os.kill(pid, 9)
            except Exception:
                pass

# ================================================================ GROUP H
def hook(fx, *flags, stdin=None):
    """Run `aoci hook pretool` the way a host does: flags on argv and, for the
    Claude Code channel, the host's PreToolUse JSON on stdin. Returns
    (exit code, stdout, stderr)."""
    proc = subprocess.run([BIN, "--repo", fx, "hook", "pretool", *flags], cwd=fx, input=stdin,
                          capture_output=True, text=True, encoding="utf-8", errors="replace", timeout=60)
    return proc.returncode, proc.stdout, proc.stderr


def group_h():
    """Pre-write hook: `aoci hook pretool` under the Claude stdin-JSON and Codex flag shapes.

    The hook runs inside another program's write path, so its first contract is
    fail-open: malformed host JSON, a host call that carries no file path, a
    path the repository cannot resolve, or a target the index has never seen
    must never block the host. Its second contract is the injection: the Entry
    of a known target comes back verbatim before the write, a stale one carries
    a STALE warning, and under hook_strict a stale target is the only thing
    that blocks, with the agent's own exit code: Claude Code reads exit 2 and
    feeds stderr back to the model, while the generic channel Codex and Cursor
    use is exit 4 with the text on stdout. A CRLF-only rewrite is the team
    line-ending tolerance, never stale. Every decision lands in the local
    Ledger as a hook_trigger that names the tool and whether drift was warned.
    """
    g = "H"
    fx = make_fixture("fx-hook", 3)
    s = Session(fx)
    try:
        m, t, err = maintain(s)
        applied, tw, err = submit_batch(s, m)
    finally:
        s.close()
    if applied.get("status") != "applied" or applied.get("aligned") is not True:
        record(g, "H0.setup", "FAIL", f"fixture authoring failed: {tw[:160]}")
        return

    def claude(path, tool="Edit"):
        """The PreToolUse JSON Claude Code writes to the hook's stdin."""
        return json.dumps({"tool_name": tool, "tool_input": {"file_path": path}})

    entry1, entry2, entry3 = entry_line("pkg/f001.go"), entry_line("pkg/f002.go"), entry_line("pkg/f003.go")
    decisions = 0  # calls that reach an Entry decision; each must land in the Ledger

    # H1: unparseable host JSON is not the hook's problem to raise.
    rc, out, err = hook(fx, "--agent", "claude", "--stdin-json", stdin="not json {\n")
    ok = rc == 0 and not out.strip() and not err.strip()
    record(g, "H1.malformed-host-json-fails-open", "PASS" if ok else "FAIL",
           f"rc={rc} stdout={out[:60]!r} stderr={err[:60]!r}")

    # H2: a target without an Entry passes, and the hook says how to add one.
    rc, out, err = hook(fx, "--agent", "claude", "--stdin-json", stdin=claude("pkg/nope.go"))
    decisions += 1
    ok = rc == 0 and "pkg/nope.go" in out and "aoci_update_entry" in out
    record(g, "H2.unindexed-target-fails-open-and-names-aoci_update_entry", "PASS" if ok else "FAIL",
           f"rc={rc} | {out[:120]!r}")

    # H3: Claude passes file_path relative or absolute; both inject the same Entry.
    rc1, out1, _ = hook(fx, "--agent", "claude", "--stdin-json", stdin=claude("pkg/f001.go"))
    rc2, out2, _ = hook(fx, "--agent", "claude", "--stdin-json",
                        stdin=claude(os.path.join(fx, "pkg", "f001.go"), "Write"))
    decisions += 2
    ok = rc1 == 0 and rc2 == 0 and entry1 in out1 and entry1 in out2 and "STALE" not in out1 + out2
    record(g, "H3.claude-stdin-json-injects-the-entry-for-relative-and-absolute-paths", "PASS" if ok else "FAIL",
           f"rc={rc1},{rc2} | {out1[:100]!r} | {out2[:100]!r}")

    # H4: the Codex channel carries the same facts as flags.
    rc, out, err = hook(fx, "--agent", "codex", "--tool", "Edit", "--path", "pkg/f001.go")
    decisions += 1
    ok = rc == 0 and entry1 in out and "STALE" not in out
    record(g, "H4.codex-flag-shape-injects-the-same-entry", "PASS" if ok else "FAIL", f"rc={rc} | {out[:120]!r}")

    # H5: a stale Entry is a warning, not a block, until the team says strict.
    with open(os.path.join(fx, "pkg", "f001.go"), "a") as fh:
        fh.write("\nfunc F001b() int { return 1001 }\n")
    rc, out, err = hook(fx, "--agent", "claude", "--stdin-json", stdin=claude("pkg/f001.go"))
    decisions += 1
    ok = rc == 0 and "STALE" in out and entry1 in out
    record(g, "H5.stale-entry-warns-but-does-not-block-without-strict", "PASS" if ok else "FAIL",
           f"rc={rc} | {out[:120]!r}")

    # H6: strict + stale blocks Claude with its contract code and the text on stderr.
    rc_cfg, _, out_cfg, errs_cfg = cli(fx, "config", "set", "hook_strict", "true")
    rc, out, err = hook(fx, "--agent", "claude", "--stdin-json", stdin=claude("pkg/f001.go"))
    decisions += 1
    ok = rc_cfg == 0 and rc == 2 and not out.strip() and "STALE" in err and "f001.go" in err
    record(g, "H6.strict-stale-target-blocks-claude-with-exit-2-on-stderr", "PASS" if ok else "FAIL",
           f"config rc={rc_cfg} hook rc={rc} stdout={out[:60]!r} stderr={err[:140]!r}")

    # H7: the generic channel blocks with 4 and keeps the text on stdout.
    rc, out, err = hook(fx, "--agent", "codex", "--tool", "Edit", "--path", "pkg/f001.go")
    rcc, outc, errc = hook(fx, "--agent", "cursor", "--tool", "Edit", "--path", "pkg/f001.go")
    decisions += 2
    ok = rc == 4 and rcc == 4 and "STALE" in out and entry1 in out and "STALE" in outc and entry1 in outc
    record(g, "H7.strict-stale-target-blocks-codex-and-cursor-with-exit-4-on-stdout", "PASS" if ok else "FAIL",
           f"rc={rc},{rcc} | {out[:120]!r}")

    # H8: strict blocks only what is stale: a current sibling injects, and a
    # line-ending-only rewrite is the tolerance the team configured, not drift.
    # The fixture is written in text mode, so on Windows it already carries
    # CRLF; flip whichever ending the file has, which is the same change in
    # both directions, instead of turning CRLF into CR CR LF.
    rc1, out1, _ = hook(fx, "--agent", "claude", "--stdin-json", stdin=claude("pkg/f002.go"))
    with open(os.path.join(fx, "pkg", "f003.go"), "rb") as fh:
        body = fh.read()
    flipped = body.replace(b"\r\n", b"\n") if b"\r\n" in body else body.replace(b"\n", b"\r\n")
    with open(os.path.join(fx, "pkg", "f003.go"), "wb") as fh:
        fh.write(flipped)
    rc2, out2, _ = hook(fx, "--agent", "claude", "--stdin-json", stdin=claude("pkg/f003.go"))
    decisions += 2
    ok = rc1 == 0 and entry2 in out1 and rc2 == 0 and entry3 in out2 and "STALE" not in out1 + out2
    record(g, "H8.strict-leaves-a-current-sibling-and-a-crlf-only-rewrite-open", "PASS" if ok else "FAIL",
           f"rc={rc1},{rc2} | {out1[:80]!r} | {out2[:80]!r}")

    # H9: strict changes nothing about fail-open.
    rc1, out1, err1 = hook(fx, "--agent", "claude", "--stdin-json", stdin="{{{")
    rc2, out2, err2 = hook(fx, "--agent", "claude", "--stdin-json",
                           stdin=json.dumps({"tool_name": "Bash", "tool_input": {"command": "ls"}}))
    rc3, out3, err3 = hook(fx, "--agent", "claude", "--stdin-json", stdin=claude("/etc/hosts"))
    ok = (rc1 == 0 and rc2 == 0 and rc3 == 0 and not (out1 + out2 + err1 + err2 + err3).strip()
          and "STALE" not in out3)
    record(g, "H9.strict-still-fails-open-on-malformed-json-no-path-and-foreign-path", "PASS" if ok else "FAIL",
           f"rc={rc1},{rc2},{rc3} | {out3[:100]!r} {err3[:60]!r}")

    # H10: the Ledger holds one hook_trigger per decision, with the tool and the drift flag.
    events = []
    try:
        with open(os.path.join(fx, ".aoci", "ledger.jsonl"), encoding="utf-8") as fh:
            for line in fh:
                try:
                    event = json.loads(line)
                except ValueError:
                    continue
                if event.get("op") == "hook_trigger":
                    events.append(event)
    except OSError:
        pass
    warned = sum(1 for e in events if e.get("drift_warned"))
    tools = sorted({str(e.get("tag_filter")) for e in events})
    ok = len(events) == decisions and warned == 4 and tools == ["Edit", "Write"]
    record(g, "H10.every-decision-lands-in-the-ledger-with-its-tool-and-drift-flag", "PASS" if ok else "FAIL",
           f"hook_trigger={len(events)} expected={decisions} drift_warned={warned} tools={tools}")


# ---------------------------------------------------------------- main
# The no-argument run is the published contract: every group, in exactly this
# order (group D reuses the fixture group B returns), then the host-window gate
# and the published-count binding. --group filters this list by letter without
# reordering it; a filtered run cannot be held to the published count, so that
# binding is skipped and the run says so.

# ================================================================ GROUP L
def group_l_parallel():
    """Parallel current batches (L4): under code_cognition_parallel_batches=2 two
    Sessions author sibling batches of one logical plan against one fixture.
    Siblings are disjoint and issued against one preimage, a sibling delivered
    by batch_id carries its issued targets and nothing else, both apply orders
    reach aligned with the same Entry set, a foreign edit between siblings
    replans the outstanding ones and keeps the applied one formal, two Applies
    at once serialize and both succeed, and one target's source drift
    invalidates one sibling only."""
    g = "L"
    BUDGET = 24 * 1024

    def parallel_fixture(name):
        fx = make_fixture(name, 120)
        rc, _, out, errs = cli(fx, "config", "set", "code_cognition_parallel_batches", "2")
        if rc != 0: raise RuntimeError(f"config set code_cognition_parallel_batches failed: {out[:200]} {errs[:200]}")
        return fx

    def deliver(s, batch_id):
        t, err = text_of(s.call("aoci_maintain", {"batch_id": batch_id}))
        return jload(t), t, err

    def targets(m): return [c["path"] for c in (m.get("candidates") or [])]
    def siblings(m): return (m.get("code_plan") or {}).get("siblings") or []

    def entry_lines(fx):
        with open(os.path.join(fx, "aoci.code.txt"), encoding="utf-8") as fh:
            return sorted(ln for ln in fh.read().splitlines() if ln and not ln.startswith("===") and not ln.startswith("#"))

    # L1 + L2: one plan, two siblings, delivered to two sessions.
    fx = parallel_fixture("fx-parallel")
    a, b = Session(fx), Session(fx)
    m1, t1, err1 = maintain(a)
    plan = m1.get("code_plan") or {}
    sibs = siblings(m1)
    ta = targets(m1)
    tb = (sibs[0].get("targets") or []) if sibs else []
    ok = (not err1) and len(plan.get("logical_plan") or "") == 64 and len(sibs) == 1 and sibs[0].get("state") == "issued" \
        and bool(ta) and bool(tb) and not (set(ta) & set(tb)) \
        and plan.get("remaining") == (plan.get("total_targets") or 0) - len(ta) - len(tb) and len(t1.encode("utf-8")) <= BUDGET
    record(g, "L1.n2-issues-disjoint-siblings-under-one-plan", "PASS" if ok else "FAIL",
           f"logical={str(plan.get('logical_plan'))[:12]} a={len(ta)} b={len(tb)} overlap={len(set(ta) & set(tb))} "
           f"remaining={plan.get('remaining')} total={plan.get('total_targets')} bytes={len(t1.encode('utf-8'))} siblings={len(sibs)}")
    m2, t2, err2 = deliver(b, sibs[0].get("batch_id", "0" * 64) if sibs else "0" * 64)
    p2 = m2.get("code_plan") or {}
    ok = (not err2) and m2.get("status") == "repair_required" and targets(m2) == tb \
        and p2.get("sibling_of") == plan.get("logical_plan") and p2.get("batch_id") == (sibs[0].get("batch_id") if sibs else None) \
        and [x.get("batch_id") for x in siblings(m2)] == [plan.get("batch_id")] and len(t2.encode("utf-8")) <= BUDGET
    record(g, "L2.sibling-delivered-by-batch-id-carries-its-targets-only", "PASS" if ok else "FAIL",
           f"status={m2.get('status')} targets={len(targets(m2))}/{len(tb)} sibling_of_ok={p2.get('sibling_of') == plan.get('logical_plan')} "
           f"others={[str(x.get('batch_id'))[:8] + ':' + str(x.get('state')) for x in siblings(m2)]} bytes={len(t2.encode('utf-8'))}")
    # L5: the two siblings apply at once from the two sessions; the second
    # meets the first's postimage under the lock and reprojects.
    import threading
    results = {}
    def apply_in(name, s, m): results[name] = submit_batch(s, m)
    threads = [threading.Thread(target=apply_in, args=("a", a, m1)), threading.Thread(target=apply_in, args=("b", b, m2))]
    for th in threads: th.start()
    for th in threads: th.join()
    ra, rb = results["a"][0], results["b"][0]
    rc, v, _, _ = cli(fx, "verify", expect_ok=False)
    missing = set(((v.get("governance") or {}).get("code_drift") or {}).get("missing") or [])
    ok = ra.get("status") == "applied" and rb.get("status") == "applied" and ra.get("applied") == len(ta) and rb.get("applied") == len(tb) \
        and not (missing & set(ta + tb))
    record(g, "L5.two-applies-at-once-serialize-and-both-succeed", "PASS" if ok else "FAIL",
           f"a={ra.get('status')}/{ra.get('applied')} b={rb.get('status')}/{rb.get('applied')} still_missing={len(missing & set(ta + tb))} "
           f"a_err={str((ra.get('findings') or ra.get('error') or ''))[:80]} b_err={str((rb.get('findings') or rb.get('error') or ''))[:80]}")
    a.close(); b.close()

    # L3: A-then-B and B-then-A over two fresh fixtures end aligned with the same Entry set.
    def build(fx, reverse):
        s = Session(fx)
        for _ in range(40):
            m, _, err = maintain(s)
            if err or (m.get("status") == "applied" and m.get("aligned")):
                break
            batches = [m]
            for sib in siblings(m):
                if sib.get("state") == "applied": continue
                batches.append(deliver(s, sib.get("batch_id", ""))[0])
            if reverse: batches.reverse()
            for batch in batches:
                r, _, _ = submit_batch(s, batch)
                if r.get("status") != "applied":
                    s.close(); return False, f"apply={r.get('status')} {str(r.get('findings') or r.get('error') or '')[:100]}"
        s.close()
        aligned, _ = fixture_aligned(fx)
        return aligned, "aligned" if aligned else "not aligned"
    fwd = parallel_fixture("fx-parallel-fwd")
    okf, df = build(fwd, False)
    rev = parallel_fixture("fx-parallel-rev")
    okr, dr = build(rev, True)
    same = entry_lines(fwd) == entry_lines(rev) if (okf and okr) else False
    ok = okf and okr and same and len(entry_lines(fwd)) >= 120
    record(g, "L3.a-then-b-and-b-then-a-reach-aligned-with-the-same-entry-set", "PASS" if ok else "FAIL",
           f"forward={df} reverse={dr} same_entry_set={same} entries={len(entry_lines(fwd))}")

    # L4: a write outside the plan between siblings invalidates the outstanding one; the applied one stays formal.
    fx4 = parallel_fixture("fx-parallel-foreign")
    s4 = Session(fx4)
    m4, _, _ = maintain(s4)
    p4 = m4.get("code_plan") or {}
    sib4 = siblings(m4)[0] if siblings(m4) else {}
    r4, _, _ = submit_batch(s4, m4)
    applied4 = targets(m4)
    first = (m4.get("candidates") or [{}])[0]
    direct_t, direct_err = text_of(s4.call("aoci_update_entry", {"path": first.get("path"), "source_sha256": first.get("source_sha256"),
        "new_entry": f"{os.path.basename(first.get('path', ''))}[CG5T]: F:Provides fixture constant unit after a foreign edit | R:- | A:- | S:-"}))
    d4, _, _ = deliver(s4, sib4.get("batch_id", "0" * 64))
    m4b, _, _ = maintain(s4)
    p4b = m4b.get("code_plan") or {}
    s4.close()
    rc, v4, _, _ = cli(fx4, "verify", expect_ok=False)
    missing4 = set(((v4.get("governance") or {}).get("code_drift") or {}).get("missing") or [])
    state4 = (siblings(d4) or [{}])[0].get("state")
    ok = r4.get("status") == "applied" and (not direct_err) and jload(direct_t).get("status") == "applied" \
        and d4.get("status") == "stopped" and state4 == "invalidated" and d4.get("next_action") == "call_no_argument_aoci_maintain_for_current_machine_batch" \
        and p4b.get("logical_plan") not in (None, p4.get("logical_plan")) and not (missing4 & set(applied4)) \
        and not (set(targets(m4b)) & set(applied4))
    record(g, "L4.foreign-edit-replans-outstanding-and-keeps-applied-formal", "PASS" if ok else "FAIL",
           f"first_apply={r4.get('status')} direct={jload(direct_t).get('status')} delivery={d4.get('status')}/{state4} "
           f"next={str(d4.get('next_action'))[:60]} replanned={p4b.get('logical_plan') != p4.get('logical_plan')} applied_still_missing={len(missing4 & set(applied4))}")

    # L6: one target's source drift invalidates the sibling holding it and nothing else.
    fx6 = parallel_fixture("fx-parallel-drift")
    s6 = Session(fx6)
    m6, _, _ = maintain(s6)
    p6 = m6.get("code_plan") or {}
    sib6 = siblings(m6)[0] if siblings(m6) else {}
    drifted = (sib6.get("targets") or ["pkg/f001.go"])[0]
    with open(os.path.join(fx6, drifted), "a", encoding="utf-8") as fh:
        fh.write("\n// drift after issuance\n")
    d6, _, _ = deliver(s6, sib6.get("batch_id", "0" * 64))
    still6, _, _ = deliver(s6, p6.get("batch_id", "0" * 64))
    m6b, _, _ = maintain(s6)
    p6b = m6b.get("code_plan") or {}
    listed = siblings(m6b)
    # The surviving sibling applies, the plan completes, and the next plan
    # issues the drifted target again under its new digest.
    r6, _, _ = submit_batch(s6, m6b)
    m6c, _, _ = maintain(s6)
    p6c = m6c.get("code_plan") or {}
    s6.close()
    issued_next = set(targets(m6c)) | {t for sib in siblings(m6c) for t in (sib.get("targets") or [])}
    ok = (siblings(d6) or [{}])[0].get("state") == "invalidated" and d6.get("status") == "stopped" \
        and still6.get("status") == "repair_required" and targets(still6) == targets(m6) \
        and p6b.get("logical_plan") == p6.get("logical_plan") and p6b.get("batch_id") == p6.get("batch_id") \
        and len(listed) == 1 and listed[0].get("batch_id") == sib6.get("batch_id") and listed[0].get("state") == "invalidated" \
        and r6.get("status") == "applied" and p6c.get("logical_plan") not in (None, p6.get("logical_plan")) and drifted in issued_next
    record(g, "L6.one-targets-source-drift-invalidates-one-sibling-only", "PASS" if ok else "FAIL",
           f"drifted_delivery={d6.get('status')}/{(siblings(d6) or [{}])[0].get('state')} other={still6.get('status')} "
           f"same_plan={p6b.get('logical_plan') == p6.get('logical_plan')} listed={[str(x.get('batch_id'))[:8] + ':' + str(x.get('state')) for x in listed]} "
           f"survivor={r6.get('status')} next_plan_new={p6c.get('logical_plan') != p6.get('logical_plan')} drifted_reissued={drifted in issued_next}")

GROUP_RUNS = [
    ("A", group_a), ("B", group_b), ("C", group_c), ("D", group_d), ("E", group_e), ("F", group_f),
    ("N", group_n), ("F", group_f_scope), ("F", group_f_activate), ("F", group_f_ignored_tool_file),
    ("F", group_f_held_sources), ("F", group_f_deleted_observe), ("F", group_f_excluded_tracked),
    ("F", group_f_host_dialog), ("F", group_f_observe_review), ("F", group_f_sessionless_dialog),
    ("T", group_t), ("U", group_u), ("U", group_u_detach), ("O", group_o_optimization),
    ("O", group_o_s_absent_priority), ("P", group_p_special_names), ("P", group_p_unspellable_directory),
    ("H", group_h), ("L", group_l_parallel), ("M", group_m_delta),
]
HOST_WINDOW_GROUP = "W"


def group_letters():
    """Group letters in first-run order, the host-window gate last."""
    letters = []
    for letter, _ in GROUP_RUNS:
        if letter not in letters:
            letters.append(letter)
    return letters + [HOST_WINDOW_GROUP]


def list_groups(out=sys.stdout):
    """One line per group function: letter, function name, docstring first line."""
    for letter in group_letters():
        if letter == HOST_WINDOW_GROUP:
            doc = (host_window_summary.__doc__ or "").strip().splitlines()
            print(f"{letter}  host_window_summary: {doc[0] if doc else '(no docstring)'}", file=out)
            continue
        for run_letter, fn in GROUP_RUNS:
            if run_letter == letter:
                doc = (fn.__doc__ or "").strip().splitlines()
                print(f"{letter}  {fn.__name__}: {doc[0] if doc else '(no docstring)'}", file=out)


def main(argv=None):
    parser = argparse.ArgumentParser(
        description="AOCI MCP scenario suite: safety scenarios over disposable fixtures, "
                    "driving the binary under test from outside the process.",
        epilog="Groups: " + " ".join(group_letters()) + ". --list prints what each runs.")
    parser.add_argument("--group", action="append", default=[], metavar="LETTER",
                        help="run only this group letter (repeatable, case-insensitive); the W gate always "
                             "runs; without --group every group runs in the published order and the "
                             "published scenario count is enforced")
    parser.add_argument("--list", action="store_true",
                        help="print the group letters with each group function's docstring first line, then exit")
    args = parser.parse_args(argv)
    if args.list:
        list_groups()
        return 0
    letters = group_letters()
    wanted = [letter.upper() for letter in args.group]
    unknown = sorted(set(wanted) - set(letters))
    if unknown:
        print(f"unknown group(s) {unknown}; valid letters: {' '.join(letters)}", file=sys.stderr)
        return 2

    os.makedirs(WORK, exist_ok=True)
    bigfx = None
    for letter, fn in GROUP_RUNS:
        if wanted and letter not in wanted:
            continue
        # A group that dies part way, such as a status-page request that stalls
        # past its timeout, is recorded as that group's failure: the remaining
        # groups still run and the summary still prints, and the run stays red.
        try:
            if fn is group_b:
                bigfx = group_b()
            elif fn is group_d:
                group_d(bigfx)
            else:
                fn()
        except Exception as error:
            record(letter, f"{fn.__name__}.crashed", "FAIL", f"{type(error).__name__}: {error}"[:200])
    ok, detail = host_window_summary()
    record("W", "W1.every-non-overview-response-fits-host-window", "PASS" if ok else "FAIL", detail)
    print()
    npass = sum(1 for r in RESULTS if r[2] == "PASS")
    nchar = sum(1 for r in RESULTS if r[2] == "CHAR")
    nfail = sum(1 for r in RESULTS if r[2] == "FAIL")

    # Documentation binding. README.md, README.zh-CN.md and this suite's own
    # README all advertise the scenario count to downstream verifiers, and all
    # three had drifted to a stale 22. The run is the authority, so it enforces
    # them. Kept out of RESULTS: the published number counts safety scenarios,
    # and counting a documentation check among them would make it self-describing.
    doc_drift = []
    if wanted:
        print("SKIP [docs] published-scenario-count-matches-this-run | partial run (--group); "
              "the binding needs the complete run")
    else:
        doc_drift = verify_published_scenario_count(len(RESULTS))
        print(("PASS " if not doc_drift else "FAIL ")
              + "[docs] published-scenario-count-matches-this-run | "
              + ("; ".join(doc_drift) if doc_drift else str(len(RESULTS))))

    print(f"SCENARIOS: {npass} PASS, {nchar} CHARACTERIZED, {nfail} FAIL")
    for grp, name, st, d in RESULTS:
        if st != "PASS":
            print(f"  {st} [{grp}] {name} | {d[:220]}")
    for drift in doc_drift:
        print(f"  FAIL [docs] {drift}")
    if not _KEEP_WORK:
        shutil.rmtree(WORK, ignore_errors=True)
    return 1 if (nfail or doc_drift) else 0


if __name__ == "__main__":
    sys.exit(main())
