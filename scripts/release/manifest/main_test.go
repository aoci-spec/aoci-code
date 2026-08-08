package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestManifestRoundTripAndTamperDetection(t *testing.T) {
	dist := t.TempDir()
	writeFixture(t, filepath.Join(dist, "aoci_test_linux_amd64.tar.gz"), "archive")
	writeFixture(t, filepath.Join(dist, "aoci_test_linux_amd64.tar.gz.sbom.json"), "sbom")
	writeFixture(t, filepath.Join(dist, "SHA256SUMS"), "checksums")
	output := filepath.Join(dist, "RELEASE-MANIFEST.json")
	artifacts, err := collectArtifacts(dist, output)
	if err != nil {
		t.Fatal(err)
	}
	manifest := releaseManifest{
		Schema:       previousReleaseManifestSchema,
		Version:      "test",
		SourceCommit: "commit",
		BuildDate:    "2026-01-01T00:00:00Z",
		GoVersion:    "go1.26",
		GoReleaser:   "v2",
		Syft:         "v1",
		Contracts:    currentReleaseContracts("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"),
		Artifacts:    artifacts,
	}
	if err := writeJSONAtomic(output, manifest); err != nil {
		t.Fatal(err)
	}
	if err := verifyExisting(output); err != nil {
		t.Fatalf("verify valid manifest: %v", err)
	}
	writeFixture(t, filepath.Join(dist, "aoci_test_linux_amd64.tar.gz"), "tampered")
	if err := verifyExisting(output); err == nil {
		t.Fatal("expected tampered archive to fail verification")
	}
}

func TestSignatureEnvelopeIsExcluded(t *testing.T) {
	dist := t.TempDir()
	output := filepath.Join(dist, "RELEASE-MANIFEST.json")
	writeFixture(t, output+".sig", "signature")
	artifacts, err := collectArtifacts(dist, output)
	if err != nil {
		t.Fatal(err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("signature envelope was included: %#v", artifacts)
	}
}

func TestLegacyV2ManifestRemainsVerifiable(t *testing.T) {
	dist := t.TempDir()
	writeFixture(t, filepath.Join(dist, "aoci_test_linux_amd64.tar.gz"), "archive")
	writeFixture(t, filepath.Join(dist, "aoci_test_linux_amd64.tar.gz.sbom.json"), "sbom")
	writeFixture(t, filepath.Join(dist, "SHA256SUMS"), "checksums")
	output := filepath.Join(dist, "RELEASE-MANIFEST.json")
	artifacts, err := collectArtifacts(dist, output)
	if err != nil {
		t.Fatal(err)
	}
	toolsSHA := "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
	manifest := releaseManifest{
		Schema: legacyReleaseManifestSchema, Version: "rc17", SourceCommit: "commit",
		BuildDate: "2026-01-01T00:00:00Z", GoVersion: "go1.26", GoReleaser: "v2", Syft: "v1",
		Contracts: legacyReleaseContracts(toolsSHA), Artifacts: artifacts,
	}
	if err := writeJSONAtomic(output, manifest); err != nil {
		t.Fatal(err)
	}
	if err := verifyExisting(output); err != nil {
		t.Fatalf("verify legacy v2 manifest: %v", err)
	}
}

func TestCurrentManifestBindsApplyAuthorizationContracts(t *testing.T) {
	contracts := currentReleaseContracts("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef")
	if contracts.ApplyAuthorizationPolicy != machinecontract.ApplyAuthorizationPolicyV1 ||
		contracts.PolicyBoundApproval != machinecontract.PolicyBoundApprovalV1 {
		t.Fatalf("authorization contracts missing from release manifest: %+v", contracts)
	}
}

func TestManifestRequiresExactlyOneSBOMPerArchive(t *testing.T) {
	tests := []struct {
		name      string
		paths     []string
		wantError string
	}{
		{
			name: "one per archive",
			paths: []string{
				"SHA256SUMS",
				"aoci_linux_amd64.tar.gz",
				"aoci_linux_amd64.tar.gz.sbom.json",
				"aoci_windows_amd64.zip",
				"aoci_windows_amd64.zip.sbom.json",
			},
		},
		{
			name: "missing sbom",
			paths: []string{
				"SHA256SUMS",
				"aoci_linux_amd64.tar.gz",
				"aoci_linux_amd64.tar.gz.sbom.json",
				"aoci_windows_amd64.zip",
			},
			wantError: "archive has no matching SBOM: aoci_windows_amd64.zip",
		},
		{
			name: "orphan sbom",
			paths: []string{
				"SHA256SUMS",
				"aoci_linux_amd64.tar.gz",
				"aoci_linux_amd64.tar.gz.sbom.json",
				"aoci_windows_amd64.zip.sbom.json",
			},
			wantError: "SBOM has no matching archive: [aoci_windows_amd64.zip.sbom.json]",
		},
		{
			name: "duplicate sbom",
			paths: []string{
				"SHA256SUMS",
				"aoci_linux_amd64.tar.gz",
				"aoci_linux_amd64.tar.gz.sbom.json",
				"aoci_linux_amd64.tar.gz.spdx.json",
			},
			wantError: "archive has multiple matching SBOMs: aoci_linux_amd64.tar.gz",
		},
		{
			name: "sbom name must retain archive extension",
			paths: []string{
				"SHA256SUMS",
				"aoci_linux_amd64.tar.gz",
				"aoci_linux_amd64.sbom.json",
			},
			wantError: "SBOM does not identify an archive: aoci_linux_amd64.sbom.json",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest := releaseManifest{
				Schema:       previousReleaseManifestSchema,
				Version:      "test",
				SourceCommit: "commit",
				BuildDate:    "2026-01-01T00:00:00Z",
				GoVersion:    "go1.26",
				GoReleaser:   "v2",
				Syft:         "v1",
				Contracts:    currentReleaseContracts("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"),
				Artifacts:    artifactFixtures(tt.paths...),
			}
			err := validateManifest(manifest)
			if tt.wantError == "" {
				if err != nil {
					t.Fatalf("validate manifest: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("expected error containing %q, got %v", tt.wantError, err)
			}
		})
	}
}

func TestManifestRejectsArtifactKindMismatch(t *testing.T) {
	artifacts := artifactFixtures("SHA256SUMS", "aoci_linux_amd64.tar.gz", "aoci_linux_amd64.tar.gz.sbom.json")
	artifacts[0].Kind = "archive"
	manifest := releaseManifest{
		Schema:       previousReleaseManifestSchema,
		Version:      "test",
		SourceCommit: "commit",
		BuildDate:    "2026-01-01T00:00:00Z",
		GoVersion:    "go1.26",
		GoReleaser:   "v2",
		Syft:         "v1",
		Contracts:    currentReleaseContracts("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"),
		Artifacts:    artifacts,
	}
	if err := validateManifest(manifest); err == nil || !strings.Contains(err.Error(), "invalid artifact kind") {
		t.Fatalf("expected artifact kind mismatch to fail, got %v", err)
	}
}

func TestManifestRejectsPlaceholderToolIdentity(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		set       func(*releaseManifest, string)
		wantError string
	}{
		{
			name:      "go missing",
			set:       func(manifest *releaseManifest, value string) { manifest.GoVersion = value },
			wantError: "Go identity is missing or placeholder",
		},
		{
			name:      "goreleaser embedded unknown case insensitive",
			value:     "GoReleaser Version: UnKnOwN",
			set:       func(manifest *releaseManifest, value string) { manifest.GoReleaser = value },
			wantError: "GoReleaser identity is missing or placeholder",
		},
		{
			name:      "syft not provided",
			value:     "[not provided]",
			set:       func(manifest *releaseManifest, value string) { manifest.Syft = value },
			wantError: "Syft identity is missing or placeholder",
		},
		{
			name:      "syft none case insensitive",
			value:     "NONE",
			set:       func(manifest *releaseManifest, value string) { manifest.Syft = value },
			wantError: "Syft identity is missing or placeholder",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifest := releaseManifest{
				Schema:       previousReleaseManifestSchema,
				Version:      "test",
				SourceCommit: "commit",
				BuildDate:    "2026-01-01T00:00:00Z",
				GoVersion:    "go1.26",
				GoReleaser:   "v2.17.1",
				Syft:         "v1.44.0",
				Contracts:    currentReleaseContracts("0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"),
				Artifacts:    artifactFixtures("SHA256SUMS", "aoci_linux_amd64.tar.gz", "aoci_linux_amd64.tar.gz.sbom.json"),
			}
			tt.set(&manifest, tt.value)
			err := validateManifest(manifest)
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("expected error containing %q, got %v", tt.wantError, err)
			}
		})
	}
}

func artifactFixtures(paths ...string) []artifact {
	artifacts := make([]artifact, 0, len(paths))
	for _, path := range paths {
		artifacts = append(artifacts, artifact{
			Path:   path,
			Kind:   artifactKind(path),
			SHA256: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
			Size:   1,
		})
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].Path < artifacts[j].Path })
	return artifacts
}

func writeFixture(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Fatal(err)
	}
}
