<!-- aoci:begin -->
## AOCI 仓库认知

AOCI 为本仓库维护一个稳定、可版本化、可增量更新的仓库级认知层，供模型跨任务复用对系统的理解。

`aoci.txt` 是面向模型的结构化认知索引。它以每个受管理文件、数据库表或其他受管理对象一条独立 Entry 的方式，用符号标签与 F/R/A/S 语义表达对象的核心职责、重要关系、对外契约，以及理解或修改系统时必须知道的非显然约束和设计决策。

Header、目录段和全部 Entry 共同组成完整仓库索引，可以覆盖前端、后端、配置、数据库结构及其他受管理内容。受管理内容发生变化时，通常只需维护受影响的认知条目，不需要重新生成整个索引。

AOCI 用于帮助模型先获得系统全貌、定位相关对象、识别跨模块关系和关键约束，再进入具体开发。它与源码阅读、测试、LSP、CodeGraph及其他结构化工具互补而不替代它们。当前源码、测试、配置和数据库结构仍是具体实现、接口、字段、标识符与运行行为的事实依据。

### 工作原理

AOCI 采用“模型生成、模型读取”的认知闭环。

生成或修订 Header 时，模型按当前Guide调查仓库事实，建立项目规则、标签字典和校准示例。生成或更新 Entry 时，模型先阅读当前 Header，再阅读目标源码、测试、配置、数据库结构及必要关联证据，为每个受管理对象独立生成认知条目。

Entry 的语义必须来自模型对真实证据的理解。不得仅依据路径、文件名、扩展名、AST、符号列表、依赖扫描、正则、固定模板或规则引擎推导、预填、拼接或改写索引语义。结构化工具可以辅助定位需要核实的事实，但不能代替模型完成语义理解和裁决。

对新的 Fresh Bootstrap，自动沿用现有持久 Onboarding 流程。Host 模型根据当前 Evidence 创作 Root、Meta、Tag 和 F/R/A/S，提供自己的 authoring-run 声明，并把该声明绑定到 Plan、Evidence 与完整 Candidate。不得要求 AOCI 填写 `origin=host_model`、制造 Receipt 或把程序生成的 Framework 当作语义。内部批次不是用户决策；只有遇到既有批准边界或真实的安全、漂移、CAS、Recovery 条件才停止。

使用索引时，模型读取完整索引，在一次认知建立中获得系统架构、对象职责、重要关系、对外契约和关键设计约束，再根据当前任务阅读相关源码、测试、配置或数据库结构，核实具体实现细节。

AOCI 索引是面向模型的高密度认知地图，不是源码副本。它可以提供大量仓库级架构、职责、关系和约束信息，但不保证保存全部逐字标识符、精确调用链和实现细节；这些事实仍应通过源码、测试、数据库结构、LSP或其他结构化工具核实。

### 最小使用入口

- `aoci_rules`：取得当前AOCI版本的会话运行合同。
- `aoci_overview`：建立或恢复本仓库的完整认知。
- `aoci_maintain`：在代码和验证状态稳定后检查认知是否需要维护。
- `aoci_update_entry`：提交与当前证据和源码摘要绑定的完整语义更新批次。
- `aoci_report`：证据不足、无法可靠生成语义时登记待办，不猜写。

其他MCP工具、CLI命令、参数和专项流程，以当前工具说明、Guide和 `--help` 返回内容为准，不在本文件中重复完整手册。

本区块只规定仓库接入、认知使用和收尾原则。`aoci_rules` 承载当前会话合同，Guide实时输出承载当前Plan的执行顺序与停点，工具Schema、Spec和Validator承载机器结构与判据；Prompt、Description、README和静态文档不能覆盖这些机器事实。

### 建立、生成和恢复认知

1. 每个新的 Agent Run 开始时，应先判断：

   - 本仓库是否已经存在可用的完整AOCI索引；
   - 当前上下文中是否已有与本仓库根、当前索引版本和当前AOCI服务相匹配，并且模型仍可可靠使用的完整仓库认知。

2. 仓库已经存在可用的完整索引，但当前Run没有可靠完整认知时，先调用 `aoci_rules`，再调用 `aoci_overview`。

	完整认知仍可靠时直接复用。只有局部不确定时，优先调查当前源码或按需使用局部Entry与搜索，不机械重读系统全貌。

	AOCI可以针对 `context_compaction`、项目 `cognition_refresh_threshold` 下的机器 `semantic_threshold` 或主要 `phase_transition` 提供checkpoint与认知状态事实。只需要这些紧凑事实时使用 `check_only=true`；这些事实只向Agent提供建议，不替模型决定是否需要系统全貌。

	Agent显式调用普通 `aoci_overview`（未设置 `check_only` 或为false）时，只要能形成一致的CognitionSet，AOCI必须完整交付请求scope。不得因为已有receipt、阈值未达到或没有待处理刷新原因而抑制正文。正式认知Dirty或Stale时仍交付正文，但必须标记不可靠，且不得覆盖当前源码、测试、配置或数据库证据。存在未决恢复或无法形成一致snapshot时失败关闭，不返回混合正文。

	普通Overview返回 `continuation_required=true` 时，必须原样提交 `next_cursor` 并自动继续到 `completed=true`。不得询问用户、开始业务任务或给出阶段性系统结论。Host截断、缺块、重复、乱序、cursor失败、Index变化或`chunk_tokens`变化时停止本次认知链。Attestation完成前不得用Memory、源码、Spec、`aoci.txt`、历史会话、scope、search或Entry读取修补或补充Whole-Index认知。Challenge ordinal是正式Entry序列中的1-based位置；Header内容、注释、空行、Section/Overview/Chunk Marker、Receipt与Metadata均不计数，Chunk Receipt ordinal使用同一序列。Attestation必须原样回绑本次Challenge发布的当前`index_sha256`、`entry_sequence_sha256`与`entry_count`；旧Index、旧Entry序列、旧数量或旧Attestation均无效。完整链结束后只正式提交一次既有模型认知Attestation；同一响应只允许一次不改变语义答案的JSON Schema或字段格式修正。对象、Tag或F不匹配即失败且认知吸收不确定，不得语义重试或旁路补答；这会禁止无保留地声称完整系统认知，但不禁止诚实回答、读取任务相关源码和测试或继续source-bound工程。首次认知失败时还不得执行Root/Meta、Migration、全局布局或其他未重新绑定源码的系统级决策。上下文压缩刷新若传输完整、认知身份不变、治理对齐且没有Recovery或第三方冲突，即使Attestation为partial或fail也消费该refresh generation，并继续原source-bound任务，不再自动重读Overview。`system_mastery_percent`只自评系统框架——架构、职责、强关系、稳定外部契约以及高熵安全和维护约束——不表示完整源码或运行实况知识；机器索引覆盖率必须分开。默认只向用户输出由本次真实覆盖率、Challenge、块数、Token和掌握度生成的规定成功或失败一句话。Host截断时提示用户把 `overview_delivery.chunk_tokens` 设置为更小的合法值后重新开始，不得自动修改。

   加法认知等级必须与严格证明字段分开解释。`delivery_verified`表示已加载Index且Host交付已确认，但完整认知验证仍未完成；应表达为“已加载且交付已验证”，不得描述为“没有认知”或“没有理解系统”。`cognition_verified`要求严格Challenge 10/10，`cognition_governed`还要求治理对齐。通用完整读取失败句只用于真实交付故障。

   当Overview响应包含可选`cognition-state/v2`投影时，必须分别解释各维度。其Level止于`model_cognition_usable`；`strict_attestation_verified`、`governance_aligned`与`current_system_cognition_reliable`都是独立状态，绝不参与该Level。ordinal、对象身份、Tag或核心F不匹配可以导致严格Attestation失败，而模型认知仍然可用；不得仅凭这种不匹配就宣称模型没有理解系统。只有`current_system_cognition_reliable=true`允许无保留地声称当前完整系统认知可靠。投影缺失时继续使用上述Legacy解释。

   普通的只读审计、分析、检查、不修改代码或不提交、不push，不自动等于严格零写入，也不改变上述认知有效性判断。Codex Memory和历史Skill只能辅助恢复经验、用户偏好与调查方向，不能替代与当前仓库根、索引摘要、AOCI服务身份和认知范围匹配的当前认知收据；项目AGENTS、当前AOCI身份、源码和运行事实优先。

   只有用户明确禁止Ledger、元数据、`.aoci`运行资产及任何文件写入时，才按严格零写入处理。若必要的认知建立与该边界冲突，必须报告冲突并请求用户裁决或建议使用隔离副本，不得静默以Memory替代当前仓库认知。

3. 仓库没有可用的完整索引，或当前只有最小骨架、Header不完整、Entries未完成、必要Curation尚未裁决时，不得把不完整内容当作完整仓库认知。

   先取得 `aoci_rules`，然后进入当前AOCI Guide。由Guide依据仓库真实状态决定下一阶段，完成必要的起点确认、Header、Entries、Curation及其他安全步骤，直到能够取得可用的完整Overview。

   不在本文件中自行重建或硬编码完整索引生成状态机。

4. 在长程任务中，模型负责保留当前认知收据并正确使用刷新门禁：

	- 只有局部文件、模块、关系或约束不确定时，阅读当前源码，或按需使用局部条目与搜索工具；
	- 模型已知系统全貌丢失时声明 `context_compaction`，AOCI不能自行推断宿主事件；
	- 进入真正的主要阶段时声明 `phase_transition`，不得把函数、测试运行或小步骤当作阶段；
	- 在有用的稳定检查点通过 `check_only=true` 取得机器语义计数；
	- 由Agent判断当前任务是否需要再次显式获取指定scope或完整Overview；
	- 维护和aligned尚未完成时，绝不把Dirty或Stale认知当作当前实现证据。

### 普通开发

5. 使用AOCI认知理解系统位置、对象职责、跨模块关系和关键约束，然后实际阅读当前任务涉及的源码、测试、配置、数据库结构和必要关联证据。

   不得用索引摘要替代具体实现证据。索引与当前事实不一致时，以源码、测试、配置或数据库结构核实具体实现，并在任务收尾时维护过期认知。

6. 按用户需求和本仓库既有架构、规范及验证方式完成正常开发。

   AOCI不规定架构、算法、调查顺序、实现方式、测试策略、分支策略或技术栈选择，也不应成为用户任务之外的开发流程中心。

   当仓库提供开发分层时，普通开发在Commit和Push前只运行单一Fast门；Full Confidence是后台或稳定版本冻结门，Release Rehearsal是非发布型稳定版本门。普通Agent不得在每次修改后等待全仓race、原生生命周期、数据库集成或发布打包。

7. 普通任务的用户可见沟通应聚焦需求理解、源码调查、实现、测试、风险、提交和工作树状态。

   内部认知建立、状态检查和认知维护默认不主动展开。用户主动询问，或出现真实阻断、冲突、审批、人工裁决、安全风险或无法继续的运行故障时，必须如实说明必要事实。

   长程auto任务中，状态询问、解释请求、架构问题、时间问题或其他非控制消息不会取消或暂停当前Plan、Run、候选、Commit或CI身份。简洁回答后从原 `next_action` 自动继续，不要求用户发送“继续”。只有用户明确要求停止、暂停、取消、回滚、修改范围，或禁止Commit/Push时才改变执行。这只是Host-Agent解释规则，不建立持久对话Task状态。

### 任务收尾与认知维护

8. 纯只读问答、分析、版本核验，或没有产生受AOCI管理对象变化的任务，不需要调用维护工具。

9. 发生受AOCI管理对象变化的开发任务，应在业务修改、格式化、测试、Lint及必要质量检查全部完成，并且受管理对象达到本次任务的最终稳定状态后，再调用一次 `aoci_maintain`。

   不要在每次中间修改后逐文件维护。

10. 若维护结果返回真实语义候选，模型必须完整阅读受影响对象及必要关联证据，再生成对应标签和F/R/A/S更新，并通过 `aoci_update_entry` 一次提交当前机器签发批次的完整候选集合，同时原样保留每项 `source_sha256`、`candidate_id` 与对应domain批次身份。`max_entries`只限制单次请求和原子事务，不限制logical plan、Whole-Index或Managed Scope。`remaining`非零时，在当前批次成功Apply后重新调用Maintain并从新preimage继续；绝不能为满足transport上限缩减Index覆盖或自行截取返回批次。

    没有把握时使用 `aoci_report`，不得为了消除待办而猜测、套用模板或生成缺乏证据的认知。

11. 必须遵守工具返回的结构化状态和安全边界：

    - `repair_required`：只修复明确命中的候选，再重新提交当前机器签发的完整批次；
    - `stopped`：结束当前写入尝试并检查 `failed_step`、错误、正式写入证据与Recovery。auto模式下，已证明零写入则记录closure并重新Plan；完整Intent和可证明postimage则Resume；策略要求Rollback且preimage可证明则精确恢复后重新Plan。只有证据不足、第三方正式字节冲突、需要审批或外部动作，或命中其他真实安全边界时，才停止整个用户任务；
    - 冲突、审批、人工裁决、权限和安全信号不得忽略；
    - 已经对齐后不得重复维护或重复写入；`refresh_ready_for_overview` 是checkpoint事实，由Agent决定是否为下一阶段请求普通完整Overview。

    维护完成后如果又修改了任何受管理对象，之前的维护结果失效，应在新的最终稳定状态重新完成收尾。

12. 用户只限制业务文件范围，但没有明确禁止仓库托管资产时，AOCI托管资产可以在收尾阶段为保持认知一致而更新，并应在审计和提交中与业务文件区分。

    用户明确禁止修改 `aoci.txt`、`.aoci`、元数据或任何额外文件时，以用户限制为准，不得写入，并如实报告剩余不一致。

### 专项流程

初始化、完整索引生成、Header生成、Entries生成、数据库结构索引、Curation、人工评审和故障恢复，只按当前AOCI Guide或工具在对应阶段返回的指令、命令和安全停点执行。

普通开发阶段不预加载、不猜测，也不自行重建这些专项流程。平台调用方式、请求格式、批次上限、审批规则、索引格式细节和恢复步骤由对应Guide、工具说明、模型Prompt和CLI帮助按需提供。

AOCI提供稳定的仓库级认知，但不替代源码事实、测试证据、数据库结构、结构化代码分析和模型正常的软件工程判断。
<!-- aoci:end -->
