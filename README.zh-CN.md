![AOCI-CODE 标志 — 面向人工智能的代码索引](assets/aoci-logo-zh-CN.jpg)

# AOCI-CODE

**AOCI-CODE 是一种为 AI Agent 建立软件项目全局认知的索引方法，可提供持久化、可治理的代码仓库认知地图。**

[🇺🇸 English](README.md) | 🇨🇳 简体中文

![Status](https://img.shields.io/badge/status-v0.1.0--rc3-orange)
![Runtime](https://img.shields.io/badge/runtime-local--first-blue)
![MCP](https://img.shields.io/badge/MCP-9%20tools-6f42c1)
![License](https://img.shields.io/badge/license-FSL--1.1--MIT-blue)

> [!IMPORTANT]
> AOCI-CODE v0.1.0-rc3 是当前发布候选版本。它是采用 FSL-1.1-MIT 的 Fair Source/source-available 软件；具体条款见 [LICENSE](LICENSE)。可以从 canonical source 构建，也可以使用 [v0.1.0-rc3 GitHub Release](https://github.com/aoci-spec/aoci-code/releases/tag/v0.1.0-rc3) 提供的签名包；使用前请遵循[发布验证流程](https://github.com/aoci-spec/aoci-code/blob/v0.1.0-rc3/docs/install.md#signed-github-release-packages)。

## AOCI 是什么？

**AOCI（AI-Oriented Cognition Infrastructure）是位于 AI Agent 与软件系统之间的认知基础设施。**

大模型负责推理，Agent 负责规划和执行，AOCI 则将代码、配置、测试和数据库结构整理为与当前系统版本绑定、可持续维护的系统认知，供 Agent 在行动前读取和理解。

## AOCI-CODE 是什么？

AOCI-CODE 源于 AOCI。简单来说，AOCI-CODE 会对源代码、数据库结构及其他真正影响 AI 理解和修改的关键信息进行压缩，形成符号与语义结合的高信息密度索引，并用纯文本表示。

在模型上下文有限的情况下，AI Agent 可以先读取这套索引，一次性获得项目的大部分关键信息，再进入具体开发任务，从而减少反复搜索和重新理解代码的成本，提高跨任务、跨会话的开发衔接效率。

- **索引不是一次性摘要**：索引会随系统持续演进，长期保存在项目中，可以通过 Git 比较差异、审查、版本化和回滚。
- **不只是记录“文件在哪里”**：AOCI-CODE 还会保存对象职责、强关系、公开契约、事务边界、兼容性约束，以及其他难以从代码结构直接推断的信息。
- **索引具有良好的可移植性**：索引随项目保存，不绑定特定模型、AI Agent 或单次会话。在索引与代码版本一致时，不同 AI Agent 和后续会话都可以读取并复用同一份系统认知，无需每次从头理解。
- **代码与数据库可以统一理解**：模型可以为数据库表建立独立的表级索引。Code Cognition 和 Database Cognition 一起交付时，AI Agent 能更完整地理解整个软件系统。


本文中的名称分工如下：**AOCI** 指认知范式和协议，**AOCI-CODE** 指承载该方法的项目与索引本身。


## 一键使用

把下面这段话发给你的 AI Agent，它会下载 AOCI-CODE、完成接入并为当前项目建立索引：

```text
AOCI-CODE 项目地址：https://github.com/aoci-spec/aoci-code

请从 https://github.com/aoci-spec/aoci-code/releases 下载适合当前操作系统和 CPU 架构的最新发布包，并按照发布页的安装说明完成校验。

解压后，请把 aoci（Windows 为 aoci.exe）放在稳定的绝对路径中，然后为我的项目完成初始化、MCP 接入和索引建立。

如果没有适合当前系统的发布包，或者我明确要求使用最新源码，请从官方仓库构建。
```
## 手动接入

请从 canonical source 获取 AOCI-CODE，或使用 GitHub Releases 提供的签名包。使用预构建二进制前，请按[安装指南](https://github.com/aoci-spec/aoci-code/blob/v0.1.0-rc3/docs/install.md#signed-github-release-packages)选择基础、推荐或完整校验层级，并准确说明已完成的层级。请把本 README 和已验证二进制的稳定绝对路径交给 Codex、Claude Code、Cursor、OpenCode 等受信任 AI Agent。AI Agent 可以按照项目内的说明运行初始化、完成 MCP 接入并建立第一份索引。

首次生成完整认知所需时间取决于仓库规模。正常接入过程包括：准备二进制文件、让 AI Agent 或用户初始化目标仓库、在宿主中提出“建立索引”、验证对齐。后续开发不需要在每个需求末尾重复提醒“维护索引”；项目规则和 AOCI MCP 工作流会在受管对象变化后引导 AI Agent 完成增量认知维护。

### 0. 环境要求

- 经验证的 Release 软件包，或 canonical AOCI-CODE 源码仓库的工作副本；
- `go.mod` 声明的 Go 工具链、`make` 及仓库要求的其他工具；
- 一个受支持的 MCP 宿主，例如 Codex、Claude Code、Cursor 或 OpenCode；
- 对目标仓库的正常读写权限。

签名包路线和可执行验证命令见[安装指南](https://github.com/aoci-spec/aoci-code/blob/v0.1.0-rc3/docs/install.md#signed-github-release-packages)；下面仍保留从源码构建的路线。

### 1. 当前 RC：使用经验证的软件包或从源码构建

签名 Release 二进制报告 `aoci version 0.1.0-rc3`。源码构建则报告由精确
Git checkout 派生的版本，例如 Release tag 之后的
`v0.1.0-rc3-1-g<short-commit>`；工作树不干净时还会带 `-dirty`，并单独报告
Git commit。二者表示不同构建输入，不是版本冲突。

使用 GitHub CLI 下载前先完成认证，再下载带 tag 的 Release 资产：

```bash
gh auth login
gh release download v0.1.0-rc3 --repo aoci-spec/aoci-code
```

如需匿名下载，请在浏览器中打开
[v0.1.0-rc3 Release 页面](https://github.com/aoci-spec/aoci-code/releases/tag/v0.1.0-rc3)，
并下载所需 archive 与验证资产。

克隆 canonical 仓库、构建二进制，并保持构建结果的路径稳定：

```bash
git clone https://github.com/aoci-spec/aoci-code.git
cd aoci-code
mkdir -p build
make build
./build/aoci --version
```

Windows 可以在 PowerShell 中从同一 canonical source 构建 `aoci.exe`：

```powershell
git clone https://github.com/aoci-spec/aoci-code.git
Set-Location .\aoci-code
New-Item -ItemType Directory -Force .\build | Out-Null
make build
.\build\aoci.exe --version
```

然后把本 README 提供给已经信任该项目的 AI Agent，并提出：

```text
请阅读这份 AOCI-CODE README，使用已构建 aoci 二进制的稳定绝对路径，
为当前项目完成 AOCI 初始化和 MCP 接入，然后建立索引。
```

AI Agent 应识别当前项目根目录、使用已构建二进制的稳定绝对路径、执行适合当前宿主的初始化，并在需要宿主重启或真实人工批准时明确提示用户。构建结果可以保留在 AOCI-CODE 源码工作区，也可以放到统一的稳定工具目录，只要 MCP 配置引用正确的绝对路径。

### 2. 手工初始化路线

如果希望自己先完成初始化，可以在目标仓库根目录运行，或通过 `--repo` 传入明确路径：

```bash
AOCI=/absolute/path/to/aoci-code/build/aoci
"$AOCI" --repo . init --locale zh-CN --agent codex
"$AOCI" --repo . scan
```

`init` 会写入 Locale 配置、托管的 `AGENTS.md` 规则区块、Git 边界以及语义为空的最小认知骨架；不同宿主的配置或提示方式并不相同，详见“宿主集成”。它不会根据文件名、目录或 AST 伪造业务语义。

对于全新仓库，首次 `scan` 会建立 Managed Baseline。对于已经拥有受治理 Baseline 的项目，新增、移除或改变受管范围应进入正式 Scope Change 流程，而不是把 `scan --force` 当作重新定义治理事实的捷径；`--force` 也不能洗掉未解决的漂移、Receipt 或 Recovery 边界。

<details>
<summary>Windows PowerShell</summary>

```powershell
$Aoci = (Resolve-Path "C:\path\to\aoci-code\build\aoci.exe").Path
& $Aoci --repo . init --locale zh-CN --agent codex
& $Aoci --repo . scan
```

请确认 `$Aoci` 指向稳定的绝对路径。

</details>

### 3. 让 AI Agent 建立第一次认知（重要）

在目标项目的 AI Agent 中输入：

```text
请为这个项目建立索引。
```

宿主应读取项目内的 AOCI Rules 和实时 Guide，调查源码、测试、配置及相关证据，然后为 `index` 角色的受管对象创作 FRAS 候选。普通用户无需手工编排 Plan、Stage、Check、Diff、CAS 或 Apply。

首次建立完成后，用户可以像平常一样直接提出开发需求，例如：

```text
给任务增加优先级，完成前后端、数据库与测试修改。
```

用户不必补充“最后维护 AOCI 索引”。项目规则和 MCP 会让 AI Agent 在开发完成、代码与测试稳定后检查认知变化并按正式流程维护受影响 Entry。当项目采用 `automation.mode=auto` 时，AOCI-CODE 只应在需要真实人工批准、必须执行外部操作、无法证明 Recovery、安全检查失败或发现第三方并发冲突时打断用户；其他自动化模式遵循各自的运行时合同。

### 4. 验证对齐

引导流程完成后运行：

```bash
"$AOCI" --repo . verify
"$AOCI" --repo . check
```

预期结果是正式认知与当前受管源码重新收敛到 `aligned`。如果未对齐，请先查看实时 Guide，不要在包装脚本中复制内部状态机：

```bash
"$AOCI" --repo . index agent guide --agent codex --json
```

### 5. 运行基础诊断

```bash
"$AOCI" --repo . capabilities
"$AOCI" --repo . doctor
```

如需一次性演练，可使用仓库中的 `examples/minimal-repository`。

<details>
<summary>开发者：从源码构建 AOCI-CODE CLI</summary>

如果你正在开发 AOCI-CODE 本身，普通开发和提交前先运行快速质量门禁：

```bash
make fast
```

需要完整信心验证和可执行文件时运行：

```bash
make full
./build/aoci --version
```

`make full` 是 Full Confidence 门禁，并已包含 `make build`；`make check` 只是兼容别名，同样进入完整门禁，不需要与 `make full` 机械重复。稳定版本排练使用 `make release-check`。如果只需直接构建，可以运行：

```bash
mkdir -p build
CGO_ENABLED=0 go build -o build/aoci ./cmd/aoci
```

AOCI-CODE CLI 是一个不依赖 CGO 的 Go 单二进制文件，Windows 输出为 `build/aoci.exe`。发布或交付前，请以 `./build/aoci --version`、`./build/aoci capabilities` 和正式 Release Manifest 的实际输出为准，不要只依赖 README 中的版本字符串。

</details>

## 初始化后会出现什么？

典型的 Volume-first 仓库包含以下正式认知资产：

```text
aoci.txt                    Root：声明当前认知集合及参与 Volume
aoci.meta.txt               Meta：标签字典、FRAS 规则与创作约束
aoci.code.txt               Code：代码与仓库资产的模型创作认知
aoci.database.txt           Database：可选的表级认知；默认不存在
.aoci/
├── config.json             团队策略、Locale、Scope 与预算
├── baseline.json           源码、认知和数据库绑定的治理基线
├── curation.json           可选的文件级 include/exclude 决策
└── ...                     草稿、Ledger、事务与恢复证据，通常不进入 Git
```

新项目初始化时会创建 Volume Root、Meta 和一个空的 Code Volume；Database 默认不存在。AOCI-CODE 不会自动生成仓库业务语义或 Database 语义。


## 一次完整开发任务如何运行

下面两个图描述用户实际经历的开发流程，不用于解释 AOCI 的内部实现原理。

### 新建项目：先建立一个简单系统，再接入 AOCI 持续演进

```mermaid
flowchart TD
    I["用户提出产品想法与需求"] --> S["利用 AI Agent 现有开发能力生成一个简单的新系统"]
    S --> Q["接入 AOCI MCP"]
    Q --> V["AI Agent 建立 Whole-Index 并验证 aligned"]
    V --> N["用户继续提出普通开发需求"]
    N --> M["AI Agent 完成代码、测试与增量认知维护"]
    M --> N
```

*新系统不要求从第一行代码开始就安装 AOCI-CODE。用户可以先用 AI Agent 现有开发能力完成产品雏形，建议约 1—3 万行（不是硬性阈值）；希望从项目早期就获得跨会话认知的团队，也可以更早接入。*

### 已有项目：对当前代码仓库建立索引后持续迭代

```mermaid
flowchart TD
    R["已有代码仓库、测试、配置和可选 Schema"] --> B["从 canonical source 构建 AOCI-CODE<br/>提出生成索引要求"]
    B --> E["AI Agent 调查现有系统并首次生成索引"]
    E --> V["Verify、Check 与 Guide 收敛到 aligned"]
    V --> T["用户提出普通开发任务"]
    T --> C["AI Agent 修改代码并运行质量检查"]
    C --> U["MCP 引导 AI Agent 维护索引"]
    U --> G["正式认知与当前系统重新 aligned"]
    G --> T
```

*两个流程在首次索引完成后进入同一种日常使用方式：用户只描述业务或工程需求，AI Agent 负责结合 Whole-Index 调查当前证据、完成开发和验证，并在收尾阶段通过 MCP 维护变化对象。用户不需要学习内部 Plan、Stage、Diff、CAS 或 Baseline 命令，也不需要在每个 Prompt 中重复维护要求。*

在任何正式写入开始之前，如果完整批次被拒绝，AOCI 索引保持不变。正式写入开始以后若流程中断，系统会保留不可变 Intent、写入证据和 Recovery 状态，并根据可证明的后像继续 Resume，或按精确前像回滚；遇到第三方字节冲突时失败关闭，不会用“继续写完”覆盖外部修改。

最终状态会明确返回 `applied`、`repair_required` 或 `stopped`。`stopped` 不是成功，也不等于一定没有写入；应检查 `failed_step`、正式写入证据和 Guide 给出的恢复动作。

## 模型与 AOCI-CODE 如何分工

### 模型负责语义

宿主模型读取源码、测试、配置、文档和必要证据，然后判断：

- 对象真正承担什么职责；
- 哪些文件、模块或数据库对象是安全修改时必须关注的强关系；
- 它暴露哪些 API、命令、格式或可观察合同；
- 哪些事务、权限、并发、缓存、部署、兼容或历史约束不能从普通结构直接推出。

AOCI-CODE 不会根据文件名、路径、扩展名、AST 或模板自动拼装 FRAS，也不会静默改写模型语义。

### AOCI-CODE 负责治理

AOCI-CODE 负责：

1. 建立 Safe Inventory、Managed Scope 与当前 Baseline；
2. 交付并确认当前 Whole-Index 身份；
3. 生成确定性的 Plan、目标集合和源码 SHA-256；
4. 校验候选结构、标签字典、关系身份、Scope、Ownership、预算和影响范围；
5. 保存 Check、Diff 与审阅内容绑定；
6. 使用跨进程锁、CAS 和 AtomicWrite 提交完整批次；
7. 推进 Baseline、追加 Ledger，并在写后故障时保留恢复证据；
8. 通过 Verify、Check 与 Guide 重新证明当前治理状态；
9. 从权威资产派生 Lineage、Relations、Impact、Snapshot 与 Evolution 观察，而不创建第二事实源。

机器全绿只表示已编码的结构与治理合同成立，**不表示每一条模型语义都绝对正确**。

## AOCI 索引是如何组织的？

从概念上看，AOCI 索引包含“索引规则”和“索引条目”两层含义。当前产品同时兼容两种物理布局，它们不能混为同一种文件格式：



### Volumes v1 布局

Volume-first 项目把职责拆开：

- `aoci.txt` 只作为 Root，声明参与当前 CognitionSet 的 Volume；
- `aoci.meta.txt` 保存 Meta、标签字典、FRAS 规则、预算和创作合同；
- `aoci.code.txt` 保存 Code 对象 Entry；
- `aoci.database.txt` 在启用 Database Cognition 时保存数据库对象 Entry。

Code 和 Database Volume 继续复用同一套 FRAS 词法结构，但它们拥有独立的对象身份、Evidence 绑定、所有权和生命周期。Root、Meta 与对象 Volume 共同构成 Whole-Index；不能只读取 `aoci.txt` Root 就声称已读取完整系统认知。

可以把它理解成一张地图：

- 索引规则相当于地图的**图例和坐标体系**；
- 索引条目相当于地图中描述每个地点的**具体标记**；
- Root 相当于当前地图集的**目录和版本入口**；
- Volume 相当于由同一治理协议管理、但证据来源和生命周期不同的**分册**。

### 索引规则定义什么？

| 规则 | 作用 |
| --- | --- |
| **标签字典** | 使用紧凑标签表示对象所在的架构层、功能领域、重要程度、技术特征和规模 |
| **FRAS 字段** | 规定每条索引应记录对象的职责、强关系、公开接口和关键约束 |
| **关系规则** | 规定对象之间的关系应如何引用，避免含糊或无法验证的描述 |
| **范围与所有权** | 规定哪些对象以 `index` 角色进入认知，以及每个对象应属于哪个 Cognition Volume |
| **长度与预算** | 控制索引的信息密度，避免把源码或普通摘要直接复制进索引 |
| **验证规则** | 规定候选条目进入正式索引前必须满足的结构和治理条件 |

在 Volumes v1 中，这些规则由项目的 Meta Volume 保存；在 Legacy 布局中，它们位于单体 Header。不同项目可以使用不同的标签字典，但 FRAS 的基本语义结构保持一致。

README 只介绍这些规则的阅读方法。具体、可执行的规则应以当前项目的 Meta 或 Legacy Header、AOCI Guide 和正式规范为准。

## 一条索引记录了什么？（FRAS）

建立规则后，模型会为每个 `index` 角色的受管对象编写一条索引。`observe` 角色只参与变化观察，`exclude` 角色形成明确负空间，两者都不拥有正式 Entry。AOCI 使用 **FRAS** 组织一条 Entry 中最重要的四类语义：

- **F — Function**：这个对象负责什么；
- **R — Relations**：安全理解或修改它时，还必须关注哪些对象；
- **A — API**：它向外提供哪些接口、命令、格式或可观察契约；
- **S — Non-obvious constraints**：补充 F、R、A 尚未表达、但对理解或安全修改这个对象很重要的信息，例如关键约束、例外、边界和特殊语义；S 不应重复前三个字段。

例如，在 `===.../internal/fs/===` 目录段内，Entry 使用 basename：

```text
atomic.go[CG9L]: F:提供持久化替换 CAS、创建 CAS、原子写入和禁止覆盖的恢复移动 | R:code:internal/fs/atomic_exchange_linux.go,code:internal/fs/atomic_exchange_windows.go,code:internal/fs/lock.go | A:AtomicWrite;AtomicWriteCAS;AtomicCreateCAS;AtomicMoveCAS | S:原生发布绝不会降级为覆盖式重命名；遇到竞态、不安全类型或无法验证的字节时，必须保留第三方状态并失败关闭
```

目录段和 basename 共同解析出 Code 对象身份；在需要跨 Volume 或系统投影时，可展开为规范身份，例如 `code:internal/fs/atomic.go`。Database 对象使用自己的规范身份，例如 `database://primary/public/orders`。

这条索引可以拆成两部分理解：

```text
atomic.go [CG9L]
└──对象──┘ └标签┘

F: 核心职责
R: 必须一起理解的强关系
A: 对外接口或契约
S: F、R、A 之外的重要补充信息
```

| 字段 | 回答的问题 | 示例内容 |
| --- | --- | --- |
| **F — Function** | 这个对象的核心职责是什么？ | 提供持久化替换 CAS、创建 CAS、原子写入和禁止覆盖的恢复移动 |
| **R — Relations** | 修改它时必须同时查看什么？ | `atomic_exchange_linux.go`、`atomic_exchange_windows.go`、`lock.go` |
| **A — API** | 外部可以依赖什么？ | `AtomicWrite`、`AtomicWriteCAS`、`AtomicCreateCAS`、`AtomicMoveCAS` |
| **S — Non-obvious constraints** | F、R、A 之外还有哪些重要信息必须补充？ | 原生发布不得降级为覆盖式重命名；失败时保留第三方状态并失败关闭 |

紧凑标签为对象提供架构层、功能域、重要度、可选技术特征和规模坐标。标签字典由项目自己的 Meta Volume 或 Legacy Header 定义；程序验证字典和结构，但不决定业务含义。

**S 不是 Synopsis、普通摘要，也不是 F、R、A 的重复表达。** 它用于补充前三个字段没有覆盖、但会影响理解或工程正确性的重要信息，例如：

- Redis 访问失败后必须回退数据库；
- 某张表不能进入 AutoMigrate；
- 某个旧版兼容分支不能按常规逻辑清理；
- 写入失败时必须保留原始文件。

模型负责根据真实源码和证据编写这些语义；AOCI-CODE 负责验证条目结构、绑定源文件，并治理它如何进入正式索引。

## Cognition Volumes

AOCI-CODE 维护一个逻辑 Whole-Index，但不同类型的认知拥有独立的索引、所有权和生命周期。

| Volume | 职责 |
| --- | --- |
| **Root** | 声明当前 CognitionSet 的组成、依赖和激活入口；最后发布，避免部分资产被误认为完整集合 |
| **Meta** | 保存标签字典、FRAS 规则、配额和模型创作合同 |
| **Code** | 保存代码、测试、配置、文档和运维资产的仓库认知对象 |
| **Database** | 保存可选的表级认知对象，并与已接受的 Schema Evidence 绑定 |

每个对象只有一个合法 Owner。跨 Volume 放错对象会产生 Ownership Conflict；AOCI-CODE 仅在机器能证明错误 Owner、正确 Owner 和当前对象事实时修复，不会根据名称相似度猜测归属。

Code Volume、Database Volume 和 Scope 可以共同演进，但它们共享同一个受治理提交边界。单个 Domain 的 Apply 不应丢失其他 Domain 已有的 Baseline 投影；Volume Apply、Baseline 更新和相关 Scope 投影必须作为同一 Cognition Transaction 的一致结果发布，或者进入可证明的 Recovery，而不是留下“文件已成功但其他 Volume 无基线”的半完成状态。

## 宿主集成

`aoci init` 始终写入托管的 AI Agent 规则，但宿主接入行为不同：Codex 写入项目级 MCP 配置且不安装 Hook；Claude Code 可以安装 `PreToolUse` Hook；OpenCode V1 使用严格的项目级 `opencode.json`；Cursor 只返回参考配置片段，不写入项目配置。配置完成后，先检查当前宿主会话是否已显示 AOCI 工具；仅在尚未加载新 server 时刷新或重新打开项目会话。新会话通常先读取一次 Rules 与 Whole-Index；只要认知身份仍有效，后续任务会复用当前认知，不会机械地重复注入整个索引。

| 宿主 | 当前接入方式 | 边界 |
| --- | --- | --- |
| **Codex** | 项目级 stdio MCP 配置 | 不依赖文件编辑 Hook |
| **Claude Code** | 项目级 MCP；可选 `PreToolUse` 薄守卫 | Hook 只负责写前提示或 Stale 守卫，不是 AI Agent runtime |
| **OpenCode V1** | 通过 `--agent opencode` 写入严格的项目根 `opencode.json` | 工具已加载可直接继续；否则刷新或重新打开项目会话 |
| **Cursor** | 返回 MCP 参考配置片段 | 不写入项目配置，仍需按宿主手工完成接入 |
| **其他 MCP Host** | 连接标准 stdio Server | 需要手工配置并完成宿主专项验证 |

```bash
aoci --repo /absolute/path/to/repository init --agent codex
aoci --repo /absolute/path/to/repository init --agent claude --hooks
aoci --repo /absolute/path/to/repository init --agent opencode
aoci --repo /absolute/path/to/repository init --agent cursor
```

旧版输出保留 Level 0—4，用于兼容既有宿主和报告。当前 `cognition-state/v2` 将“模型认知可用度”表达为 Level 0—3，并把严格证明和治理事实拆成独立维度：

| 状态 | 含义 |
| --- | --- |
| `delivery_verified` | 当前 Index 已装载，Host 交付得到确认；严格 Challenge 可能仍未完成 |
| `model_cognition_usable` | 模型已获得可用于任务的系统框架认知 |
| `strict_attestation_verified` | 当前 Index 身份、Entry 序列、数量与 Challenge 严格通过 |
| `governance_aligned` | 正式认知、Baseline 和治理状态当前对齐 |
| `current_system_cognition_reliable` | 当前完整系统认知可以无保留地作为现行系统级先验使用 |

这些维度不能互相替代。Attestation 只证明当前材料的交付覆盖度与身份一致性，不代表 AI Agent 对任意未来任务都已充分理解；只有 `current_system_cognition_reliable=true` 才允许无保留地声称掌握当前完整系统认知。

## 常用 CLI 命令

| 命令 | 用途 |
| --- | --- |
| `aoci init` | 安装仓库合同和不含业务语义的初始 Volumes 布局 |
| `aoci scan` | 为首次接入建立 Baseline；已有 Managed Baseline 的范围变化进入 Scope Change |
| `aoci status --deep` | 仅用于 Legacy 深度状态，不是 Cognition Volumes 维护路线 |
| `aoci verify` | 报告 Missing、Orphan、Stale 和 Unbaselined 事实 |
| `aoci check` | 运行聚合治理门禁 |
| `aoci index agent guide` | 进入确定性的宿主智能体工作流 |
| `aoci capabilities` | 查看当前二进制提供的能力 |
| `aoci doctor` | 诊断仓库和宿主集成 |
| `aoci database` | 显式配置并验证 PostgreSQL/MySQL Schema Evidence |
| `aoci database source access` | 只读检查数据库凭据引用是否已由外部环境提供，不返回凭据值 |
| `aoci database cognition bootstrap` | 为已对齐的 Code-only Volumes 项目添加 Database Cognition |
| `aoci cognition plan` | 只读预览 Bootstrap 或 Legacy-to-Volumes 迁移计划 |
| `aoci cognition bootstrap` | 仅治理 Root 缺失或精确零 Entry 的兼容骨架；普通 fresh init 已创建 Code-only Volumes，成熟 Legacy 应使用 Migration |
| `aoci cognition migration` | 治理 Legacy 迁移的快照、映射、批准、应用、恢复或回滚 |
| `aoci cognition system lineage` | 派生重要认知对象的来源与绑定链 |
| `aoci cognition system relations` | 派生当前正式 R 关系投影 |
| `aoci cognition system impact` | 沿显式正式 R 关系查询数据库变化可能触达的代码认知对象 |
| `aoci cognition system snapshot` | 输出当前认知集合的只读快照投影 |
| `aoci cognition system evolution` | 比较调用方提供的历史 Snapshot 与当前投影 |
| `aoci mcp` | 启动 stdio MCP Server |

常用组合：

```bash
# 初始化与首次 Baseline
aoci --repo . init --locale zh-CN --agent codex
aoci --repo . scan

# Cognition Volumes 的验证与治理门禁
aoci --repo . verify --json
aoci --repo . check --json

# 实时 Guide；不要在包装器中复制状态机
aoci --repo . index agent guide --agent codex --json

# 能力与诊断
aoci --repo . capabilities
aoci --repo . doctor

# 数据库 Evidence、Access 与 Cognition 生命周期
aoci --repo . database --help
aoci --repo . database source access --source primary --json
aoci --repo . database cognition status
aoci --repo . cognition plan --help

# System Cognition 派生观察
aoci --repo . cognition system lineage
aoci --repo . cognition system relations

# MCP stdio Server
aoci --repo . mcp
```

Plan、Stage、Check、Diff、Apply、Curation、Scope Change、Bootstrap、Migration 和恢复命令仍然存在。普通用户应遵循运行中二进制返回的 Guide，而不是把内部状态机复制到脚本中。

### 关于“只读”命令

verify、check、index score 与 index inventory 的“只读”仅表示不修改正式索引或Baseline，不等同于严格零文件写入：Ledger启用时四者都可能追加本地Ledger，verify还会尝试写入Verify History；审计写入失败不改变既有退出码与治理判据。

如果必须保证零文件写入，请使用隔离副本，或显式禁用所有运行时审计写入。System Cognition 命令不会创建第二套正式状态，但其普通 CLI 调用仍应遵循当前版本对 Ledger 和本地历史记录的运行时合同。

## MCP Server

`aoci mcp` 无需常驻 Daemon，通过 stdio 暴露固定九个工具：

| 类别 | 工具 |
| --- | --- |
| **认知读取** | `aoci_rules`、`aoci_overview`、`aoci_get_entries`、`aoci_search` |
| **认知维护** | `aoci_maintain`、`aoci_update_entry`、`aoci_remove_entry` |
| **辅助证据** | `aoci_header`、`aoci_report` |

MCP 模式下，stdout 仅用于 JSON-RPC，日志和诊断写入 stderr。运行中二进制提供的 Tool Description、JSON Schema 和机器状态值才是权威；README 只是使用入口。

当前 AOCI-CODE Release 的 System Cognition 能力通过现有 CLI 和治理内核提供，不增加第十个 MCP 工具，也不改变已有九工具的名称、用途或 stdio 合同。

## 长时间运行与 Whole-Index 交付

在一次新对话开始时，AI Agent 通常会加载一次完整 Overview，以建立与当前仓库、Index 版本和 AOCI 服务身份匹配的系统认知。只要模型仍能可靠使用这份认知，就不需要在每个任务或每次工具调用前机械重复加载。

在同一对话过程中，模型会根据自身认知状态按需决定是否再次加载。当对话很长、发生上下文压缩、系统经历较大演进、进入重要阶段，或者模型已经无法从现有上下文建立可靠的完整系统认知时，模型应自主判断是读取局部证据、进行紧凑检查，还是重新加载完整 Overview。AOCI 可以提供刷新阈值、Checkpoint 和身份事实，但是否需要恢复系统级认知由模型结合当前任务判断。

每次完整 Overview 的正文都由起始标记、当前正式索引的精确内容和结束标记组成。

当 Overview 超过项目 Chunk 预算时：

1. 交付立即从 Chunk 1 开始；
2. AI Agent 必须沿 `next_cursor` 自动读取到最后一个 Chunk；
3. AI Agent 使用已交付的 Whole-Index 提交一次 Attestation；
4. 不得以局部搜索、旧记忆、源码补读或直接文件读取冒充完整交付；
5. Pending Recovery 或不一致快照会失败关闭，不会混合内容。

`overview_delivery.chunk_tokens` 是唯一的交付大小设置，默认值为 `8000`，有效范围为 `4000` 到 `24000`。`check_only=true` 是不含 Chunk 链的紧凑检查点。

认知刷新阈值默认为 30 条不同的语义路径，也可以按项目设置；具体计数规则以 Cognition Refresh 文档和运行时合同为准。

## Database Cognition

AOCI-CODE 可以将数据库结构纳入同一套认知和演进治理，但访问边界是显式且收窄的：

```text
显式 database 命令
  → 只读 PostgreSQL/MySQL 系统目录
  → Canonical Schema Evidence
  → 人工接受 Evidence Hash
  → 宿主模型根据完整证据创作表级 FRAS
  → Cognition 与 Evidence 绑定后进入受治理 Apply
```

- 未配置数据库或未执行显式数据库命令时，不进行数据库网络访问；
- 只读取基础表（base tables）的 Schema 元数据，不读取业务行；视图、例程和注释不在当前采集范围内；
- 不执行 DDL 或 DML；
- 不将主机、用户名、DSN 或凭据值写入认知资产；
- Database Cognition Apply 离线运行，写入时不重新连接数据库；
- 程序保存并比较结构 Evidence，但表的职责、关系和高价值约束仍由模型创作。

Database Volume 默认可以不存在。只有项目明确启用数据库认知并完成 Evidence、Binding 和生命周期治理后，它才进入 Whole-Index。

### Database Access Onboarding：用户不需要理解 DSN 细节

普通用户只需要声明数据库 Source 的非敏感身份。省略 `--credential-env` 时，AOCI 会从 Source ID 派生稳定的环境变量引用，例如 `primary` 对应 `AOCI_DB_PRIMARY_DSN`：

```bash
aoci --repo . database source add \
  --source-id primary \
  --engine postgresql \
  --database-name app \
  --namespace public
```

然后使用只读 Access Preflight 查看“引用是否已由外部环境提供”：

```bash
aoci --repo . database source access --source primary --json
```

该命令不连接数据库，不返回凭据值，也不会要求普通用户在对话中粘贴 DSN。数据库或基础设施管理员应在 AOCI 进程外部提供对应环境变量，并授予只读、最小权限的系统目录访问能力。Source 配置只保存凭据**引用名称**，不保存 Secret。

当前发布候选版本只提供 Environment Credential Provider。Vault、Kubernetes Secret、AWS/GCP/Azure 等 Cloud Secret Manager 可以在后续版本通过同一 Provider 边界接入，但它们不是当前能力，也不应在 README 中暗示已经可用。

这种分工避免让普通用户承担账号格式、DSN 编码和 Secret 生命周期管理，同时保留显式授权：AOCI 不自动发现凭据、不扫描 `.env`、不读取 Secret 文件，也不绕过数据库管理员的权限边界。

## System Cognition Foundation

AOCI-CODE 在 Code Cognition 与 Database Cognition 之上提供一组**派生的系统认知观察**。它们用于回答“对象从何而来”“数据库变化可能影响哪些代码认知对象”“认知从历史观察演进到现在发生了什么”，但不建立新的权威事实层。

```bash
# 认知对象的来源、Evidence 与 Receipt 绑定
aoci --repo . cognition system lineage

# 当前模型创作并进入正式 Volume 的 R 关系
aoci --repo . cognition system relations

# 从一个数据库对象出发，沿正式 R 关系查找代码影响
aoci --repo . cognition system impact \
  --object database://primary/public/orders

# 由调用方保存一个历史观察
aoci --repo . cognition system snapshot --json > previous.json

# 将调用方提供的历史观察与当前派生投影比较
aoci --repo . cognition system evolution \
  --snapshot-file previous.json
```

### 权威边界

| 能力 | 数据来源 | 是否持久化新事实 | 关键边界 |
| --- | --- | --- | --- |
| **Lineage** | Cognition、Evidence、Receipt、Baseline 绑定 | 否 | 解释来源链，不成为独立 Provenance 数据库 |
| **Relations** | 正式 Entry 中模型创作的规范 R | 否 | 只投影已被治理接受的关系 |
| **Impact** | 当前正式 R 关系及其可解析对象身份 | 否 | 不根据 SQL、import、路径或名称自动推断业务语义 |
| **Snapshot** | 当前权威资产的确定性观察 | 否 | 输出由调用方保存，不是 Baseline 或 Recovery 资产 |
| **Evolution** | 调用方提供的旧 Snapshot 与当前投影 | 否 | 比较观察，不推进独立生命周期 |

所有投影都应明确标记 `authoritative=false`。Cognition Volumes、Schema Evidence、Receipt、Baseline 和 Lineage Binding 仍是权威来源。未解析的 R 关系会形成不完整结果或诊断，而不是由程序猜测补全。

**AOCI-CODE 不是知识图谱系统。** Narrow System Relation Projection 可以被看作便于查询的关系视图，但它不拥有第二套状态管理、独立写入路径或新的事实存储。删除派生输出后，可以从当前权威资产重新计算；任何投影与权威资产冲突时，以后者为准。

## 执行模式、隐私与数据边界

| 模式 | 行为 |
| --- | --- |
| **Agent-native** | 当前宿主模型读取证据并创作语义；AOCI-CODE 不需要第二个模型 API |
| **Endpoint-native** | 可选的用户配置 OpenAI-compatible endpoint 起草候选；密钥保留在环境变量中 |
| **Deterministic-only** | 禁用 AI，保留扫描、Baseline、校验、查询、Scope、治理、CI 和恢复能力；新语义仍需模型或人类创作 |

- AOCI-CODE 本身默认本地优先，没有默认云端点、托管源码上传服务或必须运行的后台 Server。
- Agent-native 模式的数据处理取决于用户选择的宿主及其产品政策，而不是由 AOCI-CODE 代为上传。
- 数据库网络访问必须显式触发，并限制为只读 Schema 元数据。
- Ledger、草稿、事务和恢复证据默认保存在 `.aoci/`，通常被 Git 忽略；正式认知与团队治理资产可以进入 Git。
- System Cognition Projection 在本地从现有权威资产计算，不要求图数据库、向量数据库或远程图服务。

## 为什么不只是 Repo Map 或 RAG？

普通搜索、AST、LSP、代码图和 RAG 擅长回答结构或检索问题。AOCI-CODE 的独特作用，是维护一份覆盖受管范围、可版本化并随软件增量演进的认知资产。

| 能力 | 开发者实际得到的内容 |
| --- | --- |
| **Whole-Index Cognition** | 覆盖当前受管范围的统一系统地图，而不是每次任务临时拼接的片段 |
| **FRAS Semantic Objects** | 每个 `index` 角色对象的职责、强关系、公开合同和高价值约束 |
| **Cross-session Persistence** | 认知资产随仓库保存，后续会话和其他 AI Agent 可复用同一版本 |
| **Managed Scope** | 明确哪些对象进入正式认知、哪些只观察变化、哪些形成正式负空间 |
| **Drift Detection** | 区分 Missing、Orphan、Stale、Unbaselined、换行变化和策展差异 |
| **Governed Updates** | 候选经源码绑定、Plan、校验、Review、CAS、原子写入、Baseline 与恢复流程进入正式资产 |
| **Delivery Attestation** | 通过 Chunk、Cursor、Receipt 与 Challenge 证明 Whole-Index 确已完整交付 |
| **Database Cognition** | 根据显式接受的 PostgreSQL/MySQL Schema Evidence 形成并治理表级认知 |
| **System Intelligence Projection** | 从正式 R 关系和治理证据派生 Lineage、Impact 与 Evolution 观察，不复制权威状态 |

## 与其他代码理解方法的关系

AOCI-CODE 与现有工具互补，不替代它们。

| 方法 | 最擅长回答 | AOCI-CODE 的不同职责 |
| --- | --- | --- |
| **RAG / 搜索** | 与当前问题相关的原文片段在哪里 | 在查询前维护覆盖受管范围的版本化认知，并保存负空间与治理状态 |
| **AST / LSP / ctags** | 符号、类型、定义和引用在哪里 | 保存跨源码、测试、配置、数据库和运维资产的职责、意图与维护约束 |
| **CodeGraph / 调用图** | 什么与什么相连、路径如何遍历 | 维护模型创作的业务语义、长期约束、Scope、Ownership、版本身份和事务化更新 |
| **普通 Repo Map / 摘要** | 快速获得项目形态或一次性概览 | 对受管对象执行源码绑定、漂移检测、增量维护、Review、恢复和审计 |
| **AI Agent** | 调查源码、制定方案、修改代码和运行工具 | 位于 AI Agent 下方，提供持久认知并治理认知如何随软件演进 |

推荐组合是：AOCI-CODE 提供全局语义先验，代码图、LSP、搜索和源码提供精确证据，测试和运行结果负责最终验收。AOCI 的 Relations 投影不会取代精确调用图；它只表达模型在正式 FRAS 中明确创作并经治理接受的强关系。



## 技术栈

| 部分 | 实现 |
| --- | --- |
| **核心语言** | Go；版本以当前 `go.mod` 为准 |
| **分发形式** | 单个 CGO-free 可执行文件 |
| **AI Agent 协议** | stdio MCP，固定九工具；CLI 与 MCP 共用治理内核 |
| **正式认知** | UTF-8 明文 Cognition Volumes，可由 Git diff 和版本管理 |
| **机器状态** | JSON/JSONL、SHA-256、Baseline、Manifest、Receipt、Ledger 与 Recovery |
| **写入安全** | 跨进程锁、CAS、同目录临时文件、平台原子替换和失败关闭 |
| **数据库** | 核心运行不依赖业务数据库；可选 PostgreSQL/MySQL Schema Evidence 使用纯 Go 驱动 |
| **系统认知** | 从权威资产即时派生的 Lineage、Relations、Impact、Snapshot 与 Evolution 投影 |
| **运行方式** | Agent-native、Endpoint-native、Deterministic-only |

AOCI-CODE 不要求 Neo4j、向量数据库、长期 Daemon 或 AOCI 云服务。启用 Database Cognition 时，目标数据库只是显式、只读的 Schema Evidence 来源，不是 AOCI 自身的状态存储。

## 常见问题

### 为什么 Legacy `status --deep` 仍显示漂移？

`status --deep`、`index score` 和 `index agent plan` 仅用于 Legacy。对于
Cognition Volumes 仓库，应先运行实时 Guide，让宿主调用普通无参数
`aoci_maintain`，通过 `aoci_update_entry` 提交完整当前批次，并以 Verify、
Check、Guide 收口。不要直接修改 Baseline，也不要跳过源码绑定或恢复步骤。

```bash
aoci --repo . index agent guide --agent codex --json
```

### 为什么宿主启动了错误的 AOCI？

检查项目级 MCP 配置中的可执行文件路径，确保它是稳定的绝对路径，再运行：

```bash
/absolute/path/to/aoci --version
/absolute/path/to/aoci --repo . doctor
```

如果需要确认已经加载的 stdio Server，而不只是磁盘上的文件，还应检查宿主进程的实际 executable、命令行 `--repo` 和重新启动后的进程身份。

### 普通用户必须自己创建数据库账号和填写 DSN 吗？

普通用户不需要理解 DSN 语法，也不应在聊天中传递 Secret。用户声明 Source 的非敏感身份后，AOCI 给出稳定的环境变量引用；数据库管理员仍需在外部创建最小权限、只读系统目录账号并向运行环境提供该引用。当前版本不会代替组织创建数据库账号，也不会自动读取 Secret Store。

### 可以在 CI 中运行吗？

可以在不创作新语义的情况下运行扫描、验证、治理门禁和确定性检查。具体命令、审计写入和退出码以当前二进制的 `--help`、JSON Schema 及项目 CI 文档为准。

### AOCI 的绿色结果是否证明语义一定正确？

不能。绿色结果证明已编码的结构和治理条件成立；模型语义仍需通过源码、Schema、测试、运行结果和人工审查验证。

### System Cognition Graph 是新的事实源吗？

不是。当前能力是 Narrow Relation Projection，不是独立图平台。它只从正式 Cognition、Evidence、Receipt 和 Baseline 计算结果，不持久化新的权威事实；程序也不会通过 import、SQL、文件名或相似度自动生成语义关系。


## 文档导航

| 主题 | 文档 |
| --- | --- |
| 第一次使用 | [Getting Started](https://github.com/aoci-spec/aoci-code/blob/v0.1.0-rc3/docs/getting-started.md) |
| 安装、升级与回滚 | [Install](https://github.com/aoci-spec/aoci-code/blob/v0.1.0-rc3/docs/install.md) · [Upgrade](https://github.com/aoci-spec/aoci-code/blob/v0.1.0-rc3/docs/upgrading.md) · [Rollback](https://github.com/aoci-spec/aoci-code/blob/v0.1.0-rc3/docs/rollback.md) · [Uninstall](https://github.com/aoci-spec/aoci-code/blob/v0.1.0-rc3/docs/uninstall.md) |
| AI Agent 与宿主 | [Agent Integrations](https://github.com/aoci-spec/aoci-code/blob/v0.1.0-rc3/docs/agent-integrations.md) · [Windows Host Agent](https://github.com/aoci-spec/aoci-code/blob/v0.1.0-rc3/docs/windows-host-agent.md) |
| Whole-Index 与刷新 | [Overview Delivery](https://github.com/aoci-spec/aoci-code/blob/v0.1.0-rc3/docs/overview-delivery.md) · [Cognition Refresh](https://github.com/aoci-spec/aoci-code/blob/v0.1.0-rc3/docs/cognition-refresh.md) |
| Cognition Volumes | [Volumes](https://github.com/aoci-spec/aoci-code/blob/v0.1.0-rc3/docs/cognition-volumes.md) · [Volumes Contract](https://github.com/aoci-spec/aoci-code/blob/v0.1.0-rc3/spec/public/aoci-cognition-volumes-v1.txt) |
| System Cognition | [System Cognition Runtime Contract](https://github.com/aoci-spec/aoci-code/blob/v0.1.0-rc3/spec/public/aoci-system-cognition-runtime-v1.txt) |
| Managed Scope | [Managed Scope and Budget](https://github.com/aoci-spec/aoci-code/blob/v0.1.0-rc3/docs/managed-scope-and-budget.md) · [Safe Inventory](https://github.com/aoci-spec/aoci-code/blob/v0.1.0-rc3/docs/safe-inventory-and-scope-refresh.md) |
| Database | [Database Evidence](https://github.com/aoci-spec/aoci-code/blob/v0.1.0-rc3/docs/database-evidence.md) · [Database Cognition Authoring](https://github.com/aoci-spec/aoci-code/blob/v0.1.0-rc3/docs/database-cognition-authoring.md) |
| 生命周期 | [Getting Started](https://github.com/aoci-spec/aoci-code/blob/v0.1.0-rc3/docs/getting-started.md) · [Upgrade](https://github.com/aoci-spec/aoci-code/blob/v0.1.0-rc3/docs/upgrading.md) · [Cognition Refresh](https://github.com/aoci-spec/aoci-code/blob/v0.1.0-rc3/docs/cognition-refresh.md) |
| 格式与协议 | [Index Format](https://github.com/aoci-spec/aoci-code/blob/v0.1.0-rc3/spec/public/aoci-index-format-v1.txt) · [Cognition Volumes Spec](https://github.com/aoci-spec/aoci-code/blob/v0.1.0-rc3/spec/public/aoci-cognition-volumes-v1.txt) · [Object FRAS v2](https://github.com/aoci-spec/aoci-code/blob/v0.1.0-rc3/spec/public/aoci-object-fras-v2.txt) |
| 研究与发布 | [Supply Chain](https://github.com/aoci-spec/aoci-code/blob/v0.1.0-rc3/docs/supply-chain.md) |

> 上述文档和公开合同链接固定到 `v0.1.0-rc3`，因此即使从不包含 `docs/` 和
> `spec/public/` 的 Release archive 中阅读本 README，链接仍然有效。

## 研究、知识产权与许可证

AOCI-CODE 的研究论文和 Artifact 可以公开描述已发布的方法、实验协议与结果。专利申请、授权范围、法律状态、权利人和地域效力则属于法律事实，不应仅依据内部 README、历史版本号或未经核验的转述写入产品介绍。

如果未来需要公开具体专利号、授权日期或权利范围，应由权利人和法律顾问根据官方公开记录核验，并放入专门的 IP、NOTICE 或法律文件；README 只链接该权威文件，不自行扩大专利覆盖范围，也不把专利状态解释为软件许可。

<details>
<summary>贡献、安全与许可证</summary>

当前项目尚未开放一般意义上的外部贡献。提交变更前请阅读 [CONTRIBUTING.md](https://github.com/aoci-spec/aoci-code/blob/v0.1.0-rc3/CONTRIBUTING.md)；权利人批准公开贡献政策之前，inbound licensing 边界可能限制 PR 的接收和使用。

请勿在公开 Issue 中披露疑似漏洞。请遵循 [SECURITY.md](https://github.com/aoci-spec/aoci-code/blob/v0.1.0-rc3/SECURITY.md)；受监控的私密报告渠道和明确响应责任仍是公开 Release 的必要条件。

AOCI-CODE v0.1.0-rc3 是采用 FSL-1.1-MIT 的 Fair Source/source-available 软件。适用条款见 [LICENSE](LICENSE)。

</details>

---

**AOCI-CODE 的目标不是让 AI Agent 看见更多代码，而是让它在每次行动前拥有一份当前、结构化、可追溯且受治理的软件系统认知。Git 管理代码版本，Database Migration 管理数据结构演进，AOCI-CODE 管理 AI Agent 可消费的系统认知，以及这种认知演进的可追溯性、治理一致性与恢复边界。**
