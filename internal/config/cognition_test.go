package config

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestCognitionRefreshThresholdDefaultAndTeamAuthority(t *testing.T) {
	if got := DefaultConfig().CognitionRefreshThreshold; got != 30 {
		t.Fatalf("default cognition refresh threshold = %d", got)
	}
	if got := DefaultConfig().CognitionRefreshThreshold; got != machinecontract.CognitionRefreshThresholdDefault {
		t.Fatalf("default cognition refresh threshold = %d", got)
	}

	root := t.TempDir()
	writeLineEndingConfigFile(t, FilePath(root), `{"version":2,"cognition_refresh_threshold":5}`+"\n")
	writeLineEndingConfigFile(t, LocalFilePath(root), `{"version":2,"cognition_refresh_threshold":50}`+"\n")

	merged, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if merged.CognitionRefreshThreshold != 5 {
		t.Fatalf("local layer overrode team threshold: %d", merged.CognitionRefreshThreshold)
	}

	base, err := LoadBase(root)
	if err != nil {
		t.Fatal(err)
	}
	if base.CognitionRefreshThreshold != 5 {
		t.Fatalf("team threshold = %d", base.CognitionRefreshThreshold)
	}
}

func TestCognitionRefreshThresholdLegacyAndInvalidValues(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{name: "legacy missing", content: `{"version":2}`, wantErr: false},
		{name: "minimum", content: `{"version":2,"cognition_refresh_threshold":1}`, wantErr: false},
		{name: "maximum", content: `{"version":2,"cognition_refresh_threshold":100000}`, wantErr: false},
		{name: "negative", content: `{"version":2,"cognition_refresh_threshold":-1}`, wantErr: true},
		{name: "zero", content: `{"version":2,"cognition_refresh_threshold":0}`, wantErr: true},
		{name: "too large", content: `{"version":2,"cognition_refresh_threshold":100001}`, wantErr: true},
		{name: "non integer", content: `{"version":2,"cognition_refresh_threshold":"30"}`, wantErr: true},
	}

	for _, current := range tests {
		t.Run(current.name, func(t *testing.T) {
			root := t.TempDir()
			writeLineEndingConfigFile(t, FilePath(root), current.content+"\n")
			cfg, err := Load(root)
			if (err != nil) != current.wantErr {
				t.Fatalf("Load error = %v", err)
			}
			if err == nil && current.name == "legacy missing" &&
				cfg.CognitionRefreshThreshold != machinecontract.CognitionRefreshThresholdDefault {
				t.Fatalf("legacy default = %d", cfg.CognitionRefreshThreshold)
			}
		})
	}
}

func TestSaveLocalRemovesCognitionRefreshThreshold(t *testing.T) {
	root := t.TempDir()
	writeLineEndingConfigFile(
		t,
		LocalFilePath(root),
		`{"version":2,"cognition_refresh_threshold":5,"manual_key":"keep"}`+"\n",
	)
	cfg := DefaultConfig()
	cfg.AI.Model = "local-model"
	if err := SaveLocal(root, cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(LocalFilePath(root))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "cognition_refresh_threshold") {
		t.Fatalf("local policy key survived SaveLocal: %s", raw)
	}
	if !strings.Contains(string(raw), `"manual_key"`) {
		t.Fatalf("unrelated local key was removed: %s", raw)
	}
}

func TestOverviewChunkTokensDefaultBoundsAndTeamAuthority(t *testing.T) {
	if got := machinecontract.OverviewChunkTokensDefault; got != 600000 {
		t.Fatalf("machine default overview chunk tokens = %d, want 600000", got)
	}
	if got := DefaultConfig().OverviewDelivery.ChunkTokens; got != machinecontract.OverviewChunkTokensDefault {
		t.Fatalf("default overview chunk tokens = %d", got)
	}
	legacyRoot := t.TempDir()
	writeLineEndingConfigFile(t, FilePath(legacyRoot), `{"version":2}`+"\n")
	legacy, err := Load(legacyRoot)
	if err != nil {
		t.Fatal(err)
	}
	if legacy.OverviewDelivery.ChunkTokens != 600000 {
		t.Fatalf("missing legacy chunk_tokens default = %d, want 600000", legacy.OverviewDelivery.ChunkTokens)
	}
	for _, value := range []int{4000, 12000, 24000, 600000} {
		explicitRoot := t.TempDir()
		writeLineEndingConfigFile(t, FilePath(explicitRoot), fmt.Sprintf(`{"version":2,"overview_delivery":{"chunk_tokens":%d}}`, value)+"\n")
		explicit, err := Load(explicitRoot)
		if err != nil {
			t.Fatalf("explicit chunk token value %d failed: %v", value, err)
		}
		if explicit.OverviewDelivery.ChunkTokens != value {
			t.Fatalf("explicit chunk token value = %d, want %d", explicit.OverviewDelivery.ChunkTokens, value)
		}
	}
	root := t.TempDir()
	writeLineEndingConfigFile(t, FilePath(root), `{"version":2,"overview_delivery":{"chunk_tokens":12000}}`+"\n")
	writeLineEndingConfigFile(t, LocalFilePath(root), `{"version":2,"overview_delivery":{"chunk_tokens":600000}}`+"\n")
	loaded, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.OverviewDelivery.ChunkTokens != 12000 {
		t.Fatalf("local layer overrode team chunk tokens: %d", loaded.OverviewDelivery.ChunkTokens)
	}
	for _, value := range []int{3999, 600001} {
		invalidRoot := t.TempDir()
		writeLineEndingConfigFile(t, FilePath(invalidRoot), fmt.Sprintf(`{"version":2,"overview_delivery":{"chunk_tokens":%d}}`, value)+"\n")
		if _, err := Load(invalidRoot); err == nil {
			t.Fatalf("invalid chunk token value %d was accepted", value)
		}
	}
}

func TestSaveLocalRemovesOverviewDelivery(t *testing.T) {
	root := t.TempDir()
	writeLineEndingConfigFile(t, LocalFilePath(root), `{"version":2,"overview_delivery":{"chunk_tokens":12000},"manual_key":"keep"}`+"\n")
	if err := SaveLocal(root, DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(LocalFilePath(root))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "overview_delivery") || !strings.Contains(string(raw), `"manual_key"`) {
		t.Fatalf("local overview policy was not cleaned safely: %s", raw)
	}
}

func TestDatabaseCognitionBatchLimitsDefaultBoundsAndTeamAuthority(t *testing.T) {
	defaults := DefaultConfig()
	objects, evidenceBytes := defaults.DatabaseCognitionBatchLimits()
	if objects != machinecontract.DatabaseCognitionBatchObjectsDefault ||
		evidenceBytes != machinecontract.DatabaseCognitionBatchEvidenceBytesDefault {
		t.Fatalf("unexpected machine defaults: objects=%d evidence=%d", objects, evidenceBytes)
	}
	root := t.TempDir()
	writeLineEndingConfigFile(t, FilePath(root), `{"version":2,"database_cognition_batch_objects":10,"database_cognition_batch_evidence_bytes":65536}`+"\n")
	writeLineEndingConfigFile(t, LocalFilePath(root), `{"version":2,"database_cognition_batch_objects":99,"database_cognition_batch_evidence_bytes":1048576}`+"\n")
	loaded, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	objects, evidenceBytes = loaded.DatabaseCognitionBatchLimits()
	if objects != 10 || evidenceBytes != 65536 {
		t.Fatalf("local layer overrode team batch limits: objects=%d evidence=%d", objects, evidenceBytes)
	}

	for _, current := range []struct {
		name    string
		content string
	}{
		{"objects_low", `{"version":2,"database_cognition_batch_objects":-1}`},
		{"objects_high", `{"version":2,"database_cognition_batch_objects":201}`},
		{"evidence_low", `{"version":2,"database_cognition_batch_evidence_bytes":1}`},
		{"evidence_high", `{"version":2,"database_cognition_batch_evidence_bytes":16777217}`},
	} {
		t.Run(current.name, func(t *testing.T) {
			invalidRoot := t.TempDir()
			writeLineEndingConfigFile(t, FilePath(invalidRoot), current.content+"\n")
			if _, err := Load(invalidRoot); err == nil {
				t.Fatal("out-of-range batch limit was accepted")
			}
		})
	}
}

func TestSaveLocalRemovesDatabaseCognitionBatchLimits(t *testing.T) {
	root := t.TempDir()
	writeLineEndingConfigFile(t, LocalFilePath(root), `{"version":2,"database_cognition_batch_objects":5,"database_cognition_batch_evidence_bytes":65536,"manual_key":"keep"}`+"\n")
	if err := SaveLocal(root, DefaultConfig()); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(LocalFilePath(root))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "database_cognition_batch_") || !strings.Contains(string(raw), `"manual_key"`) {
		t.Fatalf("local batch policy was not cleaned safely: %s", raw)
	}
}
