package main

import (
	"bufio"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const (
	canonicalReleaseRepository = "https://github.com/aoci-spec/aoci-code"
	canonicalReleaseWorkflow   = ".github/workflows/release.yml"
	checksumAssetPath          = "SHA256SUMS"
	manifestAssetPath          = "RELEASE-MANIFEST.json"
	signatureAssetPath         = "SHA256SUMS.sigstore.json"
	sigstoreBundleMediaType    = "application/vnd.dev.sigstore.bundle.v0.3+json"
	inTotoStatementType        = "https://in-toto.io/Statement/v1"
	slsaProvenanceType         = "https://slsa.dev/provenance/v1"
	githubWorkflowBuildType    = "https://actions.github.io/buildtypes/workflow/v1"
	githubOIDCIssuer           = "https://token.actions.githubusercontent.com"
)

var releaseTargetSuffixes = []string{
	"_darwin_amd64.tar.gz",
	"_darwin_arm64.tar.gz",
	"_linux_amd64.tar.gz",
	"_linux_arm64.tar.gz",
	"_windows_amd64.zip",
	"_windows_arm64.zip",
}

type publisherIdentity struct {
	Repository string `json:"repository"`
	Workflow   string `json:"workflow"`
	Ref        string `json:"ref"`
}

type envelopeDescriptor struct {
	Path                string `json:"path"`
	Kind                string `json:"kind"`
	Subject             string `json:"subject,omitempty"`
	CertificateIdentity string `json:"certificate_identity,omitempty"`
	OIDCIssuer          string `json:"oidc_issuer,omitempty"`
	Repository          string `json:"repository,omitempty"`
	Workflow            string `json:"workflow,omitempty"`
	SourceRef           string `json:"source_ref,omitempty"`
	SourceCommit        string `json:"source_commit,omitempty"`
}

func prepareSignedReleaseManifest(manifest *releaseManifest, dist, output string, publisher publisherIdentity) error {
	if filepath.Base(output) != manifestAssetPath {
		return fmt.Errorf("signed release manifest must be named %s", manifestAssetPath)
	}
	subjects, checksum, err := collectSignedReleaseInputs(dist, output)
	if err != nil {
		return err
	}
	profile, err := releaseProfile(manifest.Version, publisher.Ref)
	if err != nil {
		return err
	}
	manifest.Schema = releaseManifestSchema
	manifest.ReleaseProfile = profile
	manifest.Publisher = &publisher
	manifest.Artifacts = nil
	manifest.ChecksumSubjects = subjects
	manifest.Checksum = &checksum
	manifest.ProvenanceSubjects = provenanceSubjectNames(subjects)
	manifest.Signature = &envelopeDescriptor{
		Path:                signatureAssetPath,
		Kind:                "sigstore-bundle",
		Subject:             checksumAssetPath,
		CertificateIdentity: publisherCertificateIdentity(publisher),
		OIDCIssuer:          githubOIDCIssuer,
	}
	manifest.Provenance = &envelopeDescriptor{
		Path:         provenanceAssetPath(manifest.Version),
		Kind:         "github-artifact-attestation",
		Repository:   publisher.Repository,
		Workflow:     publisher.Workflow,
		SourceRef:    publisher.Ref,
		SourceCommit: manifest.SourceCommit,
	}
	return nil
}

func collectSignedReleaseInputs(dist, output string) ([]artifact, artifact, error) {
	dist, err := filepath.Abs(dist)
	if err != nil {
		return nil, artifact{}, err
	}
	output, err = filepath.Abs(output)
	if err != nil {
		return nil, artifact{}, err
	}
	if filepath.Dir(output) != dist {
		return nil, artifact{}, errors.New("signed release manifest must be written at the release asset root")
	}
	entries, err := os.ReadDir(dist)
	if err != nil {
		return nil, artifact{}, err
	}
	var subjects []artifact
	var checksum artifact
	checksumSeen := false
	for _, entry := range entries {
		path := filepath.Join(dist, entry.Name())
		if path == output {
			continue
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return nil, artifact{}, fmt.Errorf("release input is not a regular top-level file: %s", path)
		}
		item, err := artifactFromFile(path, entry.Name())
		if err != nil {
			return nil, artifact{}, err
		}
		switch item.Kind {
		case "archive", "sbom":
			subjects = append(subjects, item)
		case "checksums":
			if checksumSeen {
				return nil, artifact{}, errors.New("signed release contains multiple checksum files")
			}
			checksum = item
			checksumSeen = true
		default:
			return nil, artifact{}, fmt.Errorf("undeclared release input: %s", entry.Name())
		}
	}
	if !checksumSeen {
		return nil, artifact{}, fmt.Errorf("signed release is missing %s", checksumAssetPath)
	}
	sort.Slice(subjects, func(i, j int) bool { return subjects[i].Path < subjects[j].Path })
	if err := validateSignedSubjectSet(subjects); err != nil {
		return nil, artifact{}, err
	}
	if err := validateChecksumSubjects(filepath.Join(dist, checksumAssetPath), subjects); err != nil {
		return nil, artifact{}, err
	}
	if err := validateSPDXSubjects(dist, subjects); err != nil {
		return nil, artifact{}, err
	}
	return subjects, checksum, nil
}

func artifactFromFile(path, name string) (artifact, error) {
	digest, size, err := hashFile(path)
	if err != nil {
		return artifact{}, err
	}
	return artifact{Path: filepath.ToSlash(name), Kind: artifactKind(name), SHA256: digest, Size: size}, nil
}

func validateSignedReleaseManifest(manifest releaseManifest) error {
	if manifest.SourceDirty {
		return errors.New("signed release requires source_dirty=false")
	}
	if len(manifest.SourceCommit) != 40 {
		return errors.New("signed release source_commit must be a full Git SHA-1")
	}
	if _, err := hex.DecodeString(manifest.SourceCommit); err != nil || manifest.SourceCommit != strings.ToLower(manifest.SourceCommit) {
		return errors.New("signed release source_commit must be lowercase hexadecimal")
	}
	if len(manifest.Artifacts) != 0 {
		return errors.New("signed release must use checksum_subjects instead of legacy artifacts")
	}
	if manifest.Publisher == nil {
		return errors.New("signed release publisher identity is missing")
	}
	profile, err := releaseProfile(manifest.Version, manifest.Publisher.Ref)
	if err != nil {
		return err
	}
	if manifest.ReleaseProfile != profile {
		return fmt.Errorf("release profile %q does not match publisher ref", manifest.ReleaseProfile)
	}
	if err := validatePublisher(*manifest.Publisher); err != nil {
		return err
	}
	if err := validateSignedSubjectSet(manifest.ChecksumSubjects); err != nil {
		return err
	}
	if err := validateSignedSubjectNames(manifest.Version, manifest.ChecksumSubjects); err != nil {
		return err
	}
	if manifest.Checksum == nil {
		return errors.New("signed release checksum descriptor is missing")
	}
	if err := validateArtifactRecord(*manifest.Checksum); err != nil {
		return err
	}
	if manifest.Checksum.Path != checksumAssetPath || manifest.Checksum.Kind != "checksums" {
		return fmt.Errorf("signed release checksum must be %s", checksumAssetPath)
	}
	expectedProvenanceSubjects := provenanceSubjectNames(manifest.ChecksumSubjects)
	if !equalStrings(manifest.ProvenanceSubjects, expectedProvenanceSubjects) {
		return errors.New("provenance_subjects must be the exact 12 checksum subjects plus SHA256SUMS and RELEASE-MANIFEST.json")
	}
	if manifest.Signature == nil || *manifest.Signature != (envelopeDescriptor{
		Path: signatureAssetPath, Kind: "sigstore-bundle", Subject: checksumAssetPath,
		CertificateIdentity: publisherCertificateIdentity(*manifest.Publisher), OIDCIssuer: githubOIDCIssuer,
	}) {
		return errors.New("signed release signature descriptor is incomplete")
	}
	if manifest.Provenance == nil || *manifest.Provenance != (envelopeDescriptor{
		Path: provenanceAssetPath(manifest.Version), Kind: "github-artifact-attestation",
		Repository: manifest.Publisher.Repository, Workflow: manifest.Publisher.Workflow,
		SourceRef: manifest.Publisher.Ref, SourceCommit: manifest.SourceCommit,
	}) {
		return errors.New("signed release provenance descriptor is incomplete")
	}
	return nil
}

func validateSignedSubjectSet(subjects []artifact) error {
	if len(subjects) != 12 {
		return fmt.Errorf("signed release requires exactly 12 checksum subjects, got %d", len(subjects))
	}
	previous := ""
	archives := make(map[string]struct{})
	sboms := make(map[string]struct{})
	for _, item := range subjects {
		if err := validateArtifactRecord(item); err != nil {
			return err
		}
		if item.Path <= previous {
			return errors.New("checksum_subjects must be unique and sorted by path")
		}
		previous = item.Path
		switch item.Kind {
		case "archive":
			target, ok := releaseTarget(item.Path)
			if !ok {
				return fmt.Errorf("archive is outside the six-target release matrix: %s", item.Path)
			}
			if _, exists := archives[target]; exists {
				return fmt.Errorf("duplicate archive target: %s", target)
			}
			archives[target] = struct{}{}
		case "sbom":
			if !strings.HasSuffix(item.Path, ".sbom.json") {
				return fmt.Errorf("signed release SBOM must use .sbom.json: %s", item.Path)
			}
			archivePath := strings.TrimSuffix(item.Path, ".sbom.json")
			target, ok := releaseTarget(archivePath)
			if !ok {
				return fmt.Errorf("SBOM is outside the six-target release matrix: %s", item.Path)
			}
			if _, exists := sboms[target]; exists {
				return fmt.Errorf("duplicate SBOM target: %s", target)
			}
			sboms[target] = struct{}{}
		default:
			return fmt.Errorf("checksum subject has unsupported kind %q", item.Kind)
		}
	}
	if len(archives) != len(releaseTargetSuffixes) || len(sboms) != len(releaseTargetSuffixes) {
		return fmt.Errorf("signed release requires six archives and six SBOMs, got %d and %d", len(archives), len(sboms))
	}
	for _, target := range releaseTargetSuffixes {
		if _, ok := archives[target]; !ok {
			return fmt.Errorf("release matrix is missing archive target %s", target)
		}
		if _, ok := sboms[target]; !ok {
			return fmt.Errorf("release matrix is missing SBOM target %s", target)
		}
	}
	return nil
}

func validateSignedSubjectNames(version string, subjects []artifact) error {
	normalizedVersion := strings.TrimPrefix(version, "v")
	expected := make(map[string]struct{}, 12)
	for _, target := range releaseTargetSuffixes {
		archive := "aoci_" + normalizedVersion + target
		expected[archive] = struct{}{}
		expected[archive+".sbom.json"] = struct{}{}
	}
	for _, item := range subjects {
		if _, ok := expected[item.Path]; !ok {
			return fmt.Errorf("release asset name does not match version %s: %s", version, item.Path)
		}
		delete(expected, item.Path)
	}
	if len(expected) != 0 {
		return fmt.Errorf("release asset names do not cover version %s", version)
	}
	return nil
}

func validateArtifactRecord(item artifact) error {
	if item.Path == "" || filepath.IsAbs(filepath.FromSlash(item.Path)) || item.Path == ".." || strings.HasPrefix(item.Path, "../") || filepath.Base(item.Path) != item.Path || item.Kind == "" || len(item.SHA256) != 64 || item.Size < 0 {
		return fmt.Errorf("invalid artifact entry %q", item.Path)
	}
	if _, err := hex.DecodeString(item.SHA256); err != nil {
		return fmt.Errorf("invalid artifact digest for %s", item.Path)
	}
	if item.Kind != artifactKind(item.Path) {
		return fmt.Errorf("invalid artifact kind %q for %s", item.Kind, item.Path)
	}
	return nil
}

func releaseTarget(path string) (string, bool) {
	for _, suffix := range releaseTargetSuffixes {
		if strings.HasSuffix(path, suffix) {
			return suffix, true
		}
	}
	return "", false
}

func validatePublisher(publisher publisherIdentity) error {
	if publisher.Repository != canonicalReleaseRepository {
		return fmt.Errorf("publisher repository must be %s", canonicalReleaseRepository)
	}
	if publisher.Workflow != canonicalReleaseWorkflow {
		return fmt.Errorf("publisher workflow must be %s", canonicalReleaseWorkflow)
	}
	if !strings.HasPrefix(publisher.Ref, "refs/tags/") && !strings.HasPrefix(publisher.Ref, "refs/heads/") {
		return fmt.Errorf("publisher ref must identify a tag or branch: %q", publisher.Ref)
	}
	return nil
}

func releaseProfile(version, ref string) (string, error) {
	if version == "" || strings.ContainsAny(version, "/\\\x00\r\n") {
		return "", fmt.Errorf("invalid release version %q", version)
	}
	if strings.HasPrefix(ref, "refs/tags/") {
		if ref != "refs/tags/"+releaseTag(version) {
			return "", fmt.Errorf("publisher tag %q does not match release version %q", ref, version)
		}
		return "tag", nil
	}
	if strings.HasPrefix(ref, "refs/heads/") {
		if ref != "refs/heads/main" {
			return "", fmt.Errorf("dry-run publisher ref must be refs/heads/main: %q", ref)
		}
		return "dry-run", nil
	}
	return "", fmt.Errorf("publisher ref must identify a tag or branch: %q", ref)
}

func releaseTag(version string) string {
	return "v" + strings.TrimPrefix(version, "v")
}

func provenanceAssetPath(version string) string {
	return "AOCI-CODE_" + releaseTag(version) + ".provenance.sigstore.json"
}

func publisherCertificateIdentity(publisher publisherIdentity) string {
	return publisher.Repository + "/" + publisher.Workflow + "@" + publisher.Ref
}

func provenanceSubjectNames(subjects []artifact) []string {
	names := make([]string, 0, len(subjects)+2)
	for _, item := range subjects {
		names = append(names, item.Path)
	}
	names = append(names, checksumAssetPath, manifestAssetPath)
	sort.Strings(names)
	return names
}

func verifySignedReleaseFiles(manifest releaseManifest, manifestPath string) error {
	dist := filepath.Dir(manifestPath)
	expectedNames := make(map[string]struct{}, 16)
	for _, item := range manifest.ChecksumSubjects {
		expectedNames[item.Path] = struct{}{}
	}
	for _, path := range []string{
		manifest.Checksum.Path,
		manifestAssetPath,
		manifest.Signature.Path,
		manifest.Provenance.Path,
	} {
		expectedNames[path] = struct{}{}
	}
	if len(expectedNames) != 16 {
		return fmt.Errorf("signed release asset contract contains %d unique paths, want 16", len(expectedNames))
	}
	entries, err := os.ReadDir(dist)
	if err != nil {
		return err
	}
	actualNames := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		path := filepath.Join(dist, entry.Name())
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || !entry.Type().IsRegular() {
			return fmt.Errorf("release asset is not a regular top-level file: %s", path)
		}
		if _, expected := expectedNames[entry.Name()]; !expected {
			return fmt.Errorf("undeclared release asset: %s", entry.Name())
		}
		actualNames[entry.Name()] = struct{}{}
	}
	for name := range expectedNames {
		if _, ok := actualNames[name]; !ok {
			return fmt.Errorf("signed release asset is missing: %s", name)
		}
	}
	if len(actualNames) != 16 {
		return fmt.Errorf("signed release contains %d assets, want 16", len(actualNames))
	}
	for _, item := range append(append([]artifact(nil), manifest.ChecksumSubjects...), *manifest.Checksum) {
		actual, err := artifactFromFile(filepath.Join(dist, item.Path), item.Path)
		if err != nil {
			return err
		}
		if actual != item {
			return fmt.Errorf("artifact mismatch for %s", item.Path)
		}
	}
	if err := validateChecksumSubjects(filepath.Join(dist, checksumAssetPath), manifest.ChecksumSubjects); err != nil {
		return err
	}
	if err := validateSPDXSubjects(dist, manifest.ChecksumSubjects); err != nil {
		return err
	}
	if err := verifySignatureBundle(filepath.Join(dist, manifest.Signature.Path), *manifest.Signature, manifest.Checksum.SHA256); err != nil {
		return err
	}
	return verifyProvenanceBundle(filepath.Join(dist, manifest.Provenance.Path), manifest, manifestPath)
}

func validateChecksumSubjects(path string, subjects []artifact) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	actual := make(map[string]string)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) != 2 {
			return fmt.Errorf("invalid SHA256SUMS line %q", scanner.Text())
		}
		digest := strings.ToLower(fields[0])
		name := strings.TrimPrefix(fields[1], "*")
		if len(digest) != 64 {
			return fmt.Errorf("invalid SHA256SUMS digest for %s", name)
		}
		if _, err := hex.DecodeString(digest); err != nil {
			return fmt.Errorf("invalid SHA256SUMS digest for %s", name)
		}
		if filepath.Base(name) != name {
			return fmt.Errorf("SHA256SUMS subject must be a top-level asset: %s", name)
		}
		if _, exists := actual[name]; exists {
			return fmt.Errorf("duplicate SHA256SUMS subject: %s", name)
		}
		actual[name] = digest
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if len(actual) != len(subjects) {
		return fmt.Errorf("SHA256SUMS contains %d subjects, want %d", len(actual), len(subjects))
	}
	for _, item := range subjects {
		digest, ok := actual[item.Path]
		if !ok {
			return fmt.Errorf("SHA256SUMS is missing subject %s", item.Path)
		}
		if digest != item.SHA256 {
			return fmt.Errorf("SHA256SUMS digest mismatch for %s", item.Path)
		}
	}
	return nil
}

func validateSPDXSubjects(dist string, subjects []artifact) error {
	for _, item := range subjects {
		if item.Kind != "sbom" {
			continue
		}
		file, err := os.Open(filepath.Join(dist, item.Path))
		if err != nil {
			return err
		}
		var document struct {
			SPDXVersion string `json:"spdxVersion"`
		}
		decodeErr := json.NewDecoder(file).Decode(&document)
		closeErr := file.Close()
		if decodeErr != nil {
			return fmt.Errorf("decode SPDX SBOM %s: %w", item.Path, decodeErr)
		}
		if closeErr != nil {
			return closeErr
		}
		if !strings.HasPrefix(document.SPDXVersion, "SPDX-") {
			return fmt.Errorf("SBOM is not an SPDX document: %s", item.Path)
		}
	}
	return nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
