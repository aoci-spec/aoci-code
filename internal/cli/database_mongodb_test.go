package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
)

func TestMongoDBSourceCLIDeclarationAndUnimplementedOperations(t *testing.T) {
	root := databaseCLIRepo(t)
	const uri = "mongodb+srv://secret-user:secret-password@secret-host/app"
	t.Setenv("AOCI_DB_APP_URI", uri)
	mustCLI(t, root, "--json", "database", "source", "add", "--source-id", "app", "--engine", "mongodb",
		"--database-name", "app", "--credential-env", "AOCI_DB_APP_URI", "--include-table", "orders*", "--exclude-table", "orders_temp")
	cfg, err := config.LoadBase(root)
	if err != nil || len(cfg.DatabaseSources) != 1 {
		t.Fatalf("load declaration: cfg=%+v err=%v", cfg, err)
	}
	source := cfg.DatabaseSources[0]
	if source.Engine != dbevidence.EngineMongoDB || source.Database != "app" || !source.Enabled ||
		source.CredentialEnv != "AOCI_DB_APP_URI" || !slices.Equal(source.Namespaces, []string{"app"}) ||
		!slices.Equal(source.IncludeTables, []string{"orders*"}) || !slices.Equal(source.ExcludeTables, []string{"orders_temp"}) {
		t.Fatalf("declaration did not round-trip: %+v", source)
	}
	listed := mustCLI(t, root, "--json", "database", "source", "list")
	var result struct {
		Sources         []dbevidence.SourceConfig `json:"sources"`
		CredentialSaved bool                      `json:"credential_saved"`
		NetworkAccessed bool                      `json:"network_accessed"`
	}
	if err := json.Unmarshal([]byte(listed), &result); err != nil || len(result.Sources) != 1 || result.CredentialSaved || result.NetworkAccessed {
		t.Fatalf("bad source list: %s err=%v", listed, err)
	}
	for _, command := range [][]string{{"source", "access"}, {"source", "inspect"}, {"snapshot"}, {"verify"}} {
		args := append([]string{"--json", "database"}, command...)
		stdout, stderr, code := runCLI(t, root, append(args, "--source", "app")...)
		if code != ExitConfig || !strings.Contains(stdout+stderr, "database_engine_not_implemented") {
			t.Fatalf("%v: exit=%d stdout=%s stderr=%s", command, code, stdout, stderr)
		}
		if strings.Contains(stdout+stderr, "secret-") {
			t.Fatalf("%v leaked credentials: %s%s", command, stdout, stderr)
		}
	}
	raw, err := os.ReadFile(config.FilePath(root))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw)+listed, "secret-") || strings.Contains(string(raw)+listed, "mongodb+srv://") {
		t.Fatal("source declaration stored or listed credential material")
	}
	for _, path := range []string{dbevidence.RuntimeEvidenceRoot(root), dbevidence.BaselinePath(root), filepath.Join(root, "aoci.database.txt")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("declaration created database artifact %s: %v", path, err)
		}
	}
}
