#!/usr/bin/env python3
"""AOCI MCP stdio conformance harness — drives build/aoci mcp as a real client.
Protocol-level tests only; zero formal writes expected (verified afterward by git/verify).

MCP stdio 一致性检查（60项）：以真实客户端身份走 initialize→tools 全链路，
只读不写，跑完用 git status / aoci verify 复核零写入。最后一段 locale 孪生
检查在临时目录铸造两个同样的三文件夹具（init --locale en-US / zh-CN），对照
人类语句随 locale 变化而机器字段、键集、错误码与摘要不变；宿主仓库仍只读。
本脚本跑完会核对公开文档里公布的检查数与本次实跑一致，防止文档悄悄漂移。

Usage:  python3 scripts/blackbox/mcp_conformance.py
Env:    AOCI_REPO / AOCI_BIN 覆盖仓库与二进制路径（默认取本脚本所在仓库）;
        AOCI_EXPECT_VERSION 设定后严格断言 serverInfo.version，否则只断言非空。
Requires: an established repository (aoci init + scan done), git, and a built binary.
"""
import hashlib, json, os, re, shutil, subprocess, sys, tempfile

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
REPO = os.environ.get("AOCI_REPO", _REPO_DEFAULT)
BIN = os.environ.get("AOCI_BIN", os.path.join(REPO, "build", "aoci"))
EXPECT_VERSION = os.environ.get("AOCI_EXPECT_VERSION", "")
NINE = ["aoci_rules","aoci_overview","aoci_get_entries","aoci_search",
        "aoci_update_entry","aoci_report","aoci_remove_entry","aoci_header","aoci_maintain"]
MARK = "<<<AOCI_OVERVIEW_CHUNK_BODY/v1>>>"

# The attestation lane needs a reliable receipt, and a receipt is reliable only
# over a governance-aligned repository: on a dirty host tree the four checks in
# ALIGNMENT_BOUND cannot pass whatever the server does, and every closure that
# ran the suite before governing read them as four false reds. They are
# reported as SKIP with the reason, still counted in the published total so the
# number does not depend on the outcome, and do not fail the run; a ladder
# record still needs an aligned run.
def _host_aligned():
    try:
        r = subprocess.run([BIN, "--repo", REPO, "verify", "--json"], capture_output=True, text=True,
                           encoding="utf-8", errors="replace", timeout=300)
        body = json.loads(r.stdout)
    except (OSError, ValueError, subprocess.TimeoutExpired):
        return False
    return bool((body.get("governance") or {}).get("governance_aligned"))
HOST_ALIGNED = _host_aligned()
ALIGNMENT_BOUND = {"attestation.confirmation_after_attestation_latches", "attestation.attestation_after_confirmation_latches",
                   "attestation.eight_of_ten_passes", "sessionless.overview_attested"}
SKIP_REASON = "host repository not governance-aligned: govern it, then re-run for the record"
print(f"host repository aligned: {HOST_ALIGNED}" + ("" if HOST_ALIGNED else f" ({len(ALIGNMENT_BOUND)} receipt-bound checks will SKIP)"))

PASS, FAIL, SKIP = [], [], []
def ok(name, cond, detail=""):
    if not cond and not HOST_ALIGNED and name in ALIGNMENT_BOUND:
        SKIP.append((name, SKIP_REASON))
        print("SKIP " + name + " | " + SKIP_REASON)
        return
    (PASS if cond else FAIL).append((name, detail))
    print(("PASS " if cond else "FAIL ") + name + ((" | " + detail[:140]) if detail and not cond else ""))

# host-window ledger: (tool, utf8 bytes) for every tools/call result received.
# 48 KiB is the measured flagship-host window, not an assumption: current
# Claude Code spills tool responses past ~50 KB to disk with a 2 KB preview.
RESPONSE_SIZES = []
HOST_WINDOW_BYTES = 48 * 1024

class Session:
    def __init__(self, repo=None):
        self.repo = repo or REPO
        self.p = subprocess.Popen([BIN, "--repo", self.repo, "mcp"],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            text=True, encoding="utf-8", bufsize=1)
        self.stderr_capture = BoundedStderr(self.p.stderr)
        self.next_id = 1
        self.nonjson_stdout = []
    def send_raw(self, line):
        self.p.stdin.write(line + "\n"); self.p.stdin.flush()
    def rpc(self, method, params=None, timeout=120):
        with rpc_deadline(self.p, method, timeout, getattr(self, "stderr_capture", None)):
            rid = self.next_id; self.next_id += 1
            msg = {"jsonrpc":"2.0","id":rid,"method":method}
            if params is not None: msg["params"] = params
            self.send_raw(json.dumps(msg))
            while True:
                line = self.p.stdout.readline()
                if not line:
                    raise RuntimeError(stderr_failure(getattr(self, "stderr_capture", None), "server closed stdout"))
                line = line.rstrip("\n")
                if not line: continue
                try:
                    obj = json.loads(line)
                except Exception:
                    self.nonjson_stdout.append(line)  # stdout purity violation
                    continue
                if obj.get("id") == rid:
                    return obj
                # notifications/other ids: ignore

    def notify(self, method, params=None):
        msg = {"jsonrpc":"2.0","method":method}
        if params is not None: msg["params"] = params
        self.send_raw(json.dumps(msg))
    def call(self, tool, args=None, timeout=180):
        resp = self.rpc("tools/call", {"name": tool, "arguments": args or {}}, timeout)
        try:
            text = "".join(c.get("text", "") for c in (resp.get("result") or {}).get("content") or []
                           if isinstance(c, dict))
            RESPONSE_SIZES.append((tool, len(text.encode("utf-8"))))
        except Exception:
            pass
        return resp
    def close(self):
        try:
            self.p.stdin.close(); self.p.wait(timeout=10)
        except Exception:
            self.p.kill()
        finally:
            self.stderr_capture.finish()

def text_of(resp):
    r = resp.get("result") or {}
    parts = r.get("content") or []
    return "\n".join(c.get("text","") for c in parts if c.get("type")=="text"), bool(r.get("isError"))

def meta_and_body(text):
    """Overview responses: JSON metadata, then MARK line, then exact chunk body."""
    if MARK in text:
        head, body = text.split(MARK + "\n", 1)
        return json.loads(head[head.index("{"):head.rindex("}")+1]), body
    return json.loads(text[text.index("{"):text.rindex("}")+1]), None

def overview_chunks(session):
    """Follow the exact cursor chain, bounded by its declared chunk count."""
    cursor, chunk_count, received = None, None, 0
    seen = set()
    while True:
        response = session.call("aoci_overview", {"cursor": cursor} if cursor else {})
        text, err = text_of(response)
        if err or response.get("error") is not None:
            raise RuntimeError(f"Overview chunk {received + 1} returned an error")
        meta, body = meta_and_body(text)
        if body is None:
            raise RuntimeError("Overview response has no chunk body")
        if chunk_count is None:
            chunk_count = meta.get("chunk_count")
            if type(chunk_count) is not int or chunk_count < 1:
                raise RuntimeError("Overview chunk_count must be a positive integer")
        if meta.get("chunk_count") != chunk_count:
            raise RuntimeError("Overview chunk_count changed during delivery")
        received += 1
        yield meta, body
        if meta.get("completed") is True:
            if received != chunk_count:
                raise RuntimeError("Overview completed before its declared chunk_count")
            return
        cursor = meta.get("next_cursor")
        if not isinstance(cursor, str) or not cursor:
            raise RuntimeError("Incomplete Overview has no next_cursor")
        if cursor in seen:
            raise RuntimeError("Overview repeated a continuation cursor")
        if received >= chunk_count:
            raise RuntimeError("Overview did not complete within its declared chunk_count")
        seen.add(cursor)

# ---------- Session 1: identity, tools, rules, full overview chain, aux reads ----------
s = Session()
init = s.rpc("initialize", {"protocolVersion":"2025-06-18",
    "capabilities":{}, "clientInfo":{"name":"aoci-conformance","version":"1.0"}})
si = (init.get("result") or {}).get("serverInfo") or {}
ok("initialize.serverInfo.version",
   (si.get("version") == EXPECT_VERSION) if EXPECT_VERSION else bool(si.get("version")), str(si))
# The handshake never answers past its own era: a client offering the
# sessionless 2026-07-28 revision in initialize is answered with 2025-11-25.
probe = Session()
newest = (probe.rpc("initialize", {"protocolVersion": "2026-07-28", "capabilities": {},
    "clientInfo": {"name": "aoci-conformance-probe", "version": "1.0"}}).get("result") or {}).get("protocolVersion")
probe.close()
negotiated = (init.get("result") or {}).get("protocolVersion")
ok("initialize.protocolVersion", negotiated in ("2025-06-18","2025-03-26") and newest == "2025-11-25",
   f"offered 2025-06-18 -> {negotiated}; offered 2026-07-28 -> {newest}")
s.notify("notifications/initialized")

tl = s.rpc("tools/list")
tools = [(t.get("name")) for t in (tl.get("result") or {}).get("tools", [])]
ok("tools.count==9", len(tools)==9, str(len(tools)))
ok("tools.names", sorted(tools)==sorted(NINE), str(sorted(tools)))
schemas_ok = all(isinstance(t.get("inputSchema"), dict) for t in (tl.get("result") or {}).get("tools", []))
ok("tools.inputSchemas.present", schemas_ok)

rules_text, err = text_of(s.call("aoci_rules"))
ok("rules.no_error", not err)
ok("rules.new_numbering", all(number in rules_text for number in ("1a.", "1b.", "2a.", "2b.")))
ok("rules.machine_facts", "cognition_refresh_threshold: 30" in rules_text)

# Full overview chain with byte-level integrity verification.
# The per-chunk results are printed individually but tallied as two aggregate
# checks: the chain length depends on the index size of whatever repository
# AOCI_REPO points at, and a check count that scales with the target would make
# the published number meaningless (and would redden CI the day this repository
# crosses a chunk boundary).
chunks, meta0 = [], None
chunk_sha_mismatches = []
for i, (meta, body) in enumerate(overview_chunks(s), 1):
    print(f"PASS overview.chunk{i}.no_error")
    if meta0 is None: meta0 = meta
    chunks.append((meta, body))
    # per-chunk sha256 must match the exact body bytes
    got = hashlib.sha256(body.encode()).hexdigest()
    matched = got == meta["chunk_sha256"]
    if not matched:
        chunk_sha_mismatches.append(f"chunk{i}: got={got[:12]} want={meta['chunk_sha256'][:12]}")
    print(("PASS " if matched else "FAIL ") + f"overview.chunk{i}.sha")
ok("overview.every_chunk_no_error", True, f"{len(chunks)} chunks")
ok("overview.every_chunk_sha", not chunk_sha_mismatches,
   f"{len(chunks)} chunks; " + ("; ".join(chunk_sha_mismatches) or "all bodies match"))

metaL = chunks[-1][0]
ok("overview.chunk_count", len(chunks) == meta0["chunk_count"], f"{len(chunks)} vs {meta0['chunk_count']}")
ok("overview.completed_marker", metaL.get("completed_marker") is True)
# ordinal continuity
cont = all(chunks[i][0]["first_entry_ordinal"] == chunks[i-1][0]["last_entry_ordinal"]+1 for i in range(1,len(chunks)))
ok("overview.ordinal_continuity", cont and chunks[0][0]["first_entry_ordinal"]==1 and metaL["last_entry_ordinal"]==meta0["entry_count"])
# whole-body sha over concatenated chunk bodies
whole = "".join(b for _, b in chunks)
ok("overview.body_sha", hashlib.sha256(whole.encode()).hexdigest() == meta0["body_sha256"])
ok("overview.body_bytes", len(whole.encode()) == meta0["body_utf8_bytes"], f"{len(whole.encode())} vs {meta0['body_utf8_bytes']}")
ok("overview.end_marker", whole.rstrip("\n").endswith("<<<AOCI_OVERVIEW_BODY_END/v1 scope=all>>>"))

# entry extraction by the ordinal contract (for the mechanical attestation later)
entries = []
for ln in whole.split("\n"):
    st = ln.strip()
    if not st or st.startswith("#") or st.startswith("===") or st.startswith("<<<") or st.startswith("──") or st.startswith("AOCI Cognition Asset:"):
        continue
    if re.match(r"^\S+\[[A-Za-z0-9]+\]: F:", st):
        entries.append(st)
ok("overview.entry_parse_count", len(entries) == meta0["entry_count"], f"{len(entries)} vs {meta0['entry_count']}")

# aux read tools
t, err = text_of(s.call("aoci_header"))
ok("header.no_error_and_meta", (not err) and "#AOCI-META-VOLUME: 1" in t)
t, err = text_of(s.call("aoci_search", {"keyword":"openGauss"}))
ok("search.no_error_and_hit", (not err) and "collector_opengauss.go" in t)
ok("search.session_cognition_line", "cognition: " in t.splitlines()[-1])
t, err = text_of(s.call("aoci_get_entries", {"paths":["internal/fs/atomic.go"]}))
ok("get_entries.no_error_and_hit", (not err) and "AtomicWrite" in t)
# cognition probe roundtrip: issue questions, answer from the delivered formal
# sequence (the harness IS the model here), expect a machine pass; a wrong tag
# must fail. Section roots are historical coordinates: repo-relative identities
# derive from the first (root) section prefix per the index-format contract.
def parse_entry_seq(body):
    """Formal Entry sequence as (repo-relative identity, tag, core F), by the
    index-format contract: section roots are historical coordinates and the
    shortest root is the repository root."""
    seq, hist_root, cur_rel = [], None, None
    for ln in body.split("\n"):
        st = ln.strip()
        if st.startswith("===") and st.endswith("==="):
            path = st.strip("=")
            if hist_root is None or len(path) < len(hist_root):
                hist_root = path
            cur_rel = path[len(hist_root):] if hist_root and path.startswith(hist_root) else ""
            continue
        m = re.match(r"^(\S[^\[]*)\[([A-Za-z0-9]+)\]: F:(.*?) \| R:", st)
        if m and cur_rel is not None:
            seq.append(((cur_rel or "") + m.group(1), m.group(2), m.group(3)))
    return seq
entry_seq = parse_entry_seq(whole)
pt, perr = text_of(s.call("aoci_overview", {"check_only": True, "probe": True}))
probe = (json.loads(pt) if not perr else {}).get("cognition_probe") or {}
probe_ok = probe.get("version") == "cognition-probe/v1" and len(probe.get("ordinals") or []) == 2 \
    and len(entry_seq) == meta0["entry_count"]
ok("probe.issued", probe_ok, pt[:160])
# Graded unconditionally: an unissued probe must fail these, not silently remove
# them from the tally, or the published check count would depend on the outcome.
graded, bad, grade_detail = {}, {}, "probe not issued"
if probe_ok:
    answers = []
    for o in probe["ordinals"]:
        ident, tag, coref = entry_seq[o-1]
        answers.append({"ordinal": o, "object_identity": ident, "tag": tag, "core_f": coref})
    gt, gerr = text_of(s.call("aoci_overview", {"check_only": True, "probe_answers":
        {"version": "cognition-probe/v1", "digest": probe["digest"], "answers": answers}}))
    graded = (json.loads(gt) if not gerr else {}).get("probe_result") or {}
    grade_detail = gt[:200]
    answers[0]["tag"] = "ZZ1Z"
    bt, berr = text_of(s.call("aoci_overview", {"check_only": True, "probe_answers":
        {"version": "cognition-probe/v1", "digest": probe["digest"], "answers": answers}}))
    bad = (json.loads(bt) if not berr else {}).get("probe_result") or {}
    grade_detail = (gt + " || " + bt)[:200]
ok("probe.correct_answers_pass", graded.get("result") == "pass", grade_detail)
ok("probe.wrong_answer_fails", bad.get("result") == "fail", grade_detail)

# split proof halves, order A: attestation alone grades pass but cannot latch
# (delivery unconfirmed); the later confirmation-only call must pick that pass
# up from session memory and latch reliability.
def attestation_for(mfin, m0, seq, ords, wreck_f=(), wreck_ident=(), bare=False, mastery=90):
    answers = []
    for i, o in enumerate(ords):
        ident, tag, coref = seq[o-1]
        if bare: ident = ident.rsplit("/", 1)[-1]
        if i in wreck_ident: ident = "internal/definitely/not/this.go"
        if i in wreck_f: coref = "totally wrong responsibility"
        answers.append({"ordinal": o, "object_identity": ident, "tag": tag, "core_f": coref})
    return {"version": "model-cognition-attestation/v1",
        "index_sha256": mfin["challenge_index_sha256"],
        "entry_sequence_sha256": mfin["challenge_entry_sequence_sha256"],
        "entry_count": mfin["challenge_entry_count"], "challenge_digest": mfin["challenge_digest"],
        "reported_entry_count": m0["entry_count"], "reported_estimated_tokens": m0["estimated_tokens"],
        "coverage_percent": 100, "system_mastery_percent": mastery, "confidence_percent": 90,
        "truncation_detected": False, "unseen_sections": [], "uncertainty_reasons": [],
        "challenge_answers": answers}
ords1 = metaL["challenge_ordinals"] if isinstance(metaL["challenge_ordinals"], list) else [int(x) for x in str(metaL["challenge_ordinals"]).split(",")]
confirmation1 = {"version": "overview-delivery-receipt/v1", "body_sha256": meta0["body_sha256"],
                 "body_bytes": meta0["body_utf8_bytes"], "end_marker_observed": True}
at, aerr = text_of(s.call("aoci_overview", {"model_cognition_attestation": attestation_for(metaL, meta0, entry_seq, ords1)}))
ok("attestation.alone_passes_but_unlatched", (not aerr) and "model_attestation: pass" in at
   and "delivery_integrity: unconfirmed" in at and "model_full_cognition_reliable: false" in at, at[:240])
ct, cerr = text_of(s.call("aoci_overview", {"host_delivery_confirmation": confirmation1}))
ok("attestation.confirmation_after_attestation_latches", (not cerr) and "model_full_cognition_reliable: true" in ct
   and "cognition_level: 4" in ct, ct[:240])

t, err = text_of(s.call("aoci_maintain"))
def maintain_diag(text):
    """截断的整包 JSON 无法定位阻塞原因; 只提取有界的关键治理事实。"""
    try:
        d = json.loads(text)
    except Exception:
        return text[:200]
    gov = d.get("governance") or {}
    cd = gov.get("code_drift") or {}
    ms = gov.get("managed_scope") or {}
    return json.dumps({
        "result": d.get("result"),
        "findings": [f.get("code") for f in (gov.get("findings") or [])][:8],
        "stale_n": len(cd.get("stale") or []), "stale": (cd.get("stale") or [])[:3],
        "missing_n": len(cd.get("missing") or []), "missing": (cd.get("missing") or [])[:3],
        "orphan_n": len(cd.get("orphan") or []), "unbaselined_n": len(cd.get("unbaselined") or []),
        "observed_pending_review": ms.get("observed_pending_review"),
        "scope_change_required": ms.get("scope_change_required"),
        "policy_match": ms.get("policy_identity") == ms.get("active_policy_identity"),
        "recovery_pending": gov.get("recovery_pending"),
        "third_party_conflict": gov.get("third_party_conflict"),
        "budget_status": (gov.get("budget") or {}).get("status"),
    }, ensure_ascii=False)
ok("maintain.aligned_no_candidates", (not err) and '"aligned":true' in t.replace(" ",""), maintain_diag(t))
# the authoring batch envelope advertises the machine-default team batch size
# (50): the cap under which the 24 KiB transport budget cuts each batch so a
# Maintain response fits ordinary host tool-result windows, never the 200
# wire ceiling.
try:
    _mb = (json.loads(t).get("authoring_batch") or {}).get("max_entries")
except Exception:
    _mb = None
ok("maintain.default_batch_cap_50", _mb == 50, f"authoring_batch.max_entries={_mb}")
# Parallel current batches (L4): the default configuration keeps one current
# batch per Maintain and its response carries no sibling field, byte for byte
# what the single-batch contract describes; a batch_id request is refused on a
# server whose configuration keeps one current batch, with zero writes.
try:
    _cp = json.loads(t).get("code_plan") or {}
except Exception:
    _cp = {}
ok("maintain.default_config_carries_no_parallel_fields",
   not any(k in _cp for k in ("logical_plan", "siblings", "sibling_of")) and '"siblings"' not in t and '"sibling_of"' not in t,
   f"code_plan keys={sorted(_cp.keys())[:12]}")
_pt, _perr = text_of(s.call("aoci_maintain", {"batch_id": "0" * 64}))
ok("maintain.batch_id_refused_without_parallel_batches", _perr and "maintain_batch_id_requires_parallel_batches" in _pt, _pt[:160])

# negative paths (same session)
bad = s.call("aoci_overview", {"cursor":"deadbeef:8000:1:deadbeef"})
bt, berr = text_of(bad)
ok("negative.bad_cursor_rejected", berr or "error" in bt.lower() or bad.get("error") is not None)
unk = s.rpc("tools/call", {"name":"aoci_nonexistent","arguments":{}})
ok("negative.unknown_tool", unk.get("error") is not None or text_of(unk)[1])
# zero-write negatives for the write-capable tools
t, err = text_of(s.call("aoci_remove_entry", {"path":"code:internal/fs/atomic.go"}))
ok("negative.remove_nonorphan_rejected", err or "orphan" in t.lower() or "error" in t.lower(), t[:150])
t, err = text_of(s.call("aoci_update_entry", {"code_batch_id":"0"*64, "entries":[{"path":"README.md","source_sha256":"0"*64,"candidate_id":"0"*64,"new_entry":"README.md[CG5L]: F:x | R:- | A:- | S:-"}]}))
ok("negative.update_bogus_batch_rejected", err or "stopped" in t or "error" in t.lower(), t[:150])
t, err = text_of(s.call("aoci_report", {"path":"README.md","note":"conformance harness probe; ignore"}))
ok("report.accepted_or_clean_reject", True, "")  # either outcome is contract-legal; just must not crash
ok("stdout.purity.session1", not s.nonjson_stdout, str(s.nonjson_stdout[:2]))
s.close()

# ---------- Session 2: wrong attestation must fail cleanly ----------
s2 = Session()
s2.rpc("initialize", {"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"h2","version":"1"}})
s2.notify("notifications/initialized")
c2 = list(overview_chunks(s2))
mfin = c2[-1][0]; m0 = c2[0][0]
whole2 = "".join(b for _,b in c2)
ords = mfin["challenge_ordinals"] if isinstance(mfin["challenge_ordinals"], list) else [int(x) for x in str(mfin["challenge_ordinals"]).split(",")]
seq2 = parse_entry_seq(whole2)
ok("overview.session2_entry_parse_count", len(seq2) == m0["entry_count"], f"{len(seq2)} vs {m0['entry_count']}")
confirmation2 = {"version": "overview-delivery-receipt/v1", "body_sha256": m0["body_sha256"],
                 "body_bytes": m0["body_utf8_bytes"], "end_marker_observed": True}
def attest2(**kw):
    return text_of(s2.call("aoci_overview", {"host_delivery_confirmation": confirmation2,
        "model_cognition_attestation": attestation_for(mfin, m0, seq2, ords, **kw)}))
# split proof halves, order B: confirmation first, attestation second.
ct, cerr = text_of(s2.call("aoci_overview", {"host_delivery_confirmation": confirmation2}))
ok("attestation.confirmation_alone_is_level_2", (not cerr) and "host_delivery_status: host_delivery_confirmed" in ct
   and "cognition_level: 2" in ct and "model_full_cognition_reliable: false" in ct, ct[:240])
at, aerr = text_of(s2.call("aoci_overview", {"model_cognition_attestation": attestation_for(mfin, m0, seq2, ords)}))
ok("attestation.attestation_after_confirmation_latches", (not aerr) and "model_full_cognition_reliable: true" in at
   and "challenge_passed: 10/10" in at, at[:240])
# grading ladder: bare file names miss every identity (fail); 7/10 is below the
# pass share (partial, never fail for an honest complete claim); two identity
# misses exceed the identity cap even at 8/10 (partial); two core-F misses at
# 8/10 pass because F is judged semantically and identity is intact.
wt, werr = attest2(bare=True)
ok("attestation.wrong_answer_fails", (not werr) and "model_attestation: fail" in wt, wt[:200])
pt, perr = attest2(wreck_f=(0,1,2))
ok("attestation.seven_of_ten_is_partial", (not perr) and "challenge_passed: 7/10" in pt
   and "model_attestation: partial" in pt and "model_full_cognition_reliable: false" in pt, pt[:240])
it, ierr = attest2(wreck_ident=(0,1))
ok("attestation.two_identity_misses_partial", (not ierr) and "challenge_passed: 8/10" in it
   and "model_attestation: partial" in it, it[:240])
et, eerr = attest2(wreck_f=(0,1))
ok("attestation.eight_of_ten_passes", (not eerr) and "challenge_passed: 8/10" in et
   and "model_attestation: pass" in et and "model_full_cognition_reliable: true" in et, et[:240])
ok("stdout.purity.session2", not s2.nonjson_stdout)
s2.close()

# ---------- Session 3: malformed input line -> orderly fail-closed shutdown ----------
import subprocess as sp
p3 = sp.Popen([BIN,"--repo",REPO,"mcp"], stdin=sp.PIPE, stdout=sp.PIPE, stderr=sp.PIPE, text=True, encoding="utf-8", bufsize=1)
with rpc_deadline(p3, "initialize", 120):
    p3.stdin.write(json.dumps({"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"h3","version":"1"}}})+"\n"); p3.stdin.flush()
    p3.stdout.readline()
p3.stdin.write('{"jsonrpc":"2.0","method":"notifications/initialized"}\n'); p3.stdin.flush()
p3.stdin.write("this is not json {\n"); p3.stdin.flush()
try:
    out3, err3 = p3.communicate(timeout=10)
except Exception:
    p3.kill(); out3, err3 = "", ""
ok("malformed.orderly_exit_code_1", p3.returncode == 1, str(p3.returncode))
ok("malformed.stderr_diagnostic", bool(err3.strip()))
ok("malformed.stdout_stays_pure", out3.strip() == "", repr(out3[:80]))

# ---------- Session 4: locale twins — zh-CN repeats the locale-dependent checks ----------
# Two identical three-file fixtures are minted under a temp directory, one with
# `init --locale en-US` and one with `init --locale zh-CN`, and authored to
# aligned through the same template batch. Human sentences must follow the
# locale; every machine key, field, code, and digest contract must not. The host
# repository stays read-only: nothing here touches REPO.
def has_cjk(text):
    return any("一" <= ch <= "鿿" for ch in str(text))

def _sh(cwd, *args):
    r = subprocess.run(list(args), cwd=cwd, capture_output=True, text=True, encoding="utf-8", errors="replace", timeout=120)
    if r.returncode != 0:
        raise RuntimeError(f"{args}: {r.stderr[:200]}")

def _cli(repo, *args):
    r = subprocess.run([BIN, "--repo", repo, *args, "--json"], capture_output=True, text=True, encoding="utf-8", errors="replace", timeout=120)
    try:
        body = json.loads(r.stdout)
    except ValueError:
        body = {}
    return r.returncode, body, r.stdout + r.stderr

def _jload(text):
    try:
        return json.loads(text[text.index("{"):text.rindex("}") + 1])
    except Exception:
        return {}

def parse_kv(text):
    out = {}
    for ln in text.splitlines():
        m = re.match(r"^([a-z_0-9.]+):\s*(.*)$", ln.strip())
        if m:
            k, v = m.group(1), m.group(2)
            out[k] = (v == "true") if v in ("true", "false") else (int(v) if re.fullmatch(r"-?\d+", v) else v)
    return out

def overview_single(text):
    """A small index is delivered in one response: kv metadata, the body from
    its BEGIN line through its END line (what body_sha256 covers), then the
    cognition-level lines; a chunked one keeps the JSON-plus-MARK shape."""
    if MARK in text:
        return meta_and_body(text)
    begin = text.find("<<<AOCI_OVERVIEW_BODY_BEGIN")
    end = text.find("<<<AOCI_OVERVIEW_BODY_END", begin)
    if begin < 0 or end < 0:
        return parse_kv(text), None
    stop = text.find("\n", end)
    stop = len(text) if stop < 0 else stop + 1
    return parse_kv(text[:begin] + text[stop:]), text[begin:stop]

def locale_twin(work, locale):
    """Mint one twin: three sources, `init --locale`, scan, and the same
    template batch through aoci_maintain/aoci_update_entry. Returns (root, session)."""
    fx = os.path.join(work, f"twin-{locale}")
    os.makedirs(os.path.join(fx, "pkg"))
    for i in (1, 2, 3):
        with open(os.path.join(fx, "pkg", f"f{i:03}.go"), "w", encoding="utf-8") as fh:
            fh.write(f"package pkg\n\n// fixture unit {i}\nfunc F{i:03}() int {{ return {i} }}\n")
    _sh(fx, "git", "init", "-q")
    _sh(fx, "git", "config", "user.email", "fixture@test.invalid")
    _sh(fx, "git", "config", "user.name", "fixture")
    _sh(fx, "git", "add", "-A")
    _sh(fx, "git", "commit", "-q", "-m", "fixture")
    for step in (("init", "--locale", locale), ("scan",)):
        rc, _, out = _cli(fx, *step)
        if rc != 0:
            raise RuntimeError(f"{step[0]} failed under {locale}: {out[:200]}")
    session = Session(repo=fx)
    session.rpc("initialize", {"protocolVersion": "2025-06-18", "capabilities": {},
                               "clientInfo": {"name": "aoci-conformance-locale", "version": "1.0"}})
    session.notify("notifications/initialized")
    for _ in range(4):
        m = _jload(text_of(session.call("aoci_maintain"))[0])
        if m.get("aligned") is True:
            break
        entries = [{"path": c["path"], "source_sha256": c["source_sha256"], "candidate_id": c["candidate_id"],
                    "new_entry": f"{os.path.basename(c['path'])}[CG5T]: F:Provides fixture unit "
                                 f"{os.path.basename(c['path'])} | R:- | A:- | S:-"}
                   for c in (m.get("candidates") or [])]
        r = _jload(text_of(session.call("aoci_update_entry", {
            "code_batch_id": (m.get("code_plan") or {}).get("batch_id"), "entries": entries}, timeout=300))[0])
        if r.get("aligned") is True:
            break
    return fx, session

LOCALE_CHECKS = ["locale.twin_fixtures_aligned", "locale.rules_zh_chinese_same_machine_facts",
                 "locale.overview_meta_keys_identical_message_localized",
                 "locale.attestation_machine_lines_identical", "locale.read_tools_localized_same_machine_tokens",
                 "locale.maintain_machine_fields_identical", "locale.error_code_identical_hint_localized"]
MACHINE_LINES = ("model_attestation", "model_full_cognition_reliable", "cognition_level", "cognition_level_state",
                 "delivery_integrity", "host_delivery_status", "cognition_assimilation", "challenge_passed")
_work = tempfile.mkdtemp(prefix="aoci-conformance-locale-")
_twins, _verdicts, _abort = {}, {}, ""
try:
    for _loc in ("en-US", "zh-CN"):
        _twins[_loc] = locale_twin(_work, _loc)
    (fxE, sE), (fxZ, sZ) = _twins["en-US"], _twins["zh-CN"]

    def _aligned(fx):
        rc, body, _ = _cli(fx, "verify")
        return rc == 0 and bool((body.get("governance") or {}).get("governance_aligned"))
    alE, alZ = _aligned(fxE), _aligned(fxZ)
    _verdicts["locale.twin_fixtures_aligned"] = (alE and alZ, f"en-US aligned={alE} zh-CN aligned={alZ}")

    rulesE, _ = text_of(sE.call("aoci_rules"))
    rulesZ, _ = text_of(sZ.call("aoci_rules"))
    def _machine_facts(text):
        return [ln for ln in text.splitlines() if re.match(r"^(cognition_refresh_threshold|overview_delivery\.[a-z_]+): ", ln)]
    anchors = ("1a.", "1b.", "2a.", "2b.")
    cond = (has_cjk(rulesZ) and not has_cjk(rulesE) and all(a in rulesZ for a in anchors) and all(a in rulesE for a in anchors)
            and _machine_facts(rulesE) == _machine_facts(rulesZ) and len(_machine_facts(rulesZ)) == 4
            and rulesE.splitlines()[0] != rulesZ.splitlines()[0])
    _verdicts["locale.rules_zh_chinese_same_machine_facts"] = (cond,
        f"zh_cjk={has_cjk(rulesZ)} en_cjk={has_cjk(rulesE)} machine_facts={_machine_facts(rulesZ)} zh_title={rulesZ.splitlines()[0][:40]!r}")

    oE, _ = text_of(sE.call("aoci_overview"))
    oZ, _ = text_of(sZ.call("aoci_overview"))
    mE, bE = overview_single(oE)
    mZ, bZ = overview_single(oZ)
    same_keys = set(mE) == set(mZ)
    machine_keys = ("entry_count", "section_count", "completed", "delivery_mode", "cognition_level",
                    "cognition_level_state", "host_delivery_status", "layout_mode", "effective_scope")
    same_machine = all(mE.get(k) == mZ.get(k) for k in machine_keys)
    body_ok = (bE is not None and bZ is not None
               and hashlib.sha256(bE.encode()).hexdigest() == mE.get("body_sha256")
               and hashlib.sha256(bZ.encode()).hexdigest() == mZ.get("body_sha256"))
    msgE, msgZ = mE.get("cognition_level_message"), mZ.get("cognition_level_message")
    msg_ok = has_cjk(msgZ) and not has_cjk(msgE) and msgE != msgZ
    _verdicts["locale.overview_meta_keys_identical_message_localized"] = (same_keys and same_machine and body_ok and msg_ok,
        f"keys_same={same_keys} machine_same={same_machine} body_sha={body_ok} en={str(msgE)[:40]!r} zh={str(msgZ)[:30]!r}"
        + ("" if same_keys else f" diff={sorted(set(mE) ^ set(mZ))}"))

    def _attest(session, meta, body):
        ords = meta["challenge_ordinals"] if isinstance(meta["challenge_ordinals"], list) \
            else [int(x) for x in str(meta["challenge_ordinals"]).split(",")]
        confirmation = {"version": "overview-delivery-receipt/v1", "body_sha256": meta["body_sha256"],
                        "body_bytes": meta["body_utf8_bytes"], "end_marker_observed": True}
        text, _err = text_of(session.call("aoci_overview", {"host_delivery_confirmation": confirmation,
            "model_cognition_attestation": attestation_for(meta, meta, parse_entry_seq(body), ords)}))
        return text, sorted(ln for ln in text.splitlines() if ln.split(":", 1)[0] in MACHINE_LINES)
    aE, linesE = _attest(sE, mE, bE)
    aZ, linesZ = _attest(sZ, mZ, bZ)
    cond = (linesE == linesZ and "model_attestation: pass" in linesE and "model_full_cognition_reliable: true" in linesE
            and has_cjk(aZ) and not has_cjk(aE))
    _verdicts["locale.attestation_machine_lines_identical"] = (cond, f"en={linesE} zh={linesZ}"[:300])

    hE, _ = text_of(sE.call("aoci_header"))
    hZ, _ = text_of(sZ.call("aoci_header"))
    qE, _ = text_of(sE.call("aoci_search", {"keyword": "fixture"}))
    qZ, _ = text_of(sZ.call("aoci_search", {"keyword": "fixture"}))
    gE, _ = text_of(sE.call("aoci_get_entries", {"paths": ["pkg/f001.go"]}))
    gZ, _ = text_of(sZ.call("aoci_get_entries", {"paths": ["pkg/f001.go"]}))
    entry1 = "f001.go[CG5T]: F:Provides fixture unit f001.go | R:- | A:- | S:-"
    cond = (all("#AOCI-META-VOLUME: 1" in h for h in (hE, hZ)) and all("object_ref=code:pkg/f001.go" in q for q in (qE, qZ))
            and all(entry1 in gtext for gtext in (gE, gZ)) and all("cognition: " in x.splitlines()[-1] for x in (qE, qZ, gE, gZ))
            and has_cjk(hZ) and has_cjk(qZ) and has_cjk(gZ) and not (has_cjk(hE) or has_cjk(qE) or has_cjk(gE)))
    _verdicts["locale.read_tools_localized_same_machine_tokens"] = (cond,
        f"zh header={hZ.splitlines()[0][:30]!r} search={qZ.splitlines()[0][:30]!r} entries={gZ.splitlines()[0][:30]!r}")

    mtE = _jload(text_of(sE.call("aoci_maintain"))[0])
    mtZ = _jload(text_of(sZ.call("aoci_maintain"))[0])
    cond = (mtE.get("aligned") is True and mtZ.get("aligned") is True and set(mtE) == set(mtZ)
            and all(mtE.get(k) == mtZ.get(k) for k in ("version", "result", "status", "next_action")))
    _verdicts["locale.maintain_machine_fields_identical"] = (cond,
        f"aligned={mtE.get('aligned')},{mtZ.get('aligned')} keys_same={set(mtE) == set(mtZ)} "
        f"result={mtE.get('result')},{mtZ.get('result')}")

    rE, eE = text_of(sE.call("aoci_remove_entry", {"path": "code:pkg/f001.go"}))
    rZ, eZ = text_of(sZ.call("aoci_remove_entry", {"path": "code:pkg/f001.go"}))
    cond = (eE and eZ and all("[bad_args] remove_target_not_proven_orphan" in r for r in (rE, rZ))
            and all("run_aoci_maintain_and_use_its_exact_orphan_candidate" in r for r in (rE, rZ))
            and has_cjk(rZ) and not has_cjk(rE))
    _verdicts["locale.error_code_identical_hint_localized"] = (cond, f"en={rE[:90]!r} zh={rZ[:60]!r}")
except Exception as _err:  # noqa: BLE001 - every unreached locale check fails with this reason
    _abort = f"locale lane aborted: {_err}"[:200]
finally:
    for _fx, _session in _twins.values():
        _session.close()
    shutil.rmtree(_work, ignore_errors=True)
for _name in LOCALE_CHECKS:
    _cond, _detail = _verdicts.get(_name, (False, _abort or "not reached"))
    ok(_name, _cond, _detail)

# ---------- Session 5: the sessionless 2026-07-28 revision ----------
# A client on this revision never calls initialize: every request carries the
# revision, client information, and capabilities in _meta. The lane walks what
# a Host reaches that way: discovery, the nine tools, the rules, and a complete
# Overview chain attested to the strict level.
SESSIONLESS = "2026-07-28"
class SessionlessSession(Session):
    META = {"io.modelcontextprotocol/protocolVersion": SESSIONLESS,
            "io.modelcontextprotocol/clientInfo": {"name": "aoci-conformance-sessionless", "version": "1.0"},
            "io.modelcontextprotocol/clientCapabilities": {}}
    def rpc(self, method, params=None, timeout=120):
        params = dict(params or {})
        params["_meta"] = dict(self.META)
        return super().rpc(method, params, timeout)

s5 = SessionlessSession()
discovered = s5.rpc("server/discover").get("result") or {}
ok("sessionless.discover", discovered.get("supportedVersions") == [SESSIONLESS, "2025-11-25", "2025-06-18", "2025-03-26", "2024-11-05"]
   and "tools" in (discovered.get("capabilities") or {}), str(discovered)[:200])
listed = (s5.rpc("tools/list").get("result") or {}).get("tools", [])
ok("sessionless.tools", sorted(t.get("name") for t in listed) == sorted(NINE)
   and all(isinstance(t.get("inputSchema"), dict) for t in listed), str(len(listed)))
rules5, rerr5 = text_of(s5.call("aoci_rules"))
ok("sessionless.rules", (not rerr5) and "cognition_refresh_threshold: 30" in rules5, rules5[:120])
attested5 = ""
try:
    chunks5 = list(overview_chunks(s5))
    first5, last5 = chunks5[0][0], chunks5[-1][0]
    whole5 = "".join(b for _, b in chunks5)
    seq5 = parse_entry_seq(whole5)
    ords5 = last5["challenge_ordinals"] if isinstance(last5["challenge_ordinals"], list) else [int(x) for x in str(last5["challenge_ordinals"]).split(",")]
    attested5, aerr5 = text_of(s5.call("aoci_overview", {
        "host_delivery_confirmation": {"version": "overview-delivery-receipt/v1", "body_sha256": first5["body_sha256"],
                                       "body_bytes": first5["body_utf8_bytes"], "end_marker_observed": True},
        "model_cognition_attestation": attestation_for(last5, first5, seq5, ords5)}))
    chain5 = (not aerr5) and hashlib.sha256(whole5.encode()).hexdigest() == first5["body_sha256"] and len(chunks5) == first5["chunk_count"]
except Exception as error:
    chain5, attested5 = False, f"chain failed: {error}"
ok("sessionless.overview_attested", chain5 and "model_attestation: pass" in attested5 and "cognition_level: 4" in attested5, attested5[:240])
ok("sessionless.stdout_purity", not s5.nonjson_stdout, str(s5.nonjson_stdout[:2]))
s5.close()

# ---------- host window: every non-Overview tool result fits an ordinary host window ----------
# Claude Code spills tool results past ~25k tokens to a file; a real user's index
# build collapsed on exactly that. Overview is chunked by its own budget and only reported.
_peak = {}
for _t, _b in RESPONSE_SIZES:
    _peak[_t] = max(_peak.get(_t, 0), _b)
_worst = max([b for t, b in _peak.items() if t != "aoci_overview"] or [0])
ok("host_window.non_overview_responses_fit", _worst <= HOST_WINDOW_BYTES,
   "peak: " + " ".join(f"{t.replace('aoci_', '')}={b}" for t, b in sorted(_peak.items())))

# ---------- documentation binding: the published check count must match this run ----------
# Three separate documents advertise this number to downstream verifiers, and
# nothing used to hold them to it: README.md, README.zh-CN.md and this suite's
# own README all drifted to a stale 44 while the suite grew. The run is the
# authority, so the run enforces the documents.
#
# This binding deliberately stays outside PASS/FAIL: the published number counts
# checks of the MCP wire surface, and counting a documentation check among them
# would make the number describe itself.
_TOTAL_CHECKS = len(PASS) + len(FAIL) + len(SKIP)
_DOC_COUNTS = {
    "README.md": r"\*\*Protocol conformance\*\* — (\d+) read-only checks",
    "README.zh-CN.md": r"\*\*协议一致性\*\* —— (\d+) 项只读检查",
    os.path.join("scripts", "blackbox", "README.md"): r"Protocol conformance \((\d+) checks, read-only\)",
    os.path.join("scripts", "blackbox", "mcp_conformance.py"): r"一致性检查（(\d+)项）",
}
#
# The documents belong to this script's own repository, never to AOCI_REPO: the
# suite is documented as runnable against any target, and a foreign target has
# no reason to carry these files.
_doc_drift = []
for _rel, _pattern in (_DOC_COUNTS.items() if REPO == _REPO_DEFAULT else []):
    try:
        with open(os.path.join(_REPO_DEFAULT, _rel), encoding="utf-8") as _fh:
            _match = re.search(_pattern, _fh.read())
    except OSError as _err:
        _doc_drift.append(f"{_rel}: unreadable ({_err})")
        continue
    if _match is None:
        _doc_drift.append(f"{_rel}: no documented check count found")
    elif int(_match.group(1)) != _TOTAL_CHECKS:
        _doc_drift.append(f"{_rel}: documents {_match.group(1)}, this run has {_TOTAL_CHECKS}")
print(("PASS " if not _doc_drift else "FAIL ") + "docs.published_check_count_matches_this_run"
      + ((" | " + "; ".join(_doc_drift)) if _doc_drift else f" | {_TOTAL_CHECKS}"))

print()
print(f"RESULT: {len(PASS)} passed, {len(FAIL)} failed" + (f", {len(SKIP)} skipped" if SKIP else ""))
for n, d in FAIL:
    print("  FAILED:", n, "|", d[:200])
for n, d in SKIP:
    print("  SKIPPED:", n, "|", d)
if _doc_drift:
    print("  FAILED: docs.published_check_count_matches_this_run |", "; ".join(_doc_drift))
sys.exit(1 if (FAIL or _doc_drift) else 0)
