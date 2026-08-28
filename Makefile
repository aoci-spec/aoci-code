# aoci-code 构建命令集
# 索引条目: Makefile[XMK5T]
# 目标: fast 普通提交门 / full 完整信心门 / release-check 稳定版本门
#       / build 静态编译 / test 全量测试 / vet 静态检查 / safety 公开文案扫描

# 版本号: 优先取 git tag,无 tag 时用 dev+短哈希;完整 commit 与 UTC commit 时间同步注入。
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse HEAD 2>/dev/null || echo none)
DATE    := $(shell TZ=UTC0 git show -s --date=format-local:'%Y-%m-%dT%H:%M:%SZ' --format=%cd HEAD 2>/dev/null || date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X github.com/aoci-spec/aoci-code/internal/cli.version=$(VERSION) \
           -X github.com/aoci-spec/aoci-code/internal/cli.commit=$(COMMIT) \
           -X github.com/aoci-spec/aoci-code/internal/cli.buildDate=$(DATE)

# Non-login automation shells may omit an installed Go toolchain from PATH.
# Resolve one trusted installation once and bind every target to it.
GO_BIN ?= $(shell command -v go 2>/dev/null || { test -x /usr/local/go/bin/go && echo /usr/local/go/bin/go; } || echo go)
GOFMT_BIN ?= $(shell command -v gofmt 2>/dev/null || { test -x /usr/local/go/bin/gofmt && echo /usr/local/go/bin/gofmt; } || echo gofmt)
GORELEASER_BIN ?= $(shell command -v goreleaser 2>/dev/null || { test -x "$$($(GO_BIN) env GOPATH 2>/dev/null)/bin/goreleaser" && echo "$$($(GO_BIN) env GOPATH)/bin/goreleaser"; } || echo goreleaser)
SYFT_BIN ?= $(shell command -v syft 2>/dev/null || { test -x "$$($(GO_BIN) env GOPATH 2>/dev/null)/bin/syft" && echo "$$($(GO_BIN) env GOPATH)/bin/syft"; } || echo syft)

# staticcheck 可执行文件探测: 优先 PATH,其次 GOPATH/bin(go install 默认装到此处)
STATICCHECK := $(shell command -v staticcheck 2>/dev/null || echo "$(shell $(GO_BIN) env GOPATH)/bin/staticcheck")
FAST_PACKAGES := $(shell $(GO_BIN) list ./... | grep -v '/internal/cli$$')
# GNU Make-native recursive wildcard keeps fmt-check usable under its
# failure-closed minimal-PATH test; do not add a parse-time dependency on find.
rwildcard = $(foreach d,$(wildcard $1*),$(call rwildcard,$d/,$2) $(filter $(subst *,%,$2),$d))
MAIN_GO_FILES := $(filter-out ./third_party/%,$(call rwildcard,./,*.go))
OPENGAUSS_PATCH_GO_FILES := \
	third_party/openGauss-connector-go-pq/config.go \
	third_party/openGauss-connector-go-pq/conn.go \
	third_party/openGauss-connector-go-pq/conn_go18.go \
	third_party/openGauss-connector-go-pq/connector.go \
	third_party/openGauss-connector-go-pq/logger.go \
	third_party/openGauss-connector-go-pq/ssl.go \
	third_party/openGauss-connector-go-pq/aoci_security_patch_test.go

.PHONY: build test fast fast-test fast-builds full release-check race vuln database-integration clean-room-smoke example-test vet fmt fmt-check safety check-deps toolchain-preflight opengauss-connector licenses textassets-check staticcheck check cross clean

# 静态编译单二进制,产出 build/aoci
build:
	CGO_ENABLED=0 $(GO_BIN) build -ldflags "$(LDFLAGS)" -o build/aoci ./cmd/aoci

# 全量测试(-count=1 禁用测试缓存 —— 缓存曾掩盖默认值变更引发的回归,审查纪律)
test:
	$(GO_BIN) test ./... -count=1

# 独立示例仓包含自己的go.mod，不会被根模块./...自动覆盖。
example-test:
	cd examples/minimal-repository && $(GO_BIN) test ./... -count=1

# 静态检查(官方 vet)
vet:
	$(GO_BIN) vet ./...

# 格式化主模块和AOCI拥有的connector补丁文件。未修改的完整上游镜像必须保持
# v1.0.8字节不变，不能把整个third_party目录交给递归gofmt。
fmt:
	$(GOFMT_BIN) -l -w $(MAIN_GO_FILES) $(OPENGAUSS_PATCH_GO_FILES)

# 格式检查闸(零副作用,供 check 调用;有未格式化文件即失败并列出清单)
# 闸门教训(2026-07-09): check 曾长期缺失 fmt 口径,注释格式债静默积累 18 文件才暴露。
fmt-check:
	@UNFMT=$$($(GOFMT_BIN) -l $(MAIN_GO_FILES) $(OPENGAUSS_PATCH_GO_FILES)) || exit $$?; if [ -n "$$UNFMT" ]; then echo "fmt-check: 以下文件未通过 gofmt:"; echo "$$UNFMT"; exit 1; else echo "fmt-check: 全部文件符合 gofmt"; echo "fmt-check: 主模块与AOCI connector补丁文件已检查，上游镜像未被递归改写"; fi

# 公开文案禁区扫描(D3 机器闸门)。脚本缺失是闸门损坏,必须失败 —— 跳过即假绿。
safety:
	@GO_BIN="$(GO_BIN)" bash scripts/check-public-text.sh

# 依赖方向硬校验(R17/D23 机器闸门): 确定性核心层禁止 import AI 编排层。
# 脚本仅用 go list,零新增依赖;脚本缺失是闸门损坏,必须失败 —— 跳过即假绿。
check-deps:
	@GO_BIN="$(GO_BIN)" bash scripts/check-deps.sh

# 底座工具链预检。licenses 与 opengauss-connector 在 GOTOOLCHAIN=local 下跑 Go,
# 使审计只报告 GO_BIN 选定的那一个 Go 身份;该钉法下 Go 不会下载工具链,底座低于
# go.mod 时它们直接失败。而 check-opengauss-connector.sh 把 go mod download 的任何
# 失败都包装成"could not download the pinned upstream module",于是一个补丁版落后
# 会被读成供应链故障。这里用毫秒级比较把它翻译成人话,并在 full 的最前面失败。
# 比较用 >= 而非 ==:更新的底座本来就满足每个钉点,不该被拦。
toolchain-preflight:
	@want=$$(awk '$$1=="go"{print $$2; exit}' go.mod); \
	base=$$(env GOTOOLCHAIN=local "$(GO_BIN)" version 2>/dev/null | awk '{print $$3}'); base=$${base#go}; \
	if [ -z "$$want" ]; then echo "toolchain-preflight: go.mod declares no go directive" >&2; exit 1; fi; \
	if [ -z "$$base" ]; then echo "toolchain-preflight: could not run '$(GO_BIN) version'; set GO_BIN to a Go executable" >&2; exit 1; fi; \
	if awk -v have="$$base" -v want="$$want" 'BEGIN{n=split(have,h,".");m=split(want,w,".");for(i=1;i<=3;i++){hv=(i<=n)?h[i]+0:0;wv=(i<=m)?w[i]+0:0;if(hv>wv)exit 0;if(hv<wv)exit 1}exit 0}'; then exit 0; fi; \
	{ echo "toolchain-preflight: the base Go toolchain is older than go.mod requires."; \
	  echo ""; \
	  echo "  go.mod 'go' directive : $$want"; \
	  echo "  base toolchain        : $$base   (env GOTOOLCHAIN=local $(GO_BIN) version)"; \
	  echo ""; \
	  echo "A plain 'go version' can report a newer Go: under the default GOTOOLCHAIN=auto"; \
	  echo "the go command re-executes a downloaded toolchain. The licenses and"; \
	  echo "opengauss-connector gates pin GOTOOLCHAIN=local so the audit reports one Go"; \
	  echo "identity for the toolchain GO_BIN selects, here and in CI. Under that pin Go"; \
	  echo "downloads nothing, so those gates stop with:"; \
	  echo "  go: go.mod requires go >= $$want (running go $$base; GOTOOLCHAIN=local)"; \
	  echo ""; \
	  echo "Do one of these, then re-run:"; \
	  echo "  1. Install Go $$want or newer as the base toolchain (https://go.dev/dl/)."; \
	  echo "     Replacing /usr/local/go needs root."; \
	  echo "  2. Point this build at a base toolchain already on this machine:"; \
	  echo "       make full GO_BIN=/path/to/go/bin/go"; \
	} >&2; exit 1

# 固定openGauss Connector上游身份，重放AOCI安全补丁并逐字节比较完整本地模块；
# 聚焦测试不会启动或访问数据库。
opengauss-connector: toolchain-preflight
	@GO_BIN="$(GO_BIN)" bash scripts/check-opengauss-connector.sh
	@cd third_party/openGauss-connector-go-pq && $(GO_BIN) test -count=1 -run '^(TestParseConfigStrict.*|TestConnectorContextBoundsTLSStartup|TestNewPrintfLoggerWritesToStderr|TestBadClientCertificateDoesNotWriteStdout|TestCancelTLSFailureUsesOnlyCancelConnection|TestServerPBKDF2IterationBounds|TestAuthenticationPayloadAndIterationFailuresAreDriverErrors)$$' .

# 可达外部Go包许可证闸；工具由CI和发布排练固定安装，不进入go.mod。
licenses: toolchain-preflight
	@GO_BIN="$(GO_BIN)" bash scripts/check-licenses.sh

# 嵌入文本发布闸：完整正式Locale、开发中Locale现有子集、变量与协议词、
# 清单消费符号和重复事实源检测必须共同通过。
textassets-check:
	$(GO_BIN) test ./textassets -count=1

# 深度静态分析(五重归零第五重;开发期工具,不进 go.mod)。
# Full Confidence要求固定工具已安装，禁止把缺少工具误报为通过。
# 注意: staticcheck 有独立分析缓存,增量改动后如遇 undefined 类误报,先 go clean -cache 再重跑。
staticcheck:
	@if [ -x "$(STATICCHECK)" ] || command -v staticcheck >/dev/null 2>&1; then \
		echo "运行 staticcheck..."; \
		$(STATICCHECK) ./...; \
	else \
		echo "staticcheck is required for make full"; exit 1; \
	fi

# Tier 0: ordinary local commit gate. The independent compiler, vet, dependency,
# safety, and representative short-test gates run concurrently after formatting.
# Exhaustive fault matrices explicitly skip under -short and remain in full.
fast: fmt-check
	@$(MAKE) --no-print-directory -j5 fast-test vet check-deps safety fast-builds
	@echo "★ make fast passed (Tier 0 required gate) ★"

fast-test:
	$(GO_BIN) test -short -count=1 $(FAST_PACKAGES)
	$(GO_BIN) test -short -count=1 -run '^(TestMCPInvocationUsesTopLevelCommandOnly|TestAlignedCleanGuideMatchesGoldenByteForByte|TestCheckCleanRepo|TestScopeStatusObservesTestsWithoutChangingWholeIndex|TestScopeAcknowledgePreservesIndexAuthoringDebt|TestScopeBudgetDirectEditRequiresRefreshWithoutFormalWrites|TestDatabaseEvidenceBundleContainsFactsButNoSemanticCandidate|TestEntriesAutoCleanupFailureRetryOnlyCompletesRecovery|TestHeaderApplyKeepsRecoveryWhenCASWritesThenReturnsError|TestFirstManagedScanPersistsRolesAndForceCannotWashReceipt|TestAgentPlanIDIgnoresOnlyVolatileExcludedRuntimeCounts)$$' ./internal/cli

fast-builds:
	@mkdir -p build
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO_BIN) build -o build/aoci-fast ./cmd/aoci
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO_BIN) build -o build/aoci-fast.exe ./cmd/aoci

race:
	$(GO_BIN) test -race -count=1 -timeout=15m ./...

vuln:
	@VULN=$$(command -v govulncheck 2>/dev/null || echo "$$($(GO_BIN) env GOPATH)/bin/govulncheck"); \
	if [ ! -x "$$VULN" ]; then echo "govulncheck is required for make full"; exit 1; fi; \
	"$$VULN" ./...

# The integration tests require the explicit temporary database environment.
# An unconfigured local run reports skips; remote Full Confidence supplies both engines.
database-integration:
	$(GO_BIN) test -tags=integration -count=1 ./internal/dbevidence

clean-room-smoke:
	bash scripts/release/clean-room-smoke.sh

# Tier 1: complete confidence gate. Ordinary commits do not run or wait for it.
full: toolchain-preflight fmt-check vet check-deps opengauss-connector licenses textassets-check build test example-test staticcheck safety race vuln database-integration clean-room-smoke
	@echo "★ make full passed (Tier 1 Full Confidence) ★"

# Compatibility alias retained for existing operators and automation.
check: full

# Tier 2: non-publishing release gate. Native OS and database jobs are supplied by
# the Release Rehearsal workflow; this local gate adds clean-room and package checks.
release-check: full
	@if ! command -v "$(GORELEASER_BIN)" >/dev/null 2>&1 && [ ! -x "$(GORELEASER_BIN)" ]; then echo "goreleaser is required for make release-check"; exit 1; fi
	@if ! command -v "$(SYFT_BIN)" >/dev/null 2>&1 && [ ! -x "$(SYFT_BIN)" ]; then echo "syft is required for make release-check"; exit 1; fi
	@if ! command -v sha256sum >/dev/null 2>&1; then echo "sha256sum is required for make release-check"; exit 1; fi
	@PATH="$(dir $(GO_BIN)):$(dir $(SYFT_BIN)):$$PATH" $(GORELEASER_BIN) release --snapshot --clean
	@$(GO_BIN) run ./scripts/release/archive-smoke --dist dist
	@cd dist && sha256sum -c SHA256SUMS
	@GORELEASER_VERSION=$$($(GORELEASER_BIN) --version | awk -F': *' '/^GitVersion:/ { print $$2; exit }'); \
	SYFT_VERSION=$$($(SYFT_BIN) version | awk -F': *' '/^Version:/ { print $$2; exit }'); \
	SYFT_VERSION_NORMALIZED=$$(printf '%s' "$$SYFT_VERSION" | tr '[:upper:]' '[:lower:]'); \
	case "$$SYFT_VERSION_NORMALIZED" in ''|'[not provided]'|'not provided'|'unknown'|'none') \
		SYFT_VERSION=$$($(GO_BIN) version -m "$(SYFT_BIN)" | awk '$$1 == "mod" && $$2 == "github.com/anchore/syft" { print $$3; exit }');; \
	esac; \
	SYFT_VERSION_NORMALIZED=$$(printf '%s' "$$SYFT_VERSION" | tr '[:upper:]' '[:lower:]'); \
	test -n "$$GORELEASER_VERSION" || { echo "could not determine goreleaser version"; exit 1; }; \
	case "$$SYFT_VERSION_NORMALIZED" in ''|'[not provided]'|'not provided'|'unknown'|'none') echo "could not determine syft version"; exit 1;; esac; \
	$(GO_BIN) run ./scripts/release/manifest \
		--dist dist \
		--output dist/RELEASE-MANIFEST.json \
		--version "$(VERSION)" \
		--source-commit "$$(git rev-parse HEAD)" \
		--build-date "$(DATE)" \
		--go-version "$$($(GO_BIN) version)" \
		--goreleaser-version "$$GORELEASER_VERSION" \
		--syft-version "$$SYFT_VERSION" \
		--tools-list-sha256 "$$(sha256sum testdata/golden/mcp_list_tools.json | awk '{ print $$1 }')"
	@$(GO_BIN) run ./scripts/release/manifest --verify dist/RELEASE-MANIFEST.json
	@echo "★ make release-check passed (Tier 2 local release gate) ★"

# 交叉编译本地快照（不创建远程发布）
cross:
	@if command -v "$(GORELEASER_BIN)" >/dev/null 2>&1 || [ -x "$(GORELEASER_BIN)" ]; then $(GORELEASER_BIN) release --snapshot --clean; else echo "cross: goreleaser 未安装,跳过"; fi

# 清理构建产物
clean:
	rm -rf build/ dist/
