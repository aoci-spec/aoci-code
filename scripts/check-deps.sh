#!/usr/bin/env bash
# 依赖方向硬校验(R17 / D23)—— 保证“无AI配置零依赖离线可用”
# 是架构级事实，而不是开发约定。
#
# 铁律:
#   确定性核心层绝不import AI编排层。
#   任一核心包的完整依赖闭包出现AI包，均视为架构级违规。
#
# 实现:
#   仅使用Go自带的go list获取直接与传递依赖，零新增项目依赖。
#
# 退出状态:
#   最后一条true或false命令决定脚本状态；
#   合规返回0，发现依赖方向违规返回非0。

# 模块路径前缀，与go.mod声明保持一致。
MODULE="github.com/aoci-spec/aoci-code"

# 确定性核心层:
# 无AI配置时也必须独立工作的协议、事实、存储与Agent工具底座。
#
# internal/curation承载文件画像、决策有效性、Missing分类与正式资产读写，
# 已是确定性核心，必须与index/baseline/fs/config/mcptools受到同一依赖闸保护。
CORE_PKGS=(
  "${MODULE}/internal/index"
  "${MODULE}/internal/baseline"
  "${MODULE}/internal/fs"
  "${MODULE}/internal/config"
  "${MODULE}/internal/curation"
  "${MODULE}/internal/mcptools"
  "${MODULE}/internal/dbevidence"
)

# AI编排层:
# 只允许CLI命令层等上层消费者依赖，确定性核心不得直接或传递依赖。
AI_PKGS=(
  "${MODULE}/internal/llm"
  "${MODULE}/internal/prompt"
  "${MODULE}/internal/draft"
  "${MODULE}/internal/workflow"
)

# 直接依赖是系统部署与供应链边界，必须与架构声明同步。x/sys只提供标准库
# 缺失的原子路径交换、Windows Job Object与Linux subreaper系统调用封装；
# 不引入联网、服务或AI运行时。
EXPECTED_DIRECT_MODULES=(
  "gitcode.com/opengauss/openGauss-connector-go-pq"
  "github.com/go-sql-driver/mysql"
  "github.com/jackc/pgx/v5"
  "github.com/modelcontextprotocol/go-sdk"
  "github.com/spf13/cobra"
  "golang.org/x/sys"
)

violations=0
go_bin="${GO_BIN:-go}"

echo "[check-deps] 依赖方向校验开始(核心层禁止 import AI 编排层)"

direct_modules="$(
  "${go_bin}" mod edit -json |
    awk '
      /"Require": \[/ { in_require=1; next }
      in_require && /^[[:space:]]*\],?$/ { in_require=0 }
      in_require && /"Path":/ {
        path=$2
        gsub(/[",]/, "", path)
        indirect=0
      }
      in_require && /"Indirect": true/ { indirect=1 }
      in_require && /^[[:space:]]*},?$/ {
        if (path != "" && !indirect) print path
        path=""
      }
    ' |
    sort
)"
expected_direct_modules="$(printf '%s\n' "${EXPECTED_DIRECT_MODULES[@]}" | sort)"
if [ "${direct_modules}" != "${expected_direct_modules}" ]; then
  echo "[check-deps] 违规: go.mod直接依赖集合与架构契约不一致。"
  echo "             期望: $(printf '%s' "${expected_direct_modules}" | tr '\n' ' ')"
  echo "             实际: $(printf '%s' "${direct_modules}" | tr '\n' ' ')"
  violations=$((violations + 1))
fi

# The reviewed openGauss connector is intentionally carried as a complete
# in-repository module plus a reproducible patch. A version-only require is not
# sufficient: without this exact replace, a build could silently return to the
# unpatched upstream TLS/cancellation/stdout behavior.
opengauss_module="gitcode.com/opengauss/openGauss-connector-go-pq"
opengauss_local_module="third_party/openGauss-connector-go-pq"
if ! grep -Fqx -- "replace ${opengauss_module} => ./${opengauss_local_module}" go.mod; then
  echo "[check-deps] 违规: go.mod缺少openGauss Connector的受审本地replace。"
  violations=$((violations + 1))
fi
if [ ! -f "${opengauss_local_module}/go.mod" ] ||
  [ "$(sed -n '1p' "${opengauss_local_module}/go.mod" 2>/dev/null)" != "module ${opengauss_module}" ]; then
  echo "[check-deps] 违规: 本地openGauss Connector模块身份缺失或不一致。"
  violations=$((violations + 1))
fi

for core in "${CORE_PKGS[@]}"; do
  # 包尚不存在时跳过，使依赖闸可以先于未来包结构落地。
  if ! "${go_bin}" list "${core}" >/dev/null 2>&1; then
    echo "[check-deps] 跳过(包尚不存在): ${core}"
    continue
  fi

  # 获取该核心包的完整依赖闭包，每行一个包路径。
  deps="$("${go_bin}" list -deps "${core}" 2>/dev/null)"

  for ai in "${AI_PKGS[@]}"; do
    # 整行精确匹配，避免相似包名前缀产生误报。
    if printf '%s\n' "${deps}" | grep -qxF "${ai}"; then
      echo "[check-deps] 违规: 核心包 ${core}"
      echo "             依赖了 AI 编排包 ${ai}"
      echo "             → 核心层必须保持零AI依赖(R17/D23)，请移除该import。"
      violations=$((violations + 1))
    fi
  done
done

if [ "${violations}" -gt 0 ]; then
  echo "[check-deps] 失败: 检出 ${violations} 处依赖方向违规。"
  echo "[check-deps] 修复方向: AI能力调用应位于internal/cli等上层，"
  echo "             不得进入确定性核心包。"
  false
else
  echo "[check-deps] 通过: ${#EXPECTED_DIRECT_MODULES[@]}个直接依赖已锁定；${#CORE_PKGS[@]}个核心包的依赖图均不含AI编排层。"
  true
fi
