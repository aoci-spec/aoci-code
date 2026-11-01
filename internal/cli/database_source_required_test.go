package cli

import (
	"bytes"
	"database/sql"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// Database commands without --source used to answer with cobra's English
// "required flag(s) "source" not set" under every locale, and the accept
// command Maintain and the Guide return was refused as printed for lacking
// --snapshot-sha. Now a missing --source names the declared sources, and a
// snapshot prints the accept command with the digest it binds filled in
// (2026-11-02).
func TestDatabaseCommandsNameTheSourceAndTheAcceptCommand(t *testing.T) {
	pinDefaultLocale(t)
	root := databaseCLIRepo(t)
	db, err := sql.Open("sqlite3", filepath.Join(root, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(`CREATE TABLE users (id INTEGER PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) (int, string) {
		t.Helper()
		var out, stderr bytes.Buffer
		code := executeCLI(append([]string{"--repo", root, "database"}, args...), &out, &stderr)
		return code, out.String() + stderr.String()
	}
	if code, out := run("inventory"); code != ExitConfig || !strings.Contains(out, "no database source is declared") && !strings.Contains(out, "No database source is declared") {
		t.Fatalf("a missing --source with nothing declared: code=%d %s", code, out)
	}
	if code, out := run("source", "add", "--source-id", "local", "--engine", "sqlite", "--path", "app.db"); code != ExitOK {
		t.Fatalf("source add: %d %s", code, out)
	}
	for _, command := range [][]string{{"inventory"}, {"snapshot"}, {"source", "access"}, {"verify"}} {
		code, out := run(command...)
		if code != ExitConfig || !strings.Contains(out, "--source <id>; declared sources: local") || strings.Contains(out, "required flag") {
			t.Fatalf("%v without --source: code=%d %s", command, code, out)
		}
	}
	code, out := run("snapshot", "--source", "local")
	if code != ExitOK {
		t.Fatalf("snapshot: %d %s", code, out)
	}
	match := regexp.MustCompile(`'database' 'baseline' 'accept' '--source' 'local' '--snapshot-sha' '([0-9a-f]{64})'`).FindStringSubmatch(out)
	if match == nil {
		t.Fatalf("the snapshot does not print the accept command with its digest:\n%s", out)
	}
	if code, out := run("baseline", "accept", "--source", "local", "--snapshot-sha", match[1]); code != ExitOK {
		t.Fatalf("the printed accept command does not run: %d %s", code, out)
	}
}
