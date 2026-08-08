package cognition

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectedRootDescriptorGolden(t *testing.T) {
	raw := []byte(RootManifestMarker + "\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n#Project: fixture\n#Global-Invariants: fixture\n" +
		"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled\n" +
		"#Volume: id=code kind=code path=aoci.code.txt format=object-fras-v2 depends=meta state=disabled\n")
	descriptors, findings := ParseProjectedRoot(raw)
	if len(findings) != 0 {
		t.Fatalf("valid projected Root failed: %#v", findings)
	}
	if len(descriptors) != 2 || descriptors[0].ID != "meta" || descriptors[1].State != "disabled" {
		t.Fatalf("descriptor Golden changed: %#v", descriptors)
	}
}

func TestProjectedRootRejectsUnknownFormatDependencyAndUnsafePaths(t *testing.T) {
	base := RootManifestMarker + "\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n#Project: fixture\n#Global-Invariants: fixture\n"
	tests := map[string]string{
		"unknown kind":   "#Volume: id=api kind=api path=aoci.api.txt format=api-v1 depends=meta state=enabled",
		"unknown format": "#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v2 depends=- state=enabled",
		"absolute":       "#Volume: id=meta kind=meta path=/tmp/aoci.meta.txt format=meta-v1 depends=- state=enabled",
		"traversal":      "#Volume: id=meta kind=meta path=../aoci.meta.txt format=meta-v1 depends=- state=enabled",
		"drive":          "#Volume: id=meta kind=meta path=C:\\repo\\aoci.meta.txt format=meta-v1 depends=- state=enabled",
		"unc":            "#Volume: id=meta kind=meta path=\\\\server\\share\\aoci.meta.txt format=meta-v1 depends=- state=enabled",
		"field order":    "#Volume: kind=meta id=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled",
	}
	for name, declaration := range tests {
		t.Run(name, func(t *testing.T) {
			_, findings := ParseProjectedRoot([]byte(base + declaration + "\n"))
			if len(findings) == 0 {
				t.Fatalf("unsafe descriptor accepted: %s", declaration)
			}
		})
	}
}

func TestProjectedRootRejectsCaseFoldConflictAndSymlink(t *testing.T) {
	base := RootManifestMarker + "\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n#Project: fixture\n#Global-Invariants: fixture\n"
	raw := base + "#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled\n" +
		"#Volume: id=code kind=code path=AOCI.META.TXT format=object-fras-v2 depends=meta state=enabled\n"
	_, findings := ParseProjectedRoot([]byte(raw))
	if !containsProjectedFinding(findings, "duplicate_volume_path") {
		t.Fatalf("case-fold collision was not rejected: %#v", findings)
	}
	root := t.TempDir()
	if err := os.Symlink("target", filepath.Join(root, "aoci.meta.txt")); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	findings = ValidateProjectedTargetPaths(root, []Descriptor{{ID: "meta", Path: "aoci.meta.txt"}})
	if !containsProjectedFinding(findings, "volume_path_not_regular") {
		t.Fatalf("symlink candidate target was accepted: %#v", findings)
	}
}

func TestProjectedRootLineEndingsAndBOMBoundary(t *testing.T) {
	lf := RootManifestMarker + "\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n#Project: fixture\n#Global-Invariants: fixture\n#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled\n"
	if _, findings := ParseProjectedRoot([]byte(strings.ReplaceAll(lf, "\n", "\r\n"))); len(findings) != 0 {
		t.Fatalf("CRLF candidate was not parsed consistently: %#v", findings)
	}
	if _, findings := ParseProjectedRoot(append([]byte{0xef, 0xbb, 0xbf}, []byte(lf)...)); len(findings) != 0 {
		t.Fatalf("BOM read compatibility changed: %#v", findings)
	}
}

func containsProjectedFinding(findings []Finding, code string) bool {
	for _, finding := range findings {
		if finding.Code == code {
			return true
		}
	}
	return false
}
