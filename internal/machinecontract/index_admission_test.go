package machinecontract_test

import (
	"regexp"
	"strings"
	"testing"
)

// Index growth is one-way: every added Entry is spread permanently over every later Whole-Index delivery, and taking it back goes through
// the coverage-reduction Scope Change approval. The repository has over 600 *_test.go files, at roughly 110
// tokens per Entry as measured; putting them all in would double the Whole-Index and break through the 120k target line, buying only
// what a reader can already reach from the package under test.
//
// The admission line is written in AGENTS.md under "Cognition index admission": only a harness or suite that must be run by name, with its own
// preconditions, deserves an Entry; ordinary package tests stay observe, and a fact a test locks is recorded in the locked object's S.
// This place pins that line as a machine judgement and keeps it from being quietly deleted.
func TestOrdinaryPackageTestsNeverEnterTheWholeIndex(t *testing.T) {
	volume := readRepositoryFile(t, "aoci.code.txt")

	// An Entry line looks like: name[TAG]: F:... | R:... | A:... | S:...
	entryLine := regexp.MustCompile(`^(\S[^\[]*)\[[A-Za-z0-9]+\]: F:`)
	offenders := []string{}
	for _, line := range strings.Split(volume, "\n") {
		match := entryLine.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		name := match[1]
		if strings.HasSuffix(name, "_test.go") {
			offenders = append(offenders, name)
		}
	}
	if len(offenders) > 0 {
		t.Fatalf(
			"ordinary package tests must stay observe, not become Whole-Index Entries: %s\n"+
				"See the Cognition index admission rule in AGENTS.md. A test earns an Entry only when it is an "+
				"executable contract run by name with its own preconditions; a test that locks a fact belongs in "+
				"the locked object's S field instead.",
			strings.Join(offenders, ", "),
		)
	}
}

// The admission line itself must be protected from deletion: with the rule gone, the machine judgement above loses its explanation and its source of authority.
func TestCognitionIndexAdmissionRuleIsPublished(t *testing.T) {
	agents := readRepositoryFile(t, "AGENTS.md")
	for _, anchor := range []string{
		"## Cognition index admission",
		"executable contract",
		"stay `observe`",
		"locked object's `S`",
	} {
		if !strings.Contains(agents, anchor) {
			t.Errorf("AGENTS.md no longer states the index admission rule: missing %q", anchor)
		}
	}
}

// The verification obligation is just as easily deleted: Tier-1 gates run only in make full, that fact is in no gate's output,
// and only the documentation carries it; it is exactly the pit this repository really fell into.
func TestVerificationObligationsArePublished(t *testing.T) {
	agents := readRepositoryFile(t, "AGENTS.md")
	for _, anchor := range []string{
		"## Verification obligations",
		"make fast",
		"make full",
		"clean-room-smoke",
		"scripts/blackbox/mcp_conformance.py",
		"scripts/blackbox/mcp_scenarios.py",
		"scripts/blackbox/mcp_lifecycle.py",
	} {
		if !strings.Contains(agents, anchor) {
			t.Errorf("AGENTS.md no longer states the verification obligations: missing %q", anchor)
		}
	}
}
