package cli

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/textassets"
)

// Every command the Guide, onboarding, and an interaction_required answer
// hand a Host names its repository beside the absolute binary, so it runs
// from any working directory: a Host session often starts above or beside
// the repository it was configured for. A root the target shell cannot be
// handed safely refuses the binding instead of printing a command that would
// resolve the wrong tree.
func TestAgentCommandPrefixNamesTheRepository(t *testing.T) {
	previous := textassets.ActiveLocale()
	if err := textassets.SetActiveLocale(textassets.DefaultLocale); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })
	posix, err := agentCommandPrefixFor("linux", "/opt/aoci/bin/aoci", "/home/wang/it's here/repo")
	if err != nil {
		t.Fatal(err)
	}
	if want := `'/opt/aoci/bin/aoci' --repo '/home/wang/it'"'"'s here/repo'`; posix != want {
		t.Fatalf("posix prefix %q, want %q", posix, want)
	}
	if got := bindAgentCommand("aoci scope status --json", posix); got != posix+" scope status --json" {
		t.Fatalf("bound command %q", got)
	}
	windows, err := agentCommandPrefixFor("windows", `C:\Tools\aoci.exe`, `D:\work\repo`)
	if err != nil {
		t.Fatal(err)
	}
	if want := `& "C:/Tools/aoci.exe" --repo "D:/work/repo"`; windows != want {
		t.Fatalf("windows prefix %q, want %q", windows, want)
	}
	for _, refused := range []struct{ goos, root, anchor string }{
		{"linux", "", "empty"},
		{"linux", "relative/repo", "not absolute"},
		{"linux", "/tmp/one\ntwo", "control character"},
		{"windows", `C:\re$po`, "PowerShell"},
		{"windows", `repo`, "not absolute"},
	} {
		_, err := agentCommandPrefixFor(refused.goos, "/opt/aoci/bin/aoci", refused.root)
		if refused.goos == "windows" {
			_, err = agentCommandPrefixFor(refused.goos, `C:\Tools\aoci.exe`, refused.root)
		}
		if err == nil || !strings.Contains(err.Error(), refused.anchor) {
			t.Fatalf("root %q on %s must refuse the binding naming %q, got %v", refused.root, refused.goos, refused.anchor, err)
		}
	}
}

// The exact command an interaction_required answer prints resolves the
// repository the way the invocation did and names it, once, even when the
// caller already spelled --repo.
func TestHostInteractionCommandNamesTheRepository(t *testing.T) {
	root := t.TempDir()
	previous := flagRepo
	flagRepo = root
	t.Cleanup(func() { flagRepo = previous })
	implicit := hostInteractionCommand("scope", "approve", "--json")
	explicit := hostInteractionCommand("--repo", root, "scope", "approve", "--json")
	if implicit != explicit || strings.Count(implicit, "--repo") != 1 || !strings.Contains(implicit, root) {
		t.Fatalf("interaction command must name the repository once:\nimplicit %q\nexplicit %q", implicit, explicit)
	}
}
