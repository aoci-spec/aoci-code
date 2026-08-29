package baseline

import (
	"os"
	"path/filepath"
	"testing"
)

const reuseSource = "package p\n\nimport \"fmt\"\n\nfunc F() { fmt.Println(\"x\") }\n"

func writeReuseFixture(t *testing.T, name, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// The whole point of the reuse path is that it is invisible. Whatever prior a
// caller supplies — absent, matching, stale, or corrupt — the Fingerprint must
// be the one a cold HashFile produces from the same bytes.
func TestHashFileReusingIsIndistinguishableFromAColdHash(t *testing.T) {
	path := writeReuseFixture(t, "a.go", reuseSource)
	cold, err := HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cold.FormatSHA256 == "" || cold.FormatKind != "gofmt" {
		t.Fatalf("fixture precondition: a Go file must produce a gofmt digest, got %+v", cold)
	}

	other, err := HashFile(writeReuseFixture(t, "b.go", reuseSource+"\nfunc G() {}\n"))
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		name  string
		prior Fingerprint
		// reused says whether this prior is expected to take the fast path.
		// It never changes the result, only the cost.
		reused bool
	}{
		{"empty prior", Fingerprint{}, false},
		{"exact prior", cold, true},
		{"prior for different bytes", other, false},
		{"right digests, wrong size", Fingerprint{SHA256: cold.SHA256, Size: cold.Size + 1,
			NormalizedSHA256: cold.NormalizedSHA256, FormatSHA256: cold.FormatSHA256, FormatKind: "gofmt"}, false},
		{"format kind without digest", Fingerprint{SHA256: cold.SHA256, Size: cold.Size,
			NormalizedSHA256: cold.NormalizedSHA256, FormatKind: "gofmt"}, false},
		{"format digest without kind", Fingerprint{SHA256: cold.SHA256, Size: cold.Size,
			NormalizedSHA256: cold.NormalizedSHA256, FormatSHA256: cold.FormatSHA256}, false},
		{"unknown formatter", Fingerprint{SHA256: cold.SHA256, Size: cold.Size,
			NormalizedSHA256: cold.NormalizedSHA256, FormatSHA256: cold.FormatSHA256, FormatKind: "rustfmt"}, false},
		{"missing normalized digest", Fingerprint{SHA256: cold.SHA256, Size: cold.Size,
			FormatSHA256: cold.FormatSHA256, FormatKind: "gofmt"}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := HashFileReusing(path, test.prior)
			if err != nil {
				t.Fatal(err)
			}
			if got != cold {
				t.Fatalf("reuse changed the fingerprint\n prior %+v\n  cold %+v\n   got %+v", test.prior, cold, got)
			}
		})
	}
}

// A prior may only supply a value for bytes it demonstrably describes. This is
// the guard against a hand-edited baseline.json installing a formatted digest
// that no formatter ever produced for this file.
func TestHashFileReusingRefusesAPriorThatDescribesOtherBytes(t *testing.T) {
	path := writeReuseFixture(t, "c.go", reuseSource)
	cold, err := HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	planted := cold
	planted.FormatSHA256 = "0000000000000000000000000000000000000000000000000000000000000000"
	planted.SHA256 = "1111111111111111111111111111111111111111111111111111111111111111"
	got, err := HashFileReusing(path, planted)
	if err != nil {
		t.Fatal(err)
	}
	if got.FormatSHA256 != cold.FormatSHA256 {
		t.Fatalf("a prior whose raw digest does not match the file was trusted: got %s", got.FormatSHA256)
	}
}

// A file that is not Go source carries no formatted digest, and no prior may
// give it one: the reuse path is not even consulted for a non-Go extension.
func TestHashFileReusingNeverInventsADigestForNonGoSource(t *testing.T) {
	path := writeReuseFixture(t, "notes.md", "# not go\n")
	cold, err := HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cold.FormatSHA256 != "" || cold.FormatKind != "" {
		t.Fatalf("fixture precondition: expected no formatted digest, got %+v", cold)
	}
	planted := cold
	planted.FormatSHA256 = "2222222222222222222222222222222222222222222222222222222222222222"
	planted.FormatKind = "gofmt"
	got, err := HashFileReusing(path, planted)
	if err != nil {
		t.Fatal(err)
	}
	if got != cold {
		t.Fatalf("reuse invented a formatted digest for a non-Go file: %+v", got)
	}
}

// The one accepted residual, pinned so it is a decision on the record rather than
// a surprise. A .go file that does not parse produces no formatted digest, but a
// prior that claims one for these exact bytes is carried over. HashFile can never
// produce such a prior — identical bytes parse identically — so reaching this
// requires hand-editing baseline.json. If this test ever starts failing because
// the guard was tightened, delete it; do not weaken the guard to keep it passing.
func TestHashFileReusingCarriesATamperedDigestOnlyForUnparseableGoSource(t *testing.T) {
	path := writeReuseFixture(t, "broken.go", "package ??? this does not parse\n")
	cold, err := HashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if cold.FormatSHA256 != "" {
		t.Fatalf("fixture precondition: unparseable source must produce no digest, got %+v", cold)
	}
	planted := cold
	planted.FormatSHA256 = "2222222222222222222222222222222222222222222222222222222222222222"
	planted.FormatKind = "gofmt"
	got, err := HashFileReusing(path, planted)
	if err != nil {
		t.Fatal(err)
	}
	if got.SHA256 != cold.SHA256 || got.Size != cold.Size || got.NormalizedSHA256 != cold.NormalizedSHA256 {
		t.Fatalf("a tampered prior moved an authoritative digest: %+v", got)
	}
	if got.FormatSHA256 != planted.FormatSHA256 {
		t.Log("the guard now rejects a tampered formatted digest for unparseable Go source; " +
			"this residual is closed and the test may be deleted")
	}
}
