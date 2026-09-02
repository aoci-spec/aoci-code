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
  T  human confirmation      (fixture: real pty, prompt must precede the read)
  W  host window             (every non-Overview response fits an ordinary host)

The run also checks that the scenario count published in the public READMEs and
in scripts/blackbox/README.md matches this run, so those numbers cannot drift.

Usage:  python3 scripts/blackbox/mcp_scenarios.py
Env:    AOCI_REPO / AOCI_BIN 覆盖仓库与二进制路径（默认取本脚本所在仓库）;
        AOCI_SCENARIO_WORK 指定夹具目录并保留（默认系统临时目录、跑完清理）;
        AOCI_SCENARIO_KEEP=1 保留默认临时夹具以便排查。
Requires: a built binary; group A additionally needs the host repository to hold
an established multi-chunk overview (chunk_tokens 8000). Fixtures set their own
git identity; the host repository is never written.
"""
import hashlib, json, os, random, re, select, shutil, subprocess, sys, tempfile, time

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
    def __init__(self, repo):
        self.repo = repo
        self.p = subprocess.Popen([BIN, "--repo", repo, "mcp"],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            text=True, encoding="utf-8", bufsize=1)
        self.next_id = 1
        self.rpc("initialize", {"protocolVersion": "2025-06-18", "capabilities": {},
                                "clientInfo": {"name": "aoci-scenarios", "version": "1.0"}})
        self.notify("notifications/initialized")
    def send_raw(self, line):
        self.p.stdin.write(line + "\n"); self.p.stdin.flush()
    def rpc(self, method, params=None, timeout=120):
        rid = self.next_id; self.next_id += 1
        msg = {"jsonrpc": "2.0", "id": rid, "method": method}
        if params is not None: msg["params"] = params
        self.send_raw(json.dumps(msg))
        deadline = time.time() + timeout
        while time.time() < deadline:
            line = self.p.stdout.readline()
            if not line: raise RuntimeError("server closed stdout")
            line = line.rstrip("\n")
            if not line: continue
            try: obj = json.loads(line)
            except Exception: continue
            if obj.get("id") == rid: return obj
        raise TimeoutError(method)
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
    def close(self):
        try: self.p.stdin.close(); self.p.wait(timeout=10)
        except Exception: self.kill()

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
                       capture_output=True, text=True, timeout=120)
    body = {}
    try: body = json.loads(r.stdout)
    except Exception:
        try: body = jload(r.stdout)
        except Exception: pass
    return r.returncode, body, r.stdout, r.stderr

def sh(cwd, *args):
    r = subprocess.run(list(args), cwd=cwd, capture_output=True, text=True, timeout=120)
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
    bad_ord = ":".join([parts[0], parts[1], str(int(parts[2]) + 37), parts[3]])
    t, err = text_of(s.call("aoci_overview", {"cursor": bad_ord}))
    record(g, "A2.tampered-ordinal", "PASS" if is_rejection(t, err) else "FAIL", t[:150])

    # A3: tampered previous-chunk sha
    bad_sha = ":".join(parts[:3] + ["0" * 64])
    t, err = text_of(s.call("aoci_overview", {"cursor": bad_sha}))
    record(g, "A3.tampered-prev-sha", "PASS" if is_rejection(t, err) else "FAIL", t[:150])

    # A4: cursor across server processes. Documented: with an unchanged Index and
    # chunk_tokens, the same cursor is accepted across MCP process restarts.
    s2 = Session(REAL)
    t, err = text_of(s2.call("aoci_overview", {"cursor": cur2}))
    mx, bx = meta_and_body(t)
    if bx is not None:
        ok = hashlib.sha256(bx.encode()).hexdigest() == mx.get("chunk_sha256")
        record(g, "A4.cross-session-cursor", "PASS" if ok else "FAIL",
               "deterministic re-derivation, chunk sha self-consistent" if ok else "served chunk failed its own sha")
    else:
        record(g, "A4.cross-session-cursor", "FAIL", t[:150])
    s.close(); s2.close()

# ================================================================ GROUP B (+A5)
def group_b():
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
    record(g, "B4.wrong-source-sha", "PASS" if zero_write else "FAIL", (r.get("status") or t[:100]) + " zero-write" if zero_write else t[:200])
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

    # D3: baseline-only change mid-chain (identical entry text) — the formal
    # Index bytes are unchanged, so per the documented cursor semantics the
    # continuation must proceed (delivered bytes == current formal bytes).
    # Baseline participates only in the session-scoped Level-4 governance
    # binding, never in the chunk-chain stop list.
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
        if st == "applied" and not err and (mc.get("completed") or mc.get("chunk_index")):
            record(g, "D3.baseline-only-mid-chain", "PASS",
                   "continuation proceeds; index bytes unchanged, Baseline only binds the Level-4 governance plane")
        else:
            record(g, "D3.baseline-only-mid-chain", "FAIL", t[:150])
    sC.close()

def group_e():
    """Large fresh repository, machine-default batch: the first Maintain must
    fit an ordinary Host tool-result window and stay actionable inline. This is
    the failure a real user hit — a ~1400-file repository answered its first
    Maintain with ~330 KB at 200 per batch, the Host spilled it to disk, and the
    model fell back to scripts that broke on encoding, quoting, and paths."""
    g = "E"
    NF = 260
    fx = make_fixture("fx-large", NF)
    s = Session(fx)
    m, t, err = maintain(s)
    plan = m.get("code_plan") or {}
    total = NF + INIT_GENERATED_TARGETS
    ok = (not err) and plan.get("max_entries") == 20 and plan.get("included") == 20 \
        and plan.get("remaining") == total - 20 and len(m.get("candidates") or []) == 20
    record(g, "E1.default-batch-is-20", "PASS" if ok else "FAIL",
           f"max_entries={plan.get('max_entries')} included={plan.get('included')} remaining={plan.get('remaining')} cands={len(m.get('candidates') or [])}")
    size = len(t.encode("utf-8"))
    gov = m.get("governance") or {}
    trunc = gov.get("list_truncation") or {}
    totals = trunc.get("totals") or {}
    findings = gov.get("findings") or []
    missing = (gov.get("code_drift") or {}).get("missing") or []
    ok = size < 48 * 1024 and len(findings) == 20 and len(missing) == 20 \
        and totals.get("findings") == total and totals.get("code_drift.missing") == total and trunc.get("limit") == 20
    record(g, "E2.first-maintain-fits-host-window", "PASS" if ok else "FAIL",
           f"bytes={size} findings={len(findings)}/{totals.get('findings')} missing={len(missing)}/{totals.get('code_drift.missing')} limit={trunc.get('limit')}")
    instr = " ".join(m.get("instructions") or [])
    ok = "aoci_update_entry" in instr and "governance.budget" in instr and "aoci_maintain again" in instr
    record(g, "E3.instructions-say-inline-tokens-remaintain", "PASS" if ok else "FAIL", instr[-200:])
    # candidates and plan stay complete and actionable: author the batch inline.
    r, tw, err = submit_batch(s, m)
    ok = r.get("status") == "applied" and r.get("applied") == 20 and r.get("remaining") == total - 20 \
        and "maintain" in (r.get("next_action") or "").lower()
    record(g, "E4.batch-applies-and-continues", "PASS" if ok else "FAIL",
           f"status={r.get('status')} applied={r.get('applied')} remaining={r.get('remaining')} next={str(r.get('next_action'))[:80]}")
    # the next Maintain issues the next 20 against the new preimage, same size, same shape
    m2, t2, _ = maintain(s)
    p2 = m2.get("code_plan") or {}
    ok = p2.get("included") == 20 and p2.get("remaining") == total - 40 and len(t2.encode("utf-8")) < 48 * 1024
    record(g, "E4b.next-maintain-pages-next-batch", "PASS" if ok else "FAIL",
           f"included={p2.get('included')} remaining={p2.get('remaining')} bytes={len(t2.encode('utf-8'))}")
    s.close()
    # Verify keeps the complete enumeration: the transport bound is Maintain-only.
    rc, v, _, _ = cli(fx, "verify", expect_ok=False)
    vmissing = ((v.get("governance") or {}).get("code_drift") or {}).get("missing") or []
    vtrunc = (v.get("governance") or {}).get("list_truncation")
    ok = len(vmissing) == total - 20 and vtrunc is None
    record(g, "E5.verify-lists-every-item", "PASS" if ok else "FAIL", f"verify.missing={len(vmissing)} truncation={vtrunc}")
    # Team configuration moves the batch; out-of-range values are rejected.
    rc, _, out, errs = cli(fx, "config", "set", "code_cognition_batch_entries", "50")
    s = Session(fx)
    m2, t2, err2 = maintain(s)
    s.close()
    p2 = m2.get("code_plan") or {}
    rc0, _, _, e0 = cli(fx, "config", "set", "code_cognition_batch_entries", "0", expect_ok=False)
    rc201, _, _, e201 = cli(fx, "config", "set", "code_cognition_batch_entries", "201", expect_ok=False)
    ok = rc == 0 and p2.get("max_entries") == 50 and p2.get("included") == 50 and rc0 != 0 and rc201 != 0
    record(g, "E6.batch-configurable-and-bounded", "PASS" if ok else "FAIL",
           f"set50 rc={rc} max_entries={p2.get('max_entries')} included={p2.get('included')} set0_rc={rc0} set201_rc={rc201}")


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
    Scope Change path instead, and pins both arms: tolerated and reported, but
    still fail-closed on a genuinely changed source.
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

    # Tolerance is not blindness. A genuinely changed source would let the
    # postimage Baseline stamp new bytes onto an Entry describing the old ones,
    # so that must still fail closed.
    with open(source, "wb") as fh:
        fh.write(flipped + b"\nfunc B() int { return 2 }\n")
    rc, _, out, errs = cli(d, "scope", "plan", "--prepared-at", "2026-08-29T00:00:00Z",
                           "--candidate-file", candidate, expect_ok=False)
    blob = (out or "") + (errs or "")
    record(g, "F7b.genuinely-changed-source-still-blocks-scope-change",
           "PASS" if rc != 0 and "managed_scope_index_source_stale" in blob else "FAIL",
           f"rc={rc} | {blob[:160]}")


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
    proc = subprocess.run(filled, shell=True, cwd=fx, capture_output=True, text=True, timeout=120)
    return proc.returncode, (proc.stdout or "") + (proc.stderr or "")

def overview_meta_and_full_body(text):
    """meta_and_body, but a single-shot full delivery keeps its body: a small
    index answers with kv metadata plus the body inline and no chunk marker,
    and the shared helper's kv branch returns body=None for that shape."""
    if MARK in text:
        return meta_and_body(text)
    head, sep, rest = text.partition("<<<AOCI_OVERVIEW_BODY_BEGIN")
    return parse_kv(head), (sep + rest) if sep else None

def parse_body_entries(body, fx):
    """Ordinal-ordered (rel_path, tag, core_f) parsed from a delivered body."""
    entries, section = [], ""
    for line in (body or "").splitlines():
        line = line.rstrip("\r")
        if line.startswith("===") and line.endswith("==="):
            raw = line.strip("=")
            rel = os.path.relpath(raw, fx) if raw.startswith(fx) else raw.strip("/")
            section = "" if rel == "." else rel.rstrip("/") + "/"
            continue
        m = re.match(r"^([^\s#=<│─][^\[]*)\[([A-Z0-9]+)\]: F:(.*?) \| R:", line)
        if m:
            entries.append((section + m.group(1), m.group(2), m.group(3)))
    return entries

def group_n():
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
    m2, t2, err = maintain(s)
    s.close()
    cmds = m2.get("next_commands") or []
    ack = next((c for c in cmds if "scope acknowledge" in c), "")
    shaped = (m2.get("result") == "blocked"
              and m2.get("next_action") == "explicit_orphan_remove_or_resolve_blocker"
              and ack != "" and "{agent}" in ack)
    record(g, "N2.blocked-carries-acknowledge-command", "PASS" if shaped else "FAIL",
           f"result={m2.get('result')} cmds={cmds}"[:200])
    if shaped:
        rc, out = run_returned_command(d, ack)
        s = Session(d)
        m3, _, _ = maintain(s)
        s.close()
        cleared = rc == 0 and m3.get("result") in ("aligned", "authoring_required")
        record(g, "N2b.acknowledge-command-clears-the-block", "PASS" if cleared else "FAIL",
               f"rc={rc} after={m3.get('result')}")
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


# ---------------------------------------------------------------- main
if __name__ == "__main__":
    os.makedirs(WORK, exist_ok=True)
    group_a()
    bigfx = group_b()
    group_c()
    group_d(bigfx)
    group_e()
    group_f()
    group_n()
    group_f_scope()
    group_f_deleted_observe()
    group_f_excluded_tracked()
    group_t()
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
    sys.exit(1 if (nfail or doc_drift) else 0)
