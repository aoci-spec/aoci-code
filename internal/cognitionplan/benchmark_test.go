package cognitionplan

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func BenchmarkBootstrapPlannerScale(b *testing.B) {
	for _, count := range []int{1, 100, 1000, 10000} {
		b.Run(fmt.Sprintf("entries_%d", count), func(b *testing.B) {
			root := b.TempDir()
			sourceDir := filepath.Join(root, "src")
			if err := os.MkdirAll(sourceDir, 0o755); err != nil {
				b.Fatal(err)
			}
			for index := 0; index < count; index++ {
				path := filepath.Join(sourceDir, fmt.Sprintf("file_%05d.go", index))
				if err := os.WriteFile(path, []byte("package fixture\n"), 0o644); err != nil {
					b.Fatal(err)
				}
			}
			b.ResetTimer()
			for iteration := 0; iteration < b.N; iteration++ {
				if _, err := BootstrapPlan(Options{RepositoryRoot: root, Locale: "en-US", TargetKinds: []string{"code"}}); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
