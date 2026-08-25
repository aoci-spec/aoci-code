package cli

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognitiontxn"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/textassets"
)

func TestLocaleSwitchTransactionResumesExactTargetAfterRootPublish(t *testing.T) {
	root := t.TempDir()
	if _, stderr, code := runLocaleInit(t, root); code != ExitOK {
		t.Fatalf("init exit=%d stderr=%s", code, stderr)
	}
	establishLocaleSwitchBaseline(t, root)
	paths := config.AOCIPaths(root, "aoci.txt")
	rootBefore := localeSwitchRead(t, paths.IndexPath)
	configBefore := localeSwitchRead(t, paths.ConfigPath)
	baselineBefore := localeSwitchRead(t, paths.BaselinePath)
	wantRoot, err := index.ReplaceLocaleMarker(rootBefore, textassets.LegacyLocale)
	if err != nil {
		t.Fatal(err)
	}

	previousFault := localeSwitchFault
	fired := false
	localeSwitchFault = func(point string) error {
		if point == "after_publish_aoci.txt" && !fired {
			fired = true
			return errors.New("injected locale interruption")
		}
		return nil
	}
	t.Cleanup(func() { localeSwitchFault = previousFault })
	var stdout, stderr bytes.Buffer
	code := executeCLI([]string{"--repo", root, "config", "set", "locale", textassets.LegacyLocale}, &stdout, &stderr)
	if code != ExitConfig || !fired {
		t.Fatalf("faulted Locale switch exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if got := localeSwitchRead(t, paths.IndexPath); !bytes.Equal(got, wantRoot) {
		t.Fatal("Root postimage was not the exact marker-only rewrite")
	}
	if got := localeSwitchRead(t, paths.ConfigPath); !bytes.Equal(got, configBefore) {
		t.Fatal("config published before interrupted Root step completed")
	}
	if got := localeSwitchRead(t, paths.BaselinePath); !bytes.Equal(got, baselineBefore) {
		t.Fatal("Baseline published before config")
	}
	pending, err := cognitiontxn.PendingForOperation(root, localeSwitchOperation)
	if err != nil || len(pending) != 1 {
		t.Fatalf("Locale recovery Intent missing: pending=%v err=%v", pending, err)
	}
	stdout.Reset()
	stderr.Reset()
	code = executeCLI([]string{"--repo", root, "verify"}, &stdout, &stderr)
	if code != ExitInvalid || !strings.Contains(stdout.String()+stderr.String(), "locale-") {
		t.Fatalf("ordinary command crossed pending Locale recovery: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = executeCLI([]string{"--repo", root, "config", "set", "locale", textassets.DefaultLocale}, &stdout, &stderr)
	if code != ExitConfig {
		t.Fatalf("different recovery target was accepted: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if got := localeSwitchRead(t, paths.ConfigPath); !bytes.Equal(got, configBefore) {
		t.Fatal("conflicting target changed config")
	}

	localeSwitchFault = func(string) error { return nil }
	stdout.Reset()
	stderr.Reset()
	code = executeCLI([]string{"--repo", root, "config", "set", "locale", textassets.LegacyLocale}, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("Locale recovery failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	pending, err = cognitiontxn.PendingForOperation(root, localeSwitchOperation)
	if err != nil || len(pending) != 0 {
		t.Fatalf("completed Locale Intent remained active: pending=%v err=%v", pending, err)
	}
	cfg, err := config.LoadBase(root)
	if err != nil || cfg.Locale != textassets.LegacyLocale || cfg.LocaleMigration != nil {
		t.Fatalf("recovered Volume Locale state is invalid: cfg=%+v err=%v", cfg, err)
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists || state.Files[cfg.IndexPath].SHA256 != cognitiontxn.SHA256(wantRoot) {
		t.Fatalf("recovered Baseline is not bound to Root: exists=%t err=%v", exists, err)
	}
}

func TestLocaleSwitchTransactionGuardsVolumeAssets(t *testing.T) {
	root := t.TempDir()
	if _, stderr, code := runLocaleInit(t, root); code != ExitOK {
		t.Fatalf("init exit=%d stderr=%s", code, stderr)
	}
	establishLocaleSwitchBaseline(t, root)
	paths := config.AOCIPaths(root, "aoci.txt")
	rootBefore := localeSwitchRead(t, paths.IndexPath)
	configBefore := localeSwitchRead(t, paths.ConfigPath)
	baselineBefore := localeSwitchRead(t, paths.BaselinePath)

	previousFault := localeSwitchFault
	localeSwitchFault = func(point string) error {
		if point == "after_intent" {
			return errors.New("stop after immutable intent")
		}
		return nil
	}
	t.Cleanup(func() { localeSwitchFault = previousFault })
	if _, err := applyLocaleSwitch(root, textassets.LegacyLocale); err == nil {
		t.Fatal("injected pre-publish interruption was ignored")
	}
	metaPath := filepath.Join(root, "aoci.meta.txt")
	meta := localeSwitchRead(t, metaPath)
	if err := os.WriteFile(metaPath, append(meta, []byte("# third-party drift\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	localeSwitchFault = func(string) error { return nil }
	if _, err := applyLocaleSwitch(root, textassets.LegacyLocale); err == nil || !strings.Contains(err.Error(), "guard_drift") {
		t.Fatalf("Meta drift did not stop recovery: %v", err)
	}
	if !bytes.Equal(localeSwitchRead(t, paths.IndexPath), rootBefore) ||
		!bytes.Equal(localeSwitchRead(t, paths.ConfigPath), configBefore) ||
		!bytes.Equal(localeSwitchRead(t, paths.BaselinePath), baselineBefore) {
		t.Fatal("guard failure published a Locale target")
	}
}

func TestLocaleSwitchTransactionResumesEveryPublishedPrefix(t *testing.T) {
	for _, faultPoint := range []string{"after_publish_config.json", "after_publish_baseline.json"} {
		t.Run(faultPoint, func(t *testing.T) {
			root := t.TempDir()
			if _, stderr, code := runLocaleInit(t, root); code != ExitOK {
				t.Fatalf("init exit=%d stderr=%s", code, stderr)
			}
			establishLocaleSwitchBaseline(t, root)
			previousFault := localeSwitchFault
			fired := false
			localeSwitchFault = func(point string) error {
				if point == faultPoint && !fired {
					fired = true
					return errors.New("injected published-prefix interruption")
				}
				return nil
			}
			t.Cleanup(func() { localeSwitchFault = previousFault })
			if _, err := applyLocaleSwitch(root, textassets.LegacyLocale); err == nil || !fired {
				t.Fatalf("fault %s was ignored: %v", faultPoint, err)
			}
			localeSwitchFault = func(string) error { return nil }
			if _, err := applyLocaleSwitch(root, textassets.LegacyLocale); err != nil {
				t.Fatalf("resume after %s failed: %v", faultPoint, err)
			}
			pending, err := cognitiontxn.PendingForOperation(root, localeSwitchOperation)
			if err != nil || len(pending) != 0 {
				t.Fatalf("completed prefix recovery remained pending: %v, %v", pending, err)
			}
		})
	}
}

func TestLocaleSwitchRejectsUnsafeArchiveBeforeFormalWrites(t *testing.T) {
	for _, mode := range []string{"conflict", "symlink"} {
		t.Run(mode, func(t *testing.T) {
			if mode == "symlink" && runtime.GOOS == "windows" {
				t.Skip("symlink creation is not generally available to unprivileged Windows tests")
			}
			root := t.TempDir()
			if _, stderr, code := runLocaleInit(t, root); code != ExitOK {
				t.Fatalf("init exit=%d stderr=%s", code, stderr)
			}
			establishLocaleSwitchBaseline(t, root)
			paths := config.AOCIPaths(root, "aoci.txt")
			rootBefore := localeSwitchRead(t, paths.IndexPath)
			configBefore := localeSwitchRead(t, paths.ConfigPath)
			baselineBefore := localeSwitchRead(t, paths.BaselinePath)

			previousFault := localeSwitchFault
			localeSwitchFault = func(point string) error {
				if point == "after_intent" {
					return errors.New("stop after immutable intent")
				}
				return nil
			}
			t.Cleanup(func() { localeSwitchFault = previousFault })
			if _, err := applyLocaleSwitch(root, textassets.LegacyLocale); err == nil {
				t.Fatal("injected interruption was ignored")
			}
			pending, err := cognitiontxn.PendingForOperation(root, localeSwitchOperation)
			if err != nil || len(pending) != 1 {
				t.Fatalf("Locale intent missing: pending=%v err=%v", pending, err)
			}
			active := localeSwitchIntentPath(root, pending[0])
			archive := localeSwitchArchivePath(root, pending[0])
			if mode == "symlink" {
				if err := os.Symlink(active, archive); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(archive, []byte("third-party archive\n"), 0o644); err != nil {
				t.Fatal(err)
			}

			localeSwitchFault = func(string) error { return nil }
			if _, err := applyLocaleSwitch(root, textassets.LegacyLocale); err == nil || !strings.Contains(err.Error(), "archive_invalid") {
				t.Fatalf("unsafe archive was accepted: %v", err)
			}
			if !bytes.Equal(localeSwitchRead(t, paths.IndexPath), rootBefore) ||
				!bytes.Equal(localeSwitchRead(t, paths.ConfigPath), configBefore) ||
				!bytes.Equal(localeSwitchRead(t, paths.BaselinePath), baselineBefore) {
				t.Fatal("unsafe archive changed a formal participant")
			}
			pending, err = cognitiontxn.PendingForOperation(root, localeSwitchOperation)
			if err != nil || len(pending) != 1 {
				t.Fatalf("unsafe archive lost active recovery evidence: pending=%v err=%v", pending, err)
			}
		})
	}
}

func TestLocaleSwitchTransactionRejectsStaleRootBaselineWithoutWrites(t *testing.T) {
	root := t.TempDir()
	if _, stderr, code := runLocaleInit(t, root); code != ExitOK {
		t.Fatalf("init exit=%d stderr=%s", code, stderr)
	}
	establishLocaleSwitchBaseline(t, root)
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists {
		t.Fatalf("load Baseline: exists=%t err=%v", exists, err)
	}
	fingerprint := state.Files[cfg.IndexPath]
	fingerprint.SHA256 = strings.Repeat("f", 64)
	state.Files[cfg.IndexPath] = fingerprint
	if err := baseline.Save(root, state); err != nil {
		t.Fatal(err)
	}
	paths := config.AOCIPaths(root, cfg.IndexPath)
	rootBefore := localeSwitchRead(t, paths.IndexPath)
	configBefore := localeSwitchRead(t, paths.ConfigPath)
	baselineBefore := localeSwitchRead(t, paths.BaselinePath)
	if _, err := applyLocaleSwitch(root, textassets.LegacyLocale); err == nil || !strings.Contains(err.Error(), "baseline_root_stale") {
		t.Fatalf("stale Root Baseline was accepted: %v", err)
	}
	if !bytes.Equal(localeSwitchRead(t, paths.IndexPath), rootBefore) ||
		!bytes.Equal(localeSwitchRead(t, paths.ConfigPath), configBefore) ||
		!bytes.Equal(localeSwitchRead(t, paths.BaselinePath), baselineBefore) {
		t.Fatal("stale Baseline failure was not zero-write")
	}
}

func TestLocaleSwitchWithoutIndexCreatesTeamConfig(t *testing.T) {
	root := t.TempDir()
	cfg, err := applyLocaleSwitch(root, textassets.LegacyLocale)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Locale != textassets.LegacyLocale {
		t.Fatalf("config-only Locale = %s, want %s", cfg.Locale, textassets.LegacyLocale)
	}
	loaded, err := config.LoadBase(root)
	if err != nil || loaded.Locale != textassets.LegacyLocale {
		t.Fatalf("created team config did not retain Locale: cfg=%+v err=%v", loaded, err)
	}
	if _, err := os.Lstat(filepath.Join(root, loaded.IndexPath)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("config-only switch created formal cognition: %v", err)
	}
}

func TestLocaleSwitchExistingVolumesWithoutTeamConfig(t *testing.T) {
	root := t.TempDir()
	if _, stderr, code := runLocaleInit(t, root); code != ExitOK {
		t.Fatalf("init exit=%d stderr=%s", code, stderr)
	}
	establishLocaleSwitchBaseline(t, root)
	paths := config.AOCIPaths(root, "aoci.txt")
	rootBefore := localeSwitchRead(t, paths.IndexPath)
	metaBefore := localeSwitchRead(t, filepath.Join(root, "aoci.meta.txt"))
	codeBefore := localeSwitchRead(t, filepath.Join(root, "aoci.code.txt"))
	if err := os.Remove(paths.ConfigPath); err != nil {
		t.Fatal(err)
	}

	cfg, err := applyLocaleSwitch(root, textassets.LegacyLocale)
	if err != nil {
		t.Fatal(err)
	}
	wantRoot, err := index.ReplaceLocaleMarker(rootBefore, textassets.LegacyLocale)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Locale != textassets.LegacyLocale || !bytes.Equal(localeSwitchRead(t, paths.IndexPath), wantRoot) {
		t.Fatalf("missing-config switch did not align Root and config: locale=%s", cfg.Locale)
	}
	if !bytes.Equal(localeSwitchRead(t, filepath.Join(root, "aoci.meta.txt")), metaBefore) ||
		!bytes.Equal(localeSwitchRead(t, filepath.Join(root, "aoci.code.txt")), codeBefore) {
		t.Fatal("missing-config switch changed guarded object Volumes")
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists || state.Files[cfg.IndexPath].SHA256 != cognitiontxn.SHA256(wantRoot) {
		t.Fatalf("missing-config switch did not bind Root Baseline: exists=%t err=%v", exists, err)
	}
	pending, err := cognitiontxn.PendingForOperation(root, localeSwitchOperation)
	if err != nil || len(pending) != 0 {
		t.Fatalf("missing-config switch left recovery state: pending=%v err=%v", pending, err)
	}
}

func TestLocaleSwitchRejectsUnsafeConfiguredIndexPathBeforeIntent(t *testing.T) {
	for _, unsafePath := range []string{"../outside.txt", ".AOCI/config.json"} {
		t.Run(unsafePath, func(t *testing.T) {
			root := t.TempDir()
			cfg := config.DefaultConfig()
			cfg.IndexPath = unsafePath
			if err := config.Save(root, cfg); err != nil {
				t.Fatal(err)
			}
			if _, err := applyLocaleSwitch(root, textassets.LegacyLocale); err == nil || !strings.Contains(err.Error(), "index_path_invalid") {
				t.Fatalf("unsafe configured index path was accepted: %v", err)
			}
			pending, err := cognitiontxn.PendingForOperation(root, localeSwitchOperation)
			if err != nil || len(pending) != 0 {
				t.Fatalf("unsafe path created recovery state: pending=%v err=%v", pending, err)
			}
		})
	}
}

func TestLocaleSwitchSameTargetNeedsNoMissingBaseline(t *testing.T) {
	root := t.TempDir()
	if _, stderr, code := runLocaleInit(t, root); code != ExitOK {
		t.Fatalf("init exit=%d stderr=%s", code, stderr)
	}
	paths := config.AOCIPaths(root, "aoci.txt")
	if _, err := os.Lstat(paths.BaselinePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("fixture unexpectedly has a Baseline: %v", err)
	}
	rootBefore := localeSwitchRead(t, paths.IndexPath)
	configBefore := localeSwitchRead(t, paths.ConfigPath)
	if _, err := applyLocaleSwitch(root, textassets.DefaultLocale); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(localeSwitchRead(t, paths.IndexPath), rootBefore) ||
		!bytes.Equal(localeSwitchRead(t, paths.ConfigPath), configBefore) {
		t.Fatal("same-target no-op changed Root or config")
	}
}

func TestLocaleSwitchRepairsConfigRootMismatch(t *testing.T) {
	root := t.TempDir()
	if _, stderr, code := runLocaleInit(t, root); code != ExitOK {
		t.Fatalf("init exit=%d stderr=%s", code, stderr)
	}
	establishLocaleSwitchBaseline(t, root)
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Locale = textassets.LegacyLocale
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := applyLocaleSwitch(root, textassets.LegacyLocale); err != nil {
		t.Fatal(err)
	}
	rootRaw := localeSwitchRead(t, filepath.Join(root, cfg.IndexPath))
	rootLocale, explicit, err := index.DetectLocale(string(rootRaw))
	if err != nil || !explicit || rootLocale != textassets.LegacyLocale {
		t.Fatalf("Root Locale was not repaired: locale=%s explicit=%t err=%v", rootLocale, explicit, err)
	}
	state, exists, err := baseline.Load(root)
	if err != nil || !exists || state.Files[cfg.IndexPath].SHA256 != cognitiontxn.SHA256(rootRaw) {
		t.Fatalf("repaired Root was not rebound in Baseline: exists=%t err=%v", exists, err)
	}
}

func TestLocaleSwitchRejectsNoncanonicalIntentBeforeFormalWrites(t *testing.T) {
	root := t.TempDir()
	if _, stderr, code := runLocaleInit(t, root); code != ExitOK {
		t.Fatalf("init exit=%d stderr=%s", code, stderr)
	}
	establishLocaleSwitchBaseline(t, root)
	paths := config.AOCIPaths(root, "aoci.txt")
	rootBefore := localeSwitchRead(t, paths.IndexPath)
	configBefore := localeSwitchRead(t, paths.ConfigPath)
	baselineBefore := localeSwitchRead(t, paths.BaselinePath)
	previousFault := localeSwitchFault
	localeSwitchFault = func(point string) error {
		if point == "after_intent" {
			return errors.New("stop after immutable intent")
		}
		return nil
	}
	t.Cleanup(func() { localeSwitchFault = previousFault })
	if _, err := applyLocaleSwitch(root, textassets.LegacyLocale); err == nil {
		t.Fatal("injected interruption was ignored")
	}
	pending, err := cognitiontxn.PendingForOperation(root, localeSwitchOperation)
	if err != nil || len(pending) != 1 {
		t.Fatalf("Locale intent missing: pending=%v err=%v", pending, err)
	}
	intentPath := localeSwitchIntentPath(root, pending[0])
	raw := localeSwitchRead(t, intentPath)
	if err := os.WriteFile(intentPath, append([]byte("\n"), raw...), 0o644); err != nil {
		t.Fatal(err)
	}
	localeSwitchFault = func(string) error { return nil }
	if _, err := applyLocaleSwitch(root, textassets.LegacyLocale); err == nil || !strings.Contains(err.Error(), "noncanonical") {
		t.Fatalf("noncanonical recovery Intent was accepted: %v", err)
	}
	if !bytes.Equal(localeSwitchRead(t, paths.IndexPath), rootBefore) ||
		!bytes.Equal(localeSwitchRead(t, paths.ConfigPath), configBefore) ||
		!bytes.Equal(localeSwitchRead(t, paths.BaselinePath), baselineBefore) {
		t.Fatal("invalid Intent changed a formal participant")
	}
}

func TestLocaleSwitchRejectsSymlinkedStaging(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is not generally available to unprivileged Windows tests")
	}
	root := t.TempDir()
	if _, stderr, code := runLocaleInit(t, root); code != ExitOK {
		t.Fatalf("init exit=%d stderr=%s", code, stderr)
	}
	establishLocaleSwitchBaseline(t, root)
	previousFault := localeSwitchFault
	localeSwitchFault = func(point string) error {
		if point == "after_intent" {
			return errors.New("stop after immutable intent")
		}
		return nil
	}
	t.Cleanup(func() { localeSwitchFault = previousFault })
	if _, err := applyLocaleSwitch(root, textassets.LegacyLocale); err == nil {
		t.Fatal("injected interruption was ignored")
	}
	pending, err := cognitiontxn.PendingForOperation(root, localeSwitchOperation)
	if err != nil || len(pending) != 1 {
		t.Fatalf("Locale intent missing: pending=%v err=%v", pending, err)
	}
	intent, err := loadLocaleSwitchIntent(localeSwitchIntentPath(root, pending[0]))
	if err != nil {
		t.Fatal(err)
	}
	stagingPath := filepath.Join(root, filepath.FromSlash(intent.Staging[0].StagingRel))
	staged := localeSwitchRead(t, stagingPath)
	external := filepath.Join(root, ".aoci", "staged-copy")
	if err := os.WriteFile(external, staged, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(stagingPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(external, stagingPath); err != nil {
		t.Fatal(err)
	}
	localeSwitchFault = func(string) error { return nil }
	if _, err := applyLocaleSwitch(root, textassets.LegacyLocale); err == nil || !strings.Contains(err.Error(), "staging_invalid") {
		t.Fatalf("symlinked staging postimage was accepted: %v", err)
	}
}

func TestLegacyLocaleSwitchKeepsCustomHeaderAndEntries(t *testing.T) {
	root := t.TempDir()
	indexPath := "legacy.cognition.txt"
	rootSlash := strings.TrimRight(filepath.ToSlash(root), "/")
	if err := os.WriteFile(filepath.Join(root, "keep.go"), []byte("package keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	indexBefore := []byte("#Locale: en-US\n# Custom Header KeepTerm\n===Code " + rootSlash + "/===\n" +
		"keep.go[XAP7T]: F:Keep existing semantics | R:- | A:KeepAPI | S:-\n")
	if err := os.WriteFile(filepath.Join(root, indexPath), indexBefore, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config.DefaultConfig()
	cfg.Locale = textassets.DefaultLocale
	cfg.IndexPath = indexPath
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	saveCurrentBaseline(t, root, cfg)
	want, err := index.ReplaceLocaleMarker(indexBefore, textassets.LegacyLocale)
	if err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := executeCLI([]string{"--repo", root, "config", "set", "locale", textassets.LegacyLocale}, &stdout, &stderr)
	if code != ExitOK {
		t.Fatalf("Legacy Locale switch failed: code=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	if got := localeSwitchRead(t, filepath.Join(root, indexPath)); !bytes.Equal(got, want) {
		t.Fatalf("Legacy switch translated custom Header or Entry:\n got %q\nwant %q", got, want)
	}
	cfg, err = config.LoadBase(root)
	if err != nil || cfg.Locale != textassets.LegacyLocale || cfg.LocaleMigration == nil ||
		!cfg.LocaleMigration.HeaderPending || len(cfg.LocaleMigration.EntryPaths) != 0 {
		t.Fatalf("Legacy prospective migration receipt is invalid: cfg=%+v err=%v", cfg, err)
	}
}

func localeSwitchRead(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func establishLocaleSwitchBaseline(t *testing.T, root string) {
	t.Helper()
	cfg, err := config.LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	saveCurrentBaseline(t, root, cfg)
}
