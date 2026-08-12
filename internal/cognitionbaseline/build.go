// Package cognitionbaseline builds the one existing cognition Baseline
// postimage for both Bootstrap and Legacy Migration. It is governance-only and
// consumes already validated model-authored Volume bytes.
package cognitionbaseline

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/dbcognition"
)

func BuildVolumePostimage(root string, plan *cognitionplan.Plan, projected *cognition.Set, assets map[string]cognitionplan.CandidateAsset, timestamp string) (*baseline.Baseline, []baseline.DatabaseCognitionBinding, error) {
	files := map[string]baseline.Fingerprint{}
	if _, hasCode := assets["code"]; hasCode {
		for _, object := range plan.Inventory {
			if !object.Eligible {
				continue
			}
			path := filepath.Join(root, filepath.FromSlash(object.Path))
			info, err := os.Lstat(path)
			if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
				return nil, nil, fmt.Errorf("cognition_source_guard_invalid: %s", object.Path)
			}
			fingerprint, err := baseline.HashFile(path)
			if err != nil || fingerprint.SHA256 != object.SourceSHA256 {
				return nil, nil, fmt.Errorf("cognition_source_guard_drift: %s", object.Path)
			}
			files[object.Path] = fingerprint
		}
		files["aoci.code.txt"] = baseline.HashBytes("aoci.code.txt", []byte(assets["code"].Content))
		if plan.Mapping != nil {
			for _, record := range plan.Mapping.Records {
				if !record.LegacySelfEntry || record.TargetAsset != "code" || !strings.HasPrefix(record.TargetRef, "code:") {
					continue
				}
				sourcePath := filepath.ToSlash(strings.TrimPrefix(record.TargetRef, "code:"))
				if sourcePath == "" {
					return nil, nil, fmt.Errorf("cognition_self_entry_target_invalid")
				}
				if _, exists := files[sourcePath]; exists {
					continue
				}
				rootAsset, exists := assets["root"]
				if !exists || sourcePath != "aoci.txt" {
					return nil, nil, fmt.Errorf("cognition_self_entry_source_unbound: %s", record.TargetRef)
				}
				files[sourcePath] = baseline.HashBytes(sourcePath, []byte(rootAsset.Content))
			}
		}
	}
	bindings := []baseline.DatabaseCognitionBinding{}
	if _, hasDatabase := assets["database"]; hasDatabase {
		files["aoci.database.txt"] = baseline.HashBytes("aoci.database.txt", []byte(assets["database"].Content))
		evidenceByRef := map[string]cognitionplan.EvidenceObject{}
		for _, evidence := range plan.Evidence {
			if !strings.HasSuffix(evidence.ObjectRef, "/-") {
				evidenceByRef[evidence.ObjectRef] = evidence
			}
		}
		for _, object := range projected.Volumes["database"].Objects {
			evidence, exists := evidenceByRef[object.CanonicalRef]
			if !exists {
				return nil, nil, fmt.Errorf("cognition_database_binding_missing: %s", object.CanonicalRef)
			}
			bindings = append(bindings, baseline.DatabaseCognitionBinding{
				ObjectRef: object.CanonicalRef, SourceID: evidence.SourceID,
				EvidenceVersion: evidence.EvidenceVersion, TableEvidenceSHA256: evidence.TableEvidenceSHA256,
				EntrySHA256: dbcognition.EntrySHA256(object.CanonicalLine),
			})
		}
		sort.Slice(bindings, func(i, j int) bool { return bindings[i].ObjectRef < bindings[j].ObjectRef })
	}
	value, err := baseline.NewBaselineAt(files, timestamp)
	if err != nil {
		return nil, nil, err
	}
	for _, binding := range bindings {
		if err := baseline.UpdateDatabaseCognitionBinding(value, binding); err != nil {
			return nil, nil, err
		}
	}
	return value, bindings, nil
}
