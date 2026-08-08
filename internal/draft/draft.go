// 草稿区管理: .aoci/drafts/<run_id>/ 的 run 生命周期、文件存取与审计状态。
//
// 索引条目: draft.go[WDF8M]
//
// 定位:
// 一切模型或宿主Agent生成物先落草稿区,绝不直写正式索引或策展资产。
// 正式apply由CLI上层显式执行,本包只提供确定性的草稿存取、manifest往返
// 和审计记录原语。
//
// 支持三种草稿:
//   - header: 索引头部候选;
//   - entries: 文件级索引条目候选;
//   - curation: 文件级include/exclude语义策展候选。
//
// P-23 草稿审计状态分层:
//  1. Generation state:
//     Manifest.Entries保存初始生成状态及源码指纹,后续人工改稿绝不覆盖。
//  2. Review state:
//     Manifest.Reviews追加每次check/diff的草稿SHA-256、时间和校验摘要。
//  3. Application state:
//     Manifest.Applications追加每次apply的草稿SHA-256和应用结果。
//
// 三层均为追加式历史,不得把最终状态反写覆盖生成状态。
package draft

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/runmanifest"
)

const (
	KindHeader   = runmanifest.KindHeader
	KindEntries  = runmanifest.KindEntries
	KindCuration = runmanifest.KindCuration
)

const (
	GenerationSourceHostAgent = "host_agent"
	GenerationSourceEndpoint  = "endpoint"
)

const (
	ManifestFileName     = "manifest.json"
	HeaderFileName       = "header.txt"
	HeaderIntentFileName = "header-intent.txt"
	LocaleIndexFileName  = "locale-index.txt"
	CurationFileName     = "curation.json"
	PromptFileName       = "prompt.txt"
)

const (
	ReviewActionCheck = "check"
	ReviewActionDiff  = "diff"
)

const (
	// RunResolutionRecovered means the original postimage is still current and
	// recovery only completed Baseline, Application, and audit side effects.
	RunResolutionRecovered = runmanifest.ResolutionRecovered

	// RunResolutionSuperseded means a proven later AOCI governance chain moved
	// past the old postimage without replaying the old candidate.
	RunResolutionSuperseded = runmanifest.ResolutionSuperseded

	// RunResolutionPending is a command result only. It must never be persisted
	// in Manifest.Resolutions.
	RunResolutionPending   = runmanifest.ResolutionPending
	RunResolutionZeroWrite = runmanifest.ResolutionZeroWrite

	ZeroWriteStepGenerationPlan = runmanifest.ZeroWriteStepGenerationPlan
	ZeroWriteReasonPlanGuard    = runmanifest.ZeroWriteReasonPlanGuard
	ZeroWriteReasonRecovery     = runmanifest.ZeroWriteReasonRecovery
)

var ErrNoDraft = errors.New("草稿区内没有符合条件的 run")

var runIDRe = regexp.MustCompile(`^[0-9]{8}T[0-9]{6}Z(-[0-9]+)?$`)

type EntryStatus = runmanifest.EntryStatus
type ReviewRecord = runmanifest.ReviewRecord
type ApplicationRecord = runmanifest.ApplicationRecord
type RunResolutionRecord = runmanifest.ResolutionRecord
type ZeroWriteClosure = runmanifest.ZeroWriteClosure
type Manifest = runmanifest.Manifest

func DraftsDir(root string) string {
	return filepath.Join(root, ".aoci", "drafts")
}

func RunDir(root, runID string) (string, error) {
	if !runIDRe.MatchString(runID) {
		return "", fmt.Errorf(
			"run_id 形态非法: %q(期望 20060102T150405Z 或带 -N 后缀)",
			runID,
		)
	}
	return filepath.Join(DraftsDir(root), runID), nil
}

func NewRun(root string) (string, error) {
	base := time.Now().UTC().Format("20060102T150405Z")
	candidate := base
	if err := os.MkdirAll(DraftsDir(root), 0o755); err != nil {
		return "", fmt.Errorf("创建草稿根目录失败: %w", err)
	}

	for i := 2; i <= 100; i++ {
		dir, err := RunDir(root, candidate)
		if err != nil {
			return "", err
		}
		// Mkdir的排他结果同时分配身份并保留目录，避免同秒并发调用先后
		// Stat都观察到“不存在”后共享同一run_id。后续WriteFile的MkdirAll
		// 只复用本调用已经保留的目录。
		if mkdirErr := os.Mkdir(dir, 0o755); mkdirErr == nil {
			return candidate, nil
		} else if !os.IsExist(mkdirErr) {
			return "", fmt.Errorf("保留草稿run_id失败(%s): %w", candidate, mkdirErr)
		}
		candidate = fmt.Sprintf("%s-%d", base, i)
	}

	return "", errors.New("run_id 冲突重试超限(同秒内 99 次运行,不应发生)")
}

func validateFileName(name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("草稿文件名为空")
	}
	if strings.ContainsAny(name, "/\\") ||
		name == "." ||
		name == ".." ||
		strings.Contains(name, "..") {
		return fmt.Errorf(
			"草稿文件名非法(仅允许单层简单文件名): %q",
			name,
		)
	}
	return nil
}

func WriteFile(root, runID, name string, data []byte) error {
	if err := validateFileName(name); err != nil {
		return err
	}

	dir, err := RunDir(root, runID)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建草稿目录失败: %w", err)
	}

	return afs.AtomicWrite(filepath.Join(dir, name), data)
}

func ReadFile(root, runID, name string) ([]byte, error) {
	if err := validateFileName(name); err != nil {
		return nil, err
	}

	dir, err := RunDir(root, runID)
	if err != nil {
		return nil, err
	}

	return os.ReadFile(filepath.Join(dir, name))
}

// ReadFilesSnapshot 一次性读取一组草稿文件并计算稳定SHA-256。
//
// 返回内容和摘要来自同一轮读取,供check/diff/apply使用同一内存快照,
// 避免“先算hash、后重读文件”形成TOCTOU。顺序由names决定并进入哈希。
func ReadFilesSnapshot(
	root,
	runID string,
	names []string,
) (map[string][]byte, string, error) {
	contents := make(map[string][]byte, len(names))
	hash := sha256.New()

	for _, name := range names {
		if err := validateFileName(name); err != nil {
			return nil, "", err
		}

		data, err := ReadFile(root, runID, name)
		if err != nil {
			return nil, "", fmt.Errorf(
				"读取草稿快照失败(%s): %w",
				name,
				err,
			)
		}

		contents[name] = data
		_, _ = hash.Write([]byte(name))
		_, _ = hash.Write([]byte{0})
		_, _ = hash.Write(data)
		_, _ = hash.Write([]byte{0})
	}

	return contents, hex.EncodeToString(hash.Sum(nil)), nil
}

// HashFiles保留纯摘要调用面,内部复用ReadFilesSnapshot单一算法。
func HashFiles(root, runID string, names []string) (string, error) {
	_, hash, err := ReadFilesSnapshot(root, runID, names)
	return hash, err
}

func SaveManifest(root string, manifest *Manifest) error {
	if manifest == nil {
		return errors.New("manifest 为空")
	}
	return withManifestLock(root, manifest.RunID, func() error {
		return saveManifestUnlocked(root, manifest)
	})
}

func saveManifestUnlocked(root string, manifest *Manifest) error {
	if manifest == nil {
		return errors.New("manifest 为空")
	}
	if !runIDRe.MatchString(manifest.RunID) {
		return fmt.Errorf(
			"manifest.run_id 形态非法: %q",
			manifest.RunID,
		)
	}
	if manifest.Kind != KindHeader &&
		manifest.Kind != KindEntries &&
		manifest.Kind != KindCuration {
		return fmt.Errorf(
			"manifest.kind 非法: %q(期望 %s/%s/%s)",
			manifest.Kind,
			KindHeader,
			KindEntries,
			KindCuration,
		)
	}
	if manifest.CreatedAt == "" {
		manifest.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}

	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return fmt.Errorf("manifest 序列化失败: %w", err)
	}

	return WriteFile(
		root,
		manifest.RunID,
		ManifestFileName,
		append(data, '\n'),
	)
}

func withManifestLock(root, runID string, operation func() error) error {
	if !runIDRe.MatchString(runID) {
		return fmt.Errorf("run_id 形态非法: %q", runID)
	}
	lock, err := afs.AcquireManifestLock(root, runID)
	if err != nil {
		return fmt.Errorf("获取Manifest审计锁失败: %w", err)
	}
	operationErr := operation()
	releaseErr := lock.Release()
	if operationErr != nil {
		return operationErr
	}
	if releaseErr != nil {
		return fmt.Errorf("释放Manifest审计锁失败: %w", releaseErr)
	}
	return nil
}

// AppendReview向manifest追加一次check/diff审阅记录。
func AppendReview(root, runID string, record ReviewRecord) error {
	if record.Action != ReviewActionCheck &&
		record.Action != ReviewActionDiff {
		return fmt.Errorf(
			"review.action 非法: %q(期望 %s/%s)",
			record.Action,
			ReviewActionCheck,
			ReviewActionDiff,
		)
	}

	return withManifestLock(root, runID, func() error {
		manifest, err := LoadManifest(root, runID)
		if err != nil {
			return err
		}
		if record.At == "" {
			record.At = time.Now().UTC().Format(time.RFC3339)
		}
		manifest.Reviews = append(manifest.Reviews, record)
		return saveManifestUnlocked(root, manifest)
	})
}

// AppendApplication追加一次apply结果。
func AppendApplication(
	root,
	runID string,
	record ApplicationRecord,
	markApplied bool,
) error {
	return withManifestLock(root, runID, func() error {
		manifest, err := LoadManifest(root, runID)
		if err != nil {
			return err
		}
		if record.At == "" {
			record.At = time.Now().UTC().Format(time.RFC3339)
		}
		manifest.Applications = append(manifest.Applications, record)
		if markApplied {
			manifest.AppliedAt = record.At
		}
		return saveManifestUnlocked(root, manifest)
	})
}

// AppendRunResolution appends a proven Entries terminal state. An identical
// record is idempotent; a conflicting terminal state fails closed.
func AppendRunResolution(
	root,
	runID string,
	record RunResolutionRecord,
) error {
	return withManifestLock(root, runID, func() error {
		manifest, err := LoadManifest(root, runID)
		if err != nil {
			return err
		}
		if manifest.Kind != KindEntries {
			return fmt.Errorf("entries_run_resolution_wrong_manifest_kind")
		}
		if record.At == "" {
			record.At = time.Now().UTC().Format(time.RFC3339Nano)
		}
		if err := validateRunResolutionRecord(record); err != nil {
			return err
		}
		if existing, ok := TerminalRunResolution(manifest); ok {
			existingData, _ := json.Marshal(existing)
			currentData, _ := json.Marshal(record)
			if string(existingData) == string(currentData) {
				return nil
			}
			return fmt.Errorf("entries_run_resolution_conflict")
		}
		if len(manifest.Resolutions) > 0 {
			return fmt.Errorf("entries_run_resolution_ambiguous")
		}
		manifest.Resolutions = append(manifest.Resolutions, record)
		return saveManifestUnlocked(root, manifest)
	})
}

// AppendZeroWriteClosure records a proven pre-Apply failure without deleting
// the draft or manufacturing post-write recovery evidence. Identical closure
// facts are idempotent; any competing lifecycle evidence fails closed.
func AppendZeroWriteClosure(
	root,
	runID string,
	record ZeroWriteClosure,
) error {
	return withManifestLock(root, runID, func() error {
		manifest, err := LoadManifest(root, runID)
		if err != nil {
			return err
		}
		if manifest.Kind != KindEntries || manifest.AppliedAt != "" ||
			len(manifest.Reviews) != 0 || len(manifest.Applications) != 0 ||
			len(manifest.Resolutions) != 0 {
			return fmt.Errorf("entries_zero_write_closure_conflict")
		}
		if record.At == "" {
			record.At = time.Now().UTC().Format(time.RFC3339Nano)
		}
		if err := runmanifest.ValidateZeroWriteClosure(record); err != nil {
			return err
		}
		if record.DraftHash != manifest.GenerationHash ||
			record.PreIndexSHA256 != manifest.IndexSHA256 {
			return fmt.Errorf("entries_zero_write_closure_binding_invalid")
		}
		if existing, ok := runmanifest.ZeroWriteClosed(manifest); ok {
			existingData, _ := json.Marshal(existing)
			currentData, _ := json.Marshal(record)
			if string(existingData) == string(currentData) {
				return nil
			}
			return fmt.Errorf("entries_zero_write_closure_conflict")
		}
		if len(manifest.ZeroWriteClosures) != 0 {
			return fmt.Errorf("entries_zero_write_closure_ambiguous")
		}
		manifest.ZeroWriteClosures = append(manifest.ZeroWriteClosures, record)
		return saveManifestUnlocked(root, manifest)
	})
}

// AppendRecoveredRunCompletion atomically appends the zero-write recovery
// Application and its proof-bearing terminal record in one Manifest rewrite.
func AppendRecoveredRunCompletion(
	root,
	runID string,
	application ApplicationRecord,
	resolution RunResolutionRecord,
) error {
	return withManifestLock(root, runID, func() error {
		manifest, err := LoadManifest(root, runID)
		if err != nil {
			return err
		}
		if manifest.Kind != KindEntries {
			return fmt.Errorf("entries_recovered_completion_wrong_manifest_kind")
		}
		if existing, ok := TerminalRunResolution(manifest); ok {
			if existing.Status == resolution.Status &&
				existing.TransactionID == resolution.TransactionID &&
				existing.CurrentIndexSHA256 == resolution.CurrentIndexSHA256 {
				return nil
			}
			return fmt.Errorf("entries_run_resolution_conflict")
		}
		if len(manifest.Resolutions) > 0 {
			return fmt.Errorf("entries_run_resolution_ambiguous")
		}
		if application.At == "" {
			application.At = time.Now().UTC().Format(time.RFC3339Nano)
		}
		if resolution.At == "" {
			resolution.At = application.At
		}
		if application.DraftHash == "" || application.PathsCount <= 0 ||
			application.Applied != 0 || application.Recovered != application.PathsCount ||
			application.Rejected != 0 || application.RejectKinds != "" {
			return fmt.Errorf("entries_recovered_application_invalid")
		}
		if err := validateRunResolutionRecord(resolution); err != nil {
			return err
		}
		if resolution.Status != RunResolutionRecovered {
			return fmt.Errorf("entries_recovered_completion_status_invalid")
		}
		alreadyApplied := false
		if manifest.AppliedAt != "" {
			for _, existingApplication := range manifest.Applications {
				if existingApplication.At == manifest.AppliedAt &&
					existingApplication.DraftHash == application.DraftHash &&
					existingApplication.PathsCount == application.PathsCount &&
					existingApplication.Applied == 0 &&
					existingApplication.Recovered == application.Recovered &&
					existingApplication.Rejected == 0 &&
					existingApplication.RejectKinds == "" {
					alreadyApplied = true
					application.At = existingApplication.At
					resolution.At = existingApplication.At
					break
				}
			}
			if !alreadyApplied {
				return fmt.Errorf("entries_recovered_application_binding_conflict")
			}
		}
		if !alreadyApplied {
			manifest.Applications = append(manifest.Applications, application)
			manifest.AppliedAt = application.At
		}
		manifest.Resolutions = append(manifest.Resolutions, resolution)
		return saveManifestUnlocked(root, manifest)
	})
}

// TerminalRunResolution returns the final structurally valid terminal record.
func TerminalRunResolution(manifest *Manifest) (RunResolutionRecord, bool) {
	return runmanifest.TerminalResolution(manifest)
}

// StoredTerminalRunResolution also verifies the archived transaction and every
// later receipt. Check, Guide, and Maintain never trust a Manifest-only claim.
func StoredTerminalRunResolution(root string, manifest *Manifest) (RunResolutionRecord, bool) {
	return runmanifest.StoredTerminalResolution(root, manifest)
}

func ZeroWriteRunClosed(manifest *Manifest) (ZeroWriteClosure, bool) {
	return runmanifest.ZeroWriteClosed(manifest)
}

func validateRunResolutionRecord(record RunResolutionRecord) error {
	return runmanifest.ValidateResolution(record)
}

// MarkApplied保留给尚未接入ApplicationRecord的旧调用方和header工序。
func MarkApplied(root, runID string) error {
	return withManifestLock(root, runID, func() error {
		manifest, err := LoadManifest(root, runID)
		if err != nil {
			return err
		}
		manifest.AppliedAt = time.Now().UTC().Format(time.RFC3339)
		return saveManifestUnlocked(root, manifest)
	})
}

// ClearLegacyHeaderIntent removes the transitional Manifest field after a
// completed Header Apply. New runs bind intent through HeaderIntentFileName so
// stable readers that strictly decode the older Manifest schema remain usable.
func ClearLegacyHeaderIntent(root, runID string) error {
	return withManifestLock(root, runID, func() error {
		manifest, err := LoadManifest(root, runID)
		if err != nil {
			return err
		}
		if manifest.Kind != KindHeader {
			return fmt.Errorf("header_intent_wrong_manifest_kind")
		}
		if manifest.HeaderIntent == "" {
			return nil
		}
		manifest.HeaderIntent = ""
		return saveManifestUnlocked(root, manifest)
	})
}

func ListRunIDs(root string) ([]string, error) {
	entries, err := os.ReadDir(DraftsDir(root))
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, fmt.Errorf("读取草稿区失败: %w", err)
	}

	result := []string{}
	for _, entry := range entries {
		if entry.IsDir() && runIDRe.MatchString(entry.Name()) {
			result = append(result, entry.Name())
		}
	}

	sort.Strings(result)
	return result, nil
}

func LatestRunID(root, kind string) (string, error) {
	runIDs, err := ListRunIDs(root)
	if err != nil {
		return "", err
	}

	for i := len(runIDs) - 1; i >= 0; i-- {
		if kind == "" {
			return runIDs[i], nil
		}

		manifest, loadErr := LoadManifest(root, runIDs[i])
		if loadErr != nil {
			continue
		}
		if manifest.Kind == kind {
			return runIDs[i], nil
		}
	}

	return "", ErrNoDraft
}

// LatestPendingRun scans past newer completed runs and returns the newest run
// without AppliedAt or a fully stored terminal proof. Corruption fails closed.
func LatestPendingRun(root, kind string) (string, error) {
	return runmanifest.LatestPending(root, kind)
}
