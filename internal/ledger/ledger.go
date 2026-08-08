// 遥测事件JSONL追加 / 最近事件摘要 / 粗略token估算 / 端点脱敏哈希。
//
// 纪律:
//   - O_APPEND单行写,每条事件仅执行一次Write,避免单行被本进程拆分;
//   - 写失败仅stderr警告,绝不阻塞主流程;
//   - 读取时损坏行跳过并计数,不崩全局;
//   - EstimateTokens仅粗估供趋势,对外文案不得写成精确计费;
//   - 时间戳使用UTC RFC3339Nano,避免同一秒事件失去直接排序证据;
//   - 密钥、源码正文、Prompt正文和对话正文绝不进入Ledger。
//
// Ledger版本:
//   - v1: 基础操作、路径数、耗时、漂移与来源;
//   - v2: 模型、端点脱敏、Token、草稿与Warning计量;
//   - v3: result/fail_code及批量Apply或拒绝计量;
//   - v4: schema_version、event_id、实验上下文、业务退出码和Overview交付观测字段;
//   - v5: Entries Check分别记录passed、warned、rejected、skipped并增加
//     repair_required结果，避免候选拒绝被误记为普通Warning成功。
//   - v6: Auto路径增加工具往返、认知召回、语义文件、format-only
//     与防重计量，使低开销目标可由Ledger复算。
//   - v7: 区分本次真实写入与崩溃/重放时确认已处于目标状态的恢复数量。
//   - v8: Entries post-write recovery records old pre/postimages, current
//     Baseline, recovery transaction, and later governance proof chain.
//
// v5实验上下文:
//   - CLI无法可靠探测Codex Desktop模型、Memory开关和UI会话ID;
//   - 实验Harness可通过AOCI_EXPERIMENT_ID等环境变量显式声明;
//   - Append仅在Event对应字段为空时补入环境声明,绝不覆盖调用方事实;
//   - 环境值会去除首尾空白、折叠换行与连续空白并限制为256个rune;
//   - 普通用户不设置实验变量时,这些字段保持省略。
//
// result与exit_code:
//   - result表示操作是否正常完成自身判定或写入事务;
//   - repair_required表示判定已经正常完成，但候选必须修正后才能继续;
//   - exit_code表示调用方在同一判定点形成的CLI业务退出码;
//   - 因此Verify正常发现漂移时可同时为result=ok和exit_code=1;
//   - exit_code使用指针,区分历史事件无记录与明确的成功码0。
//
// 依赖方向:
// 本包属确定性核心层,绝不import internal/llm。
package ledger

import (
	"bufio"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/textassets"
)

// EventSchemaVersion是当前新写入Ledger事件的机器协议版本。
const EventSchemaVersion = 8

// Source枚举:事件发起方。
const (
	SourceHuman  = "human"
	SourceAgent  = "agent"
	SourceCLIAI  = "cli_ai"
	SourceCI     = "ci"
	SourcePolicy = "policy"
)

// TokenSource枚举:token计量来源。
const (
	TokenSourceExact     = "exact"
	TokenSourceReported  = "reported"
	TokenSourceEstimated = "estimated"
	TokenSourceMissing   = "missing"
)

// Result枚举:操作终态。
// 旧事件允许Result为空并按成功解释;新事件默认显式写入ok。
const (
	ResultOK             = "ok"
	ResultRepairRequired = machinecontract.AutoStatusRepairRequired
	ResultRejected       = "rejected"
	ResultConflict       = "conflict"
	ResultError          = "error"
)

// Event一条治理、实验或计量事件。
//
// 所有扩展字段均使用omitempty，历史JSONL行无需迁移。
// ExitCode与FullTextIncluded使用指针,分别区分“历史未记录/明确为0”
// 以及“不适用/明确为false”。
type Event struct {
	SchemaVersion int    `json:"schema_version,omitempty"`
	EventID       string `json:"event_id,omitempty"`
	Ts            string `json:"ts"`
	Op            string `json:"op"`

	PathsCount  int    `json:"paths_count,omitempty"`
	TagFilter   string `json:"tag_filter,omitempty"`
	DurationMs  int64  `json:"duration_ms,omitempty"`
	DriftWarned bool   `json:"drift_warned,omitempty"`
	Source      string `json:"source,omitempty"`
	Confidence  string `json:"confidence,omitempty"`

	GenerationSource string `json:"generation_source,omitempty"`
	AgentName        string `json:"agent_name,omitempty"`

	Result   string `json:"result,omitempty"`
	FailCode string `json:"fail_code,omitempty"`
	ExitCode *int   `json:"exit_code,omitempty"`

	Model         string  `json:"model,omitempty"`
	Provider      string  `json:"provider,omitempty"`
	EndpointHash  string  `json:"endpoint_hash,omitempty"`
	InputTokens   int     `json:"input_tokens,omitempty"`
	OutputTokens  int     `json:"output_tokens,omitempty"`
	TokenSrc      string  `json:"token_source,omitempty"`
	CostEstimate  float64 `json:"cost_estimate,omitempty"`
	DraftRunID    string  `json:"draft_run_id,omitempty"`
	WarningsCount int     `json:"warnings_count,omitempty"`

	PassedCount  int `json:"passed_count,omitempty"`
	WarnedCount  int `json:"warned_count,omitempty"`
	SkippedCount int `json:"skipped_count,omitempty"`

	AppliedCount   int    `json:"applied_count,omitempty"`
	RecoveredCount int    `json:"recovered_count,omitempty"`
	RejectedCount  int    `json:"rejected_count,omitempty"`
	RejectKinds    string `json:"reject_kinds,omitempty"`

	// 实验上下文由调用方显式填写,或由Append从受控环境变量补充。
	ExperimentID    string `json:"experiment_id,omitempty"`
	TaskID          string `json:"task_id,omitempty"`
	ExperimentGroup string `json:"experiment_group,omitempty"`
	RunID           string `json:"run_id,omitempty"`
	SessionID       string `json:"session_id,omitempty"`
	AgentModel      string `json:"agent_model,omitempty"`
	MemoryMode      string `json:"memory_mode,omitempty"`
	RepositoryHead  string `json:"repository_head,omitempty"`
	BinaryVersion   string `json:"binary_version,omitempty"`

	// Overview或其他认知交付操作可使用的研究观测字段。
	DeliveryMode     string `json:"delivery_mode,omitempty"`
	FullTextIncluded *bool  `json:"full_text_included,omitempty"`
	FallbackReason   string `json:"fallback_reason,omitempty"`
	IndexSHA256      string `json:"index_sha256,omitempty"`
	RepositorySHA256 string `json:"repository_sha256,omitempty"`
	IndexBytes       int    `json:"index_bytes,omitempty"`
	OutputBytes      int    `json:"output_bytes,omitempty"`
	EstimatedTokens  int    `json:"estimated_tokens,omitempty"`
	SectionCount     int    `json:"section_count,omitempty"`
	EntryCount       int    `json:"entry_count,omitempty"`

	// Auto路径的低开销与防重事实。
	AOCIToolCalls     int `json:"aoci_tool_calls,omitempty"`
	ShellAOCICalls    int `json:"shell_aoci_calls,omitempty"`
	OverviewReads     int `json:"overview_reads,omitempty"`
	LocalRecalls      int `json:"local_recalls,omitempty"`
	SemanticFiles     int `json:"semantic_files,omitempty"`
	FormatOnlyFiles   int `json:"format_only_files,omitempty"`
	DuplicateApplies  int `json:"duplicate_applies,omitempty"`
	RepeatedMaintains int `json:"repeated_maintains,omitempty"`

	// Entries post-write recovery and supersession proof.
	RecoveryStatus        string   `json:"recovery_status,omitempty"`
	RecoveryTransactionID string   `json:"recovery_transaction_id,omitempty"`
	PreIndexSHA256        string   `json:"pre_index_sha256,omitempty"`
	PostIndexSHA256       string   `json:"post_index_sha256,omitempty"`
	BaselineSHA256        string   `json:"baseline_sha256,omitempty"`
	GovernanceReceipts    []string `json:"governance_receipts,omitempty"`
}

func ledgerPath(root string) string {
	return filepath.Join(root, ".aoci", "ledger.jsonl")
}

// Append追加一条事件。
// enabled=false时静默跳过;任何失败仅stderr警告不阻塞主流程。
func Append(root string, enabled bool, ev Event) {
	if !enabled {
		return
	}

	enrichEvent(&ev)

	line, err := json.Marshal(ev)
	if err != nil {
		writeDiagnostic("ledger.marshal_failed", err)
		return
	}

	dir := filepath.Dir(ledgerPath(root))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		writeDiagnostic("ledger.mkdir_failed", err)
		return
	}

	file, err := os.OpenFile(
		ledgerPath(root),
		os.O_APPEND|os.O_CREATE|os.O_WRONLY,
		0o644,
	)
	if err != nil {
		writeDiagnostic("ledger.open_failed", err)
		return
	}
	defer file.Close()

	// 单条事件通过一次Write落盘,避免本进程主动把JSONL单行拆成多次写入。
	if _, err := file.Write(append(line, '\n')); err != nil {
		writeDiagnostic("ledger.write_failed", err)
	}
}

// writeDiagnostic keeps Ledger best-effort while ensuring its user-visible
// stderr shell comes from the active Locale. A broken catalog suppresses this
// non-authoritative diagnostic instead of changing the caller's write result.
func writeDiagnostic(key string, err error) {
	detail := err.Error()
	hasHan := strings.ContainsFunc(detail, func(character rune) bool {
		return unicode.Is(unicode.Han, character)
	})
	hasASCII := strings.ContainsFunc(detail, func(character rune) bool {
		return (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z')
	})
	if (textassets.ActiveLocale() == textassets.DefaultLocale && hasHan) ||
		(textassets.ActiveLocale() == textassets.LegacyLocale && !hasHan && hasASCII) {
		key := "ledger.localized_detail_unavailable"
		arguments := []any(nil)
		if facts := textassets.DiagnosticFacts(detail); facts != "" {
			key = "ledger.localized_detail_with_facts"
			arguments = []any{facts}
		}
		localized, localizedErr := textassets.Message(textassets.ActiveLocale(), key, arguments...)
		if localizedErr != nil {
			return
		}
		detail = localized
	}
	message, messageErr := textassets.Message(textassets.ActiveLocale(), key, detail)
	if messageErr == nil {
		fmt.Fprintln(os.Stderr, message)
	}
}

// enrichEvent补齐所有新事件都应具备的基础观测字段。
func enrichEvent(ev *Event) {
	if ev.SchemaVersion == 0 {
		ev.SchemaVersion = EventSchemaVersion
	}
	if ev.EventID == "" {
		ev.EventID = newEventID()
	}

	ev.Ts = time.Now().UTC().Format(time.RFC3339Nano)

	if ev.Result == "" {
		ev.Result = ResultOK
	}

	fillContextField(&ev.ExperimentID, "AOCI_EXPERIMENT_ID")
	fillContextField(&ev.TaskID, "AOCI_TASK_ID")
	fillContextField(&ev.ExperimentGroup, "AOCI_EXPERIMENT_GROUP")
	fillContextField(&ev.RunID, "AOCI_RUN_ID")
	fillContextField(&ev.SessionID, "AOCI_SESSION_ID")
	fillContextField(&ev.AgentModel, "AOCI_AGENT_MODEL")
	fillContextField(&ev.MemoryMode, "AOCI_MEMORY_MODE")
	fillContextField(&ev.RepositoryHead, "AOCI_REPOSITORY_HEAD")
	fillContextField(&ev.BinaryVersion, "AOCI_BINARY_VERSION")
}

// fillContextField只在调用方没有提供事实时读取实验环境声明。
func fillContextField(target *string, environmentName string) {
	if *target != "" {
		return
	}

	*target = normalizeContextValue(os.Getenv(environmentName))
}

// normalizeContextValue防止实验标签把换行或超长文本带入JSONL。
func normalizeContextValue(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if value == "" {
		return ""
	}

	const maximumRunes = 256

	runes := []rune(value)
	if len(runes) > maximumRunes {
		runes = runes[:maximumRunes]
	}

	return string(runes)
}

// newEventID生成不包含仓库内容的随机事件标识。
// 系统随机源异常时使用时间、PID摘要作为不阻断主流程的后备。
func newEventID() string {
	randomBytes := make([]byte, 12)
	if _, err := rand.Read(randomBytes); err == nil {
		return hex.EncodeToString(randomBytes)
	}

	fallback := sha256.Sum256(
		[]byte(
			fmt.Sprintf(
				"%d:%d",
				time.Now().UnixNano(),
				os.Getpid(),
			),
		),
	)

	return hex.EncodeToString(fallback[:12])
}

// Recent读取最近n条有效事件(时间正序返回)。
// 返回(事件列表,损坏行数);文件缺失返回空列表零损坏。
func Recent(root string, n int) ([]Event, int) {
	file, err := os.Open(ledgerPath(root))
	if err != nil {
		return []Event{}, 0
	}
	defer file.Close()

	var all []Event

	corrupt := 0

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var event Event
		if err := json.Unmarshal(line, &event); err != nil {
			corrupt++
			continue
		}

		all = append(all, event)
	}

	if n > 0 && len(all) > n {
		all = all[len(all)-n:]
	}
	if all == nil {
		all = []Event{}
	}

	return all, corrupt
}

// EstimateTokens粗略token估算，仅供趋势参考。
func EstimateTokens(text string) int {
	return utf8.RuneCountInString(text) * 6 / 10
}

// EndpointHash计算端点URL或任意敏感标识的脱敏哈希。
// 空输入返回空串。
func EndpointHash(baseURL string) string {
	if baseURL == "" {
		return ""
	}

	sum := sha256.Sum256([]byte(baseURL))

	return hex.EncodeToString(sum[:])[:16]
}
