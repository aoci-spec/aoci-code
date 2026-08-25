#!/usr/bin/env python3
"""AOCI MCP stdio conformance harness — drives build/aoci mcp as a real client.
Protocol-level tests only; zero formal writes expected (verified afterward by git/verify).

MCP stdio 一致性检查（46项）：以真实客户端身份走 initialize→tools 全链路，
只读不写，跑完用 git status / aoci verify 复核零写入。
本脚本跑完会核对公开文档里公布的检查数与本次实跑一致，防止文档悄悄漂移。

Usage:  python3 scripts/blackbox/mcp_conformance.py
Env:    AOCI_REPO / AOCI_BIN 覆盖仓库与二进制路径（默认取本脚本所在仓库）;
        AOCI_EXPECT_VERSION 设定后严格断言 serverInfo.version，否则只断言非空。
Requires: an established repository (aoci init + scan done) and a built binary.
"""
import hashlib, json, os, re, subprocess, sys, time

_REPO_DEFAULT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
REPO = os.environ.get("AOCI_REPO", _REPO_DEFAULT)
BIN = os.environ.get("AOCI_BIN", os.path.join(REPO, "build", "aoci"))
EXPECT_VERSION = os.environ.get("AOCI_EXPECT_VERSION", "")
NINE = ["aoci_rules","aoci_overview","aoci_get_entries","aoci_search",
        "aoci_update_entry","aoci_report","aoci_remove_entry","aoci_header","aoci_maintain"]

PASS, FAIL = [], []
def ok(name, cond, detail=""):
    (PASS if cond else FAIL).append((name, detail))
    print(("PASS " if cond else "FAIL ") + name + ((" | " + detail[:140]) if detail and not cond else ""))

# host-window ledger: (tool, utf8 bytes) for every tools/call result received.
RESPONSE_SIZES = []
HOST_WINDOW_BYTES = 64 * 1024

class Session:
    def __init__(self):
        self.p = subprocess.Popen([BIN, "--repo", REPO, "mcp"],
            stdin=subprocess.PIPE, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            text=True, encoding="utf-8", bufsize=1)
        self.next_id = 1
        self.nonjson_stdout = []
    def send_raw(self, line):
        self.p.stdin.write(line + "\n"); self.p.stdin.flush()
    def rpc(self, method, params=None, timeout=120):
        rid = self.next_id; self.next_id += 1
        msg = {"jsonrpc":"2.0","id":rid,"method":method}
        if params is not None: msg["params"] = params
        self.send_raw(json.dumps(msg))
        deadline = time.time() + timeout
        while time.time() < deadline:
            line = self.p.stdout.readline()
            if not line:
                raise RuntimeError("server closed stdout")
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
        raise TimeoutError(method)
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

def text_of(resp):
    r = resp.get("result") or {}
    parts = r.get("content") or []
    text = "\n".join(c.get("text","") for c in parts if c.get("type")=="text")
    if not text and r.get("_meta"):
        text = "\n".join(f"{k}: {json.dumps(v, ensure_ascii=False) if not isinstance(v, str) else v}"
                         for k, v in sorted(r["_meta"].items()))
    return text, bool(r.get("isError"))

def overview_of(resp):
    """Return Host-private Overview metadata and model-visible body separately."""
    r = resp.get("result") or {}
    body = "\n".join(c.get("text", "") for c in r.get("content") or []
                     if c.get("type") == "text")
    return r.get("_meta") or {}, body, bool(r.get("isError"))

# ---------- Session 1: identity, tools, rules, full overview chain, aux reads ----------
s = Session()
init = s.rpc("initialize", {"protocolVersion":"2025-06-18",
    "capabilities":{}, "clientInfo":{"name":"aoci-conformance","version":"1.0"}})
si = (init.get("result") or {}).get("serverInfo") or {}
ok("initialize.serverInfo.version",
   (si.get("version") == EXPECT_VERSION) if EXPECT_VERSION else bool(si.get("version")), str(si))
ok("initialize.protocolVersion", (init.get("result") or {}).get("protocolVersion") in ("2025-06-18","2025-03-26"), str(init.get("result",{}).get("protocolVersion")))
s.notify("notifications/initialized")

tl = s.rpc("tools/list")
tools = [(t.get("name")) for t in (tl.get("result") or {}).get("tools", [])]
ok("tools.count==9", len(tools)==9, str(len(tools)))
ok("tools.names", sorted(tools)==sorted(NINE), str(sorted(tools)))
schemas_ok = all(isinstance(t.get("inputSchema"), dict) for t in (tl.get("result") or {}).get("tools", []))
update_schema = next((t.get("inputSchema") or {} for t in (tl.get("result") or {}).get("tools", [])
                      if t.get("name") == "aoci_update_entry"), {})
schemas_ok = schemas_ok and "target_index" in (update_schema.get("properties") or {})
ok("tools.inputSchemas.present", schemas_ok)

rules_text, err = text_of(s.call("aoci_rules"))
ok("rules.no_error", not err)
ok("rules.new_numbering", all(number in rules_text for number in ("1a.", "2a.", "4a.", "4d.")))
ok("rules.machine_facts", "cognition_refresh_threshold: 30" in rules_text)

# Full overview chain with byte-level integrity verification.
# The per-chunk results are printed individually but tallied as two aggregate
# checks: the chain length depends on the index size of whatever repository
# AOCI_REPO points at, and a check count that scales with the target would make
# the published number meaningless (and would redden CI the day this repository
# crosses a chunk boundary).
chunks, cursor, meta0 = [], None, None
chunk_errors, chunk_sha_mismatches = [], []
model_content_shapes = []
for i in range(1, 12):
    args = {"cursor": cursor} if cursor else {}
    response = s.call("aoci_overview", args)
    meta, body, err = overview_of(response)
    raw_result = response.get("result") or {}
    visible = raw_result.get("content") or []
    model_content_shapes.append(len(visible) == 1 and visible[0].get("type") == "text"
                                and "structuredContent" not in raw_result
                                and "AOCI Overview Metadata:" not in body)
    if err:
        chunk_errors.append(f"chunk{i}")
    print(("PASS " if not err else "FAIL ") + f"overview.chunk{i}.no_error")
    if meta0 is None: meta0 = meta
    chunks.append((meta, body))
    # per-chunk sha256 must match the exact body bytes
    got = hashlib.sha256(body.encode()).hexdigest()
    expected_sha = meta.get("chunk_sha256", meta.get("body_sha256"))
    matched = got == expected_sha
    if not matched:
        chunk_sha_mismatches.append(f"chunk{i}: got={got[:12]} want={str(expected_sha)[:12]}")
    print(("PASS " if matched else "FAIL ") + f"overview.chunk{i}.sha")
    if meta.get("completed"): break
    cursor = meta["next_cursor"]
ok("overview.every_chunk_no_error", not chunk_errors,
   f"{len(chunks)} chunks; failed: " + ", ".join(chunk_errors))
ok("overview.every_chunk_sha", not chunk_sha_mismatches,
   f"{len(chunks)} chunks; " + ("; ".join(chunk_sha_mismatches) or "all bodies match"))

metaL = chunks[-1][0]
ok("overview.chunk_count", len(chunks) == 1 and meta0.get("delivery_mode") == "full"
   and meta0.get("completed") is True and meta0.get("continuation_required") is False,
   f"chunks={len(chunks)} mode={meta0.get('delivery_mode')} completed={meta0.get('completed')}")
end_marker_ok = bool(re.search(r"<<<AOCI_OVERVIEW_BODY_END/v1 scope=[^>]+>>>\n?$", "".join(b for _, b in chunks)))
ok("overview.completed_marker", metaL.get("completed_marker", end_marker_ok) is True)
# ordinal continuity
cont = all(chunks[i][0]["first_entry_ordinal"] == chunks[i-1][0]["last_entry_ordinal"]+1 for i in range(1,len(chunks)))
ordinal_ok = (cont and chunks[0][0]["first_entry_ordinal"]==1 and metaL["last_entry_ordinal"]==meta0["entry_count"]) \
    if meta0.get("delivery_mode") == "chunked_full" else True
ok("overview.ordinal_continuity", ordinal_ok and all(model_content_shapes))
# whole-body sha over concatenated chunk bodies
whole = "".join(b for _, b in chunks)
ok("overview.body_sha", hashlib.sha256(whole.encode()).hexdigest() == meta0["body_sha256"])
ok("overview.body_bytes", len(whole.encode()) == meta0["body_utf8_bytes"], f"{len(whole.encode())} vs {meta0['body_utf8_bytes']}")
ok("overview.end_marker", bool(re.search(r"<<<AOCI_OVERVIEW_BODY_END/v1 scope=[^>]+>>>$", whole.rstrip("\n"))))

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

ok("overview.single_response_no_host_confirmation",
   meta0.get("host_delivery_confirmation_required") is False, str(meta0)[:240])
ok("overview.single_response_no_model_attestation",
   meta0.get("model_cognition_attestation_required") is False, str(meta0)[:240])

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
# (20): sized for inline authoring in one call and a Maintain response that
# fits ordinary host tool-result windows, never the 200 wire ceiling.
try:
    _mb = (json.loads(t).get("authoring_batch") or {}).get("max_entries")
except Exception:
    _mb = None
ok("maintain.default_batch_size_20", _mb == 20, f"authoring_batch.max_entries={_mb}")

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
mixed_t, mixed_err = text_of(s.call("aoci_update_entry", {"target_index":"aoci.code.target.txt", "path":"README.md", "new_entry":"x"}))
ok("negative.update_bogus_batch_rejected", (err or "stopped" in t or "error" in t.lower())
   and (mixed_err or "mixed" in mixed_t.lower() or "error" in mixed_t.lower()), (t + " | " + mixed_t)[:150])
t, err = text_of(s.call("aoci_report", {"path":"README.md","note":"conformance harness probe; ignore"}))
ok("report.accepted_or_clean_reject", True, "")  # either outcome is contract-legal; just must not crash
ok("stdout.purity.session1", not s.nonjson_stdout, str(s.nonjson_stdout[:2]))
s.close()

# ---------- Session 2: a fresh session gets the same one-call contract ----------
s2 = Session()
s2.rpc("initialize", {"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"h2","version":"1"}})
s2.notify("notifications/initialized")
c2, cur = [], None
for i in range(1, 12):
    m, b, _ = overview_of(s2.call("aoci_overview", {"cursor":cur} if cur else {})); c2.append((m,b))
    if m.get("completed"): break
    cur = m["next_cursor"]
mfin = c2[-1][0]; m0 = c2[0][0]
whole2 = "".join(b for _,b in c2)
seq2 = parse_entry_seq(whole2)
ok("overview.session2_entry_parse_count", len(seq2) == m0["entry_count"], f"{len(seq2)} vs {m0['entry_count']}")
ok("overview.session2_single_response", len(c2) == 1 and m0.get("delivery_mode") == "full")
ok("overview.session2_completed", m0.get("completed") is True and m0.get("continuation_required") is False)
ok("overview.session2_body_sha", hashlib.sha256(whole2.encode()).hexdigest() == m0.get("body_sha256"))
ok("overview.session2_body_bytes", len(whole2.encode()) == m0.get("body_utf8_bytes"))
ok("overview.session2_no_host_confirmation", m0.get("host_delivery_confirmation_required") is False)
ok("overview.session2_no_model_attestation", m0.get("model_cognition_attestation_required") is False)
ok("stdout.purity.session2", not s2.nonjson_stdout)
s2.close()

# ---------- Session 3: malformed input line -> orderly fail-closed shutdown ----------
import subprocess as sp
p3 = sp.Popen([BIN,"--repo",REPO,"mcp"], stdin=sp.PIPE, stdout=sp.PIPE, stderr=sp.PIPE, text=True, encoding="utf-8", bufsize=1)
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

# ---------- host window: every non-Overview tool result fits an ordinary host window ----------
# Claude Code spills tool results past ~25k tokens to a file; a real user's index
# build collapsed on exactly that. Overview is chunked by its own budget and only reported.
_peak = {}
for _t, _b in RESPONSE_SIZES:
    _peak[_t] = max(_peak.get(_t, 0), _b)
_worst = max([b for t, b in _peak.items() if t != "aoci_overview"] or [0])
ok("host_window.non_overview_responses_le_64k", _worst <= HOST_WINDOW_BYTES,
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
_TOTAL_CHECKS = len(PASS) + len(FAIL)
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
print(f"RESULT: {len(PASS)} passed, {len(FAIL)} failed")
for n, d in FAIL:
    print("  FAILED:", n, "|", d[:200])
if _doc_drift:
    print("  FAILED: docs.published_check_count_matches_this_run |", "; ".join(_doc_drift))
sys.exit(1 if (FAIL or _doc_drift) else 0)
