# Windows Host-Agent 与 PowerShell 5 契约

本文档定义AOCI-CODE CLI在Windows、Codex Desktop和Windows PowerShell 5环境中的稳定调用方式。它是Host-Agent自动化协议的一部分。

> English rendering: [windows-host-agent.en.md](windows-host-agent.en.md).
>
> 范围说明：本文档的Entries/Header/Curation Stage流水线（第5-13节与16.3节）适用于Legacy单体索引仓库。Volume-first新仓库（当前默认初始化布局）的Windows流程是当前Guide + 普通无参数aoci_maintain + aoci_update_entry，见[cognition-volumes.md](cognition-volumes.md)；安装、认知复用、PowerShell与双指纹章节对两种布局均适用。

## 1. 稳定安装路径

推荐固定安装位置：

~~~text
C:\aoci\bin\aoci.exe
~~~

升级顺序：

1. 关闭Codex Desktop。
2. 确认没有残留的aoci.exe MCP进程。
3. 备份旧二进制。
4. 替换稳定路径文件。
5. 校验版本与SHA-256。
6. 重新启动Codex Desktop并新建会话。

项目级Codex MCP配置必须引用稳定绝对路径：

~~~toml
[mcp_servers.aoci]
command = "C:/aoci/bin/aoci.exe"
args = ["--repo", "C:/aoci/project", "mcp"]
~~~

## 2. 完整索引认知与复用

正常任务入口依次调用一次：

~~~text
aoci_rules
aoci_overview
~~~

aoci_overview返回完整AOCI索引正文（超过传输阈值时按continuation_required分块连续交付，语义不变），使模型一次性建立：

- 系统架构；
- 文件职责；
- 模块边界；
- 文件关系；
- 关键约束；
- 标签字典；
- FRAS纪律和校准示例。

轻量凭据包含runtime_repository_root、index_sha256、mcp_service_version、认知范围、
刷新代次、最近事件与宿主模型对完整全貌可靠性的判断。状态按认知有效性分为：

- valid：凭据匹配、全貌可靠且范围足够，零召回；
- uncertain：局部事实不确定，模型选择无召回、局部Entry或搜索；
- invalid：已经存在待处理刷新，旧Overview不得当作当前完整事实。

valid时：

- 不重复调用aoci_overview；
- 不用aoci_get_entries重复读取已有Entry；
- 不用aoci_search重复检索已有信息；
- 不因重新规划、工具重试、测试失败或小步骤而重读。

任务开始后只允许三种完整刷新原因：

- `context_compaction`：宿主明确发生压缩，或模型明确知道系统全貌已丢失；
- `semantic_threshold`：AOCI计算的去重语义变化文件数达到项目阈值；
- `phase_transition`：一个主要工作阶段完成并准备进入另一个主要阶段。

函数完成、一次测试运行、工具调用、小步骤、固定时间、Token总量、风险路径、索引摘要变化
都不是独立checkpoint原因。多个同时原因合并为一次checkpoint评估；这些事实向Agent提供建议，
不替Agent否决显式认知请求。

宿主或Agent通过既有aoci_overview可选输入声明`context_compaction`或
`phase_transition`并提供幂等`refresh_event_id`；AOCI不能自动推断宿主压缩事件，
`semantic_threshold`只由机器计算。

存在语义变化时，先完成最近稳定工作单元、格式化、Lint和测试，再调用一次
aoci_maintain并完整处理applied、repair_required或stopped。Verify、Check和Guide证明aligned。
普通Overview即使在此之前也会交付完整正式认知，但必须标记Dirty且不可靠。

上下文压缩刷新若传输完整、Host确认、认知身份不变、治理对齐且没有Recovery或第三方冲突，
则Attestation为partial或fail时也消费当前refresh generation：禁止声称完整系统认知，
但继续原source-bound任务，不在同一generation循环Overview。进度、解释、架构或时间问题属于
非控制消息；简洁回答后保留原Plan、Run、Commit、Push和CI身份并继续。只有明确停止、暂停、
取消、回滚、修改范围或禁止Commit/Push才改变执行；该规则不建立持久Task状态。

机器结果为refresh_not_required、refresh_deferred_until_stable、refresh_required或
refresh_ready_for_overview。check_only=true始终只返回紧凑JSON；普通显式调用始终请求完整scope，
即使没有新触发或已经存在相同receipt也不抑制正文。

如果行为合同也已不可靠，先重新调用aoci_rules。

全局MEMORY、历史会话和外部资料只能辅助。系统级架构以当前完整索引为主要依据，具体实现以当前源码为依据。

超过传输阈值的索引由aoci_overview按continuation_required分块连续交付，Host必须自动提交cursor直到completed=true；aoci_get_entries与aoci_search只用于完整认知建立后的局部问题。不得用少量Entry代替完整系统认知。

## 3. 用户文件范围与AOCI托管资产

用户限制“只修改指定文件”时，默认约束Host-Agent直接实施的业务修改，包括源码、测试、配置和文档。

AOCI为保持认知一致性而更新的托管资产不属于业务越界，例如：

~~~text
aoci.txt
aoci.meta.txt aoci.code.txt aoci.database.txt (Volumes仓库)
.aoci/baseline.json
.aoci/curation.json
~~~

本地运行时资产包括：

~~~text
.aoci/ledger.jsonl
.aoci/reports.jsonl
.aoci/drafts/
.aoci/verify_history/
~~~

verify、check、index score 与 index inventory 的“只读”仅表示不修改正式索引或Baseline，不等同于严格零文件写入：Ledger启用时四者都可能追加本地Ledger，verify还会尝试写入Verify History；审计写入失败不改变既有退出码与治理判据。

最终报告必须区分：

~~~text
业务修改文件：
- ...

AOCI托管维护：
- ...

其他业务文件：
- 无
~~~

如果用户明确禁止修改aoci.txt、.aoci、生成文件或任何元数据，则：

- 不执行正式Entry Apply；
- 不执行正式Curation Apply；
- 不前移Baseline；
- 可以执行不落本地审计的Plan或Guide；
- 不在原工作树执行Verify、Check、Index Score或Index Inventory；如需这些运行证据，改在隔离副本中执行；
- 如实报告Stale或待维护状态；
- 不得声称已经aligned。

## 4. 日常开发任务的收尾顺序

aoci_maintain是日常开发任务的最终收尾入口，不是修改过程中的中间步骤。

~~~text
业务修改
→ 格式化
→ Lint、测试及必要检查
→ git diff检查
→ aoci_maintain
→ format-only内部快速处理；真实语义候选由模型逐文件生成
→ 保留source_sha256绑定，一次aoci_update_entry.entries提交整批
→ AOCI内部Check / Diff / CAS / AtomicWrite / Baseline / Ledger / aligned复查
→ 最终回复
~~~

维护后再次修改任何受AOCI管理的文件，会使旧维护结果失效，必须重新检查并重新维护。

aoci_maintain默认返回紧凑JSON：`applied`、`repair_required`或`stopped`、`aligned`、
语义候选、真实停点与耗时/调用/召回/文件/防重计量。达到机器阈值或已有宿主刷新事件时，
还会把`refresh_status`和`refresh_reasons`保留到Apply结果；aligned且
`refresh_ready_for_overview`表示治理已准备好建立可靠认知；先完成Verify、Check、Guide证明，
不得再次Maintain，再由Agent决定下一阶段是否需要普通完整Overview。不再让Host逐条解析内部工作流文案。

## 5. 首次Guide从生效MCP配置启动

首次Guide尚未返回commands，因此不能先尝试裸aoci。

Codex Desktop必须：

1. 读取项目`.codex/config.toml`。
2. 定位`[mcp_servers.aoci]`。
3. 读取`command`并确认它是存在的绝对可执行文件路径。
4. 保留`args`中终止子命令`mcp`之前的AOCI全局参数。
5. 移除终止子命令`mcp`。
6. 追加`index agent guide --agent codex --json`。

示例：

~~~powershell
& "C:/aoci/bin/aoci.exe" `
    --repo "C:/aoci/project" `
    index agent guide `
    --agent codex `
    --json
~~~

不得生成裸命令：

~~~text
aoci index agent guide --agent codex --json
~~~

不得把mcp继续带入Guide命令。

以下情况必须停止并报告：

- `.codex/config.toml`不存在；
- `[mcp_servers.aoci]`不存在；
- command为空或不是绝对路径；
- 可执行文件不存在；
- args无法安全识别终止子命令mcp。

不得猜测PATH、扫描磁盘或轮试多个路径。

## 6. 纯模型语义生成合同

以下内容必须由当前宿主模型阅读真实内容后逐项生成：

- Header；
- 标签字典；
- 每条标签；
- F、R、A、S；
- Curation decision、role、reason、confidence。

禁止用以下方式生成、预填、拼接或改写语义：

- AST或符号枚举；
- import、include或完整依赖扫描；
- 路径、文件名或扩展名模板；
- 正则提取；
- 固定文案模板；
- 规则引擎；
- 批量脚本；
- 工具先生成草稿、模型只做表面润色。

工具可以：

- 枚举目标；
- 读取和分段传递原文；
- 计算摘要、大小、行数和物理画像；
- 控制批次；
- 保存UTF-8请求；
- 校验、审计和落盘。

结构图、AST、LSP和符号工具可以辅助定位及核对关系，但其输出不得直接替代AOCI语义。

### 6.1 FRAS纪律

F：

- 写简短明确的核心功能锚点；
- 通常控制在10个字以内；
- 语义完整优先；
- 长度不产生机器Warning或硬拒（仅Legacy v1条目；Volumes v1对象受FRAS v2硬限F≤160 rune，权威为internal/machinecontract）。

R：

- 只列跨文件强关联；
- 不复制完整依赖或import清单；
- 无强关联写`-`。

A：

- 只列本文件对外API、入口或契约；
- 内部函数不列；
- 无对外契约写`-`。

S：

- 只写文件名、标签、F、R、A无法推出的信息；
- 该信息必须影响系统理解或安全修改；
- 无符合信息写`-`。

### 6.2 Curation纪律

每项decision、role、reason和confidence必须逐项调查。

路径、扩展名和empty、binary、oversize等物理画像只能作为调查线索。

Guide模板中的：

~~~text
confidence = -1
~~~

是无效占位，必须替换为0至100的JSON整数。

## 7. Guide命令必须原样执行

首次Guide成功后，commands已经绑定当前AOCI二进制绝对路径。

必须：

- 保留PowerShell调用运算符`&`；
- 保留路径双引号；
- 保留Guide返回的全局参数；
- 不改回裸aoci；
- 不依赖交互终端PATH；
- 不复用旧命令、旧plan_id、旧run_id或旧摘要。

## 8. Entries Auto三态

（Legacy Stage流水线的终态契约；Volumes仓库的对应终态见aoci_update_entry的applied/repair_required/stopped结果，语义一致。）

automation.mode=auto时，Entries Stage先保存标准草稿，再在内部执行机器预检和自动收口。

Host-Agent必须读取：

~~~text
auto_finalize.status
~~~

### 8.1 applied

~~~text
auto_finalize.status = applied
~~~

表示当前批次已经完成：

- Check；
- Diff；
- P-23审阅记录；
- 原子Apply；
- Application审计。

处理方式：

- 不重复调用Entries Check、Diff或Apply；
- 执行Stage返回的绝对路径next_command；
- 正常情况下该命令是Verify；
- Verify后重新运行Guide；
- 继续后续阶段或批次。

### 8.2 repair_required

~~~text
auto_finalize.status = repair_required
~~~

表示候选内容存在可修复错误，机器预检已经正常完成，但正式资产零写入。

典型错误：

- Entry结构或文件名错误；
- 标签字典维度或符号错误；
- Host-Agent或Endpoint新生成标签不可解析；
- 某个草稿目标缺少可应用候选。

稳定合同：

- Entries Stage进程退出码为0；
- auto_finalize.error为空；
- next_command为空；
- attempted为完整批次数，applied=0，remaining=attempted；
- formal_writes_started=false；
- finding_count与findings长度一致；
- preserve_other_candidates=true；
- retry_scope只包含失败对象；
- 正式资产零写入；
- Baseline不前移；
- 不形成Diff或Application；
- 失败Run与Check审计继续保留。

Host-Agent必须：

1. 读取auto_finalize.findings。
   每条Finding固定包含candidate_index、path、canonical_object_identity、
   domain、field、rule_code、expected、actual、cause和safe_repair_action；
   机器Token不本地化，cause和safe_repair_action按项目Locale输出。
2. 只修正findings中的失败条目。
3. 其他候选保持原样。
4. 保留path和source_sha256。
5. 重写当前请求中的同一完整批次。
6. 重新执行Guide返回的同一条Entries Stage命令。
7. 让新的Stage调用创建新Run。

Host Agent不得（本节所称 Host Agent 即 Host-Agent 协议角色）：

- 要求用户回复“继续”；
- 把普通回复边界当成停止点；
- 调用Entries Check、Diff或Apply；
- 绕过机器校验；
- 修改或删除旧Run；
- 重新生成与findings无关的正确候选。

同一批次的全部可修候选错误必须在第一次返回，并按domain、path、field、
rule_code、原始candidate_index稳定排序。source SHA漂移、Plan过期、CAS冲突、
Recovery pending、第三方冲突、正式写入或写后审计失败仍为stopped或既有硬失败，
不得改标为repair_required。

### 8.3 stopped

~~~text
auto_finalize.status = stopped
~~~

才表示自动流程无法安全继续。

当前版本对stopped写入尝试另有证据驱动的自动闭合：已证零写入自动关闭旧Run并重新Plan；完整Intent带可证postimage自动Resume；策略选择的Rollback带精确preimage自动回滚后重排。只有审批边界、不可证恢复状态、第三方字节冲突等真实安全边界才停给用户。

必须停止并报告：

~~~text
failed_step
error
recovery
asset_written
audit_recorded
~~~

真正停止原因包括：

- Generation Plan或源码摘要变化；
- Manifest或Generation State损坏；
- P-23、Diff或审计无法形成可信记录；
- CAS或并发写入冲突；
- 用户批准缺失；
- 正式资产写入状态不确定；
- 写后审计失败；
- 文件系统或运行环境故障。

候选内容错误不得伪装成stopped，也不得升级为用户交互停点。

统一原则：

~~~text
对用户停点放松；
对正式资产质量不放松；
内容错误自动修复并继续；
一致性、审批、写入状态或运行环境故障才停止。
~~~

## 9. Stage请求使用普通UTF-8文件

Windows PowerShell 5文本管道可能按本地代码页重新编码中文。

优先使用：

~~~text
--request-file <UTF-8 JSON文件>
~~~

支持：

- UTF-8无BOM或BOM；
- Windows绝对路径；
- 正斜杠或反斜杠；
- 空格和中文路径；
- 中文JSON内容。

拒绝：

- 目录；
- 设备或命名管道；
- UTF-16或非法UTF-8；
- 空文件；
- 超过协议上限的文件。

PowerShell 5写入UTF-8无BOM：

~~~powershell
$json = $requestObject | ConvertTo-Json -Depth 100
$utf8NoBom = New-Object System.Text.UTF8Encoding($false)

[System.IO.File]::WriteAllText(
    $requestPath,
    $json,
    $utf8NoBom
)
~~~

不得使用普通`>`保存中文JSON，也不得把中文JSON通过普通管道传给`--stdin-json`。

### 9.1 已对齐仓库的显式Header语义刷新

通常Header Stage只接受`header_required`计划，并省略请求中的`intent`。当正式
Header需要反映已经实现且已经完成Entry治理的新系统边界时，必须先让仓库回到
`aligned`，重新取得当前`plan_id`，再在Header Stage请求中显式填写：

~~~json
{
  "intent": "semantic_refresh"
}
~~~

该意图只开放Header语义刷新，不开放任意阶段写入。Stage把它固化到草稿Manifest；
Diff、P-23审阅、automation.mode批准、Apply、CAS和Baseline事务保持不变。任何源码、
索引、Header、Baseline或治理状态变化都会使Plan绑定过期。禁止手工编辑正式Header或
Manifest来绕过该入口。

## 10. PowerShell 5原生捕获JSON

Windows普通CLI的`--json`输出采用ASCII安全JSON，因此可以直接捕获：

~~~powershell
$raw = & 'C:\aoci\bin\aoci.exe' `
    --repo $repo `
    verify `
    --json

$exitCode = $LASTEXITCODE
$result = ($raw -join [Environment]::NewLine) | ConvertFrom-Json
~~~

Entries Stage捕获：

~~~powershell
$raw = & 'C:\aoci\bin\aoci.exe' `
    --repo $repo `
    index agent stage `
    --request-file $requestPath `
    --json

$exitCode = $LASTEXITCODE
$stage = ($raw -join [Environment]::NewLine) | ConvertFrom-Json

$stage.auto_finalize.status
$stage.auto_finalize.findings
$stage.auto_finalize.error
$stage.auto_finalize.recovery
$stage.auto_finalize.asset_written
$stage.next_command
~~~

判定：

~~~text
applied         → exitCode=0，执行next_command
repair_required → exitCode=0，只修findings并重新Stage
stopped         → 按failed_step、error和recovery停止报告
~~~

不得仅根据exitCode=0判断已经Apply，必须读取auto_finalize.status。

stdout和stderr必须分离；不要使用`2>&1`污染待解析JSON。

ProcessStartInfo只用于：

- 旧版AOCI；
- 需要字节级stdout/stderr分离；
- 需要超时、取消或隐藏窗口；
- 集成测试验证原始进程边界。

## 11. 请求大小与批次上限

~~~text
Entries Stage：
请求总大小 16 MiB（16777216字节）
最多200条候选

Header Stage：
请求总大小 4 MiB（4194304字节）
header字段 512 KiB（524288字节）

Curation Stage：
请求总大小 8 MiB（8388608字节）
最多200项决策
~~~

这些是协议硬上限，不表示模型必须处理到上限。
数值权威位于 `internal/machinecontract/numeric.go`；Guide 与 CLI Help 在运行时从该合同派生，本文由跨层等价测试锁定。

本文件解释Windows宿主接入，不是另一套状态机。当前Plan的命令顺序与停点只取当前Guide，机器字段与治理判据取真实Schema、公开Spec、Validator和确定性状态机；Prompt与MCP Description不能覆盖这些结果。完整分层见 `docs/zh-cn-contract-authority.md`。

## 12. Missing机器字段兼容

（本节字段分类适用于Legacy单体仓库；Volumes verify返回layout_mode/volumes/governance结构。）

Verify：

~~~text
result.missing = raw_missing
~~~

Score与Check：

~~~text
missing = raw_missing
pending_curation = pending_curation_missing
~~~

Plan与Guide：

~~~text
summary.missing = summary.raw_missing
summary.actionable_new = summary.actionable_missing
summary.curation_excluded = summary.curation_excluded_missing
summary.pending_curation = summary.pending_curation_missing
curation_targets = pending_curation_missing
curation_excluded = curation_excluded_missing
~~~

治理终态允许raw_missing大于0，前提是：

~~~text
actionable_missing = 0
pending_curation_missing = 0
orphan = 0
stale = 0
unbaselined = 0
~~~

剩余Raw Missing必须由CurationExcludedMissing或非Pending SkippedMissing解释。

## 13. Apply与安全重试

独立Apply报告可能返回：

~~~text
applied
rejected
applied_with_warnings
asset_written_audit_failed
~~~

Entries Stage auto_finalize返回：

~~~text
applied
repair_required
stopped
~~~

处理原则：

- asset_written=false：正式资产零写入，可按恢复动作重试；
- asset_written=true：不得盲目重复Apply；
- asset_written_audit_failed：先运行只读Verify、Check和Guide；
- 不得仅凭非零退出码判断未写入；
- 不得仅凭退出码0判断已经Apply；
- repair_required读取findings后自动重新Stage；
- Warning-only批次允许Auto Apply；
- F长度与措辞质量不是机器阻断项（仅Legacy v1；Volumes v1受FRAS v2硬限）。

## 14. LF与CRLF双指纹

默认换行宽容时：

- LF与CRLF差异进入LineEndingOnly；
- 不进入Stale；
- Verify和Check保持成功；
- Guide保持aligned且不生成Entries目标。

真实内容变化始终进入Stale。

团队设置`line_ending_tolerance=false`时恢复严格字节比较。

## 15. Codex Desktop升级验收

升级后至少确认：

- 新会话未继承凭据时调用aoci_rules和普通aoci_overview；
- valid凭据的后续开发通常复用认知，但普通显式Overview仍完整交付请求scope；
- uncertain优先调查源码或选择局部召回；
- Context Compaction由宿主或Agent显式声明，不能声称CLI自动探测；
- 语义变化达到团队阈值时先维护到aligned，再由Agent决定是否需要普通Overview；
- 主要阶段切换无语义变化时不主动建议重传Overview；
- 多个原因合并为一次checkpoint评估，相同refresh_event_id幂等；
- 首次Guide从`.codex/config.toml`解析绝对命令；
- Guide命令不依赖PATH；
- Guide包含applied、repair_required和stopped三态；
- repair_required不要求用户回复“继续”；
- repair_required退出码为0且正式资产零写入；
- Host-Agent只修findings中的失败项；
- 新Stage产生新Run；
- stopped只用于一致性、审批、写入状态或环境故障；
- JSON可以被PowerShell 5直接ConvertFrom-Json；
- MCP继续使用原始UTF-8 JSON-RPC；
- 最终Guide返回complete或准确停点。

## 16. Windows最低验收矩阵

### 16.1 完整认知

- 完整索引正文正常送达；
- 有效上下文内不重复overview；
- 上下文丢失、语义阈值或主要阶段切换按合同判定；
- 有漂移时Maintain、Verify、Check、Guide先于一次Overview；
- 29个语义文件不触发，默认第30个触发；
- 相同事件与无触发重复调用不传输完整正文；
- MEMORY不覆盖当前索引和源码。

### 16.2 纯模型语义

（Legacy Stage验收项；Volumes仓库以Guide+Maintain批次验收。）

- Header、标签、FRAS和Curation逐项由模型生成；
- 不用AST、路径、模板或脚本代写；
- F长度不产生机器阻断（仅Legacy v1；Volumes v1受FRAS v2硬限）；
- confidence=-1未替换时拒绝。

### 16.3 Entries Auto修复

- 全净候选返回applied；
- 文件名错误返回repair_required；
- 字典维度错误返回repair_required；
- 新生成不可解析标签返回repair_required；
- repair_required退出码为0；
- 正式索引零写入；
- 不形成Diff或Application；
- 旧Run与Check审计保留；
- 只修findings中的失败项；
- 其他候选保持原样；
- 同一完整批次重新Stage；
- 新Stage形成新Run；
- 不要求用户回复“继续”；
- 不调用Entries Check、Diff或Apply；
- 普通回复边界不终止完整生成；
- Plan变化、CAS冲突或写入状态不确定时停止。

### 16.4 JSON与UTF-8

- UTF-8无BOM及BOM请求可用；
- 中文路径和正文无损；
- UTF-16、非法UTF-8、目录和超限请求明确拒绝；
- Verify、Check、Plan、Guide和Entries Stage均可直接ConvertFrom-Json；
- stdout不与stderr混合。

### 16.5 最终治理

- Verify与Check基于最后源码状态；
- Guide返回aligned、complete或准确停点；
- Raw Missing三分守恒；
- 已排除或技术跳过文件不被强制生成Entry；
- 最终AOCI认知维护收敛。
