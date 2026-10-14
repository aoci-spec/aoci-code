package workflow

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/draft"
	"github.com/aoci-spec/aoci-code/internal/llm"
)

func TestEndpointErrorsNeverEnterPersistedDraftArtifacts(t *testing.T) {
	root, cfg, doc := buildRepo(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"synthetic-auth-value synthetic-private-source"}}`))
	}))
	defer server.Close()
	cfg.AI.BaseURL = server.URL
	client, err := llm.NewClient(llm.Options{BaseURL: server.URL, Model: "test", APIKey: "synthetic-auth-value"})
	if err != nil {
		t.Fatal(err)
	}
	result, err := RunEntriesDraft(context.Background(), root, cfg, doc, client, []string{"newfile.go"}, nil)
	if err != nil || result.Failed != 1 {
		t.Fatalf("expected one recorded failure: result=%+v err=%v", result, err)
	}
	manifest, err := draft.LoadManifest(root, result.RunID)
	if err != nil || len(manifest.Entries) != 1 || manifest.Entries[0].Note == "" {
		t.Fatalf("missing safe diagnosis: %v", err)
	}
	err = filepath.WalkDir(filepath.Join(root, ".aoci"), func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, forbidden := range []string{"synthetic-auth-value", "synthetic-private-source"} {
			if strings.Contains(string(data), forbidden) {
				t.Errorf("untrusted endpoint detail persisted in %s", filepath.Base(path))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
