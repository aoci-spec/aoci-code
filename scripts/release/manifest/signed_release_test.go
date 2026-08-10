package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"math/big"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

const signedFixtureVersion = "0.1.0-rc1"

var signedFixturePublisher = publisherIdentity{
	Repository: canonicalReleaseRepository,
	Workflow:   canonicalReleaseWorkflow,
	Ref:        "refs/tags/v0.1.0-rc1",
}

type signedReleaseFixture struct {
	dir          string
	manifestPath string
	manifest     releaseManifest
}

type provenanceFixtureOptions struct {
	omitSubject  string
	extraSubject string
	commit       string
	workflow     publisherIdentity
}

func TestSignedReleaseRoundTripAndExactInventory(t *testing.T) {
	fixture := newSignedReleaseFixture(t)
	if err := verifyExisting(fixture.manifestPath); err != nil {
		t.Fatalf("verify signed release: %v", err)
	}
}

func TestSignedReleaseGenerationDeclaresMissingEnvelopes(t *testing.T) {
	dir := t.TempDir()
	writeSignedReleaseInputs(t, dir)
	manifest := baseSignedManifest()
	output := filepath.Join(dir, manifestAssetPath)
	if err := prepareSignedReleaseManifest(&manifest, dir, output, signedFixturePublisher); err != nil {
		t.Fatal(err)
	}
	if manifest.Schema != releaseManifestSchema || len(manifest.ChecksumSubjects) != 12 || len(manifest.ProvenanceSubjects) != 14 {
		t.Fatalf("unexpected signed release contract: %+v", manifest)
	}
	if _, err := os.Stat(filepath.Join(dir, manifest.Signature.Path)); !os.IsNotExist(err) {
		t.Fatalf("signature envelope should not be required during manifest generation: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, manifest.Provenance.Path)); !os.IsNotExist(err) {
		t.Fatalf("provenance envelope should not be required during manifest generation: %v", err)
	}
}

func TestSignedReleaseFailsClosed(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*testing.T, *signedReleaseFixture)
		wantError string
	}{
		{
			name: "missing signature",
			mutate: func(t *testing.T, fixture *signedReleaseFixture) {
				if err := os.Remove(filepath.Join(fixture.dir, fixture.manifest.Signature.Path)); err != nil {
					t.Fatal(err)
				}
			},
			wantError: "signed release asset is missing: SHA256SUMS.sigstore.json",
		},
		{
			name: "missing provenance",
			mutate: func(t *testing.T, fixture *signedReleaseFixture) {
				if err := os.Remove(filepath.Join(fixture.dir, fixture.manifest.Provenance.Path)); err != nil {
					t.Fatal(err)
				}
			},
			wantError: "signed release asset is missing: AOCI-CODE_v0.1.0-rc1.provenance.sigstore.json",
		},
		{
			name: "signature covers a different checksum",
			mutate: func(t *testing.T, fixture *signedReleaseFixture) {
				writeSignatureFixture(t, filepath.Join(fixture.dir, fixture.manifest.Signature.Path), strings.Repeat("f", 64), fixture.manifest.Signature.CertificateIdentity, fixture.manifest.Signature.OIDCIssuer)
			},
			wantError: "signature bundle subject digest does not match SHA256SUMS",
		},
		{
			name: "provenance omits a formal subject",
			mutate: func(t *testing.T, fixture *signedReleaseFixture) {
				writeProvenanceFixture(t, fixture, provenanceFixtureOptions{omitSubject: fixture.manifest.ProvenanceSubjects[0]})
			},
			wantError: "provenance contains 13 subjects, want 14",
		},
		{
			name: "provenance includes an unuploaded temporary subject",
			mutate: func(t *testing.T, fixture *signedReleaseFixture) {
				writeProvenanceFixture(t, fixture, provenanceFixtureOptions{extraSubject: "metadata.json"})
			},
			wantError: "provenance contains 15 subjects, want 14",
		},
		{
			name: "provenance commit differs from artifact commit",
			mutate: func(t *testing.T, fixture *signedReleaseFixture) {
				writeProvenanceFixture(t, fixture, provenanceFixtureOptions{commit: strings.Repeat("b", 40)})
			},
			wantError: "provenance source ref or commit does not match the release manifest",
		},
		{
			name: "signature OIDC identity mismatch",
			mutate: func(t *testing.T, fixture *signedReleaseFixture) {
				writeSignatureFixture(t, filepath.Join(fixture.dir, fixture.manifest.Signature.Path), fixture.manifest.Checksum.SHA256, "https://github.com/example/other/.github/workflows/release.yml@refs/tags/v0.1.0-rc1", githubOIDCIssuer)
			},
			wantError: "signature bundle certificate: identity does not match",
		},
		{
			name: "signature OIDC issuer mismatch",
			mutate: func(t *testing.T, fixture *signedReleaseFixture) {
				writeSignatureFixture(t, filepath.Join(fixture.dir, fixture.manifest.Signature.Path), fixture.manifest.Checksum.SHA256, fixture.manifest.Signature.CertificateIdentity, "https://issuer.example.invalid")
			},
			wantError: "signature bundle certificate: OIDC issuer does not match",
		},
		{
			name: "provenance workflow identity mismatch",
			mutate: func(t *testing.T, fixture *signedReleaseFixture) {
				wrong := signedFixturePublisher
				wrong.Workflow = ".github/workflows/other.yml"
				writeProvenanceFixture(t, fixture, provenanceFixtureOptions{workflow: wrong})
			},
			wantError: "provenance publisher workflow identity does not match manifest",
		},
		{
			name: "extra undeclared release asset",
			mutate: func(t *testing.T, fixture *signedReleaseFixture) {
				writeFixture(t, filepath.Join(fixture.dir, "metadata.json"), "temporary")
			},
			wantError: "undeclared release asset: metadata.json",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := newSignedReleaseFixture(t)
			tt.mutate(t, fixture)
			err := verifyExisting(fixture.manifestPath)
			if err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("expected error containing %q, got %v", tt.wantError, err)
			}
		})
	}
}

func TestSignedReleaseRequiresExactTagVersion(t *testing.T) {
	manifest := baseSignedManifest()
	manifest.Schema = releaseManifestSchema
	manifest.ReleaseProfile = "tag"
	wrongPublisher := signedFixturePublisher
	wrongPublisher.Ref = "refs/tags/v0.1.0"
	manifest.Publisher = &wrongPublisher
	if err := validateSignedReleaseManifest(manifest); err == nil || !strings.Contains(err.Error(), "does not match release version") {
		t.Fatalf("expected mismatched tag/version to fail, got %v", err)
	}
}

func TestSignedReleaseRequiresCanonicalWorkflowAndDryRunRef(t *testing.T) {
	tests := []struct {
		name      string
		publisher publisherIdentity
		wantError string
	}{
		{
			name: "different repository workflow",
			publisher: publisherIdentity{
				Repository: canonicalReleaseRepository,
				Workflow:   ".github/workflows/other.yml",
				Ref:        "refs/tags/v0.1.0-rc1",
			},
			wantError: "publisher workflow must be .github/workflows/release.yml",
		},
		{
			name: "different dry-run branch",
			publisher: publisherIdentity{
				Repository: canonicalReleaseRepository,
				Workflow:   canonicalReleaseWorkflow,
				Ref:        "refs/heads/feature",
			},
			wantError: "dry-run publisher ref must be refs/heads/main",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := releaseProfile(signedFixtureVersion, tt.publisher.Ref); err != nil {
				if strings.Contains(err.Error(), tt.wantError) {
					return
				}
				t.Fatalf("unexpected release profile error: %v", err)
			}
			if err := validatePublisher(tt.publisher); err == nil || !strings.Contains(err.Error(), tt.wantError) {
				t.Fatalf("expected error containing %q, got %v", tt.wantError, err)
			}
		})
	}
}

func TestReleaseWorkflowUsesOneExactAttestationIdentityPolicy(t *testing.T) {
	workflowPath := filepath.Join("..", "..", "..", ".github", "workflows", "release.yml")
	workflow, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(string(workflow), "\n")
	var commands []string
	for lineIndex := 0; lineIndex < len(lines); lineIndex++ {
		line := strings.TrimSpace(lines[lineIndex])
		if !strings.HasPrefix(line, "gh attestation verify ") {
			continue
		}
		parts := []string{line}
		for strings.HasSuffix(line, "\\") {
			lineIndex++
			if lineIndex >= len(lines) {
				t.Fatal("gh attestation verify command has an unterminated continuation")
			}
			line = strings.TrimSpace(lines[lineIndex])
			parts = append(parts, line)
		}
		commands = append(commands, strings.Join(parts, " "))
	}
	if len(commands) != 2 {
		t.Fatalf("found %d gh attestation verify commands, want 2", len(commands))
	}

	requiredArguments := []string{
		`--repo "$EXPECTED_REPOSITORY"`,
		`--cert-identity "$CERTIFICATE_IDENTITY"`,
		`--cert-oidc-issuer "$EXPECTED_OIDC_ISSUER"`,
		`--signer-digest "$SOURCE_COMMIT"`,
		`--source-ref "$GITHUB_REF"`,
		`--source-digest "$SOURCE_COMMIT"`,
		`--deny-self-hosted-runners`,
	}
	for commandIndex, command := range commands {
		if strings.Contains(command, "--cert-identity ") && strings.Contains(command, "--signer-workflow ") {
			t.Fatalf("gh attestation verify command %d combines mutually exclusive identity policies: %s", commandIndex+1, command)
		}
		for _, argument := range requiredArguments {
			if !strings.Contains(command, argument) {
				t.Errorf("gh attestation verify command %d is missing %q: %s", commandIndex+1, argument, command)
			}
		}
	}
}

func TestReleaseProfileAcceptsPrereleaseAndStableTags(t *testing.T) {
	tests := []struct {
		version string
		ref     string
	}{
		{version: "0.1.0-rc2", ref: "refs/tags/v0.1.0-rc2"},
		{version: "0.1.1", ref: "refs/tags/v0.1.1"},
	}
	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			profile, err := releaseProfile(tt.version, tt.ref)
			if err != nil {
				t.Fatalf("release profile: %v", err)
			}
			if profile != "tag" {
				t.Fatalf("release profile = %q, want tag", profile)
			}
		})
	}
}

func TestReleaseWorkflowDerivesGitHubReleaseChannel(t *testing.T) {
	workflow := readRepositoryFile(t, ".github", "workflows", "release.yml")
	required := []string{
		"default: 0.1.0-rc3",
		"is_prerelease: ${{ steps.identity.outputs.is_prerelease }}",
		"is_latest: ${{ steps.identity.outputs.is_latest }}",
		"*-*)",
		"is_prerelease=true",
		"is_latest=false",
		"is_prerelease=false",
		"is_latest=true",
		`echo "is_prerelease=$is_prerelease"`,
		`echo "is_latest=$is_latest"`,
		"IS_PRERELEASE: ${{ needs.supply_chain.outputs.is_prerelease }}",
		"IS_LATEST: ${{ needs.supply_chain.outputs.is_latest }}",
		`test "$(jq -r .prerelease <<<"$release_json")" = "$IS_PRERELEASE"`,
		`test "$(jq -r .isPrerelease <<<"$release_json")" = "$IS_PRERELEASE"`,
		`if [ "$IS_LATEST" = true ]; then`,
		`latest_tag=$(gh api "repos/${EXPECTED_REPOSITORY}/releases/latest" --jq .tag_name)`,
		`test "$latest_tag" = "$TAG_NAME"`,
	}
	for _, fragment := range required {
		requireRepositoryText(t, workflow, fragment)
	}

	requireRepositoryTextCount(t, workflow, `--prerelease="$IS_PRERELEASE"`, 2)
	requireRepositoryTextCount(t, workflow, `--latest=false`, 1)
	requireRepositoryTextCount(t, workflow, `--latest="$IS_LATEST"`, 1)
	requireRepositoryTextCount(t, workflow, `test "$(jq -r .isPrerelease <<<"$release_json")" = "$IS_PRERELEASE"`, 2)
	for _, assignment := range []string{
		"is_prerelease=true",
		"is_prerelease=false",
		"is_latest=true",
		"is_latest=false",
	} {
		requireRepositoryTextCount(t, workflow, assignment, 1)
	}

	for _, forbidden := range []string{
		"default: 0.1.0-rc1",
		"--prerelease \\",
		`test "$(jq -r .prerelease <<<"$release_json")" = true`,
		`test "$(jq -r .isPrerelease <<<"$release_json")" = true`,
	} {
		if strings.Contains(workflow, forbidden) {
			t.Fatalf("release workflow retains hard-coded release channel fragment %q", forbidden)
		}
	}
}

func TestReleaseWorkflowBindsUTCBuildDateAndNonemptyChangelog(t *testing.T) {
	workflow := readRepositoryFile(t, ".github", "workflows", "release.yml")
	required := []string{
		`commit_epoch=$(git show -s --format=%ct HEAD)`,
		`build_date=$(date -u -d "@${commit_epoch}" +%Y-%m-%dT%H:%M:%SZ)`,
		`echo "build_date=$build_date"`,
		"BUILD_DATE: ${{ steps.identity.outputs.build_date }}",
		`grep -Fq "$BUILD_DATE" "$strings_file"`,
		`test "$version_output" = "aoci version ${VERSION} (commit ${SOURCE_COMMIT}, built ${BUILD_DATE})"`,
		`--build-date "$BUILD_DATE"`,
		`changelog_section="${RUNNER_TEMP}/release-changelog.md"`,
		`if ! grep -Eq '[^[:space:]]' "$changelog_section"; then`,
		`echo "CHANGELOG.md is missing a non-empty ## v${VERSION} section" >&2`,
		`cat "$changelog_section"`,
	}
	for _, fragment := range required {
		requireRepositoryText(t, workflow, fragment)
	}
	requireRepositoryTextCount(t, workflow, "BUILD_DATE: ${{ steps.identity.outputs.build_date }}", 2)
	requireRepositoryTextCount(t, workflow, `commit_epoch=$(git show -s --format=%ct HEAD)`, 1)
	requireRepositoryTextCount(t, workflow, `build_date=$(date -u -d "@${commit_epoch}" +%Y-%m-%dT%H:%M:%SZ)`, 1)
	if strings.Contains(workflow, "git show -s --format=%cI HEAD") {
		t.Fatal("release workflow still derives build_date from a non-normalized commit date")
	}
}

func TestMakefileAndGoReleaserKeepCanonicalBuildIdentity(t *testing.T) {
	makefile := readRepositoryFile(t, "Makefile")
	for _, fragment := range []string{
		`COMMIT  := $(shell git rev-parse HEAD 2>/dev/null || echo none)`,
		`DATE    := $(shell TZ=UTC0 git show -s --date=format-local:'%Y-%m-%dT%H:%M:%SZ' --format=%cd HEAD 2>/dev/null || date -u +%Y-%m-%dT%H:%M:%SZ)`,
		`--build-date "$(DATE)"`,
	} {
		requireRepositoryText(t, makefile, fragment)
	}
	for _, forbidden := range []string{
		"git rev-parse --short HEAD",
		"--build-date \"$$(git show -s --format=%cI HEAD)\"",
	} {
		if strings.Contains(makefile, forbidden) {
			t.Fatalf("Makefile retains non-canonical build identity fragment %q", forbidden)
		}
	}

	goreleaser := readRepositoryFile(t, ".goreleaser.yml")
	for _, fragment := range []string{
		"draft-to-release transition",
		"internal/cli.commit={{.FullCommit}}",
		"internal/cli.buildDate={{.CommitDate}}",
		"release:\n  disable: true",
	} {
		requireRepositoryText(t, goreleaser, fragment)
	}
	if strings.Contains(goreleaser, "draft-to-prerelease transition") {
		t.Fatal("GoReleaser release ownership comment is still prerelease-specific")
	}
}

func readRepositoryFile(t *testing.T, elements ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{"..", "..", ".."}, elements...)...)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func requireRepositoryText(t *testing.T, content, fragment string) {
	t.Helper()
	if !strings.Contains(content, fragment) {
		t.Fatalf("repository file is missing %q", fragment)
	}
}

func requireRepositoryTextCount(t *testing.T, content, fragment string, want int) {
	t.Helper()
	if got := strings.Count(content, fragment); got != want {
		t.Fatalf("repository file contains %q %d times, want %d", fragment, got, want)
	}
}

func TestBundleRejectsMultipleLeafCertificates(t *testing.T) {
	identity := publisherCertificateIdentity(signedFixturePublisher)
	material := bundleVerificationMaterial{
		X509CertificateChain: &struct {
			Certificates []bundleCertificate `json:"certificates"`
		}{
			Certificates: []bundleCertificate{
				{RawBytes: testCertificate(t, identity, githubOIDCIssuer)},
				{RawBytes: testCertificate(t, identity, githubOIDCIssuer)},
			},
		},
	}
	if err := verifyBundleCertificate(material, identity, githubOIDCIssuer); err == nil || !strings.Contains(err.Error(), "more than one leaf") {
		t.Fatalf("expected ambiguous leaf chain to fail, got %v", err)
	}
}

func newSignedReleaseFixture(t *testing.T) *signedReleaseFixture {
	t.Helper()
	dir := t.TempDir()
	writeSignedReleaseInputs(t, dir)
	manifest := baseSignedManifest()
	manifestPath := filepath.Join(dir, manifestAssetPath)
	if err := prepareSignedReleaseManifest(&manifest, dir, manifestPath, signedFixturePublisher); err != nil {
		t.Fatal(err)
	}
	if err := validateManifest(manifest); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(manifestPath, manifest); err != nil {
		t.Fatal(err)
	}
	fixture := &signedReleaseFixture{dir: dir, manifestPath: manifestPath, manifest: manifest}
	writeSignatureFixture(t, filepath.Join(dir, manifest.Signature.Path), manifest.Checksum.SHA256, manifest.Signature.CertificateIdentity, manifest.Signature.OIDCIssuer)
	writeProvenanceFixture(t, fixture, provenanceFixtureOptions{})
	return fixture
}

func baseSignedManifest() releaseManifest {
	return releaseManifest{
		Schema:       previousReleaseManifestSchema,
		Version:      signedFixtureVersion,
		SourceCommit: strings.Repeat("a", 40),
		BuildDate:    "2026-08-08T00:00:00Z",
		GoVersion:    "go1.26.5",
		GoReleaser:   "v2.17.1",
		Syft:         "v1.44.0",
		Contracts:    currentReleaseContracts(strings.Repeat("c", 64)),
	}
}

func writeSignedReleaseInputs(t *testing.T, dir string) {
	t.Helper()
	for _, target := range releaseTargetSuffixes {
		archiveName := "aoci_" + signedFixtureVersion + target
		writeFixture(t, filepath.Join(dir, archiveName), "archive:"+target)
		spdx, err := json.Marshal(map[string]string{
			"spdxVersion": "SPDX-2.3",
			"SPDXID":      "SPDXRef-DOCUMENT",
			"name":        archiveName,
		})
		if err != nil {
			t.Fatal(err)
		}
		writeFixture(t, filepath.Join(dir, archiveName+".sbom.json"), string(spdx))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, entry := range entries {
		digest, _, err := hashFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		lines = append(lines, digest+"  "+entry.Name())
	}
	sort.Strings(lines)
	writeFixture(t, filepath.Join(dir, checksumAssetPath), strings.Join(lines, "\n")+"\n")
}

func writeSignatureFixture(t *testing.T, path, checksumDigest, identity, issuer string) {
	t.Helper()
	digest, err := hex.DecodeString(checksumDigest)
	if err != nil {
		t.Fatal(err)
	}
	bundle := map[string]any{
		"mediaType": sigstoreBundleMediaType,
		"verificationMaterial": map[string]any{
			"x509CertificateChain": map[string]any{
				"certificates": []map[string]string{{"rawBytes": testCertificate(t, identity, issuer)}},
			},
		},
		"messageSignature": map[string]any{
			"messageDigest": map[string]string{
				"algorithm": "SHA2_256",
				"digest":    base64.StdEncoding.EncodeToString(digest),
			},
			"signature": base64.StdEncoding.EncodeToString([]byte("test-signature")),
		},
	}
	writeJSONFixture(t, path, bundle)
}

func writeProvenanceFixture(t *testing.T, fixture *signedReleaseFixture, options provenanceFixtureOptions) {
	t.Helper()
	publisher := options.workflow
	if publisher == (publisherIdentity{}) {
		publisher = *fixture.manifest.Publisher
	}
	commit := options.commit
	if commit == "" {
		commit = fixture.manifest.SourceCommit
	}
	var subjects []map[string]any
	for _, name := range fixture.manifest.ProvenanceSubjects {
		if name == options.omitSubject {
			continue
		}
		digest, _, err := hashFile(filepath.Join(fixture.dir, name))
		if err != nil {
			t.Fatal(err)
		}
		subjects = append(subjects, map[string]any{
			"name": name, "digest": map[string]string{"sha256": digest},
		})
	}
	if options.extraSubject != "" {
		subjects = append(subjects, map[string]any{
			"name":   options.extraSubject,
			"digest": map[string]string{"sha256": strings.Repeat("d", 64)},
		})
	}
	statement := map[string]any{
		"_type":         inTotoStatementType,
		"predicateType": slsaProvenanceType,
		"subject":       subjects,
		"predicate": map[string]any{
			"buildDefinition": map[string]any{
				"buildType": githubWorkflowBuildType,
				"externalParameters": map[string]any{
					"workflow": map[string]string{
						"repository": publisher.Repository,
						"path":       publisher.Workflow,
						"ref":        publisher.Ref,
					},
				},
				"resolvedDependencies": []map[string]any{{
					"uri":    "git+" + publisher.Repository + "@" + publisher.Ref,
					"digest": map[string]string{"gitCommit": commit},
				}},
			},
			"runDetails": map[string]any{
				"builder": map[string]string{"id": publisherCertificateIdentity(publisher)},
			},
		},
	}
	payload, err := json.Marshal(statement)
	if err != nil {
		t.Fatal(err)
	}
	bundle := map[string]any{
		"mediaType": sigstoreBundleMediaType,
		"verificationMaterial": map[string]any{
			"certificate": map[string]string{"rawBytes": testCertificate(t, publisherCertificateIdentity(*fixture.manifest.Publisher), githubOIDCIssuer)},
		},
		"dsseEnvelope": map[string]any{
			"payload":     base64.StdEncoding.EncodeToString(payload),
			"payloadType": "application/vnd.in-toto+json",
			"signatures":  []map[string]string{{"sig": base64.StdEncoding.EncodeToString([]byte("test-signature"))}},
		},
	}
	writeJSONFixture(t, filepath.Join(fixture.dir, fixture.manifest.Provenance.Path), bundle)
}

func testCertificate(t *testing.T, identity, issuer string) string {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	identityURI, err := url.Parse(identity)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{},
		NotBefore:    time.Unix(0, 0),
		NotAfter:     time.Unix(3600, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageCodeSigning},
		URIs:         []*url.URL{identityURI},
		ExtraExtensions: []pkix.Extension{{
			Id: fulcioOIDCIssuerOID, Value: []byte(issuer),
		}},
	}
	raw, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func writeJSONFixture(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}
