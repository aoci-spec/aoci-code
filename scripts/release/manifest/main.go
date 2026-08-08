package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

const (
	releaseManifestSchema         = "aoci-release-artifact-manifest/v4"
	previousReleaseManifestSchema = "aoci-release-artifact-manifest/v3"
	legacyReleaseManifestSchema   = "aoci-release-artifact-manifest/v2"
)

type releaseContracts struct {
	CapabilityManifest       string `json:"capability_manifest"`
	SafeInventory            string `json:"safe_inventory"`
	BaselineScopePlan        string `json:"baseline_scope_plan"`
	ManagedScopePolicy       string `json:"managed_scope_policy,omitempty"`
	ManagedScopeEvaluation   string `json:"managed_scope_evaluation,omitempty"`
	ManagedScopeChangePlan   string `json:"managed_scope_change_plan,omitempty"`
	ManagedScopePreview      string `json:"managed_scope_preview,omitempty"`
	ManagedScopeEnvelope     string `json:"managed_scope_envelope,omitempty"`
	ManagedScopeResult       string `json:"managed_scope_result,omitempty"`
	ManagedScopeBaseline     string `json:"managed_scope_baseline,omitempty"`
	ScopeEntryDisposition    string `json:"scope_entry_disposition,omitempty"`
	ApplyAuthorizationPolicy string `json:"apply_authorization_policy,omitempty"`
	PolicyBoundApproval      string `json:"policy_bound_approval,omitempty"`
	CognitionBudgetPolicy    string `json:"cognition_budget_policy,omitempty"`
	CognitionBudgetReport    string `json:"cognition_budget_report,omitempty"`
	CognitionBudgetValidate  string `json:"cognition_budget_validation,omitempty"`
	BusinessSourceManifest   string `json:"business_source_manifest"`
	MigrationPlan            string `json:"migration_plan"`
	MigrationApplyEnvelope   string `json:"migration_apply_envelope"`
	OverviewDeliveryReceipt  string `json:"overview_delivery_receipt"`
	OnboardingSession        string `json:"onboarding_session"`
	HostInteraction          string `json:"host_interaction"`
	MCPToolCount             int    `json:"mcp_tool_count"`
	MCPToolNamesSHA256       string `json:"mcp_tool_names_sha256"`
	MCPListToolsSHA256       string `json:"mcp_list_tools_sha256"`
}

type artifact struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type releaseManifest struct {
	Schema             string              `json:"schema"`
	Version            string              `json:"version"`
	SourceCommit       string              `json:"source_commit"`
	SourceDirty        bool                `json:"source_dirty"`
	BuildDate          string              `json:"build_date"`
	GoVersion          string              `json:"go_version"`
	GoReleaser         string              `json:"goreleaser_version"`
	Syft               string              `json:"syft_version"`
	Contracts          releaseContracts    `json:"contracts"`
	Artifacts          []artifact          `json:"artifacts,omitempty"`
	ReleaseProfile     string              `json:"release_profile,omitempty"`
	Publisher          *publisherIdentity  `json:"publisher,omitempty"`
	ChecksumSubjects   []artifact          `json:"checksum_subjects,omitempty"`
	Checksum           *artifact           `json:"checksum,omitempty"`
	ProvenanceSubjects []string            `json:"provenance_subjects,omitempty"`
	Signature          *envelopeDescriptor `json:"signature,omitempty"`
	Provenance         *envelopeDescriptor `json:"provenance,omitempty"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		writeStatus(os.Stderr, "error", "release_manifest_failed", err.Error())
		os.Exit(1)
	}
}

func run(args []string) error {
	flags := flag.NewFlagSet("release-manifest", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	dist := flags.String("dist", "dist", "artifact directory")
	output := flags.String("output", "dist/RELEASE-MANIFEST.json", "manifest output")
	verify := flags.String("verify", "", "verify an existing manifest")
	version := flags.String("version", "", "release version")
	sourceCommit := flags.String("source-commit", "", "full source commit")
	buildDate := flags.String("build-date", "", "source-derived build date")
	goVersion := flags.String("go-version", "", "Go toolchain identity")
	goreleaserVersion := flags.String("goreleaser-version", "", "GoReleaser identity")
	syftVersion := flags.String("syft-version", "", "Syft identity")
	toolsListSHA := flags.String("tools-list-sha256", "", "SHA-256 of the reviewed MCP tools/list protocol")
	allowDirty := flags.Bool("allow-dirty", false, "allow a non-release rehearsal from a dirty source tree")
	signedRelease := flags.Bool("signed-release", false, "generate the signed release v4 asset contract")
	publisherRepository := flags.String("publisher-repository", "", "HTTPS repository identity for signed provenance")
	publisherWorkflow := flags.String("publisher-workflow", "", "repository-relative release workflow path")
	publisherRef := flags.String("publisher-ref", "", "exact Git ref used by the release workflow")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if *verify != "" {
		if err := verifyExisting(*verify); err != nil {
			return err
		}
		writeStatus(os.Stdout, "ok", "release_manifest_verified", *verify)
		return nil
	}
	if *version == "" || *sourceCommit == "" || *buildDate == "" || *goVersion == "" || *goreleaserVersion == "" || *syftVersion == "" || *toolsListSHA == "" {
		return errors.New("version, source-commit, build-date, go-version, goreleaser-version, syft-version, and tools-list-sha256 are required")
	}
	actualCommit, dirty, err := inspectGit()
	if err != nil {
		return err
	}
	if actualCommit != *sourceCommit {
		return fmt.Errorf("source commit %s does not match current Git commit %s", *sourceCommit, actualCommit)
	}
	if dirty && !*allowDirty {
		return errors.New("source tree is dirty; use allow-dirty only for a non-release rehearsal")
	}
	manifest := releaseManifest{
		Schema:       previousReleaseManifestSchema,
		Version:      *version,
		SourceCommit: *sourceCommit,
		SourceDirty:  dirty,
		BuildDate:    *buildDate,
		GoVersion:    *goVersion,
		GoReleaser:   *goreleaserVersion,
		Syft:         *syftVersion,
		Contracts:    currentReleaseContracts(*toolsListSHA),
	}
	if *signedRelease {
		if *publisherRepository == "" || *publisherWorkflow == "" || *publisherRef == "" {
			return errors.New("publisher-repository, publisher-workflow, and publisher-ref are required for a signed release")
		}
		if err := prepareSignedReleaseManifest(&manifest, *dist, *output, publisherIdentity{
			Repository: *publisherRepository,
			Workflow:   *publisherWorkflow,
			Ref:        *publisherRef,
		}); err != nil {
			return err
		}
	} else {
		artifacts, err := collectArtifacts(*dist, *output)
		if err != nil {
			return err
		}
		manifest.Artifacts = artifacts
	}
	if err := validateManifest(manifest); err != nil {
		return err
	}
	if err := writeJSONAtomic(*output, manifest); err != nil {
		return err
	}
	writeStatus(os.Stdout, "ok", "release_manifest_written", *output)
	return nil
}

func currentReleaseContracts(toolsListSHA string) releaseContracts {
	return releaseContracts{
		CapabilityManifest: machinecontract.CapabilityManifestV1, SafeInventory: machinecontract.SafeInventoryV2,
		BaselineScopePlan: machinecontract.BaselineScopePlanV1, BusinessSourceManifest: machinecontract.BusinessSourceManifestV1,
		ManagedScopePolicy: machinecontract.ManagedScopePolicyV2, ManagedScopeEvaluation: machinecontract.ManagedScopeEvaluationV2,
		ManagedScopeChangePlan: machinecontract.ManagedScopeChangePlanV2, ManagedScopePreview: machinecontract.ManagedScopeChangePreviewV2,
		ManagedScopeEnvelope: machinecontract.ManagedScopeChangeEnvelopeV2, ManagedScopeResult: machinecontract.ManagedScopeChangeResultV2,
		ManagedScopeBaseline: machinecontract.ManagedScopeBaselineV1, ScopeEntryDisposition: machinecontract.ScopeEntryDispositionV1,
		ApplyAuthorizationPolicy: machinecontract.ApplyAuthorizationPolicyV1, PolicyBoundApproval: machinecontract.PolicyBoundApprovalV1,
		CognitionBudgetPolicy: machinecontract.CognitionBudgetPolicyV1, CognitionBudgetReport: machinecontract.CognitionBudgetReportV1,
		CognitionBudgetValidate: machinecontract.CognitionBudgetValidationV1,
		MigrationPlan:           machinecontract.CognitionMigrationPlanV2, MigrationApplyEnvelope: machinecontract.CognitionMigrationApplyEnvelopeV2,
		OverviewDeliveryReceipt: machinecontract.OverviewDeliveryReceiptV1, OnboardingSession: machinecontract.CognitionOnboardingSessionV1,
		HostInteraction: machinecontract.HostInteractionV1, MCPToolCount: len(machinecontract.MCPToolNames()),
		MCPToolNamesSHA256: machinecontract.MCPToolNameIdentity(), MCPListToolsSHA256: toolsListSHA,
	}
}

func legacyReleaseContracts(toolsListSHA string) releaseContracts {
	return releaseContracts{
		CapabilityManifest: machinecontract.CapabilityManifestV1, SafeInventory: machinecontract.SafeInventoryV2,
		BaselineScopePlan: machinecontract.BaselineScopePlanV1, BusinessSourceManifest: machinecontract.BusinessSourceManifestV1,
		MigrationPlan: machinecontract.CognitionMigrationPlanV2, MigrationApplyEnvelope: machinecontract.CognitionMigrationApplyEnvelopeV2,
		OverviewDeliveryReceipt: machinecontract.OverviewDeliveryReceiptV1, OnboardingSession: machinecontract.CognitionOnboardingSessionV1,
		HostInteraction: machinecontract.HostInteractionV1, MCPToolCount: len(machinecontract.MCPToolNames()),
		MCPToolNamesSHA256: machinecontract.MCPToolNameIdentity(), MCPListToolsSHA256: toolsListSHA,
	}
}

func collectArtifacts(dist, output string) ([]artifact, error) {
	dist, err := filepath.Abs(dist)
	if err != nil {
		return nil, err
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dist)
	if err != nil {
		return nil, err
	}
	var artifacts []artifact
	for _, entry := range entries {
		path := filepath.Join(dist, entry.Name())
		if entry.IsDir() {
			continue
		}
		if ignoredEnvelope(path, output) {
			continue
		}
		if entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return nil, fmt.Errorf("release artifact is not a regular file: %s", path)
		}
		digest, size, err := hashFile(path)
		if err != nil {
			return nil, err
		}
		rel, err := filepath.Rel(dist, path)
		if err != nil {
			return nil, err
		}
		artifacts = append(artifacts, artifact{
			Path:   filepath.ToSlash(rel),
			Kind:   artifactKind(rel),
			SHA256: digest,
			Size:   size,
		})
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	return artifacts, nil
}

func verifyExisting(path string) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	var manifest releaseManifest
	decodeErr := decoder.Decode(&manifest)
	if decodeErr == nil {
		var extra any
		if trailingErr := decoder.Decode(&extra); !errors.Is(trailingErr, io.EOF) {
			if trailingErr == nil {
				decodeErr = errors.New("unexpected trailing JSON value")
			} else {
				decodeErr = trailingErr
			}
		}
	}
	closeErr := file.Close()
	if decodeErr != nil {
		return decodeErr
	}
	if closeErr != nil {
		return closeErr
	}
	if err := validateManifest(manifest); err != nil {
		return err
	}
	if manifest.Schema == releaseManifestSchema {
		return verifySignedReleaseFiles(manifest, path)
	}
	dist := filepath.Dir(path)
	actual, err := collectArtifacts(dist, path)
	if err != nil {
		return err
	}
	if len(actual) != len(manifest.Artifacts) {
		return fmt.Errorf("artifact count mismatch: got %d want %d", len(actual), len(manifest.Artifacts))
	}
	for i := range actual {
		if actual[i] != manifest.Artifacts[i] {
			return fmt.Errorf("artifact mismatch for %s", actual[i].Path)
		}
	}
	return nil
}

func validateManifest(manifest releaseManifest) error {
	if manifest.Schema != releaseManifestSchema && manifest.Schema != previousReleaseManifestSchema && manifest.Schema != legacyReleaseManifestSchema {
		return fmt.Errorf("unsupported schema %q", manifest.Schema)
	}
	if manifest.Version == "" || manifest.SourceCommit == "" || manifest.BuildDate == "" {
		return errors.New("release identity is incomplete")
	}
	for _, identity := range []struct {
		name  string
		value string
	}{
		{name: "Go", value: manifest.GoVersion},
		{name: "GoReleaser", value: manifest.GoReleaser},
		{name: "Syft", value: manifest.Syft},
	} {
		if placeholderToolIdentity(identity.value) {
			return fmt.Errorf("%s identity is missing or placeholder", identity.name)
		}
	}
	expectedContracts := currentReleaseContracts(manifest.Contracts.MCPListToolsSHA256)
	if manifest.Schema == legacyReleaseManifestSchema {
		expectedContracts = legacyReleaseContracts(manifest.Contracts.MCPListToolsSHA256)
	}
	if manifest.Contracts != expectedContracts || len(manifest.Contracts.MCPListToolsSHA256) != 64 {
		return errors.New("release contract manifest is incomplete or unsupported")
	}
	if manifest.Schema == releaseManifestSchema {
		return validateSignedReleaseManifest(manifest)
	}
	if len(manifest.Artifacts) == 0 {
		return errors.New("release manifest has no artifacts")
	}
	previous := ""
	kinds := make(map[string]int)
	for _, artifact := range manifest.Artifacts {
		if artifact.Path == "" || artifact.Path <= previous || filepath.IsAbs(filepath.FromSlash(artifact.Path)) || artifact.Path == ".." || strings.HasPrefix(artifact.Path, "../") || artifact.Kind == "" || len(artifact.SHA256) != 64 || artifact.Size < 0 {
			return fmt.Errorf("invalid artifact entry %q", artifact.Path)
		}
		if _, err := hex.DecodeString(artifact.SHA256); err != nil {
			return fmt.Errorf("invalid artifact digest for %s", artifact.Path)
		}
		if artifact.Kind != artifactKind(artifact.Path) {
			return fmt.Errorf("invalid artifact kind %q for %s", artifact.Kind, artifact.Path)
		}
		kinds[artifact.Kind]++
		previous = artifact.Path
	}
	if kinds["checksums"] != 1 || kinds["archive"] < 1 || kinds["sbom"] < 1 {
		return errors.New("release manifest requires one checksum file plus archives and SBOMs")
	}
	if err := validateArchiveSBOMPairs(manifest.Artifacts); err != nil {
		return err
	}
	return nil
}

func placeholderToolIdentity(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if normalized == "" || strings.Contains(normalized, "not provided") {
		return true
	}
	for _, token := range strings.FieldsFunc(normalized, func(r rune) bool {
		return !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'))
	}) {
		if token == "unknown" || token == "none" {
			return true
		}
	}
	return false
}

func validateArchiveSBOMPairs(artifacts []artifact) error {
	archives := make(map[string]struct{})
	sboms := make(map[string][]string)
	for _, artifact := range artifacts {
		switch artifact.Kind {
		case "archive":
			archives[artifact.Path] = struct{}{}
		case "sbom":
			archivePath, ok := archivePathForSBOM(artifact.Path)
			if !ok {
				return fmt.Errorf("SBOM does not identify an archive: %s", artifact.Path)
			}
			sboms[archivePath] = append(sboms[archivePath], artifact.Path)
		}
	}

	archivePaths := make([]string, 0, len(archives))
	for archivePath := range archives {
		archivePaths = append(archivePaths, archivePath)
	}
	sort.Strings(archivePaths)
	for _, archivePath := range archivePaths {
		matches := sboms[archivePath]
		switch len(matches) {
		case 0:
			return fmt.Errorf("archive has no matching SBOM: %s", archivePath)
		case 1:
			delete(sboms, archivePath)
		default:
			sort.Strings(matches)
			return fmt.Errorf("archive has multiple matching SBOMs: %s: %v", archivePath, matches)
		}
	}

	orphanArchives := make([]string, 0, len(sboms))
	for archivePath := range sboms {
		orphanArchives = append(orphanArchives, archivePath)
	}
	sort.Strings(orphanArchives)
	if len(orphanArchives) > 0 {
		archivePath := orphanArchives[0]
		matches := append([]string(nil), sboms[archivePath]...)
		sort.Strings(matches)
		return fmt.Errorf("SBOM has no matching archive: %v", matches)
	}
	return nil
}

func archivePathForSBOM(path string) (string, bool) {
	for _, suffix := range []string{".sbom.json", ".spdx.json"} {
		if strings.HasSuffix(path, suffix) {
			archivePath := strings.TrimSuffix(path, suffix)
			if strings.HasSuffix(archivePath, ".tar.gz") || strings.HasSuffix(archivePath, ".zip") {
				return archivePath, true
			}
			return "", false
		}
	}
	return "", false
}

func inspectGit() (string, bool, error) {
	commitOutput, err := exec.Command("git", "rev-parse", "HEAD").CombinedOutput()
	if err != nil {
		return "", false, fmt.Errorf("read Git commit: %w: %s", err, strings.TrimSpace(string(commitOutput)))
	}
	statusOutput, err := exec.Command("git", "status", "--porcelain=v1", "--untracked-files=all").CombinedOutput()
	if err != nil {
		return "", false, fmt.Errorf("read Git status: %w: %s", err, strings.TrimSpace(string(statusOutput)))
	}
	return strings.TrimSpace(string(commitOutput)), len(strings.TrimSpace(string(statusOutput))) > 0, nil
}

func artifactKind(path string) string {
	name := filepath.Base(path)
	switch {
	case name == "SHA256SUMS":
		return "checksums"
	case strings.HasSuffix(name, ".sbom.json") || strings.HasSuffix(name, ".spdx.json"):
		return "sbom"
	case name == "artifacts.json" || name == "metadata.json":
		return "build-metadata"
	case strings.HasSuffix(name, ".tar.gz") || strings.HasSuffix(name, ".zip"):
		return "archive"
	default:
		return "auxiliary"
	}
}

func ignoredEnvelope(path, output string) bool {
	if path == output {
		return true
	}
	for _, suffix := range []string{".sig", ".pem", ".crt", ".bundle"} {
		if path == output+suffix {
			return true
		}
	}
	return false
}

func hashFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	h := sha256.New()
	size, err := io.Copy(h, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), size, nil
}

func writeJSONAtomic(path string, value any) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".release-manifest-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	encoder := json.NewEncoder(tmp)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(value); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpPath, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func writeStatus(file *os.File, status, code, detail string) {
	_ = json.NewEncoder(file).Encode(map[string]string{"status": status, "code": code, "detail": detail})
}
