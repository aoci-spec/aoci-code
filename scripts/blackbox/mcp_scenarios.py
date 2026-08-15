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

Usage:  python3 scripts/blackbox/mcp_scenarios.py
Env:    AOCI_REPO / AOCI_BIN 覆盖仓库与二进制路径（默认取本脚本所在仓库）;
        AOCI_SCENARIO_WORK 指定夹具目录并保留（默认系统临时目录、跑完清理）;
        AOCI_SCENARIO_KEEP=1 保留默认临时夹具以便排查。
Requires: a built binary; group A additionally needs the host repository to hold
an established multi-chunk overview (chunk_tokens 8000). Fixtures set their own
git identity; the host repository is never written.
"""
import hashlib, json, os, random, re, shutil, subprocess, sys, tempfile, time

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

# ---------------------------------------------------------------- MCP client
class Session:
    def __init__(self, repo):
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
        return self.rpc("tools/call", {"name": tool, "arguments": args or {}}, timeout)
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
    if n != NF + 1:  # fixtures + init-generated AGENTS.md
        record(g, "B1.maintain-missing", "FAIL", f"expected {NF+1} candidates, got {n}: {t[:150]}"); s.close(); return None
    record(g, "B1.maintain-missing", "PASS", f"{n} create candidates (incl AGENTS.md)")
    r, t, err = submit_batch(s, m)
    ok = r.get("status") == "applied" and r.get("applied") == NF + 1 and r.get("remaining") == 0
    record(g, "B1.apply-batch", "PASS" if ok else "FAIL", t[:200] if not ok else f"applied={NF+1}")
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

    # B3: repair_required round trip (S over the C5 quota of 200 runes)
    with open(os.path.join(fx, "pkg", "f002.go"), "a") as f:
        f.write("\nfunc F002b() int { return 1002 }\n")
    m, t, err = maintain(s)
    long_s = "x" * 250
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
    total = NF + 1  # init-generated AGENTS.md is a target too
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
    ok = size < 64 * 1024 and len(findings) == 20 and len(missing) == 20 \
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
    ok = p2.get("included") == 20 and p2.get("remaining") == total - 40 and len(t2.encode("utf-8")) < 64 * 1024
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


# ---------------------------------------------------------------- main
if __name__ == "__main__":
    os.makedirs(WORK, exist_ok=True)
    group_a()
    bigfx = group_b()
    group_c()
    group_d(bigfx)
    group_e()
    print()
    npass = sum(1 for r in RESULTS if r[2] == "PASS")
    nchar = sum(1 for r in RESULTS if r[2] == "CHAR")
    nfail = sum(1 for r in RESULTS if r[2] == "FAIL")
    print(f"SCENARIOS: {npass} PASS, {nchar} CHARACTERIZED, {nfail} FAIL")
    for grp, name, st, d in RESULTS:
        if st != "PASS":
            print(f"  {st} [{grp}] {name} | {d[:220]}")
    if not _KEEP_WORK:
        shutil.rmtree(WORK, ignore_errors=True)
    sys.exit(1 if nfail else 0)
