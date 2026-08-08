package dbevidence

import (
	"fmt"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// SourceConfigMatchesManifest compares the non-secret source selection facts
// that determine which database objects a saved Evidence snapshot represents.
// Credentials and timeout policy deliberately do not participate.
func SourceConfigMatchesManifest(source SourceConfig, manifest SourceManifest) bool {
	return source.SourceID == manifest.SourceID &&
		source.Engine == manifest.Engine &&
		source.Database == manifest.Database &&
		slices.Equal(source.Namespaces, manifest.Namespaces) &&
		slices.Equal(source.IncludeNamespaces, manifest.IncludeNamespaces) &&
		slices.Equal(source.ExcludeNamespaces, manifest.ExcludeNamespaces) &&
		slices.Equal(source.IncludeTables, manifest.IncludeTables) &&
		slices.Equal(source.ExcludeTables, manifest.ExcludeTables)
}

const (
	defaultConnectTimeoutSeconds = 10
	defaultQueryTimeoutSeconds   = 30
)

var (
	sourceIDPattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)
	envNamePattern  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

func NormalizeSources(sources []SourceConfig) ([]SourceConfig, error) {
	normalized := append([]SourceConfig(nil), sources...)
	seen := make(map[string]struct{}, len(normalized))
	for index := range normalized {
		if err := NormalizeSource(&normalized[index]); err != nil {
			return nil, fmt.Errorf("database source %d: %w", index+1, err)
		}
		if _, exists := seen[normalized[index].SourceID]; exists {
			return nil, fmt.Errorf("duplicate database source_id %q", normalized[index].SourceID)
		}
		seen[normalized[index].SourceID] = struct{}{}
	}
	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i].SourceID < normalized[j].SourceID
	})
	return normalized, nil
}

func NormalizeSource(source *SourceConfig) error {
	if source == nil {
		return fmt.Errorf("source is nil")
	}
	if !sourceIDPattern.MatchString(source.SourceID) {
		return fmt.Errorf("source_id must match %s and be a logical non-secret name", sourceIDPattern)
	}
	if source.Engine != EnginePostgreSQL && source.Engine != EngineMySQL {
		return fmt.Errorf("engine must be postgresql or mysql")
	}
	if looksCredentialLike(source.Database) {
		return fmt.Errorf("database must be a name, not connection or credential material")
	}
	if err := validateIdentifier("database", source.Database); err != nil {
		return err
	}
	if !envNamePattern.MatchString(source.CredentialEnv) {
		return fmt.Errorf("credential_env must be an environment variable name, not a credential value")
	}
	if source.ConnectTimeoutSeconds == 0 {
		source.ConnectTimeoutSeconds = defaultConnectTimeoutSeconds
	}
	if source.QueryTimeoutSeconds == 0 {
		source.QueryTimeoutSeconds = defaultQueryTimeoutSeconds
	}
	if source.ConnectTimeoutSeconds < 1 || source.ConnectTimeoutSeconds > 300 {
		return fmt.Errorf("connect_timeout_seconds must be between 1 and 300")
	}
	if source.QueryTimeoutSeconds < 1 || source.QueryTimeoutSeconds > 900 {
		return fmt.Errorf("query_timeout_seconds must be between 1 and 900")
	}
	var err error
	source.Namespaces, err = normalizeIdentifiers("namespace", source.Namespaces)
	if err != nil {
		return err
	}
	if len(source.Namespaces) == 0 {
		if source.Engine == EnginePostgreSQL {
			source.Namespaces = []string{"public"}
		} else {
			source.Namespaces = []string{source.Database}
		}
	}
	if source.Engine == EngineMySQL {
		for _, namespace := range source.Namespaces {
			if namespace != source.Database {
				return fmt.Errorf("mysql namespace must equal the configured database in v1")
			}
		}
	}
	for _, target := range []struct {
		name  string
		value *[]string
	}{
		{"include_namespaces", &source.IncludeNamespaces},
		{"exclude_namespaces", &source.ExcludeNamespaces},
		{"include_tables", &source.IncludeTables},
		{"exclude_tables", &source.ExcludeTables},
	} {
		*target.value, err = normalizePatterns(target.name, *target.value)
		if err != nil {
			return err
		}
	}
	return nil
}

func validateIdentifier(kind, value string) error {
	if value == "" || !utf8.ValidString(value) {
		return fmt.Errorf("%s must be non-empty UTF-8", kind)
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must not have leading or trailing whitespace", kind)
	}
	for _, current := range value {
		if current == 0 || unicode.IsControl(current) {
			return fmt.Errorf("%s contains a control character", kind)
		}
	}
	return nil
}

func normalizeIdentifiers(kind string, values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		if looksCredentialLike(value) {
			return nil, fmt.Errorf("%s must not contain connection or credential material", kind)
		}
		if err := validateIdentifier(kind, value); err != nil {
			return nil, err
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

func normalizePatterns(kind string, values []string) ([]string, error) {
	result := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		if looksCredentialLike(value) {
			return nil, fmt.Errorf("%s must not contain connection or credential material", kind)
		}
		if err := validateIdentifier(kind, value); err != nil {
			return nil, err
		}
		if _, err := path.Match(value, "probe"); err != nil {
			return nil, fmt.Errorf("%s pattern %q is invalid: %w", kind, value, err)
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

func looksCredentialLike(value string) bool {
	lower := strings.ToLower(value)
	if strings.Contains(lower, "://") || strings.Contains(lower, "@tcp(") {
		return true
	}
	for _, marker := range []string{"password=", "passwd=", "pwd=", "token=", "secret=", "api_key=", "apikey="} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return strings.Contains(lower, "host=") && strings.Contains(lower, "user=")
}

func FindSource(sources []SourceConfig, sourceID string) (SourceConfig, bool) {
	for _, source := range sources {
		if source.SourceID == sourceID {
			return source, true
		}
	}
	return SourceConfig{}, false
}

func Included(source SourceConfig, namespace, table string) bool {
	if systemNamespace(source.Engine, namespace) || !containsExact(source.Namespaces, namespace) {
		return false
	}
	if matchesAny(source.ExcludeNamespaces, namespace) || matchesAny(source.ExcludeTables, table) {
		return false
	}
	if len(source.IncludeNamespaces) > 0 && !matchesAny(source.IncludeNamespaces, namespace) {
		return false
	}
	return len(source.IncludeTables) == 0 || matchesAny(source.IncludeTables, table)
}

func systemNamespace(engine Engine, namespace string) bool {
	switch engine {
	case EnginePostgreSQL:
		return namespace == "information_schema" || namespace == "pg_catalog" ||
			strings.HasPrefix(namespace, "pg_toast") || strings.HasPrefix(namespace, "pg_temp_")
	case EngineMySQL:
		switch strings.ToLower(namespace) {
		case "information_schema", "mysql", "performance_schema", "sys":
			return true
		}
	}
	return false
}

func containsExact(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

func matchesAny(patterns []string, value string) bool {
	for _, pattern := range patterns {
		matched, _ := path.Match(databaseGlobText(pattern), databaseGlobText(value))
		if matched {
			return true
		}
	}
	return false
}

// databaseGlobText keeps slash an ordinary identifier character instead of a
// path separator. Control characters are rejected from both patterns and
// identifiers, so the separator replacement cannot collide with input.
func databaseGlobText(value string) string {
	return strings.ReplaceAll(value, "/", "\x01")
}
