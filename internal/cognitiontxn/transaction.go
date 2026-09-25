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

// receiptKinds lists every receipt kind AOCI writes directly under
// .aoci/transactions/, in match order (database-bootstrap before bootstrap,
// which is its suffix). remove, entries, and header are the MCP write
// receipts. Until v0.1.0-rc15 Pending recognised only the first five, while
// the Overview guard stopped on any receipt file: a stale remove receipt
// blocked full cognition delivery with Verify, Guide, and Maintain reporting
// the repository aligned and nothing pointing at the file (#74).
var receiptKinds = []string{"database-bootstrap", "bootstrap", "migration", "reversal", "scope", "remove", "entries", "header"}

// OperationUnknown marks a .json file under .aoci/transactions/ that no AOCI
// receipt kind wrote. It is still pending: a foreign file in the transaction
// directory is a state nobody can prove, so every consumer fails closed on it
// and the Guide asks for it to be moved out by hand.
const OperationUnknown = "unknown"

// LayoutTransaction reports whether the receipt belongs to a transaction that
// rewrites the cognition layout itself (bootstrap, migration, reversal, scope,
// database bootstrap). While one is pending no MCP tool can load the layout;
// an MCP write receipt (remove, entries, header) is instead closed by the tool
// that wrote it, which must be able to load the repository to do so.
func (t PendingTransaction) LayoutTransaction() bool {
	switch t.Operation {
	case "database-bootstrap", "bootstrap", "migration", "reversal", "scope":
		return true
	}
	return false
}

// PendingLayout is Pending narrowed to layout transactions.
func PendingLayout(root string) ([]PendingTransaction, error) {
	all, err := Pending(root)
	if err != nil {
		return nil, err
	}
	layout := make([]PendingTransaction, 0, len(all))
	for _, item := range all {
		if item.LayoutTransaction() {
			layout = append(layout, item)
		}
	}
	return layout, nil
}

// ParseReceiptName reads a top-level receipt filename into its kind and id.
// ok is false for names that are not receipt files at all.
func ParseReceiptName(filename string) (PendingTransaction, bool) {
	if !strings.HasSuffix(filename, ".json") {
		return PendingTransaction{}, false
	}
	name := strings.TrimSuffix(filename, ".json")
	for _, prefix := range receiptKinds {
		if strings.HasPrefix(name, prefix+"-") {
			return PendingTransaction{Operation: prefix, ID: strings.TrimPrefix(name, prefix+"-"), Filename: filename}, true
		}
	}
	return PendingTransaction{Operation: OperationUnknown, ID: name, Filename: filename}, true
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
		receipt, ok := ParseReceiptName(entry.Name())
		if !ok {
			continue
		}
		result = append(result, receipt)
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

// RejectOtherPending refuses to start or resume a layout transaction while
// another layout transaction, or a file no receipt kind wrote, is pending. An
// MCP write receipt (remove, entries, header) does not block it: those
// receipts bind Volume images that the layout transaction may legitimately
// move past, and their closures judge the object afterwards. Refusing on them
// would leave a repository that carried both kinds across an upgrade with two
// closures that each wait for the other. Write tools check the whole
// directory themselves before they commit.
func RejectOtherPending(root, allowedFilename string) error {
	pending, err := Pending(root)
	if err != nil {
		return err
	}
	for _, item := range pending {
		if item.Filename == allowedFilename || !(item.LayoutTransaction() || item.Operation == OperationUnknown) {
			continue
		}
		return fmt.Errorf("other_pending_aoci_transaction: %s", item.Filename)
	}
	return nil
}

// OtherPending names the first pending receipt of any kind other than the
// caller's own; write tools refuse to commit over it.
func OtherPending(root, ownFilename string) (string, error) {
	pending, err := Pending(root)
	if err != nil {
		return "", err
	}
	for _, item := range pending {
		if item.Filename != ownFilename {
			return item.Filename, nil
		}
	}
	return "", nil
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
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(item.StagingRel)))
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
		if existing, readErr := os.ReadFile(path); readErr == nil && bytes.Equal(existing, data) {
			return nil
		}
		return err
	}
	return nil
}

func ArchiveImmutable(active, archive string, expected []byte) error {
	data, err := os.ReadFile(active)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if existing, archiveErr := os.ReadFile(archive); archiveErr == nil && bytes.Equal(existing, expected) {
				return nil
			}
		}
		return err
	}
	if !bytes.Equal(data, expected) {
		return fmt.Errorf("immutable_intent_changed")
	}
	if existing, readErr := os.ReadFile(archive); readErr == nil {
		if !bytes.Equal(existing, data) {
			return fmt.Errorf("immutable_archive_conflict")
		}
	} else if errors.Is(readErr, os.ErrNotExist) {
		if err := SaveImmutable(archive, data); err != nil {
			return err
		}
	} else {
		return readErr
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
