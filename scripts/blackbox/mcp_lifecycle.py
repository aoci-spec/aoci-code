#!/usr/bin/env python3
"""AOCI lifecycle harness — frozen fixtures, real-agent, model-swappable.

生命周期黑盒：三个冻结的真实项目（repo-a TypeScript 无库 / repo-b Python+MySQL /
repo-c 453 文件分层服务）从 scripts/blackbox/fixtures/ 复制到临时工作副本后走 AOCI
生命周期。母本永不被写，"重置"即复制，无需清洗。repo-a 与 repo-b 走完整的
init→漂移→重对齐全程；repo-c 只到 aligned，不注入漂移，它换来的是多批创作覆盖。
场景分两轨：

  确定性轨 (免模型、结果二值):
    bringup      init 双语/agent 集成/doctor/scan/治理初态; 子模块边界、
                 exclude_root_dirs 三角色与 config set 归一、remove-entry
                 预览与真删、机器文档(source manifest/capabilities)、
                 zh-CN 孪生对照(人类语句无英文碎片、--json 机器字段同一) (repo-a + repo-b)
    incremental  模板条目建索引→改/增/删/换行符 四类增量维护      (repo-a)
    database     MySQL 证据全链: source→snapshot→accept→bootstrap→
                 模板表FRAS→v2 ALTER 漂移→重对齐 (含 inventory 离线语义) (repo-b)
                 默认 mysql:8.4; --mysql-image mysql:5.7 跑同一套断言, 夹具里
                 两条 8.0 专有语句在内存里映射成 5.7 形态 (MYSQL57_FIXTURE_REWRITES)
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
  python3 scripts/blackbox/mcp_lifecycle.py --suites database --mysql-image mysql:5.7
  python3 scripts/blackbox/mcp_lifecycle.py --compare results/A.json results/B.json

Env:  AOCI_REPO / AOCI_BIN / AOCI_OPENCODE 覆盖仓库、二进制与 opencode 路径。
Results: scripts/blackbox/results/<run>.json + artifacts/（gitignored）。
"""
import argparse, atexit, base64, hashlib, json, os, re, shutil, socket, subprocess, sys, tempfile, time, traceback

# The suites print fixture output that may carry Chinese text; on a Windows
# runner the console codec is cp1252, so print the report as UTF-8 regardless.
for _stream in (sys.stdout, sys.stderr):
    if hasattr(_stream, "reconfigure"):
        try:
            _stream.reconfigure(encoding="utf-8", errors="replace")
        except (ValueError, OSError):
            pass


_HERE = os.path.dirname(os.path.abspath(__file__))
sys.path.insert(0, _HERE)
# mcp_scenarios reads AOCI_SCENARIO_WORK at import; a directory this module
# creates for that is its own to remove, or every import leaked one.
if "AOCI_SCENARIO_WORK" not in os.environ:
    _LIB_WORK = tempfile.mkdtemp(prefix="aoci-lifecycle-lib-")
    os.environ["AOCI_SCENARIO_WORK"] = _LIB_WORK
    atexit.register(shutil.rmtree, _LIB_WORK, True)
from mcp_scenarios import Session, text_of, jload, parse_kv, meta_and_body, cli, sh, maintain, host_window_summary, mark_team_raised_batch, repo_digest  # noqa: E402

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


# A real Java repository (RuoYi) put 141 third-party files under static/ajax/libs/
# into the index role and the model spent a third of the first build authoring
# Entries for jQuery. init now persists starter observe rules for the production
# profile, so the first scan must classify a vendored bundle as observe while the
# project's own code beside it keeps index. The probe is planted in the working
# copy before init, so the frozen masters and every count bound to them stay as
# they are.
STATIC_PROBE_ROLES = {"static/ajax/libs/jquery.min.js": "observe", "static/app/js/ry-ui.js": "index"}


def plant_static_probe(fx):
    for rel, body in (("static/ajax/libs/jquery.min.js", '/*! jQuery v3.7.1 */!function(e,t){"use strict"}();\n'),
                      ("static/app/js/ry-ui.js", "// project code under static/, not a vendored bundle\nwindow.ry = {ready: true};\n")):
        path = os.path.join(fx, rel)
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w", encoding="utf-8") as fh:
            fh.write(body)
    sh(fx, "git", "add", "-A")
    sh(fx, "git", "commit", "-q", "-m", "static probe")


IGNORED_TREE_FILES = 400


def plant_ignored_tree(fx):
    """#97: an ignored directory full of files (and a symlink back into it) must
    cost the inventory one line, not one entry per file. node_modules is in
    .gitignore, so git is the only thing that could ever walk it.

    The link node_modules/loop-target/back points at "..", which from
    loop-target is node_modules itself: a real cycle. git lstat()s a symlink and
    never follows it, so the link is not what this check discriminates on; it is
    there so a walker that did follow links would loop. Returns whether the cycle
    was planted, so the record says so; where os.symlink is unavailable or not
    permitted (Windows without the privilege) the tree is planted without it
    and the check is unchanged."""
    os.makedirs(os.path.join(fx, "node_modules", "loop-target"), exist_ok=True)
    for i in range(IGNORED_TREE_FILES):
        d = os.path.join(fx, "node_modules", f"pkg{i:03d}")
        os.makedirs(d, exist_ok=True)
        with open(os.path.join(d, "index.js"), "w", encoding="utf-8") as fh:
            fh.write("module.exports = %d;\n" % i)
    link = os.path.join(fx, "node_modules", "loop-target", "back")
    try:
        os.symlink("..", link, target_is_directory=True)
        planted = os.path.samefile(link, os.path.join(fx, "node_modules"))
    except (OSError, NotImplementedError):
        planted = False
    gi = os.path.join(fx, ".gitignore")
    existing = open(gi, encoding="utf-8").read() if os.path.exists(gi) else ""
    if "node_modules" not in existing:
        with open(gi, "a", encoding="utf-8") as fh:
            fh.write(("" if existing.endswith("\n") or not existing else "\n") + "node_modules/\n")
        sh(fx, "git", "add", ".gitignore")
        sh(fx, "git", "commit", "-q", "-m", "ignore node_modules")
    return planted


def ignored_tree_facts(fx):
    """How the inventory accounted for the planted tree: the collapsed entry and the ignored count."""
    rc, _, out, _ = cli(fx, "scope", "status", "--json", expect_ok=False)
    try:
        status = json.loads(out)
    except ValueError:
        return {"rc": rc, "ignored": None, "collapsed": False, "descended": None}
    ev = status.get("evaluation") or {}
    summary = ev.get("safe_inventory") or {}
    paths = [e.get("path") for k in ("index", "observe", "exclude") for e in (ev.get(k) or [])]
    return {"rc": rc, "ignored": summary.get("ignored"), "collapsed": "node_modules/" in paths,
            "descended": any(p.startswith("node_modules/pkg") for p in paths if p)}


def baseline_roles(fx, rels):
    path = os.path.join(fx, ".aoci", "baseline.json")
    if not os.path.exists(path):
        return {}
    with open(path, encoding="utf-8") as fh:
        files = json.load(fh).get("files") or {}
    return {rel: (files.get(rel) or {}).get("role") for rel in rels}


# Curation 探针 (空/超限/二进制) 不是授权债务: 首次 Maintain 照常发批, 探针以
# code_skipped 只读上报并带原因, 自动模式一路对齐。团队排除若在首次 scan 之前落进初始
# 策略, 探针改记 code_curation_excluded; 事后再改会构成覆盖缩减、要求真人复核
# (governance 场景专门断言这几种行为)。
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
        if batch_entries > 50:
            # The transport budget is the operative bound; the raised-limit
            # suites raise it to the maximum so the count cap is what they test.
            rc, _, out, errs = cli(fx, "config", "set", "maintain_transport_budget_bytes", str(192 * 1024))
            if rc != 0:
                raise RuntimeError(f"config set maintain_transport_budget_bytes failed: {out[:200]} {errs[:200]}")
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
# MySQL 5.7 cannot parse the two 8.0-only statements in the frozen repo-b
# schema (fixtures are never edited; the rewrite happens in memory). Each is
# mapped to the nearest 5.7 state: an invisible index is one the optimizer
# never uses, so on 5.7 it does not exist in v1 and is added in v2, where 8.x
# makes it visible. The drift arithmetic is therefore the same on both
# releases (customers changed, products changed, shipments new) and no
# assertion below is weakened. Every anchor must occur exactly once, so a
# fixture edit cannot silently send 8.x SQL to a 5.7 server.
MYSQL57_FIXTURE_REWRITES = {
    "init.sql": [(",\n    KEY idx_products_name (name) INVISIBLE", "")],
    "v2_alter.sql": [("ALTER TABLE products ALTER INDEX idx_products_name VISIBLE;",
                      "ALTER TABLE products ADD INDEX idx_products_name (name);")],
}


def mysql57_fixture_sql(name, sql):
    for old, new in MYSQL57_FIXTURE_REWRITES.get(name, []):
        if sql.count(old) != 1:
            raise RuntimeError(f"{name}: 5.7 rewrite anchor occurs {sql.count(old)} times: {old!r}")
        sql = sql.replace(old, new)
    return sql


class MySQLBox:
    def __init__(self, image, run_id):
        self.image, self.name = image, f"aoci-lc-mysql-{run_id}"
        self.port, self.pw, self.db = None, "lcpass", "aoci_shop"
        self.server_version, self.major = "", 0

    def dsn(self):
        return f"root:{self.pw}@tcp(127.0.0.1:{self.port})/{self.db}"

    def probe_version(self):
        r = subprocess.run(["docker", "exec", self.name, "mysql", "-uroot", f"-p{self.pw}",
                            "-N", "-s", "-e", "SELECT VERSION()"],
                           capture_output=True, text=True, encoding="utf-8", errors="replace")
        reported = r.stdout.strip()
        m = re.match(r"(\d+)\.(\d+)\.(\d+)", reported)
        if r.returncode != 0 or not m:
            raise RuntimeError(f"mysql version probe failed: {reported[:80]} {r.stderr[:120]}")
        self.server_version, self.major = reported, int(m.group(1))

    def start(self, timeout=180):
        with socket.socket() as s:
            s.bind(("127.0.0.1", 0))
            self.port = s.getsockname()[1]
        subprocess.run(["docker", "rm", "-f", self.name], capture_output=True)
        r = subprocess.run(["docker", "run", "-d", "--rm", "--name", self.name,
                            "-e", f"MYSQL_ROOT_PASSWORD={self.pw}", "-e", f"MYSQL_DATABASE={self.db}",
                            "-p", f"127.0.0.1:{self.port}:3306", self.image],
                           capture_output=True, text=True, encoding="utf-8", errors="replace")
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
                self.probe_version()
                return self
            time.sleep(2)
        raise RuntimeError("mysql readiness timeout")

    def apply_sql(self, path):
        with open(path, encoding="utf-8") as f:
            sql = f.read()
        if self.major < 8:
            sql = mysql57_fixture_sql(os.path.basename(path), sql)
        r = subprocess.run(["docker", "exec", "-i", self.name, "mysql", "-uroot",
                            f"-p{self.pw}", self.db], input=sql, capture_output=True, text=True, encoding="utf-8", errors="replace")
        if r.returncode != 0:
            raise RuntimeError(f"apply {os.path.basename(path)} failed: {r.stderr[:300]}")

    def digest(self):
        r = subprocess.run(["docker", "inspect", "--format", "{{index .RepoDigests 0}}", self.image],
                           capture_output=True, text=True, encoding="utf-8", errors="replace")
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
        plant_static_probe(fx)
        symlink_planted = plant_ignored_tree(fx)
        rc, _, out, errs = cli(fx, "init", "--locale", "en-US")
        rec_ok = rc == 0 and os.path.exists(os.path.join(fx, "AGENTS.md"))
        rep.rec(g, f"repo-{key}.init-en", "PASS" if rec_ok else "FAIL", (out + errs)[:150] if not rec_ok else "")
        rc, _, out, errs = cli(fx, "init", "--agent", "opencode")
        oc = os.path.join(fx, "opencode.json")
        ok = rc == 0 and os.path.exists(oc) and BIN in open(oc, encoding="utf-8").read() and fx in open(oc, encoding="utf-8").read()
        rep.rec(g, f"repo-{key}.init-agent-opencode", "PASS" if ok else "FAIL")
        rc, _, out, errs = cli(fx, "scan")
        ok = rc == 0 and os.path.exists(os.path.join(fx, ".aoci", "baseline.json"))
        rep.rec(g, f"repo-{key}.scan", "PASS" if ok else "FAIL")
        roles = baseline_roles(fx, STATIC_PROBE_ROLES)
        rep.rec(g, f"repo-{key}.starter-rules-observe-vendored-static",
                "PASS" if roles == STATIC_PROBE_ROLES else "FAIL", f"roles={roles}")
        facts = dict(ignored_tree_facts(fx), symlink_planted=symlink_planted)
        ok = facts["rc"] == 0 and facts["collapsed"] and not facts["descended"] and (facts["ignored"] or 0) < IGNORED_TREE_FILES
        rep.rec(g, f"repo-{key}.ignored-directory-collapsed-not-walked", "PASS" if ok else "FAIL", f"facts={facts}")
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
    suite_bringup_submodule(rep, work)
    suite_bringup_exclude_root_dirs(rep, work)
    suite_bringup_sensitive_name_rules(rep, work)
    suite_bringup_remove_entry(rep, work)
    suite_bringup_machine_documents(rep, work)
    suite_bringup_locale_twins(rep, work)


# ---------------------------------------------------------------- bringup lanes
EXCLUDE_ROOT_DIRS = ["backup", "backups", "build", "cache", "coverage", "dist", "target", "tmp",
                     "uploads", "artifacts", "vendor"]
NINE_TOOLS = sorted(["aoci_rules", "aoci_overview", "aoci_get_entries", "aoci_search", "aoci_update_entry",
                     "aoci_report", "aoci_remove_entry", "aoci_header", "aoci_maintain"])
HEX64 = re.compile(r"^[0-9a-f]{64}$")


def human(repo, *args):
    """Run the CLI as an operator does, without --json: (rc, stdout + stderr)."""
    proc = subprocess.run([BIN, "--repo", repo, *args], capture_output=True, text=True, encoding="utf-8", errors="replace", timeout=120)
    return proc.returncode, (proc.stdout or "") + (proc.stderr or "")


def init_only(fx, locale="en-US"):
    """init plus the fixture's curation exclusions; scan is left to a caller that wants its report."""
    rc, _, out, errs = cli(fx, "init", "--locale", locale)
    if rc != 0:
        raise RuntimeError(f"init failed: {out[:200]} {errs[:200]}")
    exclude = CURATION_EXCLUDE.get(repo_key_of(fx))
    if exclude:
        rc, _, out, errs = cli(fx, "config", "set", "curation_exclude", exclude)
        if rc != 0:
            raise RuntimeError(f"config set curation_exclude failed: {out[:200]} {errs[:200]}")


def volume_text(fx):
    with open(os.path.join(fx, "aoci.code.txt"), encoding="utf-8") as fh:
        return fh.read()


def suite_bringup_submodule(rep, work):
    """A gitlink is a boundary, not a file (#107).

    repo-b is added to repo-a as a git submodule. The superproject tracks only
    the submodule's commit, so scan reports the gitlink once as a git_submodule,
    scope explain on a path beneath it names the boundary with a reason that
    starts git_submodule: and the hint that the submodule is its own repository,
    and the superproject authors to aligned without an Entry for anything under
    the submodule: its code is indexed only by initializing that repository.
    """
    g = "bringup"
    source = deploy("b", work, "submodule-source")
    fx = deploy("a", work, "submodule")
    # git 2.38+ refuses the file transport for submodules unless allowed explicitly.
    sh(fx, "git", "-c", "protocol.file.allow=always", "submodule", "add", "--quiet", source, "vendor/shopfloor")
    sh(fx, "git", "commit", "-q", "-m", "add shopfloor submodule")
    init_only(fx)
    rc, scan, out, errs = cli(fx, "scan")
    inventory = scan.get("safe_inventory") or {}
    ok = rc == 0 and scan.get("git_submodules") == 1 and inventory.get("unsafe_filesystem_excluded") == 1
    rep.rec(g, "repo-a.submodule.scan-counts-the-gitlink-once", "PASS" if ok else "FAIL",
            f"rc={rc} git_submodules={scan.get('git_submodules')} unsafe_excluded={inventory.get('unsafe_filesystem_excluded')}")
    inside = "vendor/shopfloor/app/main.py"
    rc, item, _, _ = cli(fx, "scope", "explain", inside, expect_ok=False)
    hrc, text = human(fx, "scope", "explain", inside)
    lines = [ln for ln in text.splitlines() if ln.strip()]
    ok = (rc == 0 and item.get("role") == "exclude" and item.get("rule_source") == "safety"
          and str(item.get("reason", "")).startswith("git_submodule:") and item.get("reads_content") is False
          and item.get("enters_whole_index") is False and hrc == 0 and "git_submodule:" in text
          and len(lines) >= 2 and "submodule" in lines[-1])
    rep.rec(g, "repo-a.submodule.explain-names-the-boundary", "PASS" if ok else "FAIL",
            f"rc={rc} role={item.get('role')} reason={item.get('reason')} | {(lines[-1] if lines else '')[:100]!r}")
    ok_all, rounds, detail = author_all(fx)
    al, v = aligned(fx)
    volume = volume_text(fx)
    rc, status, _, _ = cli(fx, "scope", "status", expect_ok=False)
    excluded = {e.get("path"): e.get("reason") for e in ((status.get("evaluation") or {}).get("exclude") or [])}
    ok = (ok_all and al and "vendor/shopfloor" not in volume
          and str(excluded.get("vendor/shopfloor", "")).startswith("git_submodule:"))
    rep.rec(g, "repo-a.submodule.superproject-aligns-without-submodule-entries", "PASS" if ok else "FAIL",
            f"outcome={detail} rounds={rounds} aligned={al} volume_mentions_submodule={'vendor/shopfloor' in volume} "
            f"status_reason={excluded.get('vendor/shopfloor')}")


def suite_bringup_exclude_root_dirs(rep, work):
    """exclude_root_dirs is a root list, and config set keeps it one (#104, #105).

    A fresh init persists the eleven names. A tracked src/build/tool.go is
    source while a tracked build/output.txt at the root is excluded with a
    reason that names the list, the name, and the configured category. config
    set drops a trailing separator and a duplicate, and refuses a path, which
    could never match one component, instead of saving a dead entry.
    """
    g = "bringup"
    fx = deploy("a", work, "rootdirs")
    for rel, body in (("src/build/tool.go", "package build\n\n// a build helper that is project source\nfunc Tool() int { return 1 }\n"),
                      ("build/output.txt", "compiled artifact\n")):
        path = os.path.join(fx, *rel.split("/"))
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w", encoding="utf-8") as fh:
            fh.write(body)
    sh(fx, "git", "add", "-A")
    sh(fx, "git", "commit", "-q", "-m", "root and nested build directories")
    init_only(fx)
    rc, value, _, _ = cli(fx, "config", "get", "exclude_root_dirs", expect_ok=False)
    rep.rec(g, "repo-a.exclude-root-dirs.init-persists-the-root-list",
            "PASS" if rc == 0 and value == EXCLUDE_ROOT_DIRS else "FAIL", f"rc={rc} value={value}")
    rc, _, out, errs = cli(fx, "scan")
    roles = baseline_roles(fx, ["src/build/tool.go", "build/output.txt"])
    rc_n, nested, _, _ = cli(fx, "scope", "explain", "src/build/tool.go", expect_ok=False)
    rc_r, root_item, _, _ = cli(fx, "scope", "explain", "build/output.txt", expect_ok=False)
    ok = (rc == 0 and roles == {"src/build/tool.go": "index", "build/output.txt": None}
          and nested.get("role") == "index" and root_item.get("role") == "exclude"
          and root_item.get("reason") == "exclude_root_dirs:build:configured" and root_item.get("reads_content") is False)
    rep.rec(g, "repo-a.exclude-root-dirs.nested-build-is-source-root-build-is-excluded", "PASS" if ok else "FAIL",
            f"roles={roles} nested={nested.get('role')}/{nested.get('reason')} root={root_item.get('role')}/{root_item.get('reason')}")
    rc, result, out, errs = cli(fx, "config", "set", "exclude_root_dirs", "build/, cache, build", expect_ok=False)
    rc2, stored, _, _ = cli(fx, "config", "get", "exclude_root_dirs", expect_ok=False)
    ok = rc == 0 and result.get("value") == ["build", "cache"] and stored == ["build", "cache"]
    rep.rec(g, "repo-a.exclude-root-dirs.config-set-normalizes-the-list", "PASS" if ok else "FAIL",
            f"rc={rc} value={result.get('value')} stored={stored}")
    rc, result, out, errs = cli(fx, "config", "set", "exclude_root_dirs", "app/build", expect_ok=False)
    rc2, stored, _, _ = cli(fx, "config", "get", "exclude_root_dirs", expect_ok=False)
    blob = (result.get("message") or "") + out + errs
    ok = rc != 0 and result.get("ok") is False and "app/build" in blob and stored == ["build", "cache"]
    rep.rec(g, "repo-a.exclude-root-dirs.config-set-refuses-a-path", "PASS" if ok else "FAIL",
            f"rc={rc} stored={stored} | {' '.join(blob.split())[:120]}")


# #148: names operational repositories give their credentials. The bodies are
# placeholders; the rule reads names and never content.
SENSITIVE_NAME_PROBES = {
    "conf/prod.env": "DB_HOST=db.internal\nDB_PORT=5432\n",
    "config/api-credentials.yaml": "service: shopfloor\nregion: eu-west\n",
    "config/secrets.yaml.example": "service: shopfloor\nregion: <fill in>\n",
    "src/tokenizer.ts": "// splits a query into tokens; the name is not a credential\nexport const tokenize = (s: string) => s.split(/\\s+/);\n",
    "src/password_hasher.ts": "// hashes a password; source, not a secret\nexport const hash = (p: string) => p;\n",
}


def plant_sensitive_name_probes(fx):
    for rel, body in SENSITIVE_NAME_PROBES.items():
        path = os.path.join(fx, *rel.split("/"))
        os.makedirs(os.path.dirname(path), exist_ok=True)
        with open(path, "w", encoding="utf-8") as fh:
            fh.write(body)
    sh(fx, "git", "add", "-A")
    sh(fx, "git", "commit", "-q", "-m", "credential-shaped names")


def suite_bringup_sensitive_name_rules(rep, work):
    """sensitive_name_rules is a persisted rule-set choice (#148).

    A fresh init persists v2, so a tracked conf/prod.env or api-credentials.yaml
    is excluded by name with the built-in sensitive reason and never read, while
    secrets.yaml.example is a template and tokenizer.ts is source. A team
    configuration without the key is read under v1, the rc22 rule, so the same
    prod.env is index there and doctor lists it as what v2 would take out;
    config set refuses a third value and keeps the stored one; config set v2
    makes the next verify answer scope_change_required, and scope activate
    previews the removal and pauses for approval because it is a coverage
    reduction, writing no formal byte.
    """
    g = "bringup"
    fx = deploy("a", work, "sensitive")
    plant_sensitive_name_probes(fx)
    init_only(fx)
    rc, value, _, _ = cli(fx, "config", "get", "sensitive_name_rules", expect_ok=False)
    rep.rec(g, "repo-a.sensitive-name-rules.init-persists-v2", "PASS" if rc == 0 and value == "v2" else "FAIL",
            f"rc={rc} value={value}")
    rc, _, out, errs = cli(fx, "scan")
    roles = baseline_roles(fx, list(SENSITIVE_NAME_PROBES))
    want = {"conf/prod.env": None, "config/api-credentials.yaml": None, "config/secrets.yaml.example": "index",
            "src/tokenizer.ts": "index", "src/password_hasher.ts": "index"}
    rc_e, item, _, _ = cli(fx, "scope", "explain", "conf/prod.env", expect_ok=False)
    ok = (rc == 0 and roles == want and item.get("role") == "exclude"
          and item.get("reason") == "builtin_sensitive_name:sensitive" and item.get("reads_content") is False)
    rep.rec(g, "repo-a.sensitive-name-rules.v2-excludes-credential-names-and-keeps-source", "PASS" if ok else "FAIL",
            f"rc={rc} roles={roles} explain={item.get('role')}/{item.get('reason')}/{item.get('reads_content')}")
    # The rc22 shape: a team configuration without the key is read as v1.
    old = deploy("a", work, "sensitive-v1")
    plant_sensitive_name_probes(old)
    init_only(old)
    cfg_path = os.path.join(old, ".aoci", "config.json")
    with open(cfg_path, encoding="utf-8") as fh:
        cfg = json.load(fh)
    cfg.pop("sensitive_name_rules", None)
    with open(cfg_path, "w", encoding="utf-8") as fh:
        json.dump(cfg, fh, indent=2, ensure_ascii=False)
        fh.write("\n")
    rc, _, _, _ = cli(old, "scan")
    rc_g, value, _, _ = cli(old, "config", "get", "sensitive_name_rules", expect_ok=False)
    roles = baseline_roles(old, ["conf/prod.env", "config/api-credentials.yaml", "config/secrets.yaml.example"])
    rc_d, text = human(old, "doctor")
    listed = "conf/prod.env" in text and "config/api-credentials.yaml" in text
    ok = (rc == 0 and rc_g == 0 and value == "v1" and rc_d == 0 and listed
          and roles == {"conf/prod.env": "index", "config/api-credentials.yaml": "index", "config/secrets.yaml.example": None})
    rep.rec(g, "repo-a.sensitive-name-rules.absent-key-is-v1-and-doctor-lists-the-upgrade", "PASS" if ok else "FAIL",
            f"rc={rc} value={value} roles={roles} doctor_rc={rc_d} listed={listed}")
    rc, result, out, errs = cli(old, "config", "set", "sensitive_name_rules", "v3", expect_ok=False)
    rc2, stored, _, _ = cli(old, "config", "get", "sensitive_name_rules", expect_ok=False)
    blob = (result.get("message") or "") + out + errs
    ok = rc != 0 and result.get("ok") is False and "v3" in blob and stored == "v1"
    rep.rec(g, "repo-a.sensitive-name-rules.config-set-refuses-a-third-value", "PASS" if ok else "FAIL",
            f"rc={rc} stored={stored} | {' '.join(blob.split())[:120]}")
    rc, result, _, _ = cli(old, "config", "set", "sensitive_name_rules", "v2", expect_ok=False)
    rc_v, v, _, _ = cli(old, "verify", expect_ok=False)
    scope = (v.get("governance") or {}).get("managed_scope") or {}
    before = repo_digest(old)
    rc_a, act, out, errs = cli(old, "scope", "activate", expect_ok=False)
    after = repo_digest(old)
    unchanged = all(before[p] == after.get(p) for p in (".aoci/baseline.json", "aoci.txt", "aoci.meta.txt", "aoci.code.txt"))
    details = act.get("details") or {}
    previewed = False
    try:
        with open(details.get("preview_file", ""), encoding="utf-8") as fh:
            previewed = "conf/prod.env" in fh.read()
    except OSError:
        pass
    ok = (rc == 0 and result.get("value") == "v2" and scope.get("scope_change_required") is True
          and rc_a == 2 and act.get("error_code") == "managed_scope_human_approval_required"
          and details.get("interaction_required") is True and unchanged and previewed)
    rep.rec(g, "repo-a.sensitive-name-rules.opting-in-is-a-governed-coverage-reduction", "PASS" if ok else "FAIL",
            f"set_rc={rc} scope_change_required={scope.get('scope_change_required')} activate_rc={rc_a} "
            f"error={act.get('error_code')} unchanged={unchanged} previewed={previewed}")


def suite_bringup_remove_entry(rep, work):
    """The maintainer's remove-entry walks the same pipeline as aoci_remove_entry.

    After the fixture is aligned one source is deleted and committed, so verify
    reports exactly that orphan. --preview with the Volumes object reference
    shows the Entry line it would remove and writes nothing; the real removal
    takes the Entry out of the Code Volume and leaves verify aligned. The help
    says a live file's Entry may be removed as well; what this binary does with
    that request is recorded as a characterization, not judged.
    """
    g = "bringup"
    fx = deploy("a", work, "remove-entry")
    init_and_scan(fx)
    ok_all, rounds, detail = author_all(fx)
    if not ok_all:
        rep.rec(g, "repo-a.remove-entry.preview-is-zero-write", "FAIL", f"fixture authoring failed: {detail}")
        rep.rec(g, "repo-a.remove-entry.orphan-removed-and-realigned", "FAIL", "fixture authoring failed")
        rep.rec(g, "repo-a.remove-entry.live-file-preview", "CHAR", "not reached")
        return
    target = "src/utils/time.ts"
    os.remove(os.path.join(fx, "src", "utils", "time.ts"))
    sh(fx, "git", "add", "-A")
    sh(fx, "git", "commit", "-q", "-m", "drop time.ts")
    _, v0 = aligned(fx)
    orphans = ((v0.get("governance") or {}).get("code_drift") or {}).get("orphan") or []
    before = volume_text(fx)
    rc, text = human(fx, "remove-entry", "--path", "code:" + target, "--preview")
    after_preview = volume_text(fx)
    _, v1 = aligned(fx)
    still = ((v1.get("governance") or {}).get("code_drift") or {}).get("orphan") or []
    ok = (orphans == [target] and rc == 0 and "code:" + target in text and "time.ts[" in text
          and after_preview == before and still == [target])
    rep.rec(g, "repo-a.remove-entry.preview-is-zero-write", "PASS" if ok else "FAIL",
            f"orphans={orphans} rc={rc} volume_unchanged={after_preview == before} still_orphan={still} | "
            f"{' '.join(text.split())[:100]}")
    rc, text = human(fx, "remove-entry", "--path", "code:" + target)
    after = volume_text(fx)
    al, v2 = aligned(fx)
    gone = ((v2.get("governance") or {}).get("code_drift") or {}).get("orphan") or []
    ok = rc == 0 and after != before and "time.ts[" not in after and al and gone == []
    rep.rec(g, "repo-a.remove-entry.orphan-removed-and-realigned", "PASS" if ok else "FAIL",
            f"rc={rc} volume_changed={after != before} entry_gone={'time.ts[' not in after} aligned={al} orphans={gone} | "
            f"{' '.join(text.split())[:100]}")
    live = "src/utils/id.ts"
    rc, text = human(fx, "remove-entry", "--path", "code:" + live, "--preview")
    rep.rec(g, "repo-a.remove-entry.live-file-preview", "CHAR",
            f"rc={rc} volume_unchanged={volume_text(fx) == after} | {' '.join(text.split())[:140]}")


def suite_bringup_machine_documents(rep, work):
    """source manifest and capabilities are machine documents: a fixed shape, the same bytes twice."""
    g = "bringup"
    fx = deploy("a", work, "machine-docs")
    init_and_scan(fx)
    stamp = "2026-01-01T00:00:00Z"
    rc1, m1, out1, errs1 = cli(fx, "source", "manifest", "--generated-at", stamp, expect_ok=False)
    rc2, m2, out2, errs2 = cli(fx, "source", "manifest", "--generated-at", stamp, expect_ok=False)
    required = {"version", "ordered_paths", "files", "inclusion_exclusion_identity", "safe_inventory_rules_identity",
                "curation_identity", "git_head", "line_ending_policy", "aggregate_algorithm", "aggregate_sha256",
                "safe_inventory", "generated_at", "network_accessed"}
    files = m1.get("files") or []
    paths = m1.get("ordered_paths") or []
    ok = (rc1 == 0 and rc2 == 0 and out1 == out2 and required <= set(m1)
          and m1.get("version") == "business-source-manifest/v1" and m1.get("generated_at") == stamp
          and m1.get("network_accessed") is False and bool(HEX64.match(str(m1.get("aggregate_sha256"))))
          and bool(files) and [f.get("path") for f in files] == paths
          and all(HEX64.match(str(f.get("sha256"))) and isinstance(f.get("size_bytes"), int) for f in files)
          and "src/server.ts" in paths and "aoci.txt" not in paths)
    rep.rec(g, "repo-a.source-manifest-json-shape-and-deterministic", "PASS" if ok else "FAIL",
            f"rc={rc1},{rc2} identical={out1 == out2} missing={sorted(required - set(m1))} files={len(files)}"
            + (f" | {(errs1 or out1)[:100]}" if rc1 else ""))
    rc1, c1, out1, errs1 = cli(fx, "capabilities", expect_ok=False)
    rc2, c2, out2, _ = cli(fx, "capabilities", expect_ok=False)
    required = {"version", "product", "binary", "aoci_version", "mcp_protocol", "mcp_tools", "mcp_tool_count",
                "mcp_tool_name_identity", "cli_lifecycle_capabilities", "input_schema_versions", "current_layout",
                "network_accessed"}
    ok = (rc1 == 0 and rc2 == 0 and out1 == out2 and required <= set(c1)
          and c1.get("version") == "aoci-capability-manifest/v1" and sorted(c1.get("mcp_tools") or []) == NINE_TOOLS
          and c1.get("mcp_tool_count") == 9 and "business-source-manifest/v1" in (c1.get("input_schema_versions") or [])
          and c1.get("network_accessed") is False)
    rep.rec(g, "repo-a.capabilities-json-shape-and-deterministic", "PASS" if ok else "FAIL",
            f"rc={rc1},{rc2} identical={out1 == out2} missing={sorted(required - set(c1))} tools={c1.get('mcp_tool_count')}"
            + (f" | {(errs1 or out1)[:100]}" if rc1 else ""))


# The zh-CN lane compares a zh-CN run against an en-US run of the same commands
# on the same fixture. The en-US run is the en-US catalog as this binary renders
# it, so the suite needs no copy of the catalog: an English sentence fragment is
# a run of three or more words from the en-US human output that is not part of
# a command line (`aoci ...`, which both locales quote verbatim, example
# arguments included), not a product name without a lowercase word ("Qoder CLI
# MCP"), and not machine data the --json output carries verbatim (a rule reason
# such as "default production role"). Machine fields are compared after
# normalizing what legitimately differs: the fixture root, digests and
# timestamps, token and byte estimates of localized text, timings, and the
# fields that carry human sentences.
EN_RUN = re.compile(r"(?<![\w/.\-])[A-Za-z][A-Za-z'\-]*(?: [A-Za-z][A-Za-z'\-]*){2,}(?![\w/.\-])")
COMMAND_SPAN = re.compile(r"\baoci\b(?: +[^\s;。]+)*")
LOCALE_VOLATILE_KEYS = {"whole_index_tokens", "structure_tokens", "size_bytes", "deterministic_ms", "duration_ms",
                        "message", "instructions", "authoring_meta", "hint", "cognition_level_message", "next_step",
                        "detail", "summary"}
HEX_RUN = re.compile(r"\b[0-9a-f]{40,64}\b")
TIMESTAMP_RUN = re.compile(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?Z|\d{8}T\d{6}Z")
LOCALE_HUMAN_COMMANDS = [("scan", "--dry-run"), ("doctor",), ("scope", "status"), ("scope", "explain", "src/utils/time.ts"),
                         ("scope", "rule", "list"), ("verify",), ("check",), ("status",),
                         ("source", "manifest", "--generated-at", "2026-01-01T00:00:00Z")]
LOCALE_JSON_COMMANDS = [("scope", "status"), ("scope", "explain", "src/utils/time.ts"), ("scope", "rule", "list"),
                        ("config", "get", "exclude_root_dirs"), ("verify",), ("check",), ("status",),
                        ("index", "agent", "guide", "--agent", "claude"), ("capabilities",),
                        ("source", "manifest", "--generated-at", "2026-01-01T00:00:00Z")]
LOCALE_POST_COMMANDS = [("verify",), ("check",), ("status",)]


def has_cjk(text):
    return any("一" <= ch <= "鿿" for ch in str(text))


def machine_view(obj, root):
    if isinstance(obj, dict):
        return {k: machine_view(v, root) for k, v in obj.items() if k not in LOCALE_VOLATILE_KEYS}
    if isinstance(obj, list):
        return [machine_view(v, root) for v in obj]
    if isinstance(obj, str):
        return TIMESTAMP_RUN.sub("<ts>", HEX_RUN.sub("<hex>", obj.replace(root, "<root>")))
    return obj


def json_diff(a, b, path="$"):
    out = []
    if isinstance(a, dict) and isinstance(b, dict):
        for k in sorted(set(a) | set(b)):
            if k not in a or k not in b:
                out.append(f"{path}.{k}: only in {'en-US' if k in a else 'zh-CN'}")
            else:
                out += json_diff(a[k], b[k], f"{path}.{k}")
    elif isinstance(a, list) and isinstance(b, list):
        if len(a) != len(b):
            out.append(f"{path}: {len(a)} vs {len(b)} items")
        for i, (x, y) in enumerate(zip(a, b)):
            out += json_diff(x, y, f"{path}[{i}]")
    elif a != b:
        out.append(f"{path}: {str(a)[:60]!r} vs {str(b)[:60]!r}")
    return out


def without_command_spans(text):
    return COMMAND_SPAN.sub(" ", text)


# Host names are proper nouns the operator sees unchanged in every locale.
# `doctor` prints the detected set as one bracketed list, so on a machine with
# three hosts installed the run "claude codex qoder" reads as an English
# sentence to EN_RUN and was flagged as a leak on developer machines while CI
# runners, which detect none, passed. A fragment made only of host names is
# runtime data, not a sentence.
HOST_NAMES = {"claude", "codex", "cursor", "opencode", "qoder", "windsurf"}


def en_sentence_fragments(text, machine_blob):
    out = set()
    for fragment in EN_RUN.findall(without_command_spans(text)):
        if not any(word[:1].islower() for word in fragment.split(" ")):
            continue
        if all(word.lower() in HOST_NAMES for word in fragment.split(" ")):
            continue
        if fragment in machine_blob:
            continue
        out.add(fragment)
    return out


def suite_bringup_locale_twins(rep, work):
    """repo-a under zh-CN against repo-a under en-US: the same commands, twice.

    Every human line an operator sees must be in the locale they chose, with no
    sentence fragment carried over from the en-US rendering, while every --json
    document must be the same machine document in both runs.
    """
    g = "bringup"
    runs = {}
    for locale in ("en-US", "zh-CN"):
        fx = deploy("a", work, f"locale-{locale}")
        human_out, docs, failures = {}, {}, []
        rc, text = human(fx, "init", "--locale", locale)
        human_out[("init",)] = text
        if rc != 0:
            failures.append(f"init rc={rc}")
        rc, _, out, errs = cli(fx, "config", "set", "curation_exclude", CURATION_EXCLUDE["a"])
        if rc != 0:
            failures.append(f"config set rc={rc}")
        for command in LOCALE_HUMAN_COMMANDS:
            if command == ("scan", "--dry-run"):
                rc, text = human(fx, *command)
                human_out[command] = text
                rc, scan, out, errs = cli(fx, "scan")
                docs[("scan",)] = scan if rc == 0 else {"_rc": rc, "_out": out + errs}
        for command in LOCALE_JSON_COMMANDS:
            rc, body, out, errs = cli(fx, *command, expect_ok=False)
            docs[command] = body if body else {"_rc": rc, "_out": out + errs}
        for command in LOCALE_HUMAN_COMMANDS:
            if command != ("scan", "--dry-run"):
                rc, text = human(fx, *command)
                human_out[command] = text
        ok_all, rounds, detail = author_all(fx)
        for command in LOCALE_POST_COMMANDS:
            rc, body, out, errs = cli(fx, *command, expect_ok=False)
            docs[("post",) + command] = body if body else {"_rc": rc, "_out": out + errs}
            rc, text = human(fx, *command)
            human_out[("post",) + command] = text
        al, _ = aligned(fx)
        runs[locale] = {"fx": fx, "human": human_out, "docs": docs, "aligned": ok_all and al, "rounds": rounds,
                        "detail": detail, "failures": failures}
    en, zh = runs["en-US"], runs["zh-CN"]
    machine_blob = json.dumps([machine_view(doc, en["fx"]) for doc in en["docs"].values()], ensure_ascii=False)
    fragments = set()
    for text in en["human"].values():
        fragments |= en_sentence_fragments(text, machine_blob)
    zh_blob = without_command_spans("\n".join(zh["human"].values()))
    leaks = sorted(fragment for fragment in fragments if fragment in zh_blob)
    without_chinese = [" ".join(command) for command, text in zh["human"].items() if not has_cjk(text)]
    ok = not leaks and not without_chinese and not zh["failures"] and not en["failures"]
    rep.rec(g, "repo-a.zh-CN.human-output-free-of-en-US-sentences", "PASS" if ok else "FAIL",
            f"en_fragments={len(fragments)} leaks={leaks[:6]} commands_without_chinese={without_chinese} "
            f"failures={zh['failures'] + en['failures']}")
    diffs = []
    for command in en["docs"]:
        diffs += [f"{' '.join(command)}: {line}" for line in
                  json_diff(machine_view(en["docs"][command], en["fx"]), machine_view(zh["docs"].get(command), zh["fx"]))]
    rep.rec(g, "repo-a.zh-CN.json-machine-fields-identical-to-en-US", "PASS" if not diffs else "FAIL",
            f"documents={len(en['docs'])} differences={len(diffs)} {diffs[:5]}")
    ok = en["aligned"] and zh["aligned"] and en["rounds"] == zh["rounds"]
    rep.rec(g, "repo-a.zh-CN.aligned-in-the-same-rounds-as-en-US", "PASS" if ok else "FAIL",
            f"en aligned={en['aligned']} rounds={en['rounds']} | zh aligned={zh['aligned']} rounds={zh['rounds']} {zh['detail'][:60]}")


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
        rep.rec(g, "snapshot", "PASS" if rc == 0 and snap_sha else "FAIL",
                out[:120] if not snap_sha else f"server={box.server_version}")
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

    # 机器默认(上限 50、传输预算 24 KiB)下的真实首轮: 453 文件的新仓库, 每次
    # Maintain 都必须装进传输预算, 逐轮滚动直到对齐。这是真实用户撞到的形状 ——
    # 200/批时首次 Maintain 约 212 KB, 宿主落盘, 模型退回脚本旁路; 现在必须一路内联。
    fx = deploy("c", work, "scale-default")
    init_and_scan(fx)
    started = time.time()
    outcome, rounds, applied = scale_author_rounds(fx, "none", {}, max_rounds=40)
    al, _ = aligned(fx)
    batches = [r for r in rounds if r["status"] == "applied"]
    sizes = [r["maintain_bytes"] for r in rounds if r.get("maintain_bytes")]
    ok = outcome == "aligned" and al and len(batches) >= 10 \
        and all(r.get("max_entries") == 50 for r in rounds) \
        and sizes and max(sizes) <= 24 * 1024
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
    # 无排除: 三个探针 (空/超限/二进制) 不是授权债务。首次 Maintain 直接发批, 探针以
    # code_skipped 只读上报并带原因, 自动模式一路对齐。rc14 曾把它们伪装成
    # "pending_curation:" 孤儿把批次挡在 stopped, 而 Volumes v1 没有裁决通道, 用户第一次
    # 建索引就没有出口。
    fx = deploy("a", work, "gov-held")
    init_and_scan(fx, curation_exclude=None)
    s = Session(fx)
    m, t, _ = maintain(s)
    s.close()
    gov = m.get("governance") or {}
    markers = [str(o) for o in (m.get("orphan_remove_candidates") or [])]
    probes = {"empty": "data/empty.txt", "oversized": "data/audit_dump_rows.txt", "binary": "assets/logo.png"}
    drift = gov.get("code_drift") or {}
    skipped = set(drift.get("skipped") or [])
    ok = (m.get("status") == "repair_required" and not markers and bool(m.get("authoring_meta"))
          and skipped == set(probes.values()))
    rep.rec(g, "probes-held-out-not-blocking", "PASS" if ok else "FAIL",
            f"status={m.get('status')} markers={markers[:4]} skipped={sorted(skipped)}")
    cands = {c.get("path") for c in (m.get("candidates") or [])}
    for kind, p in probes.items():
        rep.rec(g, f"probe-{kind}-not-ordinary-candidate", "PASS" if p not in cands else "FAIL", p)
    # Maintain bounds its per-item governance lists to a leading sample; verify
    # lists every finding, so the causes are read there.
    _, v0 = aligned(fx)
    causes = {f.get("target"): f.get("cause") for f in ((v0.get("governance") or {}).get("findings") or [])
              if f.get("code") == "code_skipped"}
    want = {"data/empty.txt": "empty", "data/audit_dump_rows.txt": "oversize", "assets/logo.png": "binary"}
    rep.rec(g, "probe-causes-named", "PASS" if causes == want else "FAIL", f"causes={causes}")
    # A new production repository persists the lockfile starter rules, so the
    # fixture's package-lock.json is observed and never an authoring candidate.
    rep.rec(g, "lockfile-observed-not-candidate", "PASS" if "package-lock.json" not in cands else "FAIL",
            f"candidate={'package-lock.json' in cands}")
    ok_all, rounds, detail = author_all(fx)
    al, v = aligned(fx)
    rep.rec(g, "auto-mode-aligns-with-probes-held", "PASS" if ok_all and al else "FAIL",
            f"outcome={detail} rounds={rounds} aligned={al}")
    # 直接为二进制写 Entry 仍然允许; 之后它是普通对象, 离开 skipped, 对齐不动。
    s = Session(fx)
    with open(os.path.join(fx, "assets", "logo.png"), "rb") as fh:
        sha = hashlib.sha256(fh.read()).hexdigest()
    r = jload(text_of(s.call("aoci_update_entry", {"path": "assets/logo.png", "source_sha256": sha,
            "new_entry": "logo.png[CG1T]: F:Brand mark shown by the service | R:- | A:- | S:-"}))[0]) or {}
    s.close()
    al2, v2 = aligned(fx)
    skipped2 = set((((v2.get("governance") or {}).get("code_drift") or {}).get("skipped")) or [])
    ok = r.get("status") == "applied" and al2 and skipped2 == {"data/empty.txt", "data/audit_dump_rows.txt"}
    rep.rec(g, "held-file-can-still-be-authored-directly", "PASS" if ok else "FAIL",
            f"status={r.get('status')} aligned={al2} skipped={sorted(skipped2)}")
    # 事后排除 = 覆盖缩减, auto 必须被挡 (防 agent 私自收缩认知面)
    fx = deploy("a", work, "gov-blocked")
    init_and_scan(fx, curation_exclude=None)
    cli(fx, "config", "set", "curation_exclude", CURATION_EXCLUDE["a"])
    cs = os.path.join(work, "gov-cs.json")
    open(cs, "w", encoding="utf-8").write('{"version":"managed-scope-candidate-set/v1","entries":[],"dispositions":[]}')
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
        plan = (jload(open(pv, encoding="utf-8").read()) or {}).get("plan") or {}
        cause_visible = bool((plan.get("risk") or {}).get("cognition_coverage_reduction"))
    except Exception:
        cause_visible = False
    cause_visible = cause_visible or "coverage_reduction" in blob
    rep.rec(g, "post-scan-exclude-needs-human", "PASS" if refused and cause_visible else "FAIL",
            f"refused={refused} cause_visible={cause_visible} | " + (v.get("message") or out)[:90])
    # 预置排除: 首次 scan 之前写进 curation_exclude 的文件根本不进 index 角色, 于是既不是
    # 候选也不在 skipped 里, 批次照发
    fx2 = deploy("a", work, "gov-excluded")
    init_and_scan(fx2)
    s = Session(fx2)
    m, t, _ = maintain(s)
    s.close()
    drift = (m.get("governance") or {}).get("code_drift") or {}
    cands2 = {c.get("path") for c in (m.get("candidates") or [])}
    ok = (bool(cands2) and not (m.get("orphan_remove_candidates") or []) and not (cands2 & set(probes.values()))
          and not drift.get("skipped") and not drift.get("curation_excluded"))
    rep.rec(g, "pre-scan-exclude-keeps-probes-out-of-scope", "PASS" if ok else "FAIL",
            f"cands={len(cands2)} excluded={drift.get('curation_excluded')} skipped={drift.get('skipped')}")
    rc, v, out, _ = cli(fx2, "scope", "budget", expect_ok=False)
    rep.rec(g, "budget-visible", "PASS" if rc == 0 else "FAIL")
    suite_governance_stale_source(rep, work)


def read_baseline_digest(fx, rel):
    with open(os.path.join(fx, ".aoci", "baseline.json"), encoding="utf-8") as fh:
        return ((json.load(fh).get("files") or {}).get(rel) or {}).get("sha256")


def suite_governance_stale_source(rep, work):
    """#47 on the real fixture: a pending policy edit plus one stale index source.

    Until rc12 a Volumes repository in that state had no legal move: Maintain
    and update_entry stopped with scope_change_required until the policy was
    active, the policy-only preview was refused with
    managed_scope_index_source_stale, and the Entry candidate that clears that
    block under Legacy was projected into the Root manifest (which holds no
    Entries) and would have applied as low risk. The order that works now is
    the natural one: activate the policy with an empty candidate set, then let
    Maintain plan the stale source.
    """
    g = "governance"
    fx = deploy("a", work, "gov-stale")
    init_and_scan(fx)
    ok, rounds, detail = author_all(fx)
    rep.rec(g, "stale-fixture-aligned", "PASS" if ok else "FAIL", f"rounds={rounds} {detail[:80]}")
    if not ok:
        return
    target = "src/utils/id.ts"
    old_sha = read_baseline_digest(fx, target)
    with open(os.path.join(fx, target), "ab") as fh:
        fh.write(b"\n// stale-source probe: appended after alignment\n")
    rc, _, out, errs = cli(fx, "scope", "rule", "add", "no-op-future", "--action", "exclude",
                           "--pattern", "never-present.txt", "--pattern-kind", "file", "--order", "100",
                           "--reason", "lifecycle no-op policy change")
    if rc != 0:
        rep.rec(g, "stale-plus-policy-edit-stops-maintain", "FAIL", f"rule add rc={rc} {(out + errs)[:120]}")
        return
    s = Session(fx)
    m, t, _ = maintain(s)
    s.close()
    rep.rec(g, "stale-plus-policy-edit-stops-maintain",
            "PASS" if m.get("status") == "stopped" and "scope_change_required" in t else "FAIL",
            f"status={m.get('status')} candidates={len(m.get('candidates') or [])}")

    cs = os.path.join(work, "gov-stale-cs.json")
    with open(cs, "w", encoding="utf-8") as fh:
        fh.write('{"version":"managed-scope-candidate-set/v1","entries":[],"dispositions":[]}')
    with open(os.path.join(fx, "aoci.txt"), "rb") as fh:
        manifest_before = fh.read()
    rc, env, out, errs = cli(fx, "scope", "preview", "--candidate-file", cs, expect_ok=False)
    retained = [i.get("path") for i in ((env.get("plan") or {}).get("source_stale_retained") or [])]
    kept = None
    try:
        raw = base64.b64decode((env.get("baseline_postimage") or {}).get("postimage_bytes") or "")
        kept = ((json.loads(raw).get("files") or {}).get(target) or {}).get("sha256") == old_sha
    except Exception as err:  # noqa: BLE001
        kept = f"undecodable: {err}"
    rep.rec(g, "policy-only-preview-retains-stale-source",
            "PASS" if rc == 0 and old_sha is not None and retained == [target] and kept is True else "FAIL",
            f"rc={rc} retained={retained} old_digest_kept={kept} | {(out + errs)[:100] if rc else ''}")
    if rc != 0:
        return
    pv = os.path.join(work, "gov-stale-pv.json")
    with open(pv, "w", encoding="utf-8") as fh:
        fh.write(out)
    rc, v, out, errs = cli(fx, "scope", "apply", "--preview-file", pv, expect_ok=False)
    with open(os.path.join(fx, "aoci.txt"), "rb") as fh:
        manifest_same = fh.read() == manifest_before
    digest_kept = old_sha is not None and read_baseline_digest(fx, target) == old_sha
    rep.rec(g, "policy-only-apply-keeps-manifest-and-old-digest",
            "PASS" if rc == 0 and v.get("status") == "applied" and manifest_same and digest_kept else "FAIL",
            f"rc={rc} status={v.get('status')} manifest_same={manifest_same} digest_kept={digest_kept} | "
            + ((v.get("message") or out + errs)[:100] if rc else ""))
    s = Session(fx)
    m, t, _ = maintain(s)
    s.close()
    planned = [c.get("path") for c in (m.get("candidates") or [])]
    rep.rec(g, "maintain-plans-retained-stale-source",
            "PASS" if m.get("status") == "repair_required" and planned == [target] else "FAIL",
            f"status={m.get('status')} candidates={planned}")
    ok, rounds, detail = author_all(fx)
    rep.rec(g, "stale-source-authored-to-alignment", "PASS" if ok else "FAIL", f"rounds={rounds} {detail[:80]}")

    # Under Volumes the candidate channel has no document to edit, so a set
    # that carries Entries is refused before projection rather than applied.
    with open(os.path.join(fx, target), "rb") as fh:
        live_sha = hashlib.sha256(fh.read()).hexdigest()
    carrying = os.path.join(work, "gov-stale-entry-cs.json")
    with open(carrying, "w", encoding="utf-8") as fh:
        json.dump({"version": "managed-scope-candidate-set/v1",
                   "entries": [{"candidate_id": "c1", "path": target, "source_sha256": live_sha,
                                "new_entry": "id.ts[CG5T]: F:Provides fixture identifiers | R:- | A:- | S:-",
                                "review_status": "reviewed"}],
                   "dispositions": []}, fh)
    rc, v, out, errs = cli(fx, "scope", "preview", "--candidate-file", carrying, expect_ok=False)
    blob = (v.get("message") or "") + out + errs
    rep.rec(g, "volumes-entry-candidates-refused",
            "PASS" if rc != 0 and "managed_scope_volumes_entry_candidates_unsupported" in blob else "FAIL",
            f"rc={rc} | {blob[:120]}")


# ---------------------------------------------------------------- M suites
def opencode_run(fx, model, prompt, timeout_s, artifact):
    # 超时在进程内强制 (subprocess timeout), 不依赖 coreutils timeout(1), macOS 可用
    t0 = time.time()
    try:
        r = subprocess.run([OPENCODE, "run", prompt, "-m", model,
                            "--dir", fx, "--auto", "--format", "json",
                            "--title", f"aoci-lifecycle-{os.path.basename(fx)}"],
                           capture_output=True, text=True, encoding="utf-8", errors="replace", timeout=timeout_s)
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
                          capture_output=True, text=True, encoding="utf-8", errors="replace").stdout.strip()
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
