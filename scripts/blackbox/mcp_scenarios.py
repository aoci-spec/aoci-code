#!/usr/bin/env python3
"""AOCI MCP scenario suite — black-box end-to-end tests over the public MCP + CLI
surfaces only. Write scenarios run against disposable fixture repositories;
delivery fault injection also uses an established disposable fixture with an
explicit small Chunk budget. Safety properties, not implementation choices, are asserted; where the
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
Requires: a built binary. Fixtures set their own git identity; the host
repository is never written.
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
# window; Claude Code spills anything past ~25k tokens to a file on disk, and a
# real user's index build collapsed into scripts and encoding errors on exactly
# that spill. The ledger records the UTF-8 size of every tools/call result the
# suites receive; the gate at the end asserts no non-Overview response crossed
# HOST_WINDOW_BYTES. Overview is chunked by its own configured budget and is
# reported but not gated here.
HOST_WINDOW_BYTES = 64 * 1024
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

def meta_and_body(value):
    if isinstance(value, dict):
        result = value.get("result") or {}
        text, _ = text_of(value)
        meta = result.get("_meta")
        if isinstance(meta, dict):
            return meta, text or None
    else:
        text = value
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

def write_code_target(repo, replacements=(), reuse=()):
    """Write a complete Code target from formal Code and return both texts."""
    formal_path = os.path.join(repo, "aoci.code.txt")
    with open(formal_path, encoding="utf-8") as f:
        formal = f.read()
    target = formal
    for before, after in replacements:
        if target.count(before) != 1:
            raise RuntimeError(f"target Entry is not unique: {before[:80]}")
        target = target.replace(before, after, 1)
    reuse_lines = "".join(f"#Target-Reuse: code:{path}\n" for path in reuse)
    if reuse_lines:
        marker = "#AOCI-CODE-VOLUME: 1\n"
        if marker not in target:
            raise RuntimeError("Code Volume marker missing")
        target = target.replace(marker, marker + reuse_lines, 1)
    with open(os.path.join(repo, "aoci.code.target.txt"), "w", encoding="utf-8") as f:
        f.write(target)
    return formal, target

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
def group_a(fx):
    g = "A"
    # -- A1 replay + A2 tamper + A3 sha tamper + A4 cross-session: fixture chain
    rc, _, out, _ = cli(fx, "config", "set", "overview_delivery.chunk_tokens", "4000")
    if rc != 0:
        record(g, "chain-setup", "FAIL", out[:150]); return
    s = Session(fx)
    resp = s.call("aoci_overview")
    t, err = text_of(resp)
    m1, _ = meta_and_body(resp)
    cur1 = m1.get("next_cursor")
    if not cur1:
        record(g, "chain-setup", "FAIL", "fixture overview did not chunk at 4000 tokens"); s.close(); return
    resp = s.call("aoci_overview", {"cursor": cur1})
    t, err = text_of(resp)
    m2, _ = meta_and_body(resp)
    cur2 = m2.get("next_cursor")
    attack_cursor = cur2 or cur1

    # A1: replay cur1 after consuming it once. A two-chunk fixture has no cur2,
    # but every cursor contract below still needs only one genuine cursor.
    # Documented: an exact replay of a genuine cursor idempotently re-serves the
    # identical Chunk bytes (spec/public/aoci-overview-delivery-v1.txt, cursor §).
    resp = s.call("aoci_overview", {"cursor": cur1})
    t, err = text_of(resp)
    mrep, brep = meta_and_body(resp)
    if brep is not None and hashlib.sha256(brep.encode()).hexdigest() == m2.get("chunk_sha256"):
        record(g, "A1.replayed-cursor", "PASS", "idempotent re-serve of identical chunk bytes")
    else:
        record(g, "A1.replayed-cursor", "FAIL", t[:200])

    # A2: tampered ordinal inside the cursor
    parts = attack_cursor.split(":")
    if len(parts) != 4:
        record(g, "chain-setup", "FAIL", "unexpected cursor field count"); s.close(); return
    bad_ord = ":".join([parts[0], parts[1], str(int(parts[2]) + 37), parts[3]])
    t, err = text_of(s.call("aoci_overview", {"cursor": bad_ord}))
    record(g, "A2.tampered-ordinal", "PASS" if is_rejection(t, err) else "FAIL", t[:150])

    # A3: tampered previous-chunk sha
    bad_sha = ":".join(parts[:3] + ["0" * 64])
    t, err = text_of(s.call("aoci_overview", {"cursor": bad_sha}))
    record(g, "A3.tampered-prev-sha", "PASS" if is_rejection(t, err) else "FAIL", t[:150])

    # A4: cursor across server processes. Documented: with an unchanged Index and
    # chunk_tokens, the same cursor is accepted across MCP process restarts.
    s2 = Session(fx)
    resp = s2.call("aoci_overview", {"cursor": attack_cursor})
    t, err = text_of(resp)
    mx, bx = meta_and_body(resp)
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
        resp = sc.call("aoci_overview")
        t, err = text_of(resp)
        mm, bb = meta_and_body(resp)
        if not mm.get("next_cursor"):
            record("A", "A5.chunk-tokens-change", "CHAR", f"fixture index fits one chunk at 4000 tokens (est={mm.get('estimated_tokens')}); scenario skipped")
        else:
            cur = mm["next_cursor"]
            cli(fx, "config", "set", "overview_delivery.chunk_tokens", "6000")
            t, err = text_of(sc.call("aoci_overview", {"cursor": cur}))
            record("A", "A5.chunk-tokens-change", "PASS" if is_rejection(t, err) else "FAIL", t[:150])
            cli(fx, "config", "set", "overview_delivery.chunk_tokens", "600000")
        sc.close()

    # B2: target mode binds one changed Entry and one explicit semantic reuse.
    # Omitting that reuse declaration must stop before any formal write.
    before = entry_line("pkg/f001.go")
    after = "f001.go[CG5T]: F:Provides revised fixture constant unit 1 | R:- | A:- | S:-"
    formal, _ = write_code_target(fx, [(before, after)])
    for path, body in (("f001.go", "\nfunc F001b() int { return 1001 }\n"),
                       ("f002.go", "\nfunc F002b() int { return 1002 }\n")):
        with open(os.path.join(fx, "pkg", path), "a") as f:
            f.write(body)
    tw, err = text_of(s.call("aoci_update_entry", {"target_index": "aoci.code.target.txt"}, timeout=300))
    stopped = jload(tw)
    with open(os.path.join(fx, "aoci.code.txt"), encoding="utf-8") as f:
        formal_after_stop = f.read()
    zero_write = (stopped.get("status") == "stopped"
                  and stopped.get("formal_writes_started") is False
                  and formal_after_stop == formal)
    record(g, "B2.target-missing-reuse-zero-write", "PASS" if zero_write else "FAIL", tw[:180])

    write_code_target(fx, [(before, after)], reuse=["pkg/f002.go"])
    tw, err = text_of(s.call("aoci_update_entry", {"target_index": "aoci.code.target.txt"}, timeout=300))
    applied = jload(tw)
    al, _ = fixture_aligned(fx)
    with open(os.path.join(fx, "aoci.code.txt"), encoding="utf-8") as f:
        formal_after = f.read()
    with open(os.path.join(fx, "aoci.code.target.txt"), encoding="utf-8") as f:
        target_after = f.read()
    ok = (not err and applied.get("status") == "applied" and applied.get("aligned") is True
          and applied.get("attempted") == 2 and applied.get("applied") == 2 and al
          and target_after == formal_after and "#Target-Reuse:" not in target_after)
    record(g, "B2.target-batch-applies-aligned", "PASS" if ok else "FAIL", tw[:180])

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
    resp = s2.call("aoci_overview")
    t, err = text_of(resp)
    m2, b2 = meta_and_body(resp)
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
    resp = sC.call("aoci_overview")
    t, _ = text_of(resp)
    mm, _ = meta_and_body(resp)
    cur = mm.get("next_cursor")
    if not cur:
        record(g, "D2.index-change-mid-chain", "CHAR", "fixture fits one chunk; cannot open a chain")
    else:
        st = land_write(fx, "f011.go", "Provides revised fixture constant unit 11")
        resp = sC.call("aoci_overview", {"cursor": cur})
        t, err = text_of(resp)
        mrej, brej = meta_and_body(resp)
        ok = st == "applied" and brej is None and ("overview_snapshot_changed" in t or "bad_args" in t or err)
        record(g, "D2.index-change-mid-chain", "PASS" if ok else "FAIL", t[:150])
    sC.close()

    # D3: a Baseline-only change leaves formal bytes stable. Middle chunks may
    # continue, but the final governance confirmation must reject the old
    # delivery generation. A two-chunk fixture reaches that final boundary on
    # its first continuation.
    sC = Session(fx)
    resp = sC.call("aoci_overview")
    t, _ = text_of(resp)
    mm, _ = meta_and_body(resp)
    cur = mm.get("next_cursor")
    chunk_count = int(mm.get("chunk_count") or 0)
    if not cur:
        record(g, "D3.baseline-only-mid-chain", "CHAR", "no chain")
    else:
        st = land_write(fx, "f012.go", "Provides fixture constant unit 12")  # unchanged text => index bytes stable
        resp = sC.call("aoci_overview", {"cursor": cur})
        t, err = text_of(resp)
        mc, bc = meta_and_body(resp)
        if chunk_count >= 3 and st == "applied" and not err and bc is not None and not mc.get("completed"):
            record(g, "D3.baseline-only-mid-chain", "PASS",
                   "middle continuation proceeds; final governance confirmation remains pending")
        elif chunk_count == 2 and st == "applied" and bc is None and (err or "cognition_snapshot_unavailable" in t):
            record(g, "D3.baseline-only-mid-chain", "PASS",
                   "two-chunk chain rejects at the final governance boundary")
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
    bigfx = group_b()
    group_a(bigfx)
    group_c()
    group_d(bigfx)
    group_e()
    group_f()
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
