package dbevidence

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"

	afs "github.com/aoci-spec/aoci-code/internal/fs"
)

const databaseBaselineName = "database-baseline.json"

func RuntimeEvidenceRoot(repoRoot string) string {
	return filepath.Join(repoRoot, ".aoci", "database", "evidence")
}

func BaselinePath(repoRoot string) string {
	return filepath.Join(repoRoot, ".aoci", databaseBaselineName)
}

func WriteSnapshot(repoRoot string, manifest SourceManifest, snapshot Snapshot, tableFiles map[string][]byte) (returnErr error) {
	lock, err := afs.AcquireIndexLock(repoRoot)
	if err != nil {
		return fmt.Errorf("acquire shared governance lock: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, lock.Release())
	}()
	return writeSnapshotLocked(repoRoot, manifest, snapshot, tableFiles)
}

func writeSnapshotLocked(repoRoot string, manifest SourceManifest, snapshot Snapshot, tableFiles map[string][]byte) error {
	if manifest.SourceID != snapshot.SourceID || manifest.Engine != snapshot.Engine || manifest.Database != snapshot.Database {
		return fmt.Errorf("source manifest and snapshot identity differ")
	}
	if err := validateSourceManifest(manifest); err != nil {
		return err
	}
	if err := validateSnapshot(snapshot); err != nil {
		return err
	}
	if !sameSourceSelection(manifest, snapshot) || !equalCaseSemantics(manifest.CaseSemantics, snapshot.CaseSemantics) {
		return fmt.Errorf("source manifest and snapshot semantics differ")
	}
	root := RuntimeEvidenceRoot(repoRoot)
	sourceDir, tablesDir, err := ensureEvidenceDirectories(repoRoot, manifest.SourceID)
	if err != nil {
		return err
	}
	for _, record := range snapshot.Tables {
		data, exists := tableFiles[record.ObjectRef]
		if !exists {
			return fmt.Errorf("missing canonical evidence bytes for %s", record.ObjectRef)
		}
		if sha256Hex(data) != record.TableEvidenceSHA256 {
			return fmt.Errorf("canonical evidence hash mismatch for %s", record.ObjectRef)
		}
		var table TableEvidence
		if err := decodeStrict(data, &table); err != nil {
			return fmt.Errorf("invalid canonical evidence for %s", record.ObjectRef)
		}
		canonical, canonicalData, digest, components, err := CanonicalTable(table)
		if err != nil || canonical.ObjectRef != record.ObjectRef || digest != record.TableEvidenceSHA256 || components != record.ComponentHashes || !bytes.Equal(canonicalData, data) {
			return fmt.Errorf("non-canonical evidence for %s", record.ObjectRef)
		}
		target, err := resolveEvidenceRef(root, snapshot.SourceID, record)
		if err != nil {
			return err
		}
		if filepath.Dir(target) != tablesDir {
			return fmt.Errorf("unsafe evidence_ref")
		}
		if _, currentTablesDir, err := ensureEvidenceDirectories(repoRoot, manifest.SourceID); err != nil || currentTablesDir != tablesDir {
			return fmt.Errorf("unsafe database evidence directory")
		}
		if err := rejectEvidenceWriteTarget(target); err != nil {
			return err
		}
		if err := afs.AtomicWrite(target, data); err != nil {
			return fmt.Errorf("write table evidence: %w", err)
		}
	}
	manifestData, err := CanonicalJSON(manifest)
	if err != nil {
		return err
	}
	snapshotData, err := CanonicalJSON(snapshot)
	if err != nil {
		return err
	}
	currentSourceDir, _, err := ensureEvidenceDirectories(repoRoot, manifest.SourceID)
	if err != nil || currentSourceDir != sourceDir {
		return fmt.Errorf("unsafe database evidence directory")
	}
	if err := rejectEvidenceWriteTarget(filepath.Join(sourceDir, "source.json")); err != nil {
		return err
	}
	if err := rejectEvidenceWriteTarget(filepath.Join(sourceDir, "snapshot.json")); err != nil {
		return err
	}
	if err := afs.AtomicWrite(filepath.Join(sourceDir, "source.json"), manifestData); err != nil {
		return fmt.Errorf("write source manifest: %w", err)
	}
	if err := afs.AtomicWrite(filepath.Join(sourceDir, "snapshot.json"), snapshotData); err != nil {
		return fmt.Errorf("write snapshot manifest: %w", err)
	}
	return nil
}

func ensureEvidenceDirectories(repoRoot, sourceID string) (string, string, error) {
	if !sourceIDPattern.MatchString(sourceID) {
		return "", "", fmt.Errorf("invalid source_id")
	}
	current := repoRoot
	var sourceDir, tablesDir string
	for index, component := range []string{".aoci", "database", "evidence", sourceID, "tables"} {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if errors.Is(err, os.ErrNotExist) {
			if err := os.Mkdir(current, 0o755); err != nil {
				return "", "", fmt.Errorf("create database evidence directory")
			}
			info, err = os.Lstat(current)
		}
		if err != nil {
			return "", "", fmt.Errorf("database evidence directory unavailable")
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return "", "", fmt.Errorf("unsafe database evidence directory")
		}
		if index == 3 {
			sourceDir = current
		}
		if index == 4 {
			tablesDir = current
		}
	}
	return sourceDir, tablesDir, nil
}

func rejectEvidenceWriteTarget(path string) error {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("database evidence target unavailable")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return fmt.Errorf("unsafe database evidence target")
	}
	return nil
}

func LoadSnapshot(repoRoot, sourceID string) (SourceManifest, Snapshot, bool, error) {
	if !sourceIDPattern.MatchString(sourceID) {
		return SourceManifest{}, Snapshot{}, false, fmt.Errorf("invalid source_id")
	}
	manifestData, err := readEvidenceFile(repoRoot, sourceID+"/source.json")
	if errors.Is(err, os.ErrNotExist) {
		return SourceManifest{}, Snapshot{}, false, nil
	}
	if err != nil {
		return SourceManifest{}, Snapshot{}, false, fmt.Errorf("read source manifest: %w", err)
	}
	snapshotData, err := readEvidenceFile(repoRoot, sourceID+"/snapshot.json")
	if err != nil {
		return SourceManifest{}, Snapshot{}, true, fmt.Errorf("read snapshot manifest: %w", err)
	}
	var manifest SourceManifest
	var snapshot Snapshot
	if err := decodeStrict(manifestData, &manifest); err != nil {
		return SourceManifest{}, Snapshot{}, true, fmt.Errorf("invalid source manifest: %w", err)
	}
	if err := decodeStrict(snapshotData, &snapshot); err != nil {
		return SourceManifest{}, Snapshot{}, true, fmt.Errorf("invalid snapshot manifest: %w", err)
	}
	if manifest.Version != SourceManifestVersion || snapshot.Version != SnapshotVersion || snapshot.EvidenceVersion != EvidenceVersion {
		return SourceManifest{}, Snapshot{}, true, fmt.Errorf("unsupported database evidence version")
	}
	if manifest.SourceID != sourceID || snapshot.SourceID != sourceID || manifest.Engine != snapshot.Engine || manifest.Database != snapshot.Database {
		return SourceManifest{}, Snapshot{}, true, fmt.Errorf("database evidence identity mismatch")
	}
	if !sameSourceSelection(manifest, snapshot) || !equalCaseSemantics(manifest.CaseSemantics, snapshot.CaseSemantics) {
		return SourceManifest{}, Snapshot{}, true, fmt.Errorf("database evidence semantics mismatch")
	}
	if err := validateSourceManifest(manifest); err != nil {
		return SourceManifest{}, Snapshot{}, true, err
	}
	if err := validateSnapshot(snapshot); err != nil {
		return SourceManifest{}, Snapshot{}, true, err
	}
	canonicalManifest, err := CanonicalJSON(manifest)
	if err != nil || !bytes.Equal(canonicalManifest, manifestData) {
		return SourceManifest{}, Snapshot{}, true, fmt.Errorf("source manifest is not canonical")
	}
	canonicalSnapshot, err := CanonicalJSON(snapshot)
	if err != nil || !bytes.Equal(canonicalSnapshot, snapshotData) {
		return SourceManifest{}, Snapshot{}, true, fmt.Errorf("snapshot manifest is not canonical")
	}
	for _, record := range snapshot.Tables {
		_, err := resolveEvidenceRef(RuntimeEvidenceRoot(repoRoot), snapshot.SourceID, record)
		if err != nil {
			return SourceManifest{}, Snapshot{}, true, err
		}
		data, err := readEvidenceFile(repoRoot, record.EvidenceRef)
		if err != nil {
			return SourceManifest{}, Snapshot{}, true, fmt.Errorf("read evidence for %s: %w", record.ObjectRef, err)
		}
		if sha256Hex(data) != record.TableEvidenceSHA256 {
			return SourceManifest{}, Snapshot{}, true, fmt.Errorf("evidence hash mismatch for %s", record.ObjectRef)
		}
		var table TableEvidence
		if err := decodeStrict(data, &table); err != nil {
			return SourceManifest{}, Snapshot{}, true, fmt.Errorf("invalid evidence for %s: %w", record.ObjectRef, err)
		}
		canonical, canonicalData, digest, components, err := CanonicalTable(table)
		if err != nil || canonical.ObjectRef != record.ObjectRef || digest != record.TableEvidenceSHA256 || string(canonicalData) != string(data) || components != record.ComponentHashes {
			return SourceManifest{}, Snapshot{}, true, fmt.Errorf("non-canonical evidence for %s", record.ObjectRef)
		}
	}
	identityData, err := snapshotIdentityBytes(snapshot)
	if err != nil || sha256Hex(identityData) != snapshot.SourceSnapshotSHA256 {
		return SourceManifest{}, Snapshot{}, true, fmt.Errorf("source snapshot hash mismatch")
	}
	return manifest, snapshot, true, nil
}

func LoadTableEvidence(repoRoot string, record SnapshotTable) (TableEvidence, error) {
	sourceID := sourceIDFromEvidenceRef(record.EvidenceRef)
	_, err := resolveEvidenceRef(RuntimeEvidenceRoot(repoRoot), sourceID, record)
	if err != nil {
		return TableEvidence{}, err
	}
	data, err := readEvidenceFile(repoRoot, record.EvidenceRef)
	if err != nil {
		return TableEvidence{}, err
	}
	if sha256Hex(data) != record.TableEvidenceSHA256 {
		return TableEvidence{}, fmt.Errorf("evidence hash mismatch for %s", record.ObjectRef)
	}
	var table TableEvidence
	if err := decodeStrict(data, &table); err != nil {
		return TableEvidence{}, err
	}
	canonical, canonicalData, digest, components, err := CanonicalTable(table)
	if err != nil || canonical.ObjectRef != record.ObjectRef || digest != record.TableEvidenceSHA256 || components != record.ComponentHashes || !bytes.Equal(canonicalData, data) {
		return TableEvidence{}, fmt.Errorf("non-canonical evidence for %s", record.ObjectRef)
	}
	return canonical, nil
}

func readEvidenceFile(repoRoot, evidenceRef string) ([]byte, error) {
	components := append([]string{".aoci", "database", "evidence"}, strings.Split(evidenceRef, "/")...)
	current := repoRoot
	for index, component := range components {
		if component == "" || component == "." || component == ".." || strings.ContainsAny(component, `/\\`) {
			return nil, fmt.Errorf("unsafe database evidence path")
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil, os.ErrNotExist
			}
			return nil, fmt.Errorf("database evidence path unavailable")
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("unsafe database evidence symlink")
		}
		if index < len(components)-1 && !info.IsDir() {
			return nil, fmt.Errorf("unsafe database evidence path")
		}
		if index == len(components)-1 && !info.Mode().IsRegular() {
			return nil, fmt.Errorf("unsafe database evidence file")
		}
	}
	data, err := os.ReadFile(current)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, os.ErrNotExist
		}
		return nil, fmt.Errorf("database evidence file unavailable")
	}
	return data, nil
}

func LoadBaseline(repoRoot string) (Baseline, bool, error) {
	data, err := os.ReadFile(BaselinePath(repoRoot))
	if errors.Is(err, os.ErrNotExist) {
		return Baseline{Version: BaselineVersion, Sources: []BaselineSource{}}, false, nil
	}
	if err != nil {
		return Baseline{}, false, err
	}
	var baseline Baseline
	if err := decodeStrict(data, &baseline); err != nil {
		return Baseline{}, true, fmt.Errorf("database evidence baseline is invalid: %w", err)
	}
	if baseline.Version != BaselineVersion {
		return Baseline{}, true, fmt.Errorf("unsupported database evidence baseline version %q", baseline.Version)
	}
	if baseline.Sources == nil {
		baseline.Sources = []BaselineSource{}
	}
	if err := validateBaseline(baseline); err != nil {
		return Baseline{}, true, fmt.Errorf("database evidence baseline is invalid: %w", err)
	}
	canonical, err := CanonicalJSON(baseline)
	if err != nil || !bytes.Equal(canonical, data) {
		return Baseline{}, true, fmt.Errorf("database evidence baseline is not canonical")
	}
	return baseline, true, nil
}

func AcceptSnapshot(repoRoot string, snapshot Snapshot, expectedSnapshotSHA256 string) (returnErr error) {
	if expectedSnapshotSHA256 == "" || expectedSnapshotSHA256 != snapshot.SourceSnapshotSHA256 {
		return fmt.Errorf("snapshot binding mismatch: explicit --snapshot-sha must match current snapshot")
	}
	if err := validateSnapshot(snapshot); err != nil {
		return err
	}
	lock, err := afs.AcquireIndexLock(repoRoot)
	if err != nil {
		return fmt.Errorf("acquire shared governance lock: %w", err)
	}
	defer func() {
		returnErr = errors.Join(returnErr, lock.Release())
	}()
	_, current, exists, err := LoadSnapshot(repoRoot, snapshot.SourceID)
	if err != nil || !exists || current.SourceSnapshotSHA256 != expectedSnapshotSHA256 {
		return fmt.Errorf("snapshot binding mismatch: explicit --snapshot-sha must match the current saved snapshot")
	}
	baseline, _, err := LoadBaseline(repoRoot)
	if err != nil {
		return err
	}
	tables := make([]BaselineTable, len(snapshot.Tables))
	for index, table := range snapshot.Tables {
		tables[index] = BaselineTable{ObjectRef: table.ObjectRef, TableEvidenceSHA256: table.TableEvidenceSHA256, ComponentHashes: table.ComponentHashes}
	}
	source := BaselineSource{
		SourceID: snapshot.SourceID, Engine: snapshot.Engine, Database: snapshot.Database,
		Namespaces: sortedCopy(snapshot.Namespaces), CaseSemantics: snapshot.CaseSemantics,
		IncludeNamespaces: sortedCopy(snapshot.IncludeNamespaces), ExcludeNamespaces: sortedCopy(snapshot.ExcludeNamespaces),
		IncludeTables: sortedCopy(snapshot.IncludeTables), ExcludeTables: sortedCopy(snapshot.ExcludeTables),
		EvidenceVersion: snapshot.EvidenceVersion, SourceSnapshotSHA256: snapshot.SourceSnapshotSHA256, Tables: tables,
	}
	replaced := false
	for index := range baseline.Sources {
		if baseline.Sources[index].SourceID == source.SourceID {
			baseline.Sources[index] = source
			replaced = true
			break
		}
	}
	if !replaced {
		baseline.Sources = append(baseline.Sources, source)
	}
	sort.Slice(baseline.Sources, func(i, j int) bool { return baseline.Sources[i].SourceID < baseline.Sources[j].SourceID })
	data, err := CanonicalJSON(baseline)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(BaselinePath(repoRoot)), 0o755); err != nil {
		return err
	}
	return afs.AtomicWrite(BaselinePath(repoRoot), data)
}

func validateSourceManifest(manifest SourceManifest) error {
	if !allStringsUTF8(reflect.ValueOf(manifest)) {
		return fmt.Errorf("source manifest contains invalid UTF-8")
	}
	if manifest.Version != SourceManifestVersion || !sourceIDPattern.MatchString(manifest.SourceID) || manifest.Database == "" {
		return fmt.Errorf("invalid source manifest identity")
	}
	if !supportedEngine(manifest.Engine) {
		return fmt.Errorf("invalid source manifest engine")
	}
	if manifest.BusinessDataRead {
		return fmt.Errorf("database evidence cannot record business data reads")
	}
	for _, values := range [][]string{manifest.Namespaces, manifest.IncludeNamespaces, manifest.ExcludeNamespaces, manifest.IncludeTables, manifest.ExcludeTables} {
		if !slices.Equal(values, sortedCopy(values)) || hasDuplicateStrings(values) {
			return fmt.Errorf("source manifest collections are not canonical")
		}
	}
	return nil
}

func validateSnapshot(snapshot Snapshot) error {
	if !allStringsUTF8(reflect.ValueOf(snapshot)) {
		return fmt.Errorf("database snapshot contains invalid UTF-8")
	}
	if snapshot.Version != SnapshotVersion || snapshot.EvidenceVersion != EvidenceVersion || !sourceIDPattern.MatchString(snapshot.SourceID) || snapshot.Database == "" {
		return fmt.Errorf("invalid database snapshot identity")
	}
	if !supportedEngine(snapshot.Engine) {
		return fmt.Errorf("invalid database snapshot engine")
	}
	if snapshot.BusinessDataRead {
		return fmt.Errorf("database snapshot is not canonical")
	}
	for _, values := range [][]string{snapshot.Namespaces, snapshot.IncludeNamespaces, snapshot.ExcludeNamespaces, snapshot.IncludeTables, snapshot.ExcludeTables} {
		if !slices.Equal(values, sortedCopy(values)) || hasDuplicateStrings(values) {
			return fmt.Errorf("database snapshot collections are not canonical")
		}
	}
	wantState := "present"
	if len(snapshot.Tables) == 0 {
		wantState = "present_empty"
	}
	if snapshot.State != wantState {
		return fmt.Errorf("database snapshot state is inconsistent")
	}
	lastRef := ""
	for _, record := range snapshot.Tables {
		if record.ObjectRef <= lastRef || !validSHA256(record.TableEvidenceSHA256) || !validComponentHashes(record.ComponentHashes) {
			return fmt.Errorf("database snapshot table inventory is invalid")
		}
		if _, err := resolveEvidenceRef(".", snapshot.SourceID, record); err != nil {
			return err
		}
		lastRef = record.ObjectRef
	}
	if !validSHA256(snapshot.SourceSnapshotSHA256) {
		return fmt.Errorf("database snapshot hash is invalid")
	}
	identity, err := snapshotIdentityBytes(snapshot)
	if err != nil || sha256Hex(identity) != snapshot.SourceSnapshotSHA256 {
		return fmt.Errorf("source snapshot hash mismatch")
	}
	return nil
}

func validateBaseline(baseline Baseline) error {
	if !allStringsUTF8(reflect.ValueOf(baseline)) {
		return fmt.Errorf("database evidence Baseline contains invalid UTF-8")
	}
	lastSource := ""
	for _, source := range baseline.Sources {
		if source.SourceID <= lastSource || !sourceIDPattern.MatchString(source.SourceID) || source.Database == "" || source.EvidenceVersion != EvidenceVersion || !validSHA256(source.SourceSnapshotSHA256) {
			return fmt.Errorf("invalid Baseline source identity")
		}
		if !supportedEngine(source.Engine) {
			return fmt.Errorf("invalid Baseline source engine")
		}
		for _, values := range [][]string{source.Namespaces, source.IncludeNamespaces, source.ExcludeNamespaces, source.IncludeTables, source.ExcludeTables} {
			if !slices.Equal(values, sortedCopy(values)) || hasDuplicateStrings(values) {
				return fmt.Errorf("Baseline source collections are not canonical")
			}
		}
		lastTable := ""
		for _, table := range source.Tables {
			if table.ObjectRef <= lastTable || !validSHA256(table.TableEvidenceSHA256) || !validComponentHashes(table.ComponentHashes) {
				return fmt.Errorf("invalid Baseline table inventory")
			}
			lastTable = table.ObjectRef
		}
		lastSource = source.SourceID
	}
	return nil
}

func sameSourceSelection(manifest SourceManifest, snapshot Snapshot) bool {
	return slices.Equal(manifest.Namespaces, snapshot.Namespaces) &&
		slices.Equal(manifest.IncludeNamespaces, snapshot.IncludeNamespaces) &&
		slices.Equal(manifest.ExcludeNamespaces, snapshot.ExcludeNamespaces) &&
		slices.Equal(manifest.IncludeTables, snapshot.IncludeTables) &&
		slices.Equal(manifest.ExcludeTables, snapshot.ExcludeTables)
}

func resolveEvidenceRef(root, sourceID string, record SnapshotTable) (string, error) {
	if !sourceIDPattern.MatchString(sourceID) || !validSHA256(record.TableEvidenceSHA256) {
		return "", fmt.Errorf("invalid evidence reference")
	}
	want := sourceID + "/tables/" + record.TableEvidenceSHA256 + ".json"
	if record.EvidenceRef != want {
		return "", fmt.Errorf("unsafe evidence reference")
	}
	return filepath.Join(root, filepath.FromSlash(want)), nil
}

func sourceIDFromEvidenceRef(ref string) string {
	for index := 0; index < len(ref); index++ {
		if ref[index] == '/' {
			return ref[:index]
		}
	}
	return ""
}

func validSHA256(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validComponentHashes(hashes ComponentHashes) bool {
	return validSHA256(hashes.Columns) && validSHA256(hashes.PrimaryKey) && validSHA256(hashes.UniqueConstraints) && validSHA256(hashes.ForeignKeys) && validSHA256(hashes.Checks) && validSHA256(hashes.Indexes) && validSHA256(hashes.Partition)
}

func hasDuplicateStrings(values []string) bool {
	for index := 1; index < len(values); index++ {
		if values[index-1] == values[index] {
			return true
		}
	}
	return false
}

func FileSHA256(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func decodeStrict(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err == nil {
		return fmt.Errorf("trailing JSON value")
	} else if !errors.Is(err, io.EOF) {
		return fmt.Errorf("trailing JSON content: %w", err)
	}
	return nil
}
