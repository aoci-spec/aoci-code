<!--
CONTRIBUTING.md 的变更证据清单在此内联，便于评审在同一屏内核对。
公开合同、机器词表与恢复语义的改动请如实填写，不确定就写不确定。
-->

## What changes and why

<!-- 问题、意图边界；不要只写"修复 bug"。 -->

## Affected public contracts

- [ ] 无公开合同变化
- [ ] MCP 工具名、输入 Schema 或响应结构
- [ ] CLI 命令、旗标、退出码或 `--json` 形状
- [ ] `spec/public/` 合同文本或机器词表（`internal/machinecontract`）
- [ ] 索引格式、Baseline、收据或事务身份推导

兼容性影响：<!-- 旧数据、旧收据、旧宿主如何表现 -->

## Verification

<!-- 实际跑过的门禁与结果，不要写计划要跑的。 -->

- [ ] `make fast`
- [ ] `make full`
- [ ] `python3 scripts/blackbox/mcp_conformance.py`
- [ ] `python3 scripts/blackbox/mcp_scenarios.py`
- [ ] `python3 scripts/blackbox/mcp_lifecycle.py`
- [ ] 其他：

## Operating system impact

<!-- Linux / macOS / Windows；触及 internal/fs 的改动必须说明 -->

## Migration and recovery

<!-- 持久化数据变化时：旧状态如何读、如何恢复、是否需要真人批准 -->

## Cognition

- [ ] 受管对象有变化，已完成 AOCI 治理循环（maintain → update_entry → verify/check），且索引与代码在同一提交内
- [ ] 无受管对象变化
