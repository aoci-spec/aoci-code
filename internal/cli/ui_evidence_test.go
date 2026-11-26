package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/internal/testkit"
	"github.com/aoci-spec/aoci-code/internal/ui"
)

func TestUIEvidenceSuggestionsUseGuideActions(t *testing.T) {
	pinDefaultLocale(t)
	for _, mixed := range []bool{false, true} {
		name := "bootstrap"
		if mixed {
			name = "only_sources_owing_evidence"
		}
		t.Run(name, func(t *testing.T) {
			root := scopeActivationFixture(t)
			configureDatabaseCLISource(t, root)
			manifest, snapshot, files := databaseCLIFixtureSnapshot(t)
			if err := dbevidence.WriteSnapshot(root, manifest, snapshot, files); err != nil {
				t.Fatal(err)
			}
			if err := dbevidence.AcceptSnapshot(root, snapshot, snapshot.SourceSnapshotSHA256); err != nil {
				t.Fatal(err)
			}
			if mixed {
				cfg, err := config.LoadBase(root)
				if err != nil {
					t.Fatal(err)
				}
				cfg.DatabaseSources = append(cfg.DatabaseSources,
					dbevidence.SourceConfig{SourceID: "missing", Engine: dbevidence.EngineSQLite, Path: "missing.db", Enabled: true},
					dbevidence.SourceConfig{SourceID: "invalid", Engine: dbevidence.EngineSQLite, Path: "invalid.db", Enabled: true},
					dbevidence.SourceConfig{SourceID: "disabled", Engine: dbevidence.EngineSQLite, Path: "disabled.db"},
					dbevidence.SourceConfig{SourceID: "declared", Engine: dbevidence.EngineMongoDB, Database: "app", Namespaces: []string{"app"}, CredentialEnv: "AOCI_DB_DECLARED_URI", Enabled: true},
				)
				if err := config.Save(root, cfg); err != nil {
					t.Fatal(err)
				}
				testkit.WriteFile(t, dbevidence.RuntimeEvidenceRoot(root), "invalid/source.json", "{broken")
			}
			state := fetchUIEvidenceState(t, root)
			if state.Facts == nil || state.Facts.Result != "evidence_required" {
				t.Fatalf("expected evidence_required, got error=%s facts=%v", state.Error, state.Facts)
			}
			var guide struct {
				Commands map[string]string `json:"commands"`
			}
			if err := json.Unmarshal(state.Guide, &guide); err != nil {
				t.Fatal(err)
			}
			if mixed {
				wantStates := map[string]string{
					"primary":  machinecontract.DatabaseCognitionCurrent,
					"missing":  machinecontract.DatabaseCognitionEvidenceUnavailable,
					"invalid":  machinecontract.DatabaseCognitionEvidenceInvalid,
					"disabled": machinecontract.DatabaseCognitionSourceDisabled,
					"declared": machinecontract.DatabaseCognitionEngineNotImplemented,
				}
				if len(state.Facts.DatabaseCognition.Sources) != len(wantStates) {
					t.Fatalf("fixture is missing database sources: %+v", state.Facts.DatabaseCognition.Sources)
				}
				for _, source := range state.Facts.DatabaseCognition.Sources {
					if source.State != wantStates[source.SourceID] {
						t.Fatalf("unexpected fixture source state: %+v", source)
					}
				}
			} else if guide.Commands["database_snapshot"] != "" || guide.Commands["database_baseline_accept"] != "" {
				t.Error("the bootstrap Guide still offers commands to collect and accept Evidence")
			}
			for _, locale := range []string{"en-US", "zh-CN"} {
				var commands []string
				for _, suggestion := range state.Suggestions[locale] {
					if suggestion.Command != "" {
						commands = append(commands, suggestion.Command)
					}
				}
				if !mixed {
					if state.Facts.NextRequiredAction != machinecontract.DatabaseCognitionActionBootstrapVolume ||
						len(commands) != 1 || !strings.Contains(commands[0], "database cognition bootstrap") ||
						commands[0] != guide.Commands["database_bootstrap"] || strings.HasPrefix(commands[0], "aoci ") {
						t.Errorf("%s bootstrap did not use its bound Guide command: %v", locale, commands)
					}
					continue
				}
				snapshotCommand := guide.Commands["database_snapshot"]
				want := []string{strings.ReplaceAll(snapshotCommand, "{source}", "invalid"), strings.ReplaceAll(snapshotCommand, "{source}", "missing")}
				if snapshotCommand == "" || !slices.Equal(commands, want) {
					t.Errorf("%s expected only bound commands for invalid and missing Evidence: got %v want %v", locale, commands, want)
				}
			}
		})
	}
}

type uiEvidenceState struct {
	ui.Snapshot
	Guide json.RawMessage `json:"guide"`
}

func fetchUIEvidenceState(t *testing.T, root string) uiEvidenceState {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	writes := baseline.FingerprintCacheWritesEnabled()
	ready := make(chan string, 1)
	done := make(chan error, 1)
	options := ui.Options{Roots: []string{root}, RegistryDir: t.TempDir(), Locale: "en-US",
		Guide: func(root string, cfg *config.Config, set *cognition.Set) (any, error) {
			return buildVolumeAgentGuide(root, cfg, set, "ui")
		}}
	go func() { done <- ui.Serve(ctx, options, func(address string, _ int) { ready <- address }) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Error(err)
		}
		baseline.SetFingerprintCacheWrites(writes)
	})
	var address string
	select {
	case address = <-ready:
	case <-ctx.Done():
		t.Fatal("UI never became ready")
	}
	page, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	token := strings.TrimPrefix(page.Fragment, "access_token=")
	page.Fragment, page.Path = "", "/api/state"
	page.RawQuery = url.Values{"repo": {root}}.Encode()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, page.String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Authorization", "Bearer "+token)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var state uiEvidenceState
	if err := json.NewDecoder(response.Body).Decode(&state); err != nil || response.StatusCode != http.StatusOK {
		t.Fatalf("UI state status=%d decode=%v", response.StatusCode, err)
	}
	return state
}
