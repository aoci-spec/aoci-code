// Package cognitiontxn contains the operation-neutral governance primitives
// shared by Cognition layout transactions. Bootstrap and Legacy Migration keep
// separate contracts and state machines, but they must not grow parallel lock,
// staging, pending-gate, Ledger, or immutable-intent implementations.
package cognitiontxn

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/ledger"
)

var auditActorPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@-]{1,127}$`)

const (
	StatePreimage       = "preimage"
	StatePostimage      = "postimage"
	StateUnknown        = "unknown"
	StateWrongType      = "wrong_type"
	StateMissingStaging = "missing_staging"
)

type PendingTransaction struct {
	Operation string
	ID        string
	Filename  string
}

type Postimage struct {
	Path string
	SHA  string
	Data []byte
}

type StagedPostimage struct {
	Path       string `json:"path"`
	SHA256     string `json:"sha256"`
	ByteSize   int64  `json:"byte_size"`
	StagingRel string `json:"staging_rel"`
}

// Pending reports every active top-level Cognition transaction intent. The
// history directory and per-transaction receipt directories are not active.
func Pending(root string) ([]PendingTransaction, error) {
	entries, err := os.ReadDir(filepath.Join(root, ".aoci", "transactions"))
	if errors.Is(err, os.ErrNotExist) {
		return []PendingTransaction{}, nil
	}
	if err != nil {
		return nil, err
	}
	result := []PendingTransaction{}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".json")
		operation := ""
		id := ""
		for _, prefix := range []string{"database-bootstrap", "bootstrap", "migration", "reversal", "scope", "locale"} {
			if strings.HasPrefix(name, prefix+"-") {
				operation = prefix
				id = strings.TrimPrefix(name, prefix+"-")
				break
			}
		}
		if operation == "" {
			continue
		}
		result = append(result, PendingTransaction{Operation: operation, ID: id, Filename: entry.Name()})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Filename < result[j].Filename })
	return result, nil
}

func PendingForOperation(root, operation string) ([]string, error) {
	all, err := Pending(root)
	if err != nil {
		return nil, err
	}
	ids := []string{}
	for _, item := range all {
		if item.Operation == operation {
			ids = append(ids, item.ID)
		}
	}
	return ids, nil
}

func RejectOtherPending(root, allowedFilename string) error {
	pending, err := Pending(root)
	if err != nil {
		return err
	}
	for _, item := range pending {
		if item.Filename != allowedFilename {
			return fmt.Errorf("other_pending_aoci_transaction: %s", item.Filename)
		}
	}
	return nil
}

func EnsureRuntimeBoundary(root, relativePath string, data []byte) error {
	for _, rel := range []string{".aoci", ".aoci/transactions", ".aoci/transactions/history"} {
		if err := EnsureSafeDirectory(root, rel); err != nil {
			return err
		}
	}
	path := filepath.Join(root, filepath.FromSlash(relativePath))
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("runtime_boundary_wrong_type")
		}
		existing, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Equal(existing, data) {
			return fmt.Errorf("runtime_boundary_conflict")
		}
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := afs.AtomicCreateCAS(path, data); err != nil {
		if existing, readErr := os.ReadFile(path); readErr == nil && bytes.Equal(existing, data) {
			return nil
		}
		return err
	}
	return nil
}

func EnsureSafeDirectory(root, relative string) error {
	current := root
	for _, component := range strings.Split(filepath.ToSlash(relative), "/") {
		if component == "" || component == "." || component == ".." {
			return fmt.Errorf("runtime_path_invalid")
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, 0o755); err != nil {
				return err
			}
			info, err = os.Lstat(current)
		}
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("runtime_directory_unsafe: %s", relative)
		}
	}
	return nil
}

func Stage(root, operation, transactionID string, posts []Postimage, fault func(string) error) ([]StagedPostimage, error) {
	base := filepath.ToSlash(filepath.Join(".aoci", "transactions", operation+"-"+transactionID))
	if err := EnsureSafeDirectory(root, base); err != nil {
		return nil, err
	}
	if err := EnsureSafeDirectory(root, filepath.ToSlash(filepath.Join(base, "staging"))); err != nil {
		return nil, err
	}
	result := make([]StagedPostimage, 0, len(posts))
	for index, post := range posts {
		if post.SHA != SHA256(post.Data) {
			return nil, fmt.Errorf("staging_postimage_identity_invalid: %s", post.Path)
		}
		rel := filepath.ToSlash(filepath.Join(base, "staging", fmt.Sprintf("%02d.post", index)))
		path := filepath.Join(root, filepath.FromSlash(rel))
		state, _, err := Classify(path, "", post.SHA, true)
		if err != nil {
			return nil, err
		}
		if state == StatePostimage {
			result = append(result, StagedPostimage{Path: post.Path, SHA256: post.SHA, ByteSize: int64(len(post.Data)), StagingRel: rel})
			continue
		}
		if state != StatePreimage {
			return nil, fmt.Errorf("staging_conflict: %s", rel)
		}
		if fault != nil {
			if err := fault("before_stage_" + fmt.Sprint(index)); err != nil {
				return nil, err
			}
		}
		if err := afs.AtomicCreateCAS(path, post.Data); err != nil {
			return nil, err
		}
		if fault != nil {
			if err := fault("after_stage_" + fmt.Sprint(index)); err != nil {
				return nil, err
			}
		}
		result = append(result, StagedPostimage{Path: post.Path, SHA256: post.SHA, ByteSize: int64(len(post.Data)), StagingRel: rel})
	}
	return result, nil
}

func ReadStaged(root string, staged []StagedPostimage, targetPath string) ([]byte, error) {
	for _, item := range staged {
		if item.Path != targetPath {
			continue
		}
		path := filepath.Join(root, filepath.FromSlash(item.StagingRel))
		info, err := os.Lstat(path)
		if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("staging_invalid: %s", targetPath)
		}
		data, err := os.ReadFile(path)
		if err != nil || SHA256(data) != item.SHA256 || int64(len(data)) != item.ByteSize {
			return nil, fmt.Errorf("staging_invalid: %s", targetPath)
		}
		return data, nil
	}
	return nil, fmt.Errorf("staging_missing: %s", targetPath)
}

// Classify compares one regular-file path against exact pre/post bytes. When
// absentPreimage is true, a missing path is the preimage. Otherwise both
// preimageSHA and postimageSHA must be non-empty.
func Classify(path, preimageSHA, postimageSHA string, absentPreimage bool) (string, string, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		if absentPreimage {
			return StatePreimage, "", nil
		}
		return StateUnknown, "", nil
	}
	if err != nil {
		return StateUnknown, "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return StateWrongType, "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return StateUnknown, "", err
	}
	digest := SHA256(data)
	if digest == postimageSHA {
		return StatePostimage, digest, nil
	}
	if !absentPreimage && digest == preimageSHA {
		return StatePreimage, digest, nil
	}
	return StateUnknown, digest, nil
}

func SaveImmutable(path string, data []byte) error {
	if err := afs.AtomicCreateCAS(path, data); err != nil {
		exists, inspectErr := inspectImmutableTarget(path, data)
		if inspectErr == nil && exists {
			return nil
		}
		if inspectErr != nil {
			return fmt.Errorf("%w: %v", err, inspectErr)
		}
		return err
	}
	return nil
}

// ValidateImmutableTarget permits only an absent target or an exact regular
// file. Callers use it before publishing transaction participants so a bad
// terminal path cannot strand an otherwise completed transaction.
func ValidateImmutableTarget(path string, expected []byte) error {
	_, err := inspectImmutableTarget(path, expected)
	return err
}

func inspectImmutableTarget(path string, expected []byte) (bool, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return true, fmt.Errorf("immutable_target_wrong_type: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return true, err
	}
	confirmed, err := os.Lstat(path)
	if err != nil || confirmed.Mode()&os.ModeSymlink != 0 || !confirmed.Mode().IsRegular() || !os.SameFile(info, confirmed) {
		return true, fmt.Errorf("immutable_target_changed: %s", path)
	}
	if !bytes.Equal(data, expected) {
		return true, fmt.Errorf("immutable_target_conflict: %s", path)
	}
	return true, nil
}

func ArchiveImmutable(active, archive string, expected []byte) error {
	activeExists, err := inspectImmutableTarget(active, expected)
	if err != nil {
		return err
	}
	archiveExists, err := inspectImmutableTarget(archive, expected)
	if err != nil {
		return err
	}
	if !activeExists {
		if archiveExists {
			return nil
		}
		return &os.PathError{Op: "lstat", Path: active, Err: os.ErrNotExist}
	}
	if !archiveExists {
		if err := SaveImmutable(archive, expected); err != nil {
			return err
		}
	}
	archiveExists, err = inspectImmutableTarget(archive, expected)
	if err != nil {
		return fmt.Errorf("immutable_archive_invalid: %w", err)
	}
	if !archiveExists {
		return fmt.Errorf("immutable_archive_missing: %s", archive)
	}
	activeExists, err = inspectImmutableTarget(active, expected)
	if err != nil {
		return err
	}
	if !activeExists {
		return nil
	}
	if err := os.Remove(active); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// EnsureLedger is the common terminal-event idempotency and read-back proof.
func EnsureLedger(root string, enabled bool, expected ledger.Event) error {
	if !enabled {
		return fmt.Errorf("cognition_transaction_ledger_required")
	}
	events, corrupt := ledger.Recent(root, 0)
	if corrupt != 0 {
		return fmt.Errorf("cognition_transaction_ledger_corrupt: %d", corrupt)
	}
	for _, event := range events {
		if event.Op != expected.Op || event.RecoveryTransactionID != expected.RecoveryTransactionID {
			continue
		}
		if terminalEventEqual(event, expected) {
			return nil
		}
		return fmt.Errorf("cognition_transaction_ledger_terminal_event_conflict")
	}
	ledger.Append(root, true, expected)
	events, corrupt = ledger.Recent(root, 0)
	if corrupt != 0 {
		return fmt.Errorf("cognition_transaction_ledger_corrupt: %d", corrupt)
	}
	for _, event := range events {
		if event.Op == expected.Op && event.RecoveryTransactionID == expected.RecoveryTransactionID && terminalEventEqual(event, expected) {
			return nil
		}
	}
	return fmt.Errorf("cognition_transaction_ledger_terminal_event_missing")
}

func terminalEventEqual(actual, expected ledger.Event) bool {
	return actual.Result == expected.Result && actual.Source == expected.Source &&
		actual.AppliedCount == expected.AppliedCount && actual.RecoveredCount == expected.RecoveredCount &&
		actual.BaselineSHA256 == expected.BaselineSHA256 && actual.IndexSHA256 == expected.IndexSHA256 &&
		actual.PreIndexSHA256 == expected.PreIndexSHA256 && actual.PostIndexSHA256 == expected.PostIndexSHA256
}

func SHA256(data []byte) string {
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:])
}

func ValidAuditActor(actor string) bool {
	return auditActorPattern.MatchString(strings.TrimSpace(actor))
}
