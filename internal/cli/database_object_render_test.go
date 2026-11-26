package cli

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/dbevidence"
	"github.com/aoci-spec/aoci-code/textassets"
)

func TestDatabaseCatalogObjectOutput(t *testing.T) {
	previous := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })
	for _, locale := range []string{"en-US", "zh-CN"} {
		t.Run(locale, func(t *testing.T) {
			if err := textassets.SetActiveLocale(locale); err != nil {
				t.Fatal(err)
			}
			for _, fixture := range []struct {
				name, ddl, want string
			}{
				{"sqlite_newline", "CREATE TABLE \"audit.ix)\nfixture\" (id INTEGER PRIMARY KEY) WITHOUT ROWID", `"audit.ix)\nfixture"`},
				{"sqlite_han", "CREATE TABLE \"客户 表\" (id INTEGER PRIMARY KEY) WITHOUT ROWID", `"客户 表"`},
				{"mysql_han", "", "分区表"},
			} {
				t.Run(fixture.name, func(t *testing.T) {
					var run func(bool) (string, string, int)
					op := "catalog_query_sqlite_table_options"
					if fixture.ddl == "" {
						op = "catalog_query_partitions_mysql_subpartition"
						// The MySQL partition collector passes the raw catalog
						// table name through SourceError.Object.
						err := databaseSourceExitError(&dbevidence.SourceError{
							SourceID: "primary", Code: "unsupported_catalog_feature", Op: op, Object: fixture.want,
						})
						run = func(jsonMode bool) (string, string, int) {
							var stdout, stderr bytes.Buffer
							code := finishBufferedExecution(err, jsonMode, nil, nil, &stdout, &stderr)
							return stdout.String(), stderr.String(), code
						}
					} else {
						root := databaseCLIRepo(t)
						db, err := sql.Open("sqlite3", filepath.Join(root, "fixture.db"))
						if err != nil {
							t.Fatal(err)
						}
						_, err = db.Exec(fixture.ddl)
						_ = db.Close()
						if err != nil {
							t.Fatal(err)
						}
						cfg, err := config.LoadBase(root)
						if err != nil {
							t.Fatal(err)
						}
						cfg.Locale = locale
						cfg.DatabaseSources = []dbevidence.SourceConfig{{SourceID: "primary", Engine: dbevidence.EngineSQLite, Path: "fixture.db", Enabled: true}}
						if err := config.Save(root, cfg); err != nil {
							t.Fatal(err)
						}
						run = func(jsonMode bool) (string, string, int) {
							args := []string{"database", "snapshot", "--source", "primary"}
							if jsonMode {
								args = append(args, "--json")
							}
							return runCLI(t, root, args...)
						}
					}
					plainOut, plainErr, plainCode := run(false)
					jsonOut, jsonErr, jsonCode := run(true)
					var envelope cliJSONErrorEnvelope
					if err := json.Unmarshal([]byte(jsonOut), &envelope); err != nil || jsonErr != "" || plainOut != "" ||
						plainCode != ExitInternal || jsonCode != ExitInternal || envelope.ErrorCode != "database_unsupported_catalog_feature" {
						t.Fatalf("invalid CLI failure envelope: text=%q JSON=%q JSON stderr=%q", plainErr, jsonOut, jsonErr)
					}
					prefix := "Database source "
					if locale == "zh-CN" {
						prefix = "数据库source "
					}
					for _, message := range []string{plainErr, envelope.Message} {
						if !strings.Contains(message, prefix) || !strings.Contains(message, "operation="+op) ||
							!strings.Contains(message, "object="+fixture.want) {
							t.Errorf("missing localized template or object in %q", message)
						}
					}
					if strings.Count(plainErr, "\n") != 1 || strings.ContainsAny(envelope.Message, "\r\n") {
						t.Errorf("object split the diagnostic: text=%q JSON message=%q", plainErr, envelope.Message)
					}
				})
			}
		})
	}
}

func TestExitMessageLocaleInferenceStillHandlesUntranslatedDiagnostics(t *testing.T) {
	previous := textassets.ActiveLocale()
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })
	for _, fixture := range []struct{ locale, foreign, fact string }{
		{"en-US", "无法访问 /tmp/aoci/config.json", "/tmp/aoci/config.json"},
		{"zh-CN", "permission denied for /tmp/aoci/config.json", "/tmp/aoci/config.json"},
	} {
		t.Run(fixture.locale, func(t *testing.T) {
			if err := textassets.SetActiveLocale(fixture.locale); err != nil {
				t.Fatal(err)
			}
			text := localizedCLIErrorMessage(&ExitError{Code: ExitConfig, Msg: fixture.foreign}, ExitConfig)
			if strings.Contains(text, fixture.foreign) || !strings.Contains(text, fixture.fact) ||
				!localeDiagnosticMatchesActiveLocale(text) {
				t.Fatalf("untranslated error was not localized with its facts: %q", text)
			}
		})
	}
}
