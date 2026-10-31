package volumegovernance

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
)

// The configured Locale and the Root's #Locale line must name one language:
// the authoring contract reads the former, every reader of the formal bytes
// sees the latter. The switch transaction keeps them together; a hand edit of
// either file is reported with that same transaction as the repair, and the
// repository is not aligned until it runs.
func TestRootLocaleMismatchBlocksAlignmentAndNamesTheSwitchAsRepair(t *testing.T) {
	root, cfg := alignedFixture(t, true, false)
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	facts, err := Assess(root, cfg, set)
	if err != nil {
		t.Fatal(err)
	}
	if !facts.GovernanceAligned {
		t.Fatalf("fixture is not aligned before the edit: %+v", facts.Findings)
	}

	cfg.Locale = "zh-CN"
	if err := config.Save(root, cfg); err != nil {
		t.Fatal(err)
	}
	facts, err = Assess(root, cfg, set)
	if err != nil {
		t.Fatal(err)
	}
	var found *Finding
	for i := range facts.Findings {
		if facts.Findings[i].Code == "root_locale_mismatch" {
			found = &facts.Findings[i]
		}
	}
	if found == nil {
		t.Fatalf("config zh-CN over a Root written in en-US was not reported: %+v", facts.Findings)
	}
	if facts.GovernanceAligned {
		t.Fatal("a Root whose Locale differs from the configured one must not be aligned")
	}
	if found.Target != "aoci.txt" || !strings.Contains(found.SafeRepairAction, "aoci config set locale zh-CN") ||
		!strings.Contains(found.SafeRepairAction, "aoci config set locale en-US") {
		t.Fatalf("the finding must name the Root and both directions of the switch: %+v", *found)
	}
}
