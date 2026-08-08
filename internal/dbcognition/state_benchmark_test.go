package dbcognition

import (
	"fmt"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func BenchmarkDatabaseCognitionScale(b *testing.B) {
	for _, count := range []int{1, 10, 100, 1000, 10000} {
		b.Run(fmt.Sprintf("tables_%d", count), func(b *testing.B) {
			names := make([]string, count)
			for index := range names {
				names[index] = fmt.Sprintf("table_%05d", index)
			}
			root, sources := databaseFixture(b, names, names)
			set := loadFixtureSet(b, root)
			unbound := baseline.NewBaseline(nil)
			initial := Assess(root, sources, set, unbound)
			bindings := make([]baseline.DatabaseCognitionBinding, 0, len(initial.Items))
			for _, item := range initial.Items {
				bindings = append(bindings, baseline.DatabaseCognitionBinding{
					ObjectRef: item.ObjectRef, SourceID: item.SourceID, EvidenceVersion: item.EvidenceVersion,
					TableEvidenceSHA256: item.TableEvidenceSHA256, EntrySHA256: EntrySHA256(item.CurrentEntry),
				})
			}
			current := baseline.NewBaseline(nil)
			current.DatabaseCognition = &baseline.DatabaseCognitionBindings{
				Version: machinecontract.DatabaseCognitionBindingVersion,
				Entries: bindings,
			}

			b.Run("status_and_binding_compare", func(b *testing.B) {
				b.ReportAllocs()
				for iteration := 0; iteration < b.N; iteration++ {
					report := Assess(root, sources, set, current)
					if !report.CognitionCurrent || report.Summary.Current != count {
						b.Fatalf("unexpected current status: %#v", report.Summary)
					}
				}
			})
			b.Run("batch_plan", func(b *testing.B) {
				b.ReportAllocs()
				for iteration := 0; iteration < b.N; iteration++ {
					plan, err := BuildPlan(root, initial, set, machinecontract.DatabaseCognitionBatchObjectsDefault, machinecontract.DatabaseCognitionBatchEvidenceBytesDefault)
					if err != nil || plan.TargetCount == 0 {
						b.Fatalf("plan failed: count=%d plan=%#v err=%v", count, plan, err)
					}
				}
			})
			b.Run("database_volume_parse", func(b *testing.B) {
				b.ReportAllocs()
				for iteration := 0; iteration < b.N; iteration++ {
					loaded, err := cognition.Load(root, "aoci.txt")
					if err != nil || loaded.Volumes["database"].ObjectCount != count {
						b.Fatalf("parse failed: count=%d err=%v", count, err)
					}
				}
			})
		})
	}
}
