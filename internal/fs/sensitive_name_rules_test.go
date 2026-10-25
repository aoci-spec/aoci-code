package fs

import (
	"reflect"
	"testing"
)

// #148: the v2 rule set reads the shapes operational repositories use for
// credentials, by name only. v1 is the rc22 rule byte for byte, because it
// is part of every existing repository's scope identity; an absent value is
// v1.
func TestSensitiveNameRulesMatrix(t *testing.T) {
	cases := []struct {
		path   string
		v1, v2 bool
	}{
		// v1 names stay sensitive under both sets.
		{".env", true, true},
		{"app/.env.production", true, true},
		{"credentials.json", true, true},
		{".ssh/id_rsa", true, true},
		{"certs/server.key", true, true},
		// the #148 shapes
		{"conf/prod.env", false, true},
		{"config/manager-login.env", false, true},
		{"api.env.local", false, true},
		{"config/api-credentials.yaml", false, true},
		{"api_credentials.json", false, true},
		{"token.txt", false, true},
		{"tokens.json", false, true},
		{"passwords.json", false, true},
		{"etc/passwd", false, true},
		{"db.secret", false, true},
		{"api-key.txt", false, true},
		{"api_key.json", false, true},
		{"secret.properties", false, true},
		{"jwt.token", false, true},
		// Templates: v2 exempts every template suffix, v1 only .env.<name> ones.
		{".env.production.example", false, false},
		{"secrets.yaml.example", true, false},
		{"prod.env.sample", false, false},
		{"prod.env.template", false, false},
		{"api-credentials.yaml.dist", false, false},
		// Source and prose are never matched by name.
		{"src/tokenizer.go", false, false},
		{"src/password_hasher.go", false, false},
		{"auth/secret_test.go", false, false},
		{"src/token.ts", false, false},
		{"docs/tokens.md", false, false},
		{"config/environment.yaml", false, false},
		{"env.config.js", false, false},
		{"scripts/env", false, false},
		{"api.yaml", false, false},
		{"src/domains/credential/credential.service.ts", false, false},
	}
	for _, current := range cases {
		v1, sourceV1 := BuiltInSafetyCategory(current.path, SensitiveNameRulesV1)
		absent, _ := BuiltInSafetyCategory(current.path, "")
		v2, sourceV2 := BuiltInSafetyCategory(current.path, SensitiveNameRulesV2)
		if absent != v1 {
			t.Errorf("%s: an absent rule set must read as v1: absent=%q v1=%q", current.path, absent, v1)
		}
		if (v1 == SafetySensitive) != current.v1 || (v1 == SafetySensitive && sourceV1 != "builtin_sensitive_name") {
			t.Errorf("%s: v1 sensitive=%v (%q), want %v", current.path, v1 == SafetySensitive, sourceV1, current.v1)
		}
		if (v2 == SafetySensitive) != current.v2 || (v2 == SafetySensitive && sourceV2 != "builtin_sensitive_name") {
			t.Errorf("%s: v2 sensitive=%v (%q), want %v", current.path, v2 == SafetySensitive, sourceV2, current.v2)
		}
	}
}

func TestSensitiveNameRulesEnterTheRulesIdentityOnlyWhenChosen(t *testing.T) {
	absent := safeInventoryRulesIdentity(WalkOptions{})
	if safeInventoryRulesIdentity(WalkOptions{SensitiveNameRules: SensitiveNameRulesV1}) != absent {
		t.Fatal("v1 must be the identity an absent value already has")
	}
	if safeInventoryRulesIdentity(WalkOptions{SensitiveNameRules: SensitiveNameRulesV2}) == absent {
		t.Fatal("v2 must change the rules identity: it changes the candidate set")
	}
	if NormalizeSensitiveNameRules("") != SensitiveNameRulesV1 || NormalizeSensitiveNameRules("v2") != SensitiveNameRulesV2 {
		t.Fatal("normalization must map an absent value to v1 and keep v2")
	}
	if !ValidSensitiveNameRules("") || !ValidSensitiveNameRules("v1") || !ValidSensitiveNameRules("v2") || ValidSensitiveNameRules("v3") {
		t.Fatal("only an absent value, v1 and v2 name a rule set")
	}
}

// The inventory under v2 excludes credential-shaped file names before any
// read, keeps source whose name merely contains a credential word, and still
// walks a directory named like one: the rule reads file names.
func TestSafeInventoryV2ExcludesCredentialNamesAndWalksATokenPackage(t *testing.T) {
	root := t.TempDir()
	excluded := []string{"conf/prod.env", "config/api-credentials.yaml", "passwords.json", "api.env.local", "deploy/manager-login.env"}
	kept := []string{"src/tokenizer.go", "token/lexer.go", "secrets/README.md", "credential/service.go"}
	for _, path := range append(append([]string{}, excluded...), kept...) {
		mustWrite(t, root, path, "body\n")
	}
	mustWrite(t, root, "config/secrets.yaml.example", "key: <fill in>\n")

	v2, err := BuildSafeInventory(root, WalkOptions{SensitiveNameRules: SensitiveNameRulesV2})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range excluded {
		if containsPath(v2.ManagedCandidates, path) {
			t.Fatalf("v2 let a credential-shaped name into the managed set: %s", path)
		}
		found := false
		for _, exclusion := range v2.Exclusions {
			if exclusion.PathSummary == path {
				found = exclusion.Category == SafetySensitive && exclusion.RuleSource == "builtin_sensitive_name"
			}
		}
		if !found {
			t.Fatalf("v2 must report %s as a built-in sensitive exclusion: %#v", path, v2.Exclusions)
		}
	}
	for _, path := range append(kept, "config/secrets.yaml.example") {
		if !containsPath(v2.ManagedCandidates, path) {
			t.Fatalf("v2 excluded source or a template: %s (candidates %v, exclusions %#v)", path, v2.ManagedCandidates, v2.Exclusions)
		}
	}
	if v2.Summary.SensitiveNameRules != SensitiveNameRulesV2 || v2.Summary.BuiltinSensitiveExcluded != len(excluded) {
		t.Fatalf("v2 summary: %#v", v2.Summary)
	}

	v1, err := BuildSafeInventory(root, WalkOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range append(append([]string{}, excluded...), kept...) {
		if !containsPath(v1.ManagedCandidates, path) {
			t.Fatalf("v1 must keep the rc22 candidate set: %s missing from %v", path, v1.ManagedCandidates)
		}
	}
	if containsPath(v1.ManagedCandidates, "config/secrets.yaml.example") || v1.Summary.SensitiveNameRules != SensitiveNameRulesV1 {
		t.Fatalf("v1 summary or candidates changed: %#v", v1.Summary)
	}
	if v1.Summary.RulesIdentity == v2.Summary.RulesIdentity {
		t.Fatal("the rule set must enter the rules identity")
	}
	want := []string{"api.env.local", "conf/prod.env", "config/api-credentials.yaml", "deploy/manager-login.env", "passwords.json"}
	if got := SensitiveNameUpgradeCandidates(v1.ManagedCandidates); !reflect.DeepEqual(got, want) {
		t.Fatalf("upgrade candidates = %v, want %v", got, want)
	}
}
