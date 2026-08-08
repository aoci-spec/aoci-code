package databasebootstrap

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/volumegovernance"
)

const databaseDescriptor = "#Volume: id=database kind=database path=aoci.database.txt format=table-fras-v2 depends=meta state=enabled"

var ErrNotReady = errors.New("database_bootstrap_not_ready")

func Prepare(root string, now time.Time) (*Preview, error) {
	pending, err := cognitiontxn.Pending(root)
	if err != nil {
		return nil, fmt.Errorf("database_bootstrap_recovery_state_unavailable")
	}
	if len(pending) != 0 {
		return nil, fmt.Errorf("database_bootstrap_recovery_pending")
	}
	return prepare(root, now.UTC().Truncate(time.Second))
}

func prepare(root string, preparedAt time.Time) (*Preview, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("database_bootstrap_repository_root_invalid")
	}
	cfg, err := config.LoadReadOnly(absRoot)
	if err != nil || cfg.IndexPath != "aoci.txt" {
		return nil, fmt.Errorf("database_bootstrap_configuration_invalid")
	}
	set, err := cognition.Load(absRoot, cfg.IndexPath)
	if err != nil || set == nil || set.LayoutMode != cognition.LayoutVolumesV1 || len(set.Errors) != 0 {
		return nil, fmt.Errorf("database_bootstrap_layout_invalid")
	}
	if set.Volumes[cognition.ScopeDatabase] != nil {
		return nil, fmt.Errorf("database_bootstrap_not_required")
	}
	if set.Volumes[cognition.ScopeCode] == nil || set.Volumes[cognition.ScopeCode].State != cognition.AssetPresent {
		return nil, fmt.Errorf("database_bootstrap_code_cognition_required")
	}
	state, exists, err := baseline.Load(absRoot)
	if err != nil || !exists || state == nil {
		return nil, fmt.Errorf("%w: database_bootstrap_baseline_required", ErrNotReady)
	}
	facts, err := volumegovernance.Assess(absRoot, cfg, set)
	if err != nil || !facts.StructureValid || !facts.ManagedScope.Aligned ||
		len(facts.CodeDrift.Missing)+len(facts.CodeDrift.Orphan)+len(facts.CodeDrift.Stale)+len(facts.CodeDrift.Unbaselined) != 0 ||
		facts.Code.AssetState != cognition.AssetPresent {
		return nil, fmt.Errorf("%w: database_bootstrap_code_governance_not_ready", ErrNotReady)
	}

	sources, err := dbevidence.NormalizeSources(cfg.DatabaseSources)
	if err != nil {
		return nil, fmt.Errorf("database_bootstrap_source_configuration_invalid")
	}
	evidenceBaseline, evidenceExists, err := dbevidence.LoadBaseline(absRoot)
	if err != nil || !evidenceExists {
		return nil, fmt.Errorf("%w: database_bootstrap_evidence_baseline_required", ErrNotReady)
	}
	accepted := make(map[string]dbevidence.BaselineSource, len(evidenceBaseline.Sources))
	for _, source := range evidenceBaseline.Sources {
		accepted[source.SourceID] = source
	}
	evidenceSources := []EvidenceSource{}
	for _, source := range sources {
		if !source.Enabled {
			continue
		}
		manifest, snapshot, snapshotExists, loadErr := dbevidence.LoadSnapshot(absRoot, source.SourceID)
		acceptedSource, acceptedExists := accepted[source.SourceID]
		if loadErr != nil || !snapshotExists || !dbevidence.SourceConfigMatchesManifest(source, manifest) ||
			!acceptedExists || acceptedSource.SourceSnapshotSHA256 != snapshot.SourceSnapshotSHA256 {
			return nil, fmt.Errorf("%w: database_bootstrap_evidence_not_ready[%s]", ErrNotReady, source.SourceID)
		}
		evidenceSources = append(evidenceSources, EvidenceSource{SourceID: source.SourceID,
			EvidenceVersion: snapshot.EvidenceVersion, SourceSnapshotSHA256: snapshot.SourceSnapshotSHA256,
			TableCount: len(snapshot.Tables)})
	}
	if len(evidenceSources) == 0 {
		return nil, fmt.Errorf("%w: database_bootstrap_enabled_source_required", ErrNotReady)
	}
	sort.Slice(evidenceSources, func(i, j int) bool { return evidenceSources[i].SourceID < evidenceSources[j].SourceID })

	rootRaw := append([]byte{}, set.Root.Raw...)
	rootPost, err := addDatabaseDescriptor(rootRaw)
	if err != nil {
		return nil, err
	}
	databasePost := []byte(cognition.DatabaseMarker + "\n")
	projected, findings := cognition.BuildProjectedSet(absRoot, rootPost, map[string][]byte{
		cognition.ScopeMeta:     set.Meta.Raw,
		cognition.ScopeCode:     set.Volumes[cognition.ScopeCode].Raw,
		cognition.ScopeDatabase: databasePost,
	})
	if len(findings) != 0 || projected == nil {
		return nil, fmt.Errorf("database_bootstrap_projected_layout_invalid")
	}

	baselinePath := filepath.Join(absRoot, ".aoci", "baseline.json")
	baselineRaw, err := readRegular(baselinePath)
	if err != nil {
		return nil, fmt.Errorf("database_bootstrap_baseline_invalid")
	}
	copyState, err := cloneBaseline(state)
	if err != nil {
		return nil, err
	}
	copyState.UpdatedAt = preparedAt.Format(time.RFC3339)
	baseline.UpdateOne(copyState, "aoci.database.txt", baseline.HashBytes("aoci.database.txt", databasePost))
	baselinePost, err := baseline.MarshalExact(copyState)
	if err != nil {
		return nil, fmt.Errorf("database_bootstrap_baseline_postimage_invalid")
	}
	evidenceRaw, err := readRegular(dbevidence.BaselinePath(absRoot))
	if err != nil {
		return nil, fmt.Errorf("database_bootstrap_evidence_baseline_invalid")
	}
	preview := &Preview{
		Version:    machinecontract.DatabaseCognitionBootstrapPreviewV1,
		Operation:  machinecontract.CognitionOperationDatabaseBootstrap,
		PreparedAt: preparedAt.Format(time.RFC3339), RootPath: "aoci.txt",
		RootPreimageSHA256: set.Root.SHA256, RootPostimageSHA256: cognitiontxn.SHA256(rootPost), RootPostimage: string(rootPost),
		MetaSHA256: set.Meta.SHA256, CodeVolumeSHA256: set.Volumes[cognition.ScopeCode].SHA256,
		DatabasePath: "aoci.database.txt", DatabasePostimageSHA256: cognitiontxn.SHA256(databasePost), DatabasePostimage: string(databasePost),
		BaselinePath: ".aoci/baseline.json", BaselinePreimageSHA256: cognitiontxn.SHA256(baselineRaw), BaselinePreimage: string(baselineRaw),
		BaselinePostimageSHA256: cognitiontxn.SHA256(baselinePost), BaselinePostimage: string(baselinePost),
		EvidenceBaselineSHA256: cognitiontxn.SHA256(evidenceRaw), EvidenceSources: evidenceSources,
		ProjectedCompositeIdentity: projected.CompositeIdentity,
		ReviewSet:                  []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt", ".aoci/baseline.json", ".aoci/database-baseline.json"},
		WriteSet:                   []string{"aoci.database.txt", "aoci.txt", ".aoci/baseline.json"},
		GuardSet:                   []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt", ".aoci/baseline.json", ".aoci/database-baseline.json"},
		WriteOrder:                 []string{"aoci.database.txt", "aoci.txt", ".aoci/baseline.json"}, RootLast: true,
		NetworkAccessed: false, BusinessDataRead: false, DDLDMLStatements: 0,
	}
	preview.PreviewDigest, err = previewDigest(preview)
	if err != nil {
		return nil, err
	}
	return preview, nil
}

func addDatabaseDescriptor(root []byte) ([]byte, error) {
	text := string(root)
	if strings.Contains(text, "id=database") || strings.Contains(text, databaseDescriptor) {
		return nil, fmt.Errorf("database_bootstrap_descriptor_conflict")
	}
	separator := "\n"
	if strings.Contains(text, "\r\n") {
		separator = "\r\n"
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	insert := -1
	for index, line := range lines {
		if strings.HasPrefix(line, "#Volume:") {
			insert = index + 1
		}
	}
	if insert < 0 {
		return nil, fmt.Errorf("database_bootstrap_root_descriptors_missing")
	}
	lines = append(lines, "")
	copy(lines[insert+1:], lines[insert:])
	lines[insert] = databaseDescriptor
	return []byte(strings.Join(lines, separator)), nil
}

func cloneBaseline(value *baseline.Baseline) (*baseline.Baseline, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result baseline.Baseline
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func readRegular(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, errors.New("path_not_regular")
	}
	return os.ReadFile(path)
}

func previewDigest(value *Preview) (string, error) {
	copyValue := *value
	copyValue.PreviewDigest = ""
	data, err := json.Marshal(copyValue)
	if err != nil {
		return "", err
	}
	return cognitiontxn.SHA256(data), nil
}

func validatePreview(value *Preview) error {
	if value == nil || value.Version != machinecontract.DatabaseCognitionBootstrapPreviewV1 ||
		value.Operation != machinecontract.CognitionOperationDatabaseBootstrap || value.NetworkAccessed ||
		value.BusinessDataRead || value.DDLDMLStatements != 0 || !value.RootLast {
		return fmt.Errorf("database_bootstrap_preview_invalid")
	}
	digest, err := previewDigest(value)
	if err != nil || digest != value.PreviewDigest {
		return fmt.Errorf("database_bootstrap_preview_digest_invalid")
	}
	return nil
}

func samePreview(left, right *Preview) bool {
	return reflect.DeepEqual(left, right)
}
