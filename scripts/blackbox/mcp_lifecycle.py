#!/usr/bin/env python3
"""AOCI lifecycle harness — frozen dual-fixture, real-agent, model-swappable.

生命周期黑盒：两个冻结的真实小项目（repo-a TypeScript 无库 / repo-b Python+MySQL）
从 scripts/blackbox/fixtures/ 复制到临时工作副本后完整走 AOCI 生命周期。母本永不
被写，"重置"即复制，无需清洗。场景分两轨：

  确定性轨 (免模型、结果二值):
    bringup      init 双语/agent 集成/doctor/scan/治理初态       (repo-a + repo-b)
    incremental  模板条目建索引→改/增/删/换行符 四类增量维护      (repo-a)
    database     MySQL 证据全链: source→snapshot→accept→bootstrap→
                 模板表FRAS→v2 ALTER 漂移→重对齐 (含 inventory 离线语义) (repo-b)
    governance   Curation 探针(空/超限/二进制)不得成为普通候选     (repo-a)

  模型轨 (真 agent 经 OpenCode/Zen 驱动, 统计性结果):
    establish    真模型从零建立全索引到 aligned                  (repo-a)
    dbauthor     真模型授权 5 张表 FRAS + ALTER 后重授权          (repo-b)
    attest       真模型走 overview 全链并答宣誓挑战               (repo-a)

Usage:
  python3 scripts/blackbox/mcp_lifecycle.py                       # 确定性轨全跑
  python3 scripts/blackbox/mcp_lifecycle.py --model opencode/claude-sonnet-5
  python3 scripts/blackbox/mcp_lifecycle.py --model opencode/deepseek-v4-pro \
      --suites establish --model-timeout 1800
  python3 scripts/blackbox/mcp_lifecycle.py --compare results/A.json results/B.json

Env:  AOCI_REPO / AOCI_BIN / AOCI_OPENCODE 覆盖仓库、二进制与 opencode 路径。
Results: scripts/blackbox/results/<run>.json + artifacts/（gitignored）。
"""
import argparse, json, os, re, shutil, socket, subprocess, sys, tempfile, time, traceback

_HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, _HERE)
os.environ.setdefault("AOCI_SCENARIO_WORK", tempfile.mkdtemp(prefix="aoci-lifecycle-lib-"))
from mcp_scenarios import Session, text_of, jload, parse_kv, meta_and_body, cli, sh, maintain  # noqa: E402

REPO = os.environ.get("AOCI_REPO", os.path.dirname(os.path.dirname(_HERE)))
BIN = os.environ.get("AOCI_BIN", os.path.join(REPO, "build", "aoci"))
OPENCODE = os.environ.get("AOCI_OPENCODE", os.path.expanduser("~/.opencode/bin/opencode"))
FIXTURES = os.path.join(_HERE, "fixtures")
RESULTS_DEFAULT = os.path.join(_HERE, "results")
D_SUITES = ["bringup", "incremental", "database", "governance"]
M_SUITES = ["establish", "dbauthor", "attest"]

PROMPT_ESTABLISH = (
    "Read AGENTS.md first. Establish complete AOCI cognition for THIS repository: "
    "call aoci_rules once, request aoci_overview and follow next_cursor until completed, "
    "then use aoci_maintain and aoci_update_entry to author every candidate entry from real "
    "source evidence per the authoring contract (preserve code_batch_id, candidate_id, "
    "source_sha256 exactly; on repair_required fix only the named candidates and resubmit the "
    "same batch). Continue until maintain reports aligned=true, then run `aoci verify` and "
    "`aoci check` via shell to confirm. Do not modify any project source file."
)
PROMPT_DBAUTHOR = (
    "Read AGENTS.md first. This repository has an aligned Code volume and accepted MySQL "
    "database Evidence with table cognition candidates pending. Call aoci_maintain, read the "
    "database candidates and their Evidence, author each table entry via aoci_update_entry "
    "(preserve batch_id and candidate_id exactly; canonical database:// identities). Continue "
    "until maintain reports aligned=true. Do not modify project files or the database."
)
PROMPT_ATTEST = (
    "Read AGENTS.md first. Call aoci_rules once, then aoci_overview and follow next_cursor "
    "until completed=true. Then submit host_delivery_confirmation and the "
    "model-cognition-attestation answering the challenge strictly from the delivered chunks. "
    "Report the challenge result. Do not read source files before the attestation is submitted."
)


# ---------------------------------------------------------------- reporting
class Report:
    def __init__(self, run_id, results_dir, meta):
        self.run_id, self.dir, self.meta = run_id, results_dir, meta
        self.records = []
        self.artifacts = os.path.join(results_dir, "artifacts", run_id)
        os.makedirs(self.artifacts, exist_ok=True)

    def rec(self, suite, name, status, detail="", **metrics):
        row = {"suite": suite, "name": name, "status": status, "detail": detail[:400]}
        if metrics:
            row["metrics"] = metrics
        self.records.append(row)
        m = f" | {metrics}" if metrics else ""
        print(f"{status:4} [{suite}] {name}" + (f" | {detail[:150]}" if detail else "") + m, flush=True)

    def save(self):
        path = os.path.join(self.dir, f"{self.run_id}.json")
        counts = {}
        for r in self.records:
            counts[r["status"]] = counts.get(r["status"], 0) + 1
        with open(path, "w") as f:
            json.dump({"meta": self.meta, "totals": counts, "records": self.records}, f,
                      ensure_ascii=False, indent=1)
        return path, counts


# ---------------------------------------------------------------- fixtures
def deploy(repo_key, work_root, tag):
    src = os.path.join(FIXTURES, f"repo-{repo_key}")
    dst = os.path.join(work_root, f"{tag}-repo-{repo_key}")
    shutil.copytree(src, dst)
    sh(dst, "git", "init", "-q")
    sh(dst, "git", "config", "user.email", "fixture@test.invalid")
    sh(dst, "git", "config", "user.name", "fixture")
    sh(dst, "git", "add", "-A")
    sh(dst, "git", "commit", "-q", "-m", "frozen fixture")
    return dst


# Curation 探针 (空/超限/二进制) 会以 pending_curation 阻塞授权批次; 团队排除必须
# 在首次 scan 之前落进初始策略, 事后再改会构成覆盖缩减、要求真人复核 (governance
# 场景专门断言这两种行为)。
CURATION_EXCLUDE = {
    "a": "assets/logo.png,data/audit_dump_rows.txt,data/empty.txt",
    "b": "data/floorplan.png,data/catalog_export.csv,data/empty.cfg",
}


def repo_key_of(fx):
    return "b" if fx.rstrip("/").endswith("repo-b") else "a"


def init_and_scan(fx, locale="en-US", agent=None, curation_exclude="default"):
    rc, _, out, errs = cli(fx, "init", "--locale", locale)
    if rc != 0:
        raise RuntimeError(f"init failed: {out[:200]} {errs[:200]}")
    if curation_exclude == "default":
        curation_exclude = CURATION_EXCLUDE.get(repo_key_of(fx))
    if curation_exclude:
        rc, _, out, errs = cli(fx, "config", "set", "curation_exclude", curation_exclude)
        if rc != 0:
            raise RuntimeError(f"config set curation_exclude failed: {out[:200]} {errs[:200]}")
    if agent:
        rc, _, out, errs = cli(fx, "init", "--agent", agent)
        if rc != 0:
            raise RuntimeError(f"init --agent {agent} failed: {out[:200]} {errs[:200]}")
    rc, _, out, errs = cli(fx, "scan")
    if rc != 0:
        raise RuntimeError(f"scan failed: {out[:200]} {errs[:200]}")


def aligned(fx):
    rc, v, _, _ = cli(fx, "verify", expect_ok=False)
    ok = bool((v.get("governance") or {}).get("governance_aligned") or v.get("governance_aligned"))
    return rc == 0 and ok, v


TABLE_TAGS = {"customers": "EB5T", "products": "EB5T", "orders": "TB5T",
              "order_items": "MB5T", "events": "LB5T", "shipments": "TB5T"}


def table_entry(table):
    tag = TABLE_TAGS.get(table, "EB5T")
    return f"{table}[{tag}]: F:Stores {table.replace('_', ' ')} records for the shopfloor service | R:- | A:- | S:-"


def template_entry(path):
    base = os.path.basename(path)
    words = re.sub(r"[^A-Za-z0-9]+", " ", base).strip() or "asset"
    return f"{base}[CG5T]: F:Carries the {words} responsibility of the fixture project | R:- | A:- | S:-"


def author_all(fx, max_rounds=30):
    """Template-author every Code candidate until aligned. Returns (ok, rounds, detail)."""
    for rnd in range(1, max_rounds + 1):
        s = Session(fx)
        m, t, err = maintain(s)
        cands = m.get("candidates") or []
        orphans = m.get("orphan_remove_candidates") or []
        if m.get("aligned") is True and not cands and not orphans:
            s.close()
            return True, rnd - 1, "aligned"
        if orphans and not cands:
            for o in orphans:
                p = o if isinstance(o, str) else (o.get("path") or o.get("object_ref") or "")
                text_of(s.call("aoci_remove_entry", {"path": p}))
            s.close()
            continue
        if not cands:
            s.close()
            return False, rnd, f"no candidates but not aligned: {t[:200]}"
        batch = (m.get("code_plan") or {}).get("batch_id")
        entries = [{"path": c["path"], "source_sha256": c["source_sha256"],
                    "candidate_id": c["candidate_id"], "new_entry": template_entry(c["path"])}
                   for c in cands if c.get("domain") != "database"]
        tw, err = text_of(s.call("aoci_update_entry", {"code_batch_id": batch, "entries": entries},
                                 timeout=300))
        r = jload(tw)
        s.close()
        if r.get("status") == "repair_required":
            return False, rnd, f"template entries hit repair_required: {tw[:300]}"
        if r.get("status") not in ("applied",):
            return False, rnd, f"unexpected status {r.get('status')}: {tw[:300]}"
    return False, max_rounds, "round budget exhausted"


# ---------------------------------------------------------------- mysql
class MySQLBox:
    def __init__(self, image, run_id):
        self.image, self.name = image, f"aoci-lc-mysql-{run_id}"
        self.port, self.pw, self.db = None, "lcpass", "aoci_shop"

    def dsn(self):
        return f"root:{self.pw}@tcp(127.0.0.1:{self.port})/{self.db}"

    def start(self, timeout=180):
        with socket.socket() as s:
            s.bind(("127.0.0.1", 0))
            self.port = s.getsockname()[1]
        subprocess.run(["docker", "rm", "-f", self.name], capture_output=True)
        r = subprocess.run(["docker", "run", "-d", "--rm", "--name", self.name,
                            "-e", f"MYSQL_ROOT_PASSWORD={self.pw}", "-e", f"MYSQL_DATABASE={self.db}",
                            "-p", f"127.0.0.1:{self.port}:3306", self.image],
                           capture_output=True, text=True)
        if r.returncode != 0:
            raise RuntimeError(f"docker run failed: {r.stderr[:300]}")
        ok_streak, deadline = 0, time.time() + timeout
        while time.time() < deadline:
            p = subprocess.run(["docker", "exec", self.name, "mysqladmin", "ping",
                                f"-p{self.pw}", "-uroot", "--silent"], capture_output=True)
            q = subprocess.run(["docker", "exec", self.name, "mysql", "-uroot", f"-p{self.pw}",
                                "-e", "SELECT 1"], capture_output=True)
            ok_streak = ok_streak + 1 if (p.returncode == 0 and q.returncode == 0) else 0
            if ok_streak >= 3:          # 首次启动会中途重启, 单次探针会假阳
                return self
            time.sleep(2)
        raise RuntimeError("mysql readiness timeout")

    def apply_sql(self, path):
        with open(path, "rb") as f:
            r = subprocess.run(["docker", "exec", "-i", self.name, "mysql", "-uroot",
                                f"-p{self.pw}", self.db], stdin=f, capture_output=True, text=True)
        if r.returncode != 0:
            raise RuntimeError(f"apply {os.path.basename(path)} failed: {r.stderr[:300]}")

    def digest(self):
        r = subprocess.run(["docker", "inspect", "--format", "{{index .RepoDigests 0}}", self.image],
                           capture_output=True, text=True)
        return r.stdout.strip()

    def stop(self):
        subprocess.run(["docker", "rm", "-f", self.name], capture_output=True)


# ---------------------------------------------------------------- ledger metrics
def ledger_metrics(fx):
    out = {"ops": {}, "corrupt": 0}
    path = os.path.join(fx, ".aoci", "ledger.jsonl")
    if not os.path.exists(path):
        return out
    for line in open(path, encoding="utf-8", errors="replace"):
        try:
            op = json.loads(line).get("op") or "?"
        except Exception:
            out["corrupt"] += 1
            continue
        out["ops"][op] = out["ops"].get(op, 0) + 1
    return out


# ---------------------------------------------------------------- D suites
def suite_bringup(rep, work):
    g = "bringup"
    for key in ("a", "b"):
        fx = deploy(key, work, "bringup")
        rc, _, out, errs = cli(fx, "init", "--locale", "en-US")
        rec_ok = rc == 0 and os.path.exists(os.path.join(fx, "AGENTS.md"))
        rep.rec(g, f"repo-{key}.init-en", "PASS" if rec_ok else "FAIL", (out + errs)[:150] if not rec_ok else "")
        rc, _, out, errs = cli(fx, "init", "--agent", "opencode")
        oc = os.path.join(fx, "opencode.json")
        ok = rc == 0 and os.path.exists(oc) and BIN in open(oc).read() and fx in open(oc).read()
        rep.rec(g, f"repo-{key}.init-agent-opencode", "PASS" if ok else "FAIL")
        rc, _, out, errs = cli(fx, "scan")
        ok = rc == 0 and os.path.exists(os.path.join(fx, ".aoci", "baseline.json"))
        rep.rec(g, f"repo-{key}.scan", "PASS" if ok else "FAIL")
        rc, _, out, _ = cli(fx, "doctor", expect_ok=False)
        rep.rec(g, f"repo-{key}.doctor-post-scan", "PASS" if rc == 0 else "FAIL",
                "" if rc == 0 else out[-200:])
        rc, v, _, _ = cli(fx, "scope", "status", expect_ok=False)
        stage = v.get("stage")
        rep.rec(g, f"repo-{key}.pre-authoring-stage", "PASS" if stage == "authoring_required" else "FAIL",
                f"stage={stage}")
    fxz = deploy("a", work, "bringup-zh")
    rc, _, _, _ = cli(fxz, "init", "--locale", "zh-CN")
    ag = os.path.join(fxz, "AGENTS.md")
    ok = rc == 0 and os.path.exists(ag) and any("一" <= ch <= "鿿" for ch in open(ag, encoding="utf-8").read())
    rep.rec(g, "repo-a.init-zh-CN", "PASS" if ok else "FAIL")


def suite_incremental(rep, work):
    g = "incremental"
    fx = deploy("a", work, "incr")
    init_and_scan(fx)
    ok, rounds, detail = author_all(fx)
    al, v = aligned(fx)
    rep.rec(g, "template-establish", "PASS" if ok and al else "FAIL", detail, rounds=rounds)
    if not (ok and al):
        return
    # a) 语义修改 → 单候选 update → 重对齐
    tgt = os.path.join(fx, "src", "utils", "time.ts")
    with open(tgt, "a") as f:
        f.write("\n// incremental probe: clarify timezone contract\n")
    s = Session(fx)
    m, t, _ = maintain(s)
    c = m.get("candidates") or []
    one = len(c) == 1 and c[0]["path"].endswith("time.ts") and c[0].get("change") == "update"
    if one:
        tw, _ = text_of(s.call("aoci_update_entry", {"code_batch_id": (m.get("code_plan") or {}).get("batch_id"),
            "entries": [{"path": c[0]["path"], "source_sha256": c[0]["source_sha256"],
                         "candidate_id": c[0]["candidate_id"], "new_entry": template_entry(c[0]["path"])}]}))
        one = jload(tw).get("status") == "applied"
    s.close()
    al, _ = aligned(fx)
    rep.rec(g, "modify-one-file", "PASS" if one and al else "FAIL", f"cands={len(c)}")
    # b) 新增文件 → create 候选
    with open(os.path.join(fx, "src", "utils", "clock_format.ts"), "w") as f:
        f.write("export function formatClock(ms: number): string {\n"
                "  const s = Math.floor(ms / 1000);\n"
                "  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;\n}\n")
    s = Session(fx)
    m, t, _ = maintain(s)
    c = m.get("candidates") or []
    ok = len(c) == 1 and c[0].get("change") == "create"
    if ok:
        tw, _ = text_of(s.call("aoci_update_entry", {"code_batch_id": (m.get("code_plan") or {}).get("batch_id"),
            "entries": [{"path": c[0]["path"], "source_sha256": c[0]["source_sha256"],
                         "candidate_id": c[0]["candidate_id"], "new_entry": template_entry(c[0]["path"])}]}))
        ok = jload(tw).get("status") == "applied"
    s.close()
    al, _ = aligned(fx)
    rep.rec(g, "add-new-file", "PASS" if ok and al else "FAIL")
    # c) 删除文件 → 孤儿显式移除
    os.remove(os.path.join(fx, "src", "utils", "clock_format.ts"))
    sh(fx, "git", "add", "-A"); sh(fx, "git", "commit", "-q", "-m", "drop clock_format")
    s = Session(fx)
    m, t, _ = maintain(s)
    orphans = m.get("orphan_remove_candidates") or []
    hit = any("clock_format" in str(o) for o in orphans)
    if hit:
        p = orphans[0].get("path") if isinstance(orphans[0], dict) else orphans[0]
        text_of(s.call("aoci_remove_entry", {"path": p}))
    s.close()
    al, _ = aligned(fx)
    rep.rec(g, "remove-file-orphan", "PASS" if hit and al else "FAIL", f"orphans={len(orphans)}")
    # d) 换行符翻转 → 特征化机器分类 (line-ending 容差是团队配置, 两种结果都合法)
    doc = os.path.join(fx, "docs", "api.md")
    body = open(doc, "rb").read().replace(b"\n", b"\r\n")
    open(doc, "wb").write(body)
    s = Session(fx)
    m, t, _ = maintain(s)
    c = m.get("candidates") or []
    drift = (m.get("governance") or {}).get("code_drift") or {}
    klass = "semantic-candidate" if c else ("line_ending_only" if drift.get("line_ending_only") else "none")
    if c:
        tw, _ = text_of(s.call("aoci_update_entry", {"code_batch_id": (m.get("code_plan") or {}).get("batch_id"),
            "entries": [{"path": c[0]["path"], "source_sha256": c[0]["source_sha256"],
                         "candidate_id": c[0]["candidate_id"], "new_entry": template_entry(c[0]["path"])}]}))
    s.close()
    al, _ = aligned(fx)
    rep.rec(g, "crlf-flip-classification", "CHAR" if al else "FAIL", f"classified={klass}")


def suite_database(rep, work, image, run_id, keep_box=False):
    """Returns (fx, box) if keep_box (dbauthor 模型轨接力), else tears down."""
    g = "database"
    fx = deploy("b", work, "db")
    init_and_scan(fx)
    ok, rounds, detail = author_all(fx)
    al, _ = aligned(fx)
    rep.rec(g, "code-side-establish", "PASS" if ok and al else "FAIL", detail, rounds=rounds)
    if not (ok and al):
        return None, None
    box = MySQLBox(image, run_id).start()
    try:
        box.apply_sql(os.path.join(fx, "schema", "init.sql"))
        rc, v, out, errs = cli(fx, "database", "source", "add", "--source-id", "shop",
                               "--engine", "mysql", "--database-name", box.db,
                               "--namespace", box.db, "--credential-env", "AOCI_DB_SHOP_DSN")
        rep.rec(g, "source-add", "PASS" if rc == 0 else "FAIL", errs[:120])
        os.environ.pop("AOCI_DB_SHOP_DSN", None)
        rc, v, out, errs = cli(fx, "database", "source", "access", "--source", "shop", expect_ok=False)
        ok = (rc == 0 and v.get("status") == "action_required"
              and v.get("credential_saved") is False and v.get("network_accessed") in (False, None))
        rep.rec(g, "access-missing-env-action-required", "PASS" if ok else "FAIL",
                f"status={v.get('status')}")
        os.environ["AOCI_DB_SHOP_DSN"] = box.dsn()
        rc1, _, _, _ = cli(fx, "database", "source", "access", "--source", "shop", expect_ok=False)
        rc2, _, _, _ = cli(fx, "database", "source", "inspect", "--source", "shop", expect_ok=False)
        rep.rec(g, "access+inspect", "PASS" if rc1 == 0 and rc2 == 0 else "FAIL", f"rc={rc1},{rc2}")
        rc, v, out, errs = cli(fx, "database", "snapshot", "--source", "shop")
        snap_sha = v.get("source_snapshot_sha256") or ""
        rep.rec(g, "snapshot", "PASS" if rc == 0 and snap_sha else "FAIL", out[:120] if not snap_sha else "")
        rc, v, out, errs = cli(fx, "database", "baseline", "accept", "--source", "shop", "--snapshot-sha", snap_sha,
                               expect_ok=False)
        vol = os.path.exists(os.path.join(fx, "aoci.database.txt"))
        if rc == 0 and not vol:
            rc2, _, out2, errs2 = cli(fx, "database", "cognition", "bootstrap", expect_ok=False)
            vol = os.path.exists(os.path.join(fx, "aoci.database.txt"))
        rep.rec(g, "baseline-accept+bootstrap", "PASS" if rc == 0 and vol else "FAIL",
                (out + errs)[:150] if not vol else "")
        rc, v, out, errs = cli(fx, "database", "cognition", "status", expect_ok=False)
        blob = json.dumps(v) if v else out
        n_tables = blob.count("database://")
        rep.rec(g, "cognition-status-tables", "PASS" if n_tables >= 5 else "FAIL", f"tables~{n_tables}")
        # 模板授权 5 张表 FRAS
        s = Session(fx)
        m, t, _ = maintain(s)
        dbc = [c for c in (m.get("candidates") or []) if c.get("domain") == "database"
               or str(c.get("object_ref", "")).startswith("database://")]
        rep.rec(g, "db-candidates", "PASS" if len(dbc) == 5 else "FAIL", f"got={len(dbc)}")
        st = ""
        if dbc:
            bid = dbc[0].get("batch_id") or m.get("database_plan", {}).get("batch_id")
            entries = []
            for c in dbc:
                table = str(c.get("object_ref", "")).rsplit("/", 1)[-1] or "table"
                entries.append({"object_ref": c["object_ref"], "candidate_id": c["candidate_id"],
                                "new_entry": table_entry(table)})
            tw, _ = text_of(s.call("aoci_update_entry", {"batch_id": bid, "entries": entries}, timeout=300))
            st = jload(tw).get("status")
        s.close()
        al, _ = aligned(fx)
        rep.rec(g, "template-table-fras", "PASS" if st == "applied" and al else "FAIL", f"status={st}")
        # 漂移: 先 offline inventory (不应见), 后 verify (应见), snapshot 刷新, 重授权
        box.apply_sql(os.path.join(fx, "schema", "v2_alter.sql"))
        rc, v, out, errs = cli(fx, "database", "inventory", "--source", "shop", expect_ok=False)
        pre_blob = json.dumps(v) if v else out
        rep.rec(g, "inventory-offline-no-drift-before-snapshot", "PASS" if "shipments" not in pre_blob else "FAIL")
        rc, v, out, errs = cli(fx, "database", "verify", "--source", "shop", expect_ok=False)
        blob = json.dumps(v) if v else out
        saw = ("shipments" in blob) and ("customers" in blob or "changed" in blob)
        rep.rec(g, "verify-detects-alter-drift", "PASS" if saw else "FAIL", blob[:150] if not saw else "")
        rc, v, out, errs = cli(fx, "database", "snapshot", "--source", "shop")
        rep.rec(g, "post-alter-snapshot", "PASS" if rc == 0 else "FAIL")
        new_sha = v.get("source_snapshot_sha256") or ""
        # 未 accept 新快照前, 授权必须停在 evidence_required 并点名陈旧表
        s = Session(fx)
        m, t, _ = maintain(s)
        s.close()
        gv = (m.get("governance") or {})
        codes = {f.get("code") for f in (gv.get("findings") or m.get("findings") or [])}
        ok = m.get("result") == "evidence_required" and "database_evidence_baseline_stale" in codes
        rep.rec(g, "stale-baseline-stops-authoring", "PASS" if ok else "FAIL",
                f"result={m.get('result')} codes={sorted(codes)[:3]}")
        rc, v, out, errs = cli(fx, "database", "baseline", "accept", "--source", "shop",
                               "--snapshot-sha", new_sha, expect_ok=False)
        rep.rec(g, "accept-new-snapshot", "PASS" if rc == 0 else "FAIL", (out + errs)[:120] if rc else "")
        s = Session(fx)
        m, t, _ = maintain(s)
        dbc = [c for c in (m.get("candidates") or []) if str(c.get("object_ref", "")).startswith("database://")]
        want = len(dbc) == 3
        st = ""
        if dbc:
            bid = dbc[0].get("batch_id")
            entries = []
            for c in dbc:
                table = str(c.get("object_ref", "")).rsplit("/", 1)[-1]
                entries.append({"object_ref": c["object_ref"], "candidate_id": c["candidate_id"],
                                "new_entry": table_entry(table)})
            tw, _ = text_of(s.call("aoci_update_entry", {"batch_id": bid, "entries": entries}, timeout=300))
            st = jload(tw).get("status")
        s.close()
        al, _ = aligned(fx)
        rep.rec(g, "post-alter-reauthor", "PASS" if want and st == "applied" and al else "FAIL",
                f"cands={len(dbc)} status={st} aligned={al}")
        if keep_box:
            return fx, box
        return None, None
    finally:
        if not keep_box:
            box.stop()


def suite_governance(rep, work):
    g = "governance"
    # 无排除: 三个探针必须以 pending_curation 阻塞授权 (机器强制人道决策)
    fx = deploy("a", work, "gov-blocked")
    init_and_scan(fx, curation_exclude=None)
    s = Session(fx)
    m, t, _ = maintain(s)
    s.close()
    markers = [str(o) for o in (m.get("orphan_remove_candidates") or [])]
    pend = [p for p in markers if p.startswith("pending_curation:")]
    ok = m.get("status") == "stopped" and len(pend) == 3
    rep.rec(g, "probes-block-as-pending-curation", "PASS" if ok else "FAIL",
            f"status={m.get('status')} markers={markers[:4]}")
    cands = {c.get("path") for c in (m.get("candidates") or [])}
    for kind, p in {"empty": "data/empty.txt", "oversized": "data/audit_dump_rows.txt",
                    "binary": "assets/logo.png"}.items():
        rep.rec(g, f"probe-{kind}-not-ordinary-candidate", "PASS" if p not in cands else "FAIL", p)
    rep.rec(g, "lockfile-classification", "CHAR", f"candidate={'package-lock.json' in cands}")
    # 事后排除 = 覆盖缩减, auto 必须被挡 (防 agent 私自收缩认知面)
    cli(fx, "config", "set", "curation_exclude", CURATION_EXCLUDE["a"])
    cs = os.path.join(work, "gov-cs.json")
    open(cs, "w").write('{"version":"managed-scope-candidate-set/v1","entries":[],"dispositions":[]}')
    rc, v, out, _ = cli(fx, "scope", "preview", "--candidate-file", cs, expect_ok=False)
    pv = os.path.join(work, "gov-pv.json")
    open(pv, "w").write(out)
    rc, v, out, _ = cli(fx, "scope", "apply", "--preview-file", pv, expect_ok=False)
    blocked = rc != 0 and "coverage_reduction" in (v.get("message") or "") + out
    rep.rec(g, "post-scan-exclude-needs-human", "PASS" if blocked else "FAIL",
            (v.get("message") or out)[:120])
    # 预置排除: 同样内容, 阻塞消失
    fx2 = deploy("a", work, "gov-excluded")
    init_and_scan(fx2)
    s = Session(fx2)
    m, t, _ = maintain(s)
    s.close()
    ok = (m.get("candidates") and not (m.get("orphan_remove_candidates") or []))
    rep.rec(g, "pre-scan-exclude-unblocks", "PASS" if ok else "FAIL",
            f"cands={len(m.get('candidates') or [])}")
    rc, v, out, _ = cli(fx2, "scope", "budget", expect_ok=False)
    rep.rec(g, "budget-visible", "PASS" if rc == 0 else "FAIL")


# ---------------------------------------------------------------- M suites
def opencode_run(fx, model, prompt, timeout_s, artifact):
    t0 = time.time()
    r = subprocess.run(["timeout", str(timeout_s), OPENCODE, "run", prompt, "-m", model,
                        "--dir", fx, "--auto", "--format", "json",
                        "--title", f"aoci-lifecycle-{os.path.basename(fx)}"],
                       capture_output=True, text=True)
    with open(artifact, "w") as f:
        f.write(r.stdout)
        if r.stderr:
            f.write("\n===STDERR===\n" + r.stderr[-8000:])
    return r.returncode, time.time() - t0, r.stdout


def m_establish(rep, work, model, timeout_s, artifacts):
    g = f"establish[{model.split('/')[-1]}]"
    fx = deploy("a", work, f"est-{model.split('/')[-1]}")
    init_and_scan(fx, agent="opencode")
    s = Session(fx)
    m, t, _ = maintain(s)
    s.close()
    expected = ((m.get("code_plan") or {}).get("total_targets")
                or len(m.get("candidates") or []))
    art = os.path.join(artifacts, f"establish-{model.split('/')[-1]}.json")
    rc, dur, _ = opencode_run(fx, model, PROMPT_ESTABLISH, timeout_s, art)
    al, v = aligned(fx)
    lm = ledger_metrics(fx)
    n_upd = sum(n for op, n in lm["ops"].items() if "update" in op)
    rep.rec(g, "aligned-after-agent", "PASS" if al else "FAIL",
            f"rc={rc} expected_targets={expected}", duration_s=round(dur), update_ops=n_upd,
            ledger_ops=lm["ops"])
    if al:
        rc2, v2, out2, _ = cli(fx, "check", expect_ok=False)
        rep.rec(g, "terminal-check", "PASS" if rc2 == 0 and (v2.get("next_action") == "none") else "FAIL")
    return al


def m_dbauthor(rep, work, model, timeout_s, artifacts, image, run_id):
    tag = model.split('/')[-1]
    g = f"dbauthor[{tag}]"
    fx = deploy("b", work, f"dba-{tag}")
    init_and_scan(fx, agent="opencode")
    ok, rounds, detail = author_all(fx)
    if not ok:
        rep.rec(g, "setup-code-side", "FAIL", detail)
        return
    box = MySQLBox(image, f"{run_id}-{tag}").start()
    try:
        box.apply_sql(os.path.join(fx, "schema", "init.sql"))
        cli(fx, "database", "source", "add", "--source-id", "shop", "--engine", "mysql",
            "--database-name", box.db, "--namespace", box.db, "--credential-env", "AOCI_DB_SHOP_DSN")
        os.environ["AOCI_DB_SHOP_DSN"] = box.dsn()
        rc, v, out, _ = cli(fx, "database", "snapshot", "--source", "shop")
        sha = v.get("source_snapshot_sha256") or ""
        cli(fx, "database", "baseline", "accept", "--source", "shop", "--snapshot-sha", sha, expect_ok=False)
        if not os.path.exists(os.path.join(fx, "aoci.database.txt")):
            cli(fx, "database", "cognition", "bootstrap", expect_ok=False)
        art = os.path.join(artifacts, f"dbauthor-{tag}.json")
        rc, dur, _ = opencode_run(fx, model, PROMPT_DBAUTHOR, timeout_s, art)
        al, v = aligned(fx)
        n_entries = 0
        vol = os.path.join(fx, "aoci.database.txt")
        if os.path.exists(vol):
            n_entries = sum(1 for ln in open(vol, encoding="utf-8")
                            if ln.strip() and not ln.startswith(("#", "=", "-", "<")))
        rep.rec(g, "five-tables-authored", "PASS" if al and n_entries >= 5 else "FAIL",
                f"rc={rc} entries={n_entries} aligned={al}", duration_s=round(dur))
        box.apply_sql(os.path.join(fx, "schema", "v2_alter.sql"))
        rc, v, _, _ = cli(fx, "database", "snapshot", "--source", "shop")
        cli(fx, "database", "baseline", "accept", "--source", "shop",
            "--snapshot-sha", v.get("source_snapshot_sha256") or "", expect_ok=False)
        art2 = os.path.join(artifacts, f"dbauthor-drift-{tag}.json")
        rc, dur, _ = opencode_run(fx, model, PROMPT_DBAUTHOR, timeout_s, art2)
        al, _ = aligned(fx)
        rep.rec(g, "post-alter-reauthor", "PASS" if al else "FAIL", f"rc={rc}", duration_s=round(dur))
    finally:
        box.stop()


def m_attest(rep, work, model, timeout_s, artifacts):
    tag = model.split('/')[-1]
    g = f"attest[{tag}]"
    fx = deploy("a", work, f"att-{tag}")
    init_and_scan(fx, agent="opencode")
    ok, rounds, detail = author_all(fx)
    if not ok:
        rep.rec(g, "setup", "FAIL", detail)
        return
    art = os.path.join(artifacts, f"attest-{tag}.json")
    rc, dur, out = opencode_run(fx, model, PROMPT_ATTEST, timeout_s, art)
    blob = out[-200000:]
    if "model_attestation: pass" in blob or '"challenge_passed": "10/10"' in blob or "challenge_passed: 10/10" in blob:
        rep.rec(g, "challenge", "PASS", "10/10 markers in session", duration_s=round(dur))
    elif "model_attestation: fail" in blob:
        n = re.search(r"challenge_passed:\s*(\d+)/(\d+)", blob)
        rep.rec(g, "challenge", "FAIL", f"attestation fail {n.group(0) if n else ''}", duration_s=round(dur))
    else:
        rep.rec(g, "challenge", "CHAR", f"no attestation marker found rc={rc}", duration_s=round(dur))


# ---------------------------------------------------------------- compare
def compare(pa, pb):
    A, B = json.load(open(pa)), json.load(open(pb))
    # 跨模型对比: 把 establish[claude-sonnet-5] 归一成 establish[*] 使行对齐
    def key(r):
        return (re.sub(r"\[[^\]]+\]$", "[*]", r["suite"]), r["name"])
    ia = {key(r): r for r in A["records"]}
    ib = {key(r): r for r in B["records"]}
    print(f"A: {A['meta'].get('models') or 'deterministic'} @ {A['meta'].get('mcp_version')}")
    print(f"B: {B['meta'].get('models') or 'deterministic'} @ {B['meta'].get('mcp_version')}")
    for k in sorted(set(ia) | set(ib)):
        ra, rb = ia.get(k), ib.get(k)
        sa, sb = (ra or {}).get("status", "-"), (rb or {}).get("status", "-")
        mark = "  " if sa == sb else "≠ "
        ma = (ra or {}).get("metrics", {})
        mb = (rb or {}).get("metrics", {})
        extra = ""
        if ma.get("duration_s") or mb.get("duration_s"):
            extra = f" | {ma.get('duration_s','-')}s vs {mb.get('duration_s','-')}s"
        print(f"{mark}{k[0]:24} {k[1]:40} {sa:4} vs {sb:4}{extra}")


# ---------------------------------------------------------------- main
def main():
    ap = argparse.ArgumentParser(description="AOCI lifecycle harness over frozen fixtures")
    ap.add_argument("--model", action="append", default=[], help="opencode/<model>; repeatable")
    ap.add_argument("--suites", default="", help=f"comma list from {D_SUITES + M_SUITES}; default: all applicable")
    ap.add_argument("--results-dir", default=RESULTS_DEFAULT)
    ap.add_argument("--mysql-image", default="mysql:8.4")
    ap.add_argument("--model-timeout", type=int, default=2400)
    ap.add_argument("--keep-work", action="store_true")
    ap.add_argument("--compare", nargs=2, metavar=("A.json", "B.json"))
    args = ap.parse_args()
    if args.compare:
        compare(*args.compare)
        return 0

    run_id = time.strftime("%Y%m%d-%H%M%S")
    desc = subprocess.run(["git", "-C", REPO, "describe", "--tags", "--always"],
                          capture_output=True, text=True).stdout.strip()
    suites = [s.strip() for s in args.suites.split(",") if s.strip()] or \
             (D_SUITES + (M_SUITES if args.model else []))
    bad = [s for s in suites if s not in D_SUITES + M_SUITES]
    if bad:
        print(f"unknown suites: {bad}", file=sys.stderr)
        return 2
    work = tempfile.mkdtemp(prefix=f"aoci-lifecycle-{run_id}-")
    os.makedirs(args.results_dir, exist_ok=True)
    rep = Report(run_id, args.results_dir,
                 {"run_id": run_id, "mcp_version": desc, "bin": BIN, "models": args.model,
                  "suites": suites, "mysql_image": args.mysql_image,
                  "started_utc": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())})
    print(f"run {run_id} | mcp {desc} | suites {suites} | models {args.model or '-'} | work {work}")
    try:
        for s in suites:
            try:
                if s == "bringup":
                    suite_bringup(rep, work)
                elif s == "incremental":
                    suite_incremental(rep, work)
                elif s == "database":
                    suite_database(rep, work, args.mysql_image, run_id)
                elif s == "governance":
                    suite_governance(rep, work)
                elif s in M_SUITES:
                    for model in args.model or []:
                        if s == "establish":
                            m_establish(rep, work, model, args.model_timeout, rep.artifacts)
                        elif s == "dbauthor":
                            m_dbauthor(rep, work, model, args.model_timeout, rep.artifacts,
                                       args.mysql_image, run_id)
                        elif s == "attest":
                            m_attest(rep, work, model, args.model_timeout, rep.artifacts)
            except Exception:
                rep.rec(s, "suite-exception", "FAIL", traceback.format_exc()[-350:])
    finally:
        path, counts = rep.save()
        print(f"\nLIFECYCLE: {counts} -> {path}")
        if not args.keep_work:
            shutil.rmtree(work, ignore_errors=True)
        else:
            print(f"work kept: {work}")
    return 1 if counts.get("FAIL") else 0


if __name__ == "__main__":
    sys.exit(main())
