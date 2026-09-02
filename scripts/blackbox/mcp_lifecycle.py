#!/usr/bin/env python3
"""AOCI lifecycle harness — frozen fixtures, real-agent, model-swappable.

生命周期黑盒：三个冻结的真实项目（repo-a TypeScript 无库 / repo-b Python+MySQL /
repo-c 453 文件分层服务）从 scripts/blackbox/fixtures/ 复制到临时工作副本后走 AOCI
生命周期。母本永不被写，"重置"即复制，无需清洗。repo-a 与 repo-b 走完整的
init→漂移→重对齐全程；repo-c 只到 aligned，不注入漂移，它换来的是多批创作覆盖。
场景分两轨：

  确定性轨 (免模型、结果二值):
    bringup      init 双语/agent 集成/doctor/scan/治理初态       (repo-a + repo-b)
    incremental  模板条目建索引→改/增/删/换行符 四类增量维护      (repo-a)
    database     MySQL 证据全链: source→snapshot→accept→bootstrap→
                 模板表FRAS→v2 ALTER 漂移→重对齐 (含 inventory 离线语义) (repo-b)
    governance   Curation 探针(空/超限/二进制)不得成为普通候选     (repo-a)
    scale        453 对象的多批滚动 + 关系闭包三模式(无关系/分层DAG/
                 大强连通簇); 团队批量显式抬到线上上限 200 后跑，
                 同一套件另行验证机器默认批量 20 装得进宿主窗口   (repo-c)

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
from mcp_scenarios import Session, text_of, jload, parse_kv, meta_and_body, cli, sh, maintain, host_window_summary, mark_team_raised_batch  # noqa: E402

REPO = os.environ.get("AOCI_REPO", os.path.dirname(os.path.dirname(_HERE)))
BIN = os.environ.get("AOCI_BIN", os.path.join(REPO, "build", "aoci"))
OPENCODE = os.environ.get("AOCI_OPENCODE", os.path.expanduser("~/.opencode/bin/opencode"))
FIXTURES = os.path.join(_HERE, "fixtures")
RESULTS_DEFAULT = os.path.join(_HERE, "results")
D_SUITES = ["bringup", "incremental", "database", "governance", "scale"]
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
    tail = fx.rstrip("/")
    for key in ("b", "c"):
        if tail.endswith(f"repo-{key}"):
            return key
    return "a"


def init_and_scan(fx, locale="en-US", agent=None, curation_exclude="default", batch_entries=None):
    rc, _, out, errs = cli(fx, "init", "--locale", locale)
    if rc != 0:
        raise RuntimeError(f"init failed: {out[:200]} {errs[:200]}")
    if batch_entries is not None:
        rc, _, out, errs = cli(fx, "config", "set", "code_cognition_batch_entries", str(batch_entries))
        if rc != 0:
            raise RuntimeError(f"config set code_cognition_batch_entries failed: {out[:200]} {errs[:200]}")
        mark_team_raised_batch(fx)
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
    suite_cognition_visibility(rep, work)


def suite_cognition_visibility(rep, work):
    """The cognition layer must reach the Baseline, on both real-project fixtures.

    Reported from a real integration: an agent protected the working tree by
    hiding AOCI's own assets from Git before init, so scan published a Baseline
    that could never govern the Volumes it omitted. The failure appeared many
    steps later as a blocked Guide naming neither the rule nor the file. The
    second class needs no rule at all — Git for Windows defaults to
    core.autocrlf=true, and that rewrite alone used to hard-block a repository
    over a difference the team tolerance policy calls equivalent.

    Run on repo-a and repo-b because this is a property of bringing up a real
    project, not of any one language or toolchain.
    """
    g = "bringup"
    for key in ("a", "b"):
        fx = deploy(key, work, f"visibility-{key}")
        rc, _, out, errs = cli(fx, "init", "--locale", "en-US")
        if rc != 0:
            rep.rec(g, f"repo-{key}.cognition-visible-to-git", "FAIL", (out + errs)[:150])
            continue

        # init must hand the project the line-ending protection AOCI applies to
        # itself; nothing else keeps a Windows checkout from rewriting Volumes.
        attributes = os.path.join(fx, ".gitattributes")
        normalized = os.path.exists(attributes) and "text=auto eol=lf" in open(attributes, encoding="utf-8").read()
        rep.rec(g, f"repo-{key}.init-normalizes-line-endings", "PASS" if normalized else "FAIL",
                "" if normalized else "no .gitattributes normalization; a Windows checkout rewrites every Volume")

        # Hiding the formal assets must stop scan, naming the asset and the rule,
        # and must not leave a half-governed Baseline behind.
        with open(os.path.join(fx, ".git", "info", "exclude"), "a", encoding="utf-8") as fh:
            fh.write("\naoci.txt\naoci.meta.txt\naoci.code.txt\n")
        rc, _, out, errs = cli(fx, "scan", expect_ok=False)
        blob = (out or "") + (errs or "")
        named = "aoci.code.txt" in blob and "exclude" in blob
        no_baseline = not os.path.exists(os.path.join(fx, ".aoci", "baseline.json"))
        rep.rec(g, f"repo-{key}.scan-refuses-hidden-cognition",
                "PASS" if rc != 0 and named and no_baseline else "FAIL",
                f"rc={rc} named={named} no_baseline={no_baseline} | {blob[:140]}")

        # With the assets visible the bring-up proceeds, and a pure line-ending
        # rewrite afterwards stays authorable with its own repair named.
        exclude_path = os.path.join(fx, ".git", "info", "exclude")
        kept = [line for line in open(exclude_path, encoding="utf-8").read().splitlines()
                if line.strip() not in ("aoci.txt", "aoci.meta.txt", "aoci.code.txt")]
        open(exclude_path, "w", encoding="utf-8").write("\n".join(kept) + "\n")
        rc, _, out, errs = cli(fx, "scan")
        if rc != 0:
            rep.rec(g, f"repo-{key}.line-ending-rewrite-stays-authorable", "FAIL",
                    f"scan failed once assets were visible: {(out + errs)[:140]}")
            continue
        for rel in ("aoci.txt", "aoci.meta.txt", "aoci.code.txt"):
            target = os.path.join(fx, rel)
            body = open(target, "rb").read()
            open(target, "wb").write(body.replace(b"\n", b"\r\n"))
        rc, guide, out, errs = cli(fx, "index", "agent", "guide", "--agent", "lifecycle")
        findings = guide.get("findings") or []
        codes = {f.get("code") for f in findings}
        repairs = [f.get("safe_repair_action") or "" for f in findings
                   if str(f.get("code", "")).endswith("_line_ending_only")]
        ok = (guide.get("stage") != "blocked"
              and "code_volume_unbaselined" not in codes
              and any(str(code).endswith("_line_ending_only") for code in codes)
              and any("eol=lf" in text or "LF" in text for text in repairs))
        rep.rec(g, f"repo-{key}.line-ending-rewrite-stays-authorable", "PASS" if ok else "FAIL",
                f"stage={guide.get('stage')} codes={sorted(c for c in codes if 'volume' in str(c))}")

        # One root cause must produce one blocker, and the remediation handed
        # back must be one this repository can run. A reported project received
        # scope_change_required together with business_source_manifest_invalid
        # and went to investigate a subsystem that was working; the Guide then
        # offered scan, which a repository with a Baseline cannot use.
        for rel in ("aoci.txt", "aoci.meta.txt", "aoci.code.txt"):
            target = os.path.join(fx, rel)
            body = open(target, "rb").read()
            open(target, "wb").write(body.replace(b"\r\n", b"\n"))
        baseline_path = os.path.join(fx, ".aoci", "baseline.json")
        state = json.load(open(baseline_path, encoding="utf-8"))
        receipt = state.get("managed_scope")
        if not isinstance(receipt, dict):
            rep.rec(g, f"repo-{key}.scope-drift-reports-one-blocker", "CHAR",
                    "fixture carries no Managed Scope receipt")
            continue
        receipt["policy_identity"] = "a" * 64
        json.dump(state, open(baseline_path, "w", encoding="utf-8"), indent=2)
        rc, check, out, errs = cli(fx, "check", expect_ok=False)
        codes = {f.get("code") for f in (check.get("findings") or [])}
        single = "scope_change_required" in codes and "business_source_manifest_invalid" not in codes
        rep.rec(g, f"repo-{key}.scope-drift-reports-one-blocker", "PASS" if single else "FAIL",
                f"codes={sorted(str(c) for c in codes)}")
        rc, guide, out, errs = cli(fx, "index", "agent", "guide", "--agent", "lifecycle", expect_ok=False)
        instructions = " ".join(guide.get("instructions") or [])
        runnable = "scope" in instructions and "aoci scan" not in instructions
        rep.rec(g, f"repo-{key}.scope-drift-remediation-is-runnable", "PASS" if runnable else "FAIL",
                f"offers_scan={'aoci scan' in instructions}")


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
        # current 表条目不进 Maintain 传输: 每个 current Item 携带完整条目文本与
        # 证据/绑定哈希, 对任何决策都没有输入; 一个 52 表全 current 的真实仓库
        # 曾为此每轮白付 20 条完整条目, 单次响应 53KB 在真宿主上被落盘外置。
        # Summary 保留计数, Verify/Check 保留全量枚举。
        s = Session(fx)
        m, t, _ = maintain(s)
        s.close()
        items = ((m.get("governance") or {}).get("database_cognition") or {}).get("items") or []
        cur_items = [i for i in items if i.get("state") == "cognition_current"]
        summ = ((m.get("governance") or {}).get("database_cognition") or {}).get("summary") or {}
        folded = len(cur_items) == 0 and summ.get("current", 0) >= 5
        rep.rec(g, "current-items-fold-out-of-maintain-transport", "PASS" if folded else "FAIL",
                f"current_items={len(cur_items)} summary.current={summ.get('current')}")
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
        # 停在 evidence_required 的响应必须携带可执行的证据刷新链, 而不是只有
        # 状态码 —— 四步 CLI 链此前只存在于操作者的记忆里。
        cmds = m.get("next_commands") or []
        chain = (len(cmds) == 2 and "database snapshot" in cmds[0]
                 and "database baseline accept" in cmds[1]
                 and all(c.startswith(("'", '"')) for c in cmds))
        rep.rec(g, "evidence-required-carries-refresh-commands", "PASS" if chain else "FAIL",
                f"cmds={cmds}"[:160])
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


# repo-c 的冻结身份 —— 场景断言以此为基准。夹具被误改时立刻失败, 而不是静默跑偏。
SCALE_FIXTURE_FILES = 453
SCALE_BATCH_LIMIT = 200


def scale_fixture_files():
    root = os.path.join(FIXTURES, "repo-c")
    return sum(len(names) for _, _, names in os.walk(root))


def published_fixture_drift():
    """Every committed fixture must be named where the suites are advertised.

    The public READMEs invite downstream users to run these suites; when repo-c
    landed, all three documents still described two fixtures, so a forker got an
    unexplained 453-file run. Binding by name rather than by count means adding
    a fixture fails here until the documents catch up.
    """
    fixtures = sorted(
        name for name in os.listdir(FIXTURES)
        if os.path.isdir(os.path.join(FIXTURES, name))
    )
    drift = []
    for rel in ("README.md", "README.zh-CN.md", os.path.join("scripts", "blackbox", "README.md")):
        try:
            with open(os.path.join(REPO, rel), encoding="utf-8") as handle:
                text = handle.read()
        except OSError as err:
            drift.append(f"{rel}: unreadable ({err})")
            continue
        missing = [name for name in fixtures if name not in text]
        if missing:
            drift.append(f"{rel}: does not name {', '.join(missing)}")
    return fixtures, drift


def domain_of(path):
    """pkg 路径 -> (域, 层); 非领域文件返回 (None, None)。"""
    parts = path.split("/")
    if len(parts) >= 4 and parts[0] == "src" and parts[1] == "domains":
        return parts[2], os.path.splitext(parts[3])[0]
    return None, None


# 分层 DAG: 上层指向下层, model 与基础设施是汇点。这是真实项目里 R 的自然形状。
LAYER_BELOW = {"handler": "service", "service": "repository", "repository": "model",
               "validator": "model", "mapper": "model", "policy": None, "model": None}


def scale_entry(path, mode, cyclic_ring):
    """按关系模式生成条目。夹具内容不含关系, 关系形状完全由这里决定。"""
    base = os.path.basename(path)
    stem = os.path.splitext(base)[0]
    domain, layer = domain_of(path)
    relation = "-"
    if mode == "layered" and domain and LAYER_BELOW.get(layer):
        relation = f"code:src/domains/{domain}/{LAYER_BELOW[layer]}.ts"
    elif mode == "cyclic" and path in cyclic_ring:
        relation = cyclic_ring[path]
    words = re.sub(r"[^A-Za-z0-9]+", " ", f"{domain or 'runtime'} {stem}").strip()
    return f"{base}[CG5T]: F:Carries the {words} responsibility | R:{relation} | A:- | S:-"


def build_cyclic_ring(paths, size):
    """在前 size 个领域文件上构造一个互引环 —— 一个 size 节点的强连通分量。"""
    members = [p for p in sorted(paths) if domain_of(p)[0]][:size]
    return {member: "code:" + members[(index + 1) % len(members)]
            for index, member in enumerate(members)}


def scale_author_rounds(fx, mode, cyclic_ring, max_rounds=12):
    """按机器指定的批次逐轮授权, 跟随替代计划; 返回 (结局, 轮次记录)。"""
    rounds, follow, applied_total = [], None, 0
    for attempt in range(1, max_rounds + 1):
        s = Session(fx)
        resp_bytes = None
        if follow is None:
            m, t, err = maintain(s)
            resp_bytes = len(t.encode("utf-8"))
            plan = m.get("code_plan") or {}
            cands = m.get("candidates") or []
            if m.get("aligned") is True and not cands:
                s.close()
                return ("aligned", rounds, applied_total)
        else:
            plan, cands = follow, (follow.get("candidates") or [])
        if not cands:
            s.close()
            return ("no_candidates", rounds, applied_total)
        entries = [{"path": c["path"], "source_sha256": c["source_sha256"],
                    "candidate_id": c["candidate_id"],
                    "new_entry": scale_entry(c["path"], mode, cyclic_ring)} for c in cands]
        tw, err = text_of(s.call("aoci_update_entry",
                                 {"code_batch_id": plan.get("batch_id"), "entries": entries},
                                 timeout=900))
        r = jload(tw)
        s.close()
        applied_total += r.get("applied") or 0
        rounds.append({"batch": (plan.get("batch_id") or "")[:8], "n": len(cands),
                       "status": r.get("status"), "applied": r.get("applied"),
                       "remaining": r.get("remaining"), "max_entries": plan.get("max_entries"),
                       "maintain_bytes": resp_bytes})
        blob = json.dumps(r, ensure_ascii=False)
        # 机器不得因为条目之间的关系而重排或停下: 出现这类信号即为回归。
        for marker in ("relation_closure_exceeds_batch_limit", "relation_replan",
                       "impact_relation_unresolved", "impact_relation_ambiguous"):
            if marker in blob:
                return ("relation_machinery_returned:" + marker, rounds, applied_total)
        if r.get("status") == "repair_required":
            codes = sorted({f.get("rule_code") for f in (r.get("findings") or [])})
            return ("repair_required:" + ",".join(c for c in codes if c), rounds, applied_total)
        follow = r.get("code_plan") or None
        if follow is None and r.get("status") != "applied":
            return (f"stopped:{r.get('status')}", rounds, applied_total)
    return ("round_budget_exhausted", rounds, applied_total)


def suite_scale(rep, work):
    g = "scale"
    files = scale_fixture_files()
    rep.rec(g, "fixture-identity", "PASS" if files == SCALE_FIXTURE_FILES else "FAIL",
            f"files={files} expected={SCALE_FIXTURE_FILES}")
    if files != SCALE_FIXTURE_FILES:
        return

    # 机器默认批量(20)下的真实首轮: 453 文件的新仓库, 首次 Maintain 必须装进普通
    # 宿主的工具结果窗口, 逐轮滚动直到对齐。这是真实用户撞到的形状 —— 200/批时
    # 首次 Maintain 约 212 KB, 宿主落盘, 模型退回脚本旁路; 现在必须一路内联。
    fx = deploy("c", work, "scale-default")
    init_and_scan(fx)
    started = time.time()
    outcome, rounds, applied = scale_author_rounds(fx, "none", {}, max_rounds=40)
    al, _ = aligned(fx)
    batches = [r for r in rounds if r["status"] == "applied"]
    sizes = [r["maintain_bytes"] for r in rounds if r.get("maintain_bytes")]
    ok = outcome == "aligned" and al and len(batches) >= 20 \
        and all(r.get("max_entries") == 20 for r in rounds) \
        and sizes and max(sizes) < 48 * 1024
    rep.rec(g, "default-batch-fits-host-window", "PASS" if ok else "FAIL",
            f"outcome={outcome} batches={len(batches)} applied={applied} aligned={al} max_maintain_bytes={max(sizes) if sizes else None}",
            duration_s=round(time.time() - started), rounds=len(rounds))

    # 下面三轮显式把团队批量拉到线上上限 200: 它们检验的是"关系永不排程"在真实
    # 上限处的多批语义, 环的大小(210)也是按上限设计的。
    # 无关系: 纯多批滚动, 必须写满全部对象并对齐。
    fx = deploy("c", work, "scale-none")
    init_and_scan(fx, batch_entries=SCALE_BATCH_LIMIT)
    started = time.time()
    outcome, rounds, applied = scale_author_rounds(fx, "none", {})
    al, _ = aligned(fx)
    batches = [r for r in rounds if r["status"] == "applied"]
    ok = outcome == "aligned" and al and len(batches) >= 3
    rep.rec(g, "multi-batch-rolling", "PASS" if ok else "FAIL",
            f"outcome={outcome} batches={len(batches)} applied={applied} aligned={al}",
            duration_s=round(time.time() - started), rounds=len(rounds))
    if ok:
        rc, v, _, _ = cli(fx, "verify", expect_ok=False)
        tokens = ((v.get("governance") or {}).get("budget") or {}).get("whole_index_tokens")
        rep.rec(g, "budget-at-scale", "PASS" if tokens else "CHAR", f"whole_index_tokens={tokens}")
        s = Session(fx)
        started = time.time()
        t, err = text_of(s.call("aoci_overview"))
        meta, _ = meta_and_body(t)
        s.close()
        chunked = bool(meta.get("next_cursor")) or (meta.get("chunk_count") or 0) > 1
        rep.rec(g, "overview-chunked-at-scale", "PASS" if chunked else "CHAR",
                f"chunk_count={meta.get('chunk_count')} entries={meta.get('entry_count')}",
                duration_s=round(time.time() - started))

    # 分层 DAG: 关系稠密但有解。机器不看关系, 必须照常一路滚完。
    fx = deploy("c", work, "scale-layered")
    init_and_scan(fx, batch_entries=SCALE_BATCH_LIMIT)
    started = time.time()
    outcome, rounds, applied = scale_author_rounds(fx, "layered", {})
    al, _ = aligned(fx)
    ok = outcome == "aligned" and al
    rep.rec(g, "layered-relations-converge", "PASS" if ok else "FAIL",
            f"outcome={outcome} rounds={len(rounds)} applied={applied} aligned={al}",
            duration_s=round(time.time() - started), rounds=len(rounds))

    # 跨批大环: 210 个对象首尾互指, 远超单批上限。这是真实代码的常态形状
    # (A 调 B, B 回调 A), 曾经会被"关系闭包"机制判成不可拆成分而彻底卡死整个
    # 索引的建立。关系是模型写给模型的语义, 不是机器的排程约束, 必须照常建成。
    fx = deploy("c", work, "scale-cyclic")
    init_and_scan(fx, batch_entries=SCALE_BATCH_LIMIT)
    ring = build_cyclic_ring([os.path.relpath(os.path.join(base, name), fx)
                              for base, _, names in os.walk(os.path.join(fx, "src"))
                              for name in names], SCALE_BATCH_LIMIT + 10)
    started = time.time()
    outcome, rounds, applied = scale_author_rounds(fx, "cyclic", ring)
    al, _ = aligned(fx)
    ok = outcome == "aligned" and al
    rep.rec(g, "oversized-cycle-still-builds-the-index", "PASS" if ok else "FAIL",
            f"outcome={outcome[:80]} rounds={len(rounds)} applied={applied} aligned={al}",
            duration_s=round(time.time() - started), rounds=len(rounds), applied=applied)
    batch_ids = [r["batch"] for r in rounds]
    rep.rec(g, "no-batch-identity-repeat", "PASS" if len(batch_ids) == len(set(batch_ids)) else "FAIL",
            f"batches={batch_ids}")


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
    # 两件事都要成立: Apply 仍被拒, 且原因仍然看得见。理由字符串本身不是判据 ——
    # 覆盖缩减现在路由给独立审批(它本就该有审批人), 于是 Apply 说的是"需要真人",
    # 而"为什么需要"由操作者刚生成的 Plan 承载。只钉字符串会把这次修复读成回归。
    blob = (v.get("message") or "") + out
    refused = rc != 0 and ("coverage_reduction" in blob or "human_approval_required" in blob)
    try:
        plan = (jload(open(pv).read()) or {}).get("plan") or {}
        cause_visible = bool((plan.get("risk") or {}).get("cognition_coverage_reduction"))
    except Exception:
        cause_visible = False
    cause_visible = cause_visible or "coverage_reduction" in blob
    rep.rec(g, "post-scan-exclude-needs-human", "PASS" if refused and cause_visible else "FAIL",
            f"refused={refused} cause_visible={cause_visible} | " + (v.get("message") or out)[:90])
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
    # 超时在进程内强制 (subprocess timeout), 不依赖 coreutils timeout(1), macOS 可用
    t0 = time.time()
    try:
        r = subprocess.run([OPENCODE, "run", prompt, "-m", model,
                            "--dir", fx, "--auto", "--format", "json",
                            "--title", f"aoci-lifecycle-{os.path.basename(fx)}"],
                           capture_output=True, text=True, timeout=timeout_s)
        rc, out, err = r.returncode, r.stdout, r.stderr
    except subprocess.TimeoutExpired as e:
        rc = 124
        out = (e.stdout or b"").decode("utf-8", "replace") if isinstance(e.stdout, bytes) else (e.stdout or "")
        err = (e.stderr or b"").decode("utf-8", "replace") if isinstance(e.stderr, bytes) else (e.stderr or "")
        err += "\n===TIMEOUT===\n"
    with open(artifact, "w") as f:
        f.write(out)
        if err:
            f.write("\n===STDERR===\n" + err[-8000:])
    return rc, time.time() - t0, out


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
                elif s == "scale":
                    suite_scale(rep, work)
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
        # Host-window gate over every tools/call the deterministic suites made:
        # no non-Overview response may exceed what an ordinary host shows inline.
        ok, detail = host_window_summary()
        rep.rec("host-window", "every-non-overview-response-fits", "PASS" if ok else "FAIL", detail)
        fixtures, fixture_drift = published_fixture_drift()
        rep.rec("docs", "published-fixtures-are-named", "PASS" if not fixture_drift else "FAIL",
                "; ".join(fixture_drift) if fixture_drift else ", ".join(fixtures))
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
