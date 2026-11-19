package mcptools

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// buildToAligned plays one session alone: no-argument Maintain, author the
// batch it delivers, apply, again, until aligned. It returns the rounds and
// the candidates each round carried; any refusal or stop fails the test.
func buildToAligned(t *testing.T, session *mcp.ClientSession) (rounds int, sizes []int) {
	t.Helper()
	for rounds < 200 {
		maintain := maintainVolumeBatch(t, session)
		if maintain.Aligned {
			return rounds, sizes
		}
		if maintain.Status != autoStatusRepairRequired || maintain.CodePlan == nil || len(maintain.Candidates) == 0 {
			t.Fatalf("round %d: one session alone was stopped or left without a batch: status=%s next=%s", rounds+1, maintain.Status, maintain.NextAction)
		}
		applied := applyVolumeBatch(t, session, codeBatchArguments(wireCodePlan(maintain)))
		if applied.Status != autoStatusApplied || applied.Applied != len(maintain.Candidates) {
			t.Fatalf("round %d: the delivered batch did not apply: %#v", rounds+1, applied)
		}
		rounds++
		sizes = append(sizes, len(maintain.Candidates))
		if applied.Aligned {
			return rounds, sizes
		}
	}
	t.Fatal("the build did not converge")
	return 0, nil
}

// The default of 2026-11-19: a first build issues three sibling batches, and
// a Host that runs no subagents loses nothing by it. One session alone takes
// the first batch of every plan at full size, because siblings nobody fetched
// are released when the first applies, and reaches aligned in the rounds a
// repository pinned to one batch takes, never stopped and never refused.
func TestOneSessionAloneBuildsUnderTheDefaultInTheSameRounds(t *testing.T) {
	if machinecontract.CodeCognitionParallelBatchesDefault != 3 {
		t.Fatalf("the first-build default is three sibling batches, not %d", machinecontract.CodeCognitionParallelBatchesDefault)
	}
	root := buildManyFileVolumesRepo(t, 150)
	session := connectMCPClient(t, root)
	first := maintainVolumeBatch(t, session)
	if first.CodePlan == nil || first.CodePlan.LogicalPlan == "" || len(first.CodePlan.Siblings) != 2 {
		t.Fatalf("work beyond one batch must issue three siblings by default: %#v", first.CodePlan)
	}
	if !strings.Contains(strings.Join(first.Instructions, "\n"), "aoci_maintain") {
		t.Fatalf("the response does not say what a Host without subagents does: %v", first.Instructions)
	}
	rounds, sizes := buildToAligned(t, session)

	pinned := buildManyFileVolumesRepo(t, 150)
	pinSingleBatch(t, pinned)
	pinnedRounds, pinnedSizes := buildToAligned(t, connectMCPClient(t, pinned))
	if rounds > pinnedRounds+1 {
		t.Fatalf("one session alone took %d rounds %v under the default, a pinned repository %d %v", rounds, sizes, pinnedRounds, pinnedSizes)
	}
	// Work that fits one batch is the single batch whatever the setting.
	small := buildManyFileVolumesRepo(t, 5)
	only := maintainVolumeBatch(t, connectMCPClient(t, small))
	if only.CodePlan == nil || only.CodePlan.LogicalPlan != "" || len(only.CodePlan.Siblings) != 0 {
		t.Fatalf("work that fits one batch must not issue siblings: %#v", only.CodePlan)
	}
}

// A sibling some session was handed keeps its plan: the first batch applying
// releases only siblings nobody fetched.
func TestAFetchedSiblingKeepsItsPlanUnderTheDefault(t *testing.T) {
	root := buildManyFileVolumesRepo(t, 150)
	session := connectMCPClient(t, root)
	first := maintainVolumeBatch(t, session)
	if first.CodePlan == nil || len(first.CodePlan.Siblings) != 2 {
		t.Fatalf("expected three siblings: %#v", first.CodePlan)
	}
	fetchedID := first.CodePlan.Siblings[0].BatchID
	var fetched volumeMaintainResult
	if err := json.Unmarshal([]byte(callVolumeTool(t, session, "aoci_maintain", map[string]any{"batch_id": fetchedID, "include_instructions": true})), &fetched); err != nil {
		t.Fatal(err)
	}
	if fetched.CodePlan == nil || fetched.CodePlan.BatchID != fetchedID || len(fetched.Candidates) == 0 {
		t.Fatalf("the sibling was not delivered whole: %#v", fetched.CodePlan)
	}
	if applied := applyVolumeBatch(t, session, codeBatchArguments(wireCodePlan(first))); applied.Status != autoStatusApplied {
		t.Fatalf("the first sibling did not apply: %#v", applied)
	}
	next := maintainVolumeBatch(t, session)
	if next.CodePlan == nil || next.CodePlan.LogicalPlan != first.CodePlan.LogicalPlan {
		t.Fatalf("a plan with a fetched sibling was released: %#v", next.CodePlan)
	}
	if applied := applyVolumeBatch(t, session, codeBatchArguments(wireCodePlan(fetched))); applied.Status != autoStatusApplied || applied.Applied != len(fetched.Candidates) {
		t.Fatalf("the fetched sibling no longer applies: %#v", applied)
	}
}

// A released sibling asked for by batch_id sends its Agent back to Maintain
// with zero writes; the session then gets a batch like any other.
func TestAReleasedSiblingSendsItsAgentBackToMaintain(t *testing.T) {
	root := buildManyFileVolumesRepo(t, 150)
	session := connectMCPClient(t, root)
	first := maintainVolumeBatch(t, session)
	releasedID := first.CodePlan.Siblings[0].BatchID
	if applied := applyVolumeBatch(t, session, codeBatchArguments(wireCodePlan(first))); applied.Status != autoStatusApplied {
		t.Fatalf("the first sibling did not apply: %#v", applied)
	}
	next := maintainVolumeBatch(t, session)
	if next.CodePlan == nil || next.CodePlan.LogicalPlan == first.CodePlan.LogicalPlan {
		t.Fatalf("siblings nobody fetched were not released: %#v", next.CodePlan)
	}
	late := callVolumeTool(t, session, "aoci_maintain", map[string]any{"batch_id": releasedID})
	if !strings.Contains(late, "maintain_batch_id_unknown") {
		t.Fatalf("a released sibling must answer the replan refusal: %s", late)
	}
	if rounds, _ := buildToAligned(t, session); rounds == 0 {
		t.Fatal("the build did not continue after the late request")
	}
}
