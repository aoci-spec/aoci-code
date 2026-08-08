package dbevidence

import (
	"context"
	"fmt"
	"os"
	"strings"
)

const (
	AccessPlanVersion           = "database-access-plan/v1"
	AccessProviderEnvironment   = "environment"
	AccessStatusReady           = "ready"
	AccessStatusActionRequired  = "action_required"
	AccessStatusSourceDisabled  = "source_disabled"
	AccessActionInspectSource   = "inspect_source"
	AccessActionProvideReadOnly = "provide_read_only_catalog_credential"
)

// CredentialRequest contains only a non-secret provider reference. Provider
// implementations must never persist or return credentials through reports.
type CredentialRequest struct {
	SourceID  string
	Reference string
}

// CredentialProvider resolves runtime-only database access. R73 ships only
// the environment adapter; later Vault, Kubernetes, or cloud adapters can
// implement this interface without changing Source selection or Evidence.
type CredentialProvider interface {
	Kind() string
	Resolve(context.Context, CredentialRequest) (string, error)
}

type environmentCredentialProvider struct{ lookup func(string) string }

func (provider environmentCredentialProvider) Kind() string { return AccessProviderEnvironment }

func (provider environmentCredentialProvider) Resolve(_ context.Context, request CredentialRequest) (string, error) {
	value := provider.lookup(request.Reference)
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("credential_unavailable")
	}
	return value, nil
}

// NewEnvironmentCredentialProvider is the R73 minimum runtime adapter. The
// lookup function is injectable for tests; nil uses the process environment.
func NewEnvironmentCredentialProvider(lookup func(string) string) CredentialProvider {
	if lookup == nil {
		lookup = os.Getenv
	}
	return environmentCredentialProvider{lookup: lookup}
}

// AccessPlan is a redacted, read-only onboarding result. It reports only a
// provider reference and readiness; it never contains the resolved value.
type AccessPlan struct {
	Version          string `json:"version"`
	SourceID         string `json:"source_id"`
	Provider         string `json:"provider"`
	Reference        string `json:"reference"`
	Status           string `json:"status"`
	NextAction       string `json:"next_action"`
	CredentialSaved  bool   `json:"credential_saved"`
	NetworkAccessed  bool   `json:"network_accessed"`
	BusinessDataRead bool   `json:"business_data_read"`
}

// InspectAccess validates configuration and checks runtime credential
// availability without opening a database connection.
func InspectAccess(ctx context.Context, source SourceConfig, provider CredentialProvider) (AccessPlan, error) {
	if err := NormalizeSource(&source); err != nil {
		return AccessPlan{}, &SourceError{Code: "configuration_invalid", SourceID: "invalid", Op: "access_preflight"}
	}
	if provider == nil {
		provider = NewEnvironmentCredentialProvider(nil)
	}
	plan := AccessPlan{
		Version: AccessPlanVersion, SourceID: source.SourceID, Provider: provider.Kind(),
		Reference: source.CredentialEnv, Status: AccessStatusActionRequired,
		NextAction: AccessActionProvideReadOnly, CredentialSaved: false,
		NetworkAccessed: false, BusinessDataRead: false,
	}
	if !source.Enabled {
		plan.Status = AccessStatusSourceDisabled
		plan.NextAction = "enable_source"
		return plan, nil
	}
	if provider.Kind() != AccessProviderEnvironment {
		return AccessPlan{}, &SourceError{Code: "credential_provider_unsupported", SourceID: source.SourceID, Op: "access_preflight"}
	}
	if _, err := provider.Resolve(ctx, CredentialRequest{SourceID: source.SourceID, Reference: source.CredentialEnv}); err != nil {
		return plan, nil
	}
	plan.Status = AccessStatusReady
	plan.NextAction = AccessActionInspectSource
	return plan, nil
}

// DefaultCredentialEnv removes a setup decision from ordinary onboarding
// while preserving the existing environment-only secret boundary.
func DefaultCredentialEnv(sourceID string) string {
	value := strings.ToUpper(strings.ReplaceAll(sourceID, "-", "_"))
	return "AOCI_DB_" + value + "_DSN"
}
