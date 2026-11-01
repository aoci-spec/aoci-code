package mcptools

import (
	"fmt"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
)

// The parallel estimate scan prints (2026-11-01) is measured with the cut
// Maintain makes for N siblings, not the sequential rounds divided by N:
// siblings are cut under the contract-reserving budget less the listing, so
// each round covers fewer Entries per session and more in total.
func TestEstimateCodeAuthoringRoundsParallelFollowsTheSiblingCut(t *testing.T) {
	root := buildHeldSourcesVolumeRepo(t, false)
	cfg, err := config.LoadReadOnly(root)
	if err != nil {
		t.Fatal(err)
	}
	set, err := cognition.Load(root, cfg.IndexPath)
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]string, 0, 240)
	for index := 0; index < 240; index++ {
		paths = append(paths, fmt.Sprintf("src/main/java/com/example/module%02d/Service%03dImpl.java", index%12, index))
	}
	sequential := EstimateCodeAuthoringRounds(cfg, set, paths)
	if EstimateCodeAuthoringRoundsParallel(cfg, set, paths, 1) != sequential {
		t.Fatalf("one session is the sequential estimate: %d", sequential)
	}
	four := EstimateCodeAuthoringRoundsParallel(cfg, set, paths, 4)
	if four < 1 || four > sequential || four < (sequential+3)/4 {
		t.Fatalf("four sessions take between ceil(%d/4) and %d rounds each, got %d", sequential, sequential, four)
	}
	if EstimateCodeAuthoringRoundsParallel(cfg, set, nil, 4) != 0 {
		t.Fatal("nothing to author is zero rounds")
	}
}
