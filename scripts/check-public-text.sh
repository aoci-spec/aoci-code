#!/usr/bin/env bash
# 公开文案禁区扫描 —— D3“渐进泄露”风险的机器防线。
# 索引条目: check-public-text.sh[Safe.Claims.8.IP.T]
#
# 机器词表、匹配模式与默认扫描集合由Go实现单点提供；本文件只保留既有Shell
# 入口，防止CI、Make和维护脚本各自复制一份安全判据。

cd "$(dirname "$0")/.."

GO_BIN="${GO_BIN:-${GO:-go}}"
exec "$GO_BIN" run ./internal/safetycmd "$@"
