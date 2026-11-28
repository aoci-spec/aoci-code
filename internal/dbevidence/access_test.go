package dbevidence

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	opengauss "gitcode.com/opengauss/openGauss-connector-go-pq"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5"
)

func TestAccessPlanIsRedactedAndDoesNotOpenNetwork(t *testing.T) {
	source := SourceConfig{SourceID: "primary", Engine: EnginePostgreSQL, Database: "app",
		Namespaces: []string{"public"}, CredentialEnv: "AOCI_DB_PRIMARY_DSN", Enabled: true}
	secret := "postgres://secret-user:secret-password@secret-host/app"
	plan, err := InspectAccess(context.Background(), source, NewEnvironmentCredentialProvider(func(name string) string {
		if name != source.CredentialEnv {
			t.Fatalf("unexpected reference %q", name)
		}
		return secret
	}))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Version != AccessPlanVersion || plan.Status != AccessStatusReady ||
		plan.NextAction != AccessActionInspectSource || plan.NetworkAccessed || plan.CredentialSaved || plan.BusinessDataRead {
		t.Fatalf("unexpected access plan: %#v", plan)
	}
	encoded, _ := CanonicalJSON(plan)
	for _, forbidden := range []string{secret, "secret-user", "secret-password", "secret-host"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("access plan leaked %q", forbidden)
		}
	}
}

func TestAccessPlanReturnsOneActionWhenCredentialIsUnavailable(t *testing.T) {
	source := SourceConfig{SourceID: "primary", Engine: EngineMySQL, Database: "app",
		CredentialEnv: DefaultCredentialEnv("primary"), Enabled: true}
	plan, err := InspectAccess(context.Background(), source, NewEnvironmentCredentialProvider(func(string) string { return "" }))
	if err != nil {
		t.Fatal(err)
	}
	if plan.Status != AccessStatusActionRequired || plan.NextAction != AccessActionProvideReadOnly ||
		plan.Reference != "AOCI_DB_PRIMARY_DSN" {
		t.Fatalf("unexpected missing-access plan: %#v", plan)
	}
}

func TestDefaultCredentialEnvIsDeterministic(t *testing.T) {
	if got := DefaultCredentialEnv("orders-read"); got != "AOCI_DB_ORDERS_READ_DSN" {
		t.Fatalf("unexpected default reference: %s", got)
	}
}

func TestConnectionStringExamplesAreDriverVerifiedAndDocumented(t *testing.T) {
	for _, engine := range []Engine{EngineSQLite, EngineMongoDB, Engine("")} {
		if got := ConnectionStringExample(engine); got != "" {
			t.Fatalf("engine %q must not publish a server DSN example, got %q", engine, got)
		}
	}

	docsBytes, err := os.ReadFile(filepath.Join("..", "..", "docs", "database-evidence.md"))
	if err != nil {
		t.Fatalf("read docs/database-evidence.md: %v", err)
	}
	docsText := string(docsBytes)

	replacePlaceholders := func(example, port string) string {
		replacer := strings.NewReplacer(
			"<user>", "reader",
			"<password>", "placeholder",
			"<host>", "db.example.test",
			"<port>", port,
			"<database>", "app",
		)
		return replacer.Replace(example)
	}

	cases := []struct {
		engine Engine
		port   string
		parse  func(string) error
	}{
		{
			engine: EnginePostgreSQL,
			port:   "5432",
			parse: func(dsn string) error {
				_, err := pgx.ParseConfig(dsn)
				return err
			},
		},
		{
			engine: EngineMySQL,
			port:   "3306",
			parse: func(dsn string) error {
				_, err := mysqlDriver.ParseDSN(dsn)
				return err
			},
		},
		{
			engine: EngineOpenGauss,
			port:   "5432",
			parse: func(dsn string) error {
				_, err := opengauss.ParseConfigStrict(dsn)
				return err
			},
		},
	}

	for _, tc := range cases {
		example := ConnectionStringExample(tc.engine)
		if example == "" {
			t.Fatalf("missing connection string example for %s", tc.engine)
		}
		for _, placeholder := range []string{"<user>", "<password>", "<host>", "<port>", "<database>"} {
			if !strings.Contains(example, placeholder) {
				t.Fatalf("%s example %q missing placeholder %s", tc.engine, example, placeholder)
			}
		}
		if err := tc.parse(replacePlaceholders(example, tc.port)); err != nil {
			t.Fatalf("%s example %q was rejected by its driver parser: %v", tc.engine, example, err)
		}
		if !strings.Contains(docsText, example) {
			t.Fatalf("docs/database-evidence.md does not contain canonical %s example %q", tc.engine, example)
		}
	}

	for _, readme := range []string{"README.md", "README.zh-CN.md"} {
		raw, err := os.ReadFile(filepath.Join("..", "..", readme))
		if err != nil {
			t.Fatalf("read %s: %v", readme, err)
		}
		if !strings.Contains(string(raw), "docs/database-evidence.md#connection-string-formats") {
			t.Fatalf("%s does not point to docs/database-evidence.md#connection-string-formats", readme)
		}
	}
}
