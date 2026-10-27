# aoci-code 构建命令集
# 索引条目: Makefile[XMK5T]
# 目标: fast 普通提交门 / full 完整信心门 / release-check 稳定版本门
#       / build 静态编译 / test 全量测试 / vet 静态检查 / safety 公开文案扫描

# 版本号: 优先取 git tag,无 tag 时用 dev+短哈希;完整 commit 与 UTC commit 时间同步注入。
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT  := $(shell git rev-parse HEAD 2>/dev/null || echo none)
DATE    := $(shell TZ=UTC0 git show -s --date=format-local:'%Y-%m-%dT%H:%M:%SZ' --format=%cd HEAD 2>/dev/null || date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -X github.com/aoci-spec/aoci-code/internal/buildinfo.Version=$(VERSION) \
           -X github.com/aoci-spec/aoci-code/internal/buildinfo.Commit=$(COMMIT) \
           -X github.com/aoci-spec/aoci-code/internal/buildinfo.BuildDate=$(DATE)

# Non-login automation shells may omit an installed Go toolchain from PATH.
# Resolve one trusted installation once and bind every target to it.
GO_BIN ?= $(shell command -v go 2>/dev/null || { test -x /usr/local/go/bin/go && echo /usr/local/go/bin/go; } || echo go)
GOFMT_BIN ?= $(shell command -v gofmt 2>/dev/null || { test -x /usr/local/go/bin/gofmt && echo /usr/local/go/bin/gofmt; } || echo gofmt)
GORELEASER_BIN ?= $(shell command -v goreleaser 2>/dev/null || { test -x "$$($(GO_BIN) env GOPATH 2>/dev/null)/bin/goreleaser" && echo "$$($(GO_BIN) env GOPATH)/bin/goreleaser"; } || echo goreleaser)
SYFT_BIN ?= $(shell command -v syft 2>/dev/null || { test -x "$$($(GO_BIN) env GOPATH 2>/dev/null)/bin/syft" && echo "$$($(GO_BIN) env GOPATH)/bin/syft"; } || echo syft)
GOEXE ?= $(shell $(GO_BIN) env GOEXE)
BUILD_BIN ?= build/aoci$(GOEXE)

# staticcheck 可执行文件探测: 优先 PATH,其次 GOPATH/bin(go install 默认装到此处)
STATICCHECK := $(shell command -v staticcheck 2>/dev/null || echo "$(shell $(GO_BIN) env GOPATH)/bin/staticcheck")
FAST_PACKAGES := $(shell $(GO_BIN) list ./... | grep -v '/internal/cli$$')
FULL_GATES := fmt-check vet check-deps opengauss-connector licenses textassets-check test example-test staticcheck safety race vuln database-integration clean-room-smoke
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

.PHONY: build test fast fast-test fast-builds full verify release-check race vuln fuzz-smoke database-integration clean-room-smoke example-test vet fmt fmt-check safety check-deps toolchain-preflight opengauss-connector licenses textassets-check update-goldens staticcheck coverage-floors ladder check cross clean axis-self longrun-refresh legacy-migration

# 静态编译单二进制,使用Go目标平台的原生可执行文件后缀。
build:
	CGO_ENABLED=0 $(GO_BIN) build -ldflags "$(LDFLAGS)" -o "$(BUILD_BIN)" ./cmd/aoci

# 全量测试(-count=1 禁用测试缓存 —— 缓存曾掩盖默认值变更引发的回归,审查纪律)
test:
	$(GO_BIN) test ./... -count=1

# 独立示例仓包含自己的go.mod，不会被根模块./...自动覆盖。
example-test:
	cd examples/minimal-repository && $(GO_BIN) test ./... -count=1
	cd examples/engine-embed && $(GO_BIN) vet ./... && CGO_ENABLED=0 $(GO_BIN) build -o /dev/null ./...

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
	base=$$(GOTOOLCHAIN=local "$(GO_BIN)" version 2>/dev/null | awk '{print $$3}'); base=$${base#go}; \
	if [ -z "$$want" ]; then echo "toolchain-preflight: go.mod declares no go directive" >&2; exit 1; fi; \
	if [ -z "$$base" ]; then echo "toolchain-preflight: could not run '$(GO_BIN) version'; set GO_BIN to a Go executable" >&2; exit 1; fi; \
	if awk -v have="$$base" -v want="$$want" 'BEGIN{n=split(have,h,".");m=split(want,w,".");for(i=1;i<=3;i++){hv=(i<=n)?h[i]+0:0;wv=(i<=m)?w[i]+0:0;if(hv>wv)exit 0;if(hv<wv)exit 1}exit 0}'; then exit 0; fi; \
	{ echo "toolchain-preflight: the base Go toolchain is older than go.mod requires."; \
	  echo ""; \
	  echo "  go.mod 'go' directive : $$want"; \
	  echo "  base toolchain        : $$base   (GOTOOLCHAIN=local $(GO_BIN) version)"; \
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
	@cd third_party/openGauss-connector-go-pq && $(GO_BIN) test -count=1 -run '^(TestParseConfigStrict.*|TestParseConfigStrictRejectsTrailingBackslashInQuotedValue|TestConnectorContextBoundsTLSStartup|TestNewPrintfLoggerWritesToStderr|TestBadClientCertificateDoesNotWriteStdout|TestCancelTLSFailureUsesOnlyCancelConnection|TestServerPBKDF2IterationBounds|TestAuthenticationPayloadAndIterationFailuresAreDriverErrors)$$' .

# 可达外部Go包许可证闸；工具由CI和发布排练固定安装，不进入go.mod。
licenses: toolchain-preflight
	@GO_BIN="$(GO_BIN)" bash scripts/check-licenses.sh

# 嵌入文本发布闸：完整正式Locale、开发中Locale现有子集、变量与协议词、
# 清单消费符号和重复事实源检测必须共同通过。
textassets-check:
	$(GO_BIN) test ./textassets -count=1

# Explicitly regenerate deterministic public Goldens from production renderers.
# The programs compute and write every digest; maintainers never copy hashes
# from failed assertions.
update-goldens:
	AOCI_UPDATE_GOLDEN=1 $(GO_BIN) test ./internal/hooks -run '^TestAgentsNewFileOutputMatchesCompatibilityDigest$$' -count=1
	AOCI_UPDATE_GOLDEN=1 $(GO_BIN) test ./internal/index -run '^TestRuntimeRulesMatchCompatibilityDigest$$' -count=1
	AOCI_UPDATE_GOLDEN=1 $(GO_BIN) test ./internal/mcptools -run '^(TestRegenerateListToolsGolden|TestMaintainToolDescriptionMatchesGoldenByteForByte)$$' -count=1
	AOCI_UPDATE_GOLDEN=1 $(GO_BIN) test ./internal/cli -run '^TestCLIHelpAssetsMatchRealCommandTree$$' -count=1

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

# Per-package statement-coverage floors for the safety core (scripts/coverage-floors.txt).
# Runs the listed packages' own tests under -short; full-confidence feeds it the
# coverage snapshot profile instead. Not part of fast: it is a Tier-1 regression
# gate for test erosion, not a commit-time check. Script missing = gate broken.
coverage-floors:
	@GO_BIN="$(GO_BIN)" bash scripts/check-coverage-floors.sh

# Tier 0: ordinary local commit gate. The independent compiler, vet, dependency,
# safety, and representative short-test gates run concurrently after formatting.
# Exhaustive fault matrices explicitly skip under -short and remain in full.
fast: fmt-check
	@$(MAKE) --no-print-directory -j5 fast-test vet check-deps safety fast-builds
	@echo "★ make fast passed (Tier 0 required gate) ★"

fast-test:
	$(GO_BIN) test -short -count=1 $(FAST_PACKAGES)
	$(GO_BIN) test -short -count=1 -run '^(TestMCPInvocationUsesTopLevelCommandOnly|TestCheckCleanRepo|TestScopeStatusObservesTestsWithoutChangingWholeIndex|TestScopeAcknowledgePreservesIndexAuthoringDebt|TestScopeBudgetDirectEditRequiresRefreshWithoutFormalWrites|TestDatabaseEvidenceBundleContainsFactsButNoSemanticCandidate|TestFirstManagedScanPersistsRolesAndForceCannotWashReceipt)$$' ./internal/cli

fast-builds:
	@mkdir -p build
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 $(GO_BIN) build -o build/aoci-fast ./cmd/aoci
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64 $(GO_BIN) build -o build/aoci-fast.exe ./cmd/aoci
	cd examples/engine-embed && CGO_ENABLED=0 $(GO_BIN) build -o /dev/null ./...

# The timeout is per test binary and exists to stop a hang, not to pace the
# suite: internal/cli under the race detector takes about 12 minutes on a shared
# CI runner (719 s in full-confidence run 179 on 8ba516a), and the tag release
# gate on the same commit hit 15 minutes and failed with one 9-second test still
# running. A hang still fails, twice as late.
race:
	$(GO_BIN) test -race -count=1 -timeout=30m ./...

vuln:
	@VULN=$$(command -v govulncheck 2>/dev/null || echo "$$($(GO_BIN) env GOPATH)/bin/govulncheck"); \
	if [ ! -x "$$VULN" ]; then echo "govulncheck is required for make full"; exit 1; fi; \
	"$$VULN" ./...

# Native Go fuzz targets over the readers of external bytes: index text and
# Entry lines, the Overview cursor and Attestation inputs, the strict JSON
# codecs, repository-relative paths and exclude patterns, and the openGauss
# DSN. go test accepts one -fuzz pattern per package invocation, so each target
# runs alone for FUZZTIME; a crash fails the gate and leaves its minimized input
# under the package's testdata/fuzz directory for the regression commit. The
# list is checked against the Fuzz functions in the tree so a new target cannot
# be left out silently. Nightly full-confidence runs this with FUZZTIME=30s.
FUZZTIME ?= 20s
FUZZ_TARGETS := \
	./internal/index:FuzzParse \
	./internal/index:FuzzValidateEntryLine \
	./internal/index:FuzzParseTags \
	./internal/mcptools:FuzzDecodeOverviewCursor \
	./internal/mcptools:FuzzOverviewCursorChain \
	./internal/mcptools:FuzzOverviewAttestationInput \
	./internal/jsonstrict:FuzzRejectDuplicateKeys \
	./internal/jsonstrict:FuzzRejectDuplicateKeysFold \
	./internal/bootstrapapply:FuzzStrictDecode \
	./internal/fs:FuzzNormalizeRelPath \
	./internal/fs:FuzzMatchExcludePattern \
	./internal/dbevidence:FuzzParseConfigStrict

fuzz-smoke:
	@listed=""; for target in $(FUZZ_TARGETS); do listed="$$listed $${target##*:}"; done; \
	for found in $$(grep -rhoE '^func Fuzz[A-Za-z0-9_]+\(' --include='*_test.go' cmd internal | sed -E 's/^func (Fuzz[A-Za-z0-9_]+)\(/\1/' | sort -u); do \
		case " $$listed " in *" $$found "*) ;; *) echo "fuzz-smoke: $$found is declared in the tree but missing from FUZZ_TARGETS" >&2; exit 1;; esac; \
	done; \
	status=0; \
	for target in $(FUZZ_TARGETS); do \
		pkg=$${target%%:*}; name=$${target##*:}; \
		echo "fuzz-smoke: $$name in $$pkg for $(FUZZTIME)"; \
		$(GO_BIN) test -run '^$$' -fuzz "^$$name$$" -fuzztime=$(FUZZTIME) "$$pkg" || { status=1; echo "fuzz-smoke: $$name crashed or failed" >&2; }; \
	done; \
	if [ $$status -eq 0 ]; then echo "fuzz-smoke: every fuzz target ran $(FUZZTIME) without a crash"; fi; \
	exit $$status

# The integration tests require the explicit temporary database environment.
# An unconfigured local run reports skips; remote Full Confidence supplies both engines.
database-integration:
	$(GO_BIN) test -tags=integration -count=1 ./internal/dbevidence

clean-room-smoke:
	bash scripts/release/clean-room-smoke.sh

# Long-running cognition refresh black box (R67-A). A Go runner drives the
# host-native binary as a real stdio MCP server over two disposable Git
# repositories: the 29-versus-30 semantic threshold, compact check_only, Dirty
# delivery marked unreliable, a simulated Host compaction with a partial
# Attestation, three-reason merging, one injected repair_required correction,
# and final Verify/Check/Guide alignment. Not part of full; the nightly line runs
# it on schedule and dispatch. The JSON report lands under the ignored build/.
longrun-refresh: build
	$(GO_BIN) run ./scripts/blackbox/longrun-refresh --binary "$(abspath $(BUILD_BIN))" --output build/longrun-refresh-report.json

# Legacy migration drill. LEGACY_RELEASE, the last release that authors Legacy
# indexes, builds and aligns a Legacy repository; BUILD_BIN, for which that
# layout is read-only, must migrate it through onboarding, approved in the host
# dialog, to aligned Volumes v1 carrying every Code Entry byte for byte, while
# the runner runs only the commands each result names. The
# release binary comes checksum-verified through the upgrade axis's cache. Not
# part of full; the full-confidence line runs it after the axis.
LEGACY_RELEASE ?= v0.1.0-rc22
legacy-migration: build
	legacy_bin="$$(python3 scripts/blackbox/mcp_upgrade.py --fetch $(LEGACY_RELEASE))" && \
		$(GO_BIN) run ./scripts/blackbox/legacy-migration --binary "$(abspath $(BUILD_BIN))" --legacy-binary "$$legacy_bin"

# Tier 1: complete confidence gate. Ordinary commits do not run or wait for it.
full: toolchain-preflight build $(FULL_GATES)
	@echo "★ make full passed (Tier 1 Full Confidence) ★"

# Pre-tag self-axis: BUILD_BIN plays both roles of every upgrade-axis shape under
# the newest CHANGELOG version's label, with no download. The moment a candidate
# is tagged it becomes an "old version" for that suite, and a shape premise written
# for the releases before it goes stale with no run in between (rc20's root list
# turned legacydirs red on the first nightly after its tag). This target is that
# missing run; it does not replace the released matrix the nightly downloads.
axis-self:
	@test -x "$(BUILD_BIN)" || { echo "axis-self: $(BUILD_BIN) is not an executable; run make build or pass BUILD_BIN=/path/to/aoci"; exit 1; }
	AOCI_REPO="$(CURDIR)" AOCI_BIN="$(abspath $(BUILD_BIN))" python3 scripts/blackbox/mcp_upgrade.py --self

# One deterministic closure command. It reuses full's host-native binary and runs every
# black-box suite without entering the lifecycle model track, then the self-axis.
# Keep running the remaining suites after one fails so one invocation reports the
# whole result.
verify:
	@status=0; \
	$(MAKE) --no-print-directory toolchain-preflight build || exit $$?; \
	$(MAKE) --no-print-directory -k $(FULL_GATES) || status=1; \
	AOCI_REPO="$(CURDIR)" AOCI_BIN="$(abspath $(BUILD_BIN))" python3 scripts/blackbox/mcp_conformance.py || status=1; \
	AOCI_REPO="$(CURDIR)" AOCI_BIN="$(abspath $(BUILD_BIN))" python3 scripts/blackbox/mcp_scenarios.py || status=1; \
	AOCI_REPO="$(CURDIR)" AOCI_BIN="$(abspath $(BUILD_BIN))" python3 scripts/blackbox/mcp_lifecycle.py || status=1; \
	$(MAKE) --no-print-directory axis-self BUILD_BIN="$(BUILD_BIN)" || status=1; \
	python3 scripts/blackbox/ladder.py --check; \
	if [ $$status -eq 0 ]; then echo "★ make verify passed (all deterministic gates) ★"; fi; \
	exit $$status

# The run-by-name test ladder: contracts that no gate runs for you (release-time
# axis runs, real-repository tracks, Docker-only suites, nightly gates) with
# their last verification on record. --check lists what is overdue; it never
# fails on its own, a closure reads it and decides. Register every new suite.
ladder:
	@python3 scripts/blackbox/ladder.py --check

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
