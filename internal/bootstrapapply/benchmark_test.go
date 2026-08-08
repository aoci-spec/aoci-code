package bootstrapapply

import (
	"fmt"
	"path/filepath"
	"testing"
)

// BenchmarkBootstrapScale measures the complete D2-A replay, Prepare, Apply
// (including internal Verify), and Status path. One benchmark iteration uses
// isolated formal assets and never connects to a database.
func BenchmarkBootstrapScale(b *testing.B) {
	for _, objectCount := range []int{1, 100, 1_000, 10_000} {
		b.Run(fmt.Sprintf("objects-%d", objectCount), func(b *testing.B) {
			for iteration := 0; iteration < b.N; iteration++ {
				root := b.TempDir()
				for index := 0; index < objectCount; index++ {
					path := filepath.ToSlash(filepath.Join("src", fmt.Sprintf("file-%05d.go", index)))
					writeBootstrapFile(b, root, path, fmt.Sprintf("package fixture\n\nconst Value%d = %d\n", index, index))
				}
				envelope, approval := preparedFixture(b, root, []string{"code"})
				result, err := Apply(root, envelope, approval)
				if err != nil {
					b.Fatal(err)
				}
				if _, err := Status(root, result.TransactionID); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
