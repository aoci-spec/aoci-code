package cognitionbaseline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
)

const testCodeVolume = "#AOCI-CODE-VOLUME: 1\n\n===/<repository>/===\nmain.go[CG5T]: F:Runs the fixture | R:- | A:- | S:-\n"

func write(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func sourcePlan(t *testing.T, root string) *cognitionplan.Plan {
	t.Helper()
	write(t, root, "main.go", "package main\n")
	write(t, root, "skipped.bin", "x")
	fingerprint, err := baseline.HashFile(filepath.Join(root, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	return &cognitionplan.Plan{Inventory: []cognitionplan.InventoryObject{
		{Path: "main.go", SourceSHA256: fingerprint.SHA256, Eligible: true},
		{Path: "skipped.bin", SourceSHA256: "ignored", Eligible: false},
	}}
}

// The Baseline postimage Bootstrap and Migration publish records each eligible
// source at the digest the plan bound and the Code Volume at the candidate's
// bytes, and nothing else: an ineligible object is not fingerprinted.
func TestBuildVolumePostimageBindsPlannedSourcesAndTheCodeVolume(t *testing.T) {
	root := t.TempDir()
	plan := sourcePlan(t, root)
	assets := map[string]cognitionplan.CandidateAsset{"code": {Path: "aoci.code.txt", Content: testCodeVolume}}
	value, bindings, err := BuildVolumePostimage(root, plan, &cognition.Set{}, assets, "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if len(bindings) != 0 || len(value.Files) != 2 {
		t.Fatalf("unexpected postimage: files=%v bindings=%v", value.Files, bindings)
	}
	if value.Files["main.go"].SHA256 != plan.Inventory[0].SourceSHA256 ||
		value.Files["aoci.code.txt"].SHA256 != baseline.HashBytes("aoci.code.txt", []byte(testCodeVolume)).SHA256 {
		t.Fatalf("postimage fingerprints are not the planned ones: %+v", value.Files)
	}
	if _, fingerprinted := value.Files["skipped.bin"]; fingerprinted {
		t.Fatal("an ineligible object entered the Baseline")
	}
	if value.CreatedAt != "2026-01-01T00:00:00Z" {
		t.Fatalf("the postimage must carry the caller's timestamp, got %q", value.CreatedAt)
	}
}

// A source that moved since planning, vanished, or became a symlink stops the
// postimage: publishing it would bind an Entry to bytes no model read.
func TestBuildVolumePostimageRefusesDriftedOrUnsafeSources(t *testing.T) {
	assets := map[string]cognitionplan.CandidateAsset{"code": {Path: "aoci.code.txt", Content: testCodeVolume}}
	for name, breakSource := range map[string]func(t *testing.T, root string){
		"drift": func(t *testing.T, root string) { write(t, root, "main.go", "package main // edited\n") },
		"missing": func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, "main.go")); err != nil {
				t.Fatal(err)
			}
		},
		"symlink": func(t *testing.T, root string) {
			write(t, root, "real.go", "package main\n")
			if err := os.Remove(filepath.Join(root, "main.go")); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("real.go", filepath.Join(root, "main.go")); err != nil {
				t.Skip("symlinks unavailable")
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			plan := sourcePlan(t, root)
			breakSource(t, root)
			_, _, err := BuildVolumePostimage(root, plan, &cognition.Set{}, assets, "2026-01-01T00:00:00Z")
			if err == nil || !strings.Contains(err.Error(), "cognition_source_guard_") {
				t.Fatalf("a %s source must stop the postimage, got %v", name, err)
			}
		})
	}
}

// publishedRepo writes a small Volumes v1 repository whose Baseline records
// its source and Code Volume, the state VerifyVolumeState is asked to prove.
func publishedRepo(t *testing.T) (root string, set *cognition.Set, baselineSHA string) {
	t.Helper()
	root = t.TempDir()
	for _, name := range []string{"aoci.txt", "aoci.meta.txt"} {
		data, err := os.ReadFile(filepath.Join("..", "..", name))
		if err != nil {
			t.Fatal(err)
		}
		write(t, root, name, string(data))
	}
	write(t, root, "aoci.code.txt", testCodeVolume)
	plan := sourcePlan(t, root)
	value, _, err := BuildVolumePostimage(root, plan, &cognition.Set{},
		map[string]cognitionplan.CandidateAsset{"code": {Path: "aoci.code.txt", Content: testCodeVolume}}, "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if err := baseline.Save(root, value); err != nil {
		t.Fatal(err)
	}
	set, err = cognition.Load(root, "aoci.txt")
	if err != nil {
		t.Fatal(err)
	}
	fingerprint, err := baseline.HashFile(filepath.Join(root, ".aoci", "baseline.json"))
	if err != nil {
		t.Fatal(err)
	}
	return root, set, fingerprint.SHA256
}

func TestVerifyVolumeStateAcceptsThePublishedStateAndNamesEachMismatch(t *testing.T) {
	root, set, baselineSHA := publishedRepo(t)
	codeSHA := baseline.HashBytes("aoci.code.txt", []byte(testCodeVolume)).SHA256
	targets := []FormalTarget{{Path: "aoci.code.txt", SHA256: codeSHA}}
	if _, err := VerifyVolumeState(root, set.CompositeIdentity, baselineSHA, targets, []string{"code"}); err != nil {
		t.Fatalf("the published state must verify: %v", err)
	}
	for name, step := range map[string]struct {
		call func() error
		want string
	}{
		"composite": {func() error {
			_, err := VerifyVolumeState(root, "0000", baselineSHA, targets, []string{"code"})
			return err
		}, "projected composite identity mismatch"},
		"formal target": {func() error {
			_, err := VerifyVolumeState(root, set.CompositeIdentity, baselineSHA, []FormalTarget{{Path: "aoci.code.txt", SHA256: "0000"}}, []string{"code"})
			return err
		}, "formal target mismatch: aoci.code.txt"},
		"baseline digest": {func() error {
			_, err := VerifyVolumeState(root, set.CompositeIdentity, "0000", targets, []string{"code"})
			return err
		}, "baseline mismatch"},
		"absent volume": {func() error {
			_, err := VerifyVolumeState(root, set.CompositeIdentity, baselineSHA, targets, []string{"database"})
			return err
		}, "enabled Volume unavailable: database"},
	} {
		if err := step.call(); err == nil || !strings.Contains(err.Error(), step.want) {
			t.Fatalf("%s: want %q, got %v", name, step.want, err)
		}
	}
	// A source edited after publication no longer matches its Baseline record.
	write(t, root, "main.go", "package main // later edit\n")
	if _, err := VerifyVolumeState(root, set.CompositeIdentity, baselineSHA, targets, []string{"code"}); err == nil ||
		!strings.Contains(err.Error(), "baseline fingerprint mismatch: main.go") {
		t.Fatalf("a drifted source must fail verification, got %v", err)
	}
}
