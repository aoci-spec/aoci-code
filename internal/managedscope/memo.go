package managedscope

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sync"
	"sync/atomic"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/fs"
)

// evaluationMemo remembers, per repository root, the last Evaluation and the
// digest of everything it was computed from. A governance call walks the
// tree anyway, to see what changed; evaluating the policy over that walk was
// half of the call and never changes while its inputs do not, so a process
// that has evaluated the same inventory under the same policy hands back a
// copy instead. Keyed on the inputs and never on time, the memo cannot serve
// a stale verdict: a file added, removed, ignored, excluded, opted in, a rule
// edited, a walk option changed, or the root layout moving all change the
// digest. Bounded by the roots a process serves, one record each.
//
// The record is never handed out. Every hit returns a copy of the slices, so
// a caller that reorders or trims what it got does not change the next
// answer; the Rule pointers inside are shared and treated as immutable, as
// Build already treats the policy it was given.
var evaluationMemo = struct {
	mu     sync.Mutex
	byRoot map[string]evaluationMemoRecord
}{byRoot: map[string]evaluationMemoRecord{}}

type evaluationMemoRecord struct {
	key   string
	value *Evaluation
}

// evaluationMemoHits and evaluationMemoMisses count the memo's answers for
// tests and diagnostics; a disabled memo counts nothing.
var evaluationMemoHits, evaluationMemoMisses atomic.Int64

// evaluationMemoKey digests every input of Build after the walk. The policy
// and walk options go through their JSON forms, the inventory lists are
// written string by string with separators no path can contain, and the
// layout enters as the string RootLayout returned.
func evaluationMemoKey(normalized Policy, walk fs.WalkOptions, layout string, curated []string, inventory *fs.SafeInventory) (string, error) {
	hash := sha256.New()
	encoder := json.NewEncoder(hash)
	for _, value := range []any{"managed-scope-evaluation-memo/v1", normalized, walk, layout, curated, inventory.Summary, inventory.Exclusions} {
		if err := encoder.Encode(value); err != nil {
			return "", err
		}
	}
	for _, list := range [][]string{inventory.ManagedCandidates, inventory.TrackedPaths, inventory.IgnoredPaths} {
		for _, rel := range list {
			_, _ = hash.Write([]byte(rel))
			_, _ = hash.Write([]byte{0})
		}
		_, _ = hash.Write([]byte{1})
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func lookupEvaluationMemo(root, key string) (*Evaluation, bool) {
	evaluationMemo.mu.Lock()
	record, ok := evaluationMemo.byRoot[root]
	evaluationMemo.mu.Unlock()
	if !ok || record.key != key {
		evaluationMemoMisses.Add(1)
		return nil, false
	}
	evaluationMemoHits.Add(1)
	return cloneEvaluation(record.value), true
}

func storeEvaluationMemo(root, key string, value *Evaluation) {
	evaluationMemo.mu.Lock()
	evaluationMemo.byRoot[root] = evaluationMemoRecord{key: key, value: cloneEvaluation(value)}
	evaluationMemo.mu.Unlock()
}

// cloneEvaluation copies the slices and keeps them non-nil, since an empty
// list is published as [] and not null.
func cloneEvaluation(value *Evaluation) *Evaluation {
	result := *value
	result.Index = clonePathEvaluations(value.Index)
	result.Observe = clonePathEvaluations(value.Observe)
	result.Exclude = clonePathEvaluations(value.Exclude)
	return &result
}

func clonePathEvaluations(values []PathEvaluation) []PathEvaluation {
	result := make([]PathEvaluation, len(values))
	copy(result, values)
	return result
}

func processMemoDisabled() bool { return baseline.ProcessMemoDisabled() }
