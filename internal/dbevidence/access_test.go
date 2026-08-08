package dbevidence

import (
	"context"
	"strings"
	"testing"
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
