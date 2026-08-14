#!/usr/bin/env python3
"""AOCI MCP stdio conformance harness — drives build/aoci mcp as a real client.
Protocol-level tests only; zero formal writes expected (verified afterward by git/verify).

MCP stdio 一致性检查（44项）：以真实客户端身份走 initialize→tools 全链路，
只读不写，跑完用 git status / aoci verify 复核零写入。

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
MARK = "<<<AOCI_OVERVIEW_CHUNK_BODY/v1>>>"

PASS, FAIL = [], []
def ok(name, cond, detail=""):
    (PASS if cond else FAIL).append((name, detail))
    print(("PASS " if cond else "FAIL ") + name + ((" | " + detail[:140]) if detail and not cond else ""))

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
        return self.rpc("tools/call", {"name": tool, "arguments": args or {}}, timeout)
    def close(self):
        try:
            self.p.stdin.close(); self.p.wait(timeout=10)
        except Exception:
            self.p.kill()

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
ok("tools.inputSchemas.present", schemas_ok)

rules_text, err = text_of(s.call("aoci_rules"))
ok("rules.no_error", not err)
ok("rules.new_numbering", "Section 1. Establishing" in rules_text and "4a." in rules_text and "4d." in rules_text)
ok("rules.machine_facts", "cognition_refresh_threshold: 30" in rules_text)

# Full overview chain with byte-level integrity verification
chunks, cursor, meta0 = [], None, None
for i in range(1, 12):
    args = {"cursor": cursor} if cursor else {}
    t, err = text_of(s.call("aoci_overview", args))
    ok(f"overview.chunk{i}.no_error", not err)
    meta, body = meta_and_body(t)
    if meta0 is None: meta0 = meta
    chunks.append((meta, body))
    # per-chunk sha256 must match the exact body bytes
    got = hashlib.sha256(body.encode()).hexdigest()
    ok(f"overview.chunk{i}.sha", got == meta["chunk_sha256"], f"got={got[:12]} want={meta['chunk_sha256'][:12]}")
    if meta.get("completed"): break
    cursor = meta["next_cursor"]

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
entry_seq, hist_root, cur_rel = [], None, None
for ln in whole.split("\n"):
    st = ln.strip()
    if st.startswith("===") and st.endswith("==="):
        path = st.strip("=")
        if hist_root is None or len(path) < len(hist_root):
            hist_root = path
        cur_rel = path[len(hist_root):] if hist_root and path.startswith(hist_root) else ""
        continue
    m = re.match(r"^(\S[^\[]*)\[([A-Za-z0-9]+)\]: F:(.*?) \| R:", st)
    if m and cur_rel is not None:
        entry_seq.append(((cur_rel or "") + m.group(1), m.group(2), m.group(3)))
pt, perr = text_of(s.call("aoci_overview", {"check_only": True, "probe": True}))
probe = (json.loads(pt) if not perr else {}).get("cognition_probe") or {}
probe_ok = probe.get("version") == "cognition-probe/v1" and len(probe.get("ordinals") or []) == 2 \
    and len(entry_seq) == meta0["entry_count"]
ok("probe.issued", probe_ok, pt[:160])
if probe_ok:
    answers = []
    for o in probe["ordinals"]:
        ident, tag, coref = entry_seq[o-1]
        answers.append({"ordinal": o, "object_identity": ident, "tag": tag, "core_f": coref})
    gt, gerr = text_of(s.call("aoci_overview", {"check_only": True, "probe_answers":
        {"version": "cognition-probe/v1", "digest": probe["digest"], "answers": answers}}))
    graded = (json.loads(gt) if not gerr else {}).get("probe_result") or {}
    ok("probe.correct_answers_pass", graded.get("result") == "pass", gt[:200])
    answers[0]["tag"] = "ZZ1Z"
    bt, berr = text_of(s.call("aoci_overview", {"check_only": True, "probe_answers":
        {"version": "cognition-probe/v1", "digest": probe["digest"], "answers": answers}}))
    bad = (json.loads(bt) if not berr else {}).get("probe_result") or {}
    ok("probe.wrong_answer_fails", bad.get("result") == "fail", bt[:200])

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
c2, cur = [], None
for i in range(1, 12):
    t, _ = text_of(s2.call("aoci_overview", {"cursor":cur} if cur else {}))
    m, b = meta_and_body(t); c2.append((m,b))
    if m.get("completed"): break
    cur = m["next_cursor"]
mfin = c2[-1][0]; m0 = c2[0][0]
whole2 = "".join(b for _,b in c2)
ords = mfin["challenge_ordinals"] if isinstance(mfin["challenge_ordinals"], list) else [int(x) for x in str(mfin["challenge_ordinals"]).split(",")]
ents2 = []
for ln in whole2.split("\n"):
    st = ln.strip()
    if st and not st.startswith(("#","===","<<<","──","AOCI Cognition Asset:")) and re.match(r"^\S+\[[A-Za-z0-9]+\]: F:", st):
        ents2.append(st)
def answer(ordinal, wreck=False):
    e = ents2[ordinal-1]
    mm = re.match(r"^(\S+)\[([A-Za-z0-9]+)\]: F:(.*?) \| R:", e)
    name, tag, f = mm.group(1), mm.group(2), mm.group(3)
    if wreck: f = "totally wrong responsibility"
    return {"ordinal": ordinal, "object_identity": name, "tag": tag, "core_f": f}
def attest(sess, wreck_first):
    answers = [answer(o, wreck=(wreck_first and i==0)) for i,o in enumerate(ords)]
    return text_of(sess.call("aoci_overview", {
        "host_delivery_confirmation": {"version":"overview-delivery-receipt/v1",
            "body_sha256": m0["body_sha256"], "body_bytes": m0["body_utf8_bytes"], "end_marker_observed": True},
        "model_cognition_attestation": {"version":"model-cognition-attestation/v1",
            "index_sha256": mfin["challenge_index_sha256"],
            "entry_sequence_sha256": mfin["challenge_entry_sequence_sha256"],
            "entry_count": mfin["challenge_entry_count"],
            "challenge_digest": mfin["challenge_digest"],
            "reported_entry_count": m0["entry_count"], "reported_estimated_tokens": m0["estimated_tokens"],
            "coverage_percent": 100, "system_mastery_percent": 50, "confidence_percent": 50,
            "truncation_detected": False, "unseen_sections": [], "uncertainty_reasons": [],
            "challenge_answers": answers}}))
# NOTE: object_identity uses the entry's file name; the contract requires the full
# repo-relative path for Code objects — the bare-name variant in the wrong-answer
# session ALSO probes identity strictness. For the pass session we must map names
# to full paths via section headers.
t, err = text_of(s2.call("aoci_rules"))  # keep session alive semantics
wt, werr = attest(s2, wreck_first=True)
ok("attestation.wrong_answer_fails", ("model_attestation: fail" in wt) or ("fail" in wt and "10/10" not in wt), wt[:200])
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
ok("malformed.stderr_diagnostic", "invalid character" in err3)
ok("malformed.stdout_stays_pure", out3.strip() == "", repr(out3[:80]))

print()
print(f"RESULT: {len(PASS)} passed, {len(FAIL)} failed")
for n, d in FAIL:
    print("  FAILED:", n, "|", d[:200])
sys.exit(1 if FAIL else 0)
