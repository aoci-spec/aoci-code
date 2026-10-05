package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/textassets"
)

// runQoderDoctorStep runs one CLI invocation with the process stdout captured,
// because init writes its human report straight to os.Stdout; doctor writes
// through the command writer, so both streams are returned together.
func runQoderDoctorStep(t *testing.T, args ...string) (int, string) {
	t.Helper()
	// executeCLI leaves --repo in the package-level flag; restore it so later
	// tests that drive newDoctorCmd directly do not inherit a deleted root.
	previousRepo, previousJSON, previousQuiet := flagRepo, flagJSON, flagQuiet
	t.Cleanup(func() { flagRepo, flagJSON, flagQuiet = previousRepo, previousJSON, previousQuiet })
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	oldStdout := os.Stdout
	os.Stdout = writer
	defer func() { os.Stdout = oldStdout }()
	captured := make(chan []byte, 1)
	go func() {
		data, _ := io.ReadAll(reader)
		captured <- data
	}()
	var stdout, stderr bytes.Buffer
	code := executeCLI(args, &stdout, &stderr)
	_ = writer.Close()
	os.Stdout = oldStdout
	direct := <-captured
	_ = reader.Close()
	return code, string(direct) + stdout.String() + stderr.String()
}

// After `init --agent qoder`, doctor reports the Qoder row as configured, and
// the row follows the shared .mcp.json: removing that file turns it back to
// not configured. HOME and USERPROFILE point at an empty directory so the
// developer's real ~/.qoder or ~/.claude cannot change the report.
func TestDoctorReportsQoderMCPAfterInitQoder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	root := t.TempDir()
	locale := textassets.DefaultLocale

	code, out := runQoderDoctorStep(t, "--repo", root, "init", "--agent", "qoder", "--locale", locale)
	if code != ExitOK {
		t.Fatalf("init --agent qoder failed: code=%d output=%s", code, out)
	}

	message := func(key string) string {
		t.Helper()
		value, err := textassets.Message(locale, key)
		if err != nil {
			t.Fatalf("%s is missing from the %s catalog: %v", key, locale, err)
		}
		return value
	}
	// doctor takes no --locale flag; it renders in the locale init recorded in
	// the repository configuration.
	label := message("doctor.label.qoder_mcp")
	configured := stateMark(statePass) + " " + label + ": " + message("doctor.installed")
	notConfigured := stateMark(stateInfo) + " " + label + ": " + message("doctor.not_installed")

	_, out = runQoderDoctorStep(t, "--repo", root, "doctor")
	if !strings.Contains(out, configured) {
		t.Fatalf("doctor must report the Qoder MCP row as configured after init --agent qoder (want %q):\n%s", configured, out)
	}
	if strings.Contains(out, "[text asset") {
		t.Fatalf("doctor printed a missing text asset:\n%s", out)
	}

	if err := os.Remove(filepath.Join(root, ".mcp.json")); err != nil {
		t.Fatal(err)
	}
	_, out = runQoderDoctorStep(t, "--repo", root, "doctor")
	if !strings.Contains(out, notConfigured) {
		t.Fatalf("without the shared .mcp.json the Qoder MCP row must read not configured (want %q):\n%s", notConfigured, out)
	}
}
