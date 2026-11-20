package mcptools

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/config"
)

func TestCodeObjectRefNamesPathInsteadOfDatabaseBindings(t *testing.T) {
	for _, locale := range []string{"en-US", "zh-CN"} {
		for _, single := range []bool{false, true} {
			for _, omitBindings := range []bool{false, true} {
				name := locale + map[bool]string{false: "/batch", true: "/single"}[single] + map[bool]string{false: "/bound", true: "/omitted"}[omitBindings]
				t.Run(name, func(t *testing.T) {
					count := 2
					if single {
						count = 1
					}
					root := buildLargeCodeCandidateRepo(t, count)
					cfg, err := config.LoadBase(root)
					if err != nil {
						t.Fatal(err)
					}
					cfg.Locale = locale
					if err := config.Save(root, cfg); err != nil {
						t.Fatal(err)
					}
					rootPath := filepath.Join(root, "aoci.txt")
					raw := strings.Replace(string(readCodeBatchFormalAsset(t, root, "aoci.txt")), "#Locale: zh-CN", "#Locale: "+locale, 1)
					if err := os.WriteFile(rootPath, []byte(raw), 0o644); err != nil {
						t.Fatal(err)
					}
					active, _, err := baseline.Load(root)
					if err != nil {
						t.Fatal(err)
					}
					active.Files["aoci.txt"] = baseline.HashBytes("aoci.txt", []byte(raw))
					if err := baseline.Save(root, active); err != nil {
						t.Fatal(err)
					}
					session := connectMCPClient(t, root)
					maintain := maintainVolumeBatch(t, session)
					if maintain.CodePlan == nil || len(maintain.Candidates) != count {
						t.Fatalf("expected %d candidates: %#v", count, maintain)
					}
					arguments := codeBatchArguments(wireCodePlan(maintain))
					entries := arguments["entries"].([]map[string]any)
					position := len(entries) - 1
					entry := entries[position]
					path := entry["path"].(string)
					delete(entry, "path")
					entry["object_ref"] = "code:" + path
					if omitBindings {
						delete(entry, "source_sha256")
						delete(entry, "candidate_id")
					}
					if single {
						delete(arguments, "entries")
						for key, value := range entry {
							arguments[key] = value
						}
					}
					before := map[string][]byte{}
					for _, asset := range []string{"aoci.txt", "aoci.meta.txt", "aoci.code.txt", filepath.Join(".aoci", "baseline.json")} {
						before[asset] = readCodeBatchFormalAsset(t, root, asset)
					}
					rejected := applyVolumeBatch(t, session, arguments)
					if rejected.Status != autoStatusRepairRequired || rejected.Applied != 0 || rejected.FormalWritesStarted || len(rejected.Findings) != 1 {
						t.Fatalf("wrong identity must be repairable before writes: %#v", rejected)
					}
					finding := rejected.Findings[0]
					if finding.CandidateIndex != position+1 || finding.Path != path || finding.CanonicalObjectIdentity != "code:"+path ||
						finding.Domain != cognition.ScopeCode || finding.Field != "object_ref" || finding.RuleCode != "code_object_ref_requires_path" {
						t.Fatalf("wrong candidate, domain, or field: %#v", finding)
					}
					if !strings.Contains(finding.Cause, "path") || !strings.Contains(finding.SafeRepairAction, "path") ||
						!strings.Contains(finding.SafeRepairAction, "object_ref") || (locale == "zh-CN" && !strings.Contains(finding.Cause, "代码")) {
						t.Fatalf("missing localized repair direction: %#v", finding)
					}
					for asset, content := range before {
						assertCodeBatchFormalAssetUnchanged(t, root, asset, content)
					}
					// The exact issued batch remains usable through the documented path form.
					applied := applyVolumeBatch(t, session, codeBatchArguments(wireCodePlan(maintain)))
					if applied.Status != autoStatusApplied || !applied.Aligned || applied.Applied != count {
						t.Fatalf("correct path submission no longer applies: %#v", applied)
					}
				})
			}
		}
	}
}

func TestCodeObjectRefRecordsNoObserveReview(t *testing.T) {
	root := pendingObserveRepoWithWork(t, config.AutomationModeAuto)
	maintain := maintainResultOf(t, root)
	session := connectMCPClient(t, root)
	arguments := codeBatchArguments(wireCodePlan(maintain))
	entry := arguments["entries"].([]map[string]any)[0]
	entry["object_ref"] = "code:" + entry["path"].(string)
	delete(entry, "path")
	arguments["observe_review"] = map[string]any{"review_digest": maintain.ObserveReview.ReviewDigest, "reviewed_by": "codex-agent"}
	before := readCodeBatchFormalAsset(t, root, filepath.Join(".aoci", "baseline.json"))
	result := applyVolumeBatch(t, session, arguments)
	if result.Status != autoStatusRepairRequired || result.ObserveReview != nil || result.FormalWritesStarted {
		t.Fatalf("wrong object_ref recorded an Observe review: %#v", result)
	}
	assertCodeBatchFormalAssetUnchanged(t, root, filepath.Join(".aoci", "baseline.json"), before)
}
