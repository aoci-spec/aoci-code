package migrationapply

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/cognitionplan"
	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// BenchmarkMigrationScale measures the complete Snapshot, D2-A replay,
// Apply-grade Mapping, Prepare, Apply, Status, Resume, pending Rollback, and
// strict completed Reversal paths with exact local bytes and no network.
func BenchmarkMigrationScale(b *testing.B) {
	for _, objectCount := range []int{1, 100, 1_000, 10_000} {
		b.Run(fmt.Sprintf("objects-%d", objectCount), func(b *testing.B) {
			b.Run("prepare-apply-status", func(b *testing.B) {
				for iteration := 0; iteration < b.N; iteration++ {
					root := migrationScaleFixture(b, objectCount)
					envelope, approval := preparedMigrationScaleFixture(b, root)
					result, err := Apply(root, envelope, approval)
					if err != nil {
						b.Fatal(err)
					}
					if _, err := Status(root, result.TransactionID); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("resume", func(b *testing.B) {
				for iteration := 0; iteration < b.N; iteration++ {
					root := migrationScaleFixture(b, objectCount)
					envelope, approval := preparedMigrationScaleFixture(b, root)
					previous := migrationFault
					migrationFault = failMigrationOnce("before_publish_baseline")
					_, applyErr := Apply(root, envelope, approval)
					migrationFault = previous
					if applyErr == nil {
						b.Fatal("fault was not injected")
					}
					if _, err := Resume(root, transactionIdentity(envelope, approval)); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("pending-rollback", func(b *testing.B) {
				for iteration := 0; iteration < b.N; iteration++ {
					root := migrationScaleFixture(b, objectCount)
					envelope, approval := preparedMigrationScaleFixture(b, root)
					previous := migrationFault
					migrationFault = failMigrationOnce("before_internal_verify")
					_, applyErr := Apply(root, envelope, approval)
					migrationFault = previous
					if applyErr == nil {
						b.Fatal("fault was not injected")
					}
					if _, err := Rollback(root, transactionIdentity(envelope, approval)); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.Run("completed-reversal", func(b *testing.B) {
				for iteration := 0; iteration < b.N; iteration++ {
					root := migrationScaleFixture(b, objectCount)
					envelope, approval := preparedMigrationScaleFixture(b, root)
					result, err := Apply(root, envelope, approval)
					if err != nil {
						b.Fatal(err)
					}
					plan, err := PrepareReversal(root, result.TransactionID, "2026-07-30T00:02:00Z")
					if err != nil {
						b.Fatal(err)
					}
					approved, err := RecordReversalApproval(plan, "test-human", "2026-07-30T00:03:00Z", plan.PlanDigest)
					if err != nil {
						b.Fatal(err)
					}
					if _, err := ApplyReversal(root, plan, approved); err != nil {
						b.Fatal(err)
					}
				}
			})
		})
	}
}

func migrationScaleFixture(b testing.TB, objectCount int) string {
	b.Helper()
	root := b.TempDir()
	cfg := config.DefaultConfig()
	cfg.LedgerEnabled = true
	if err := config.Save(root, cfg); err != nil {
		b.Fatal(err)
	}
	legacy := []string{"#AOCI-CLI Complete Index", "#Project: Model-authored scale fixture", "#[Tag dictionary]", "#A Layer: C Code",
		"===Source " + filepath.ToSlash(filepath.Join(root, "src")) + "/==="}
	files := map[string]baseline.Fingerprint{}
	for index := 0; index < objectCount; index++ {
		name := fmt.Sprintf("file-%05d.go", index)
		relative := filepath.ToSlash(filepath.Join("src", name))
		writeMigrationFile(b, root, relative, fmt.Sprintf("package fixture\n\nconst Value%d = %d\n", index, index))
		legacy = append(legacy, fmt.Sprintf("%s[CD9S]: F:Preserve responsibility %d | R:- | A:Value%d | S:Keep source %d byte-stable during cognition migration", name, index, index, index))
		fingerprint, err := baseline.HashFile(filepath.Join(root, filepath.FromSlash(relative)))
		if err != nil {
			b.Fatal(err)
		}
		files[relative] = fingerprint
	}
	writeMigrationFile(b, root, "aoci.txt", strings.Join(legacy, "\n")+"\n")
	indexFingerprint, err := baseline.HashFile(filepath.Join(root, "aoci.txt"))
	if err != nil {
		b.Fatal(err)
	}
	files["aoci.txt"] = indexFingerprint
	value, err := baseline.NewBaselineAt(files, "2026-07-29T00:00:00Z")
	if err != nil {
		b.Fatal(err)
	}
	data, err := baseline.MarshalExact(value)
	if err != nil {
		b.Fatal(err)
	}
	writeMigrationFile(b, root, ".aoci/baseline.json", string(data))
	return root
}

func preparedMigrationScaleFixture(b testing.TB, root string) (*ApplyEnvelope, *Approval) {
	b.Helper()
	snapshot, err := CaptureSnapshot(root, "en-US", []string{"code"}, "2026-07-30T00:00:00Z")
	if err != nil {
		b.Fatal(err)
	}
	plan, err := cognitionplan.MigrationPlan(cognitionplan.Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"code"}})
	if err != nil {
		b.Fatal(err)
	}
	candidate := migrationCandidate(root, plan, []string{"code"})
	legacy := string(mustRead(b, filepath.Join(root, "aoci.txt")))
	entries := []string{}
	for _, line := range strings.Split(strings.TrimSuffix(legacy, "\n"), "\n") {
		if strings.Contains(line, "[CD9S]:") {
			entries = append(entries, line)
		}
	}
	for index := range candidate.Assets {
		if candidate.Assets[index].AssetID == "code" {
			candidate.Assets[index].Content = cognition.CodeVolumeMarker + "\n===Code " + filepath.ToSlash(filepath.Join(root, "src")) + "/===\n" + strings.Join(entries, "\n") + "\n"
		}
	}
	entryRef := map[string]string{}
	for _, record := range plan.Mapping.Records {
		if record.UnitKind != "entry" {
			continue
		}
		name, _, _ := strings.Cut(record.SourceText, "[")
		entryRef[record.UnitID] = "code:" + filepath.ToSlash(filepath.Join("src", name))
	}
	for index := range candidate.MappingResolutions {
		if target, exists := entryRef[candidate.MappingResolutions[index].UnitID]; exists {
			candidate.MappingResolutions[index].TargetAsset = "code"
			candidate.MappingResolutions[index].TargetRef = target
		}
	}
	sort.Slice(candidate.MappingResolutions, func(i, j int) bool {
		return candidate.MappingResolutions[i].UnitID < candidate.MappingResolutions[j].UnitID
	})
	preview, err := cognitionplan.ValidateCandidate(root, plan, candidate)
	if err != nil {
		b.Fatal(err)
	}
	template, err := BuildMappingTemplate(root, snapshot, plan, candidate)
	if err != nil {
		b.Fatal(err)
	}
	mapping, err := ValidateMapping(root, snapshot, plan, candidate, authorApplyGradeMapping(b, template))
	if err != nil {
		b.Fatal(err)
	}
	envelope, err := Prepare(root, &ApplyRequest{Version: machinecontract.CognitionMigrationApplyRequestV2,
		Snapshot: *snapshot, Plan: *plan, Mapping: *mapping, Candidate: *candidate, Preview: *preview,
		BaselineTimestamp: "2026-07-30T00:00:00Z"})
	if err != nil {
		b.Fatal(err)
	}
	approval, err := RecordApproval(envelope, "test-human", "2026-07-30T00:01:00Z", envelope.EnvelopeDigest)
	if err != nil {
		b.Fatal(err)
	}
	return envelope, approval
}
