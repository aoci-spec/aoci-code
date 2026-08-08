package textassets

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestManifestConsumersResolveToProductionSymbols验证清单消费方不是说明性字符串：
// 路径与符号必须真实存在，并且符号必须直接引用对应资源常量。
// 这样重命名、删除或调用链迁移会在发布测试中失败。
func TestManifestConsumersResolveToProductionSymbols(t *testing.T) {
	manifest, err := ReadManifest()
	if err != nil {
		t.Fatal(err)
	}
	repositoryRoot := filepath.Clean("..")
	constantNames := collectAssetConstantNames(t, repositoryRoot)

	for _, asset := range manifest.Assets {
		constantName, exists := constantNames[asset.ID]
		if !exists {
			t.Errorf("asset %q has no production ID constant", asset.ID)
			continue
		}
		for _, reference := range strings.Split(asset.UsedBy, ",") {
			parts := strings.Split(reference, ":")
			if len(parts) != 2 {
				t.Errorf("asset %q has invalid consumer %q", asset.ID, reference)
				continue
			}
			filePath := filepath.Join(repositoryRoot, filepath.FromSlash(parts[0]))
			node := findProductionSymbol(t, filePath, parts[1])
			if node == nil {
				t.Errorf("asset %q consumer symbol does not exist: %s", asset.ID, reference)
				continue
			}
			if nodeReferencesSelector(node, "textassets", constantName) {
				continue
			}
			t.Errorf(
				"asset %q consumer %s does not reference %s",
				asset.ID,
				reference,
				constantName,
			)
		}
		for _, reference := range asset.EnforcedBy {
			parts := strings.Split(reference, ":")
			if len(parts) != 2 {
				t.Errorf("asset %q has invalid enforcement reference %q", asset.ID, reference)
				continue
			}
			filePath := filepath.Join(repositoryRoot, filepath.FromSlash(parts[0]))
			if findProductionSymbol(t, filePath, parts[1]) == nil {
				t.Errorf(
					"asset %q enforcement symbol does not exist: %s",
					asset.ID,
					reference,
				)
			}
		}
	}
}

func collectAssetConstantNames(t *testing.T, repositoryRoot string) map[string]string {
	t.Helper()
	result := map[string]string{}
	files, err := filepath.Glob(filepath.Join(repositoryRoot, "textassets", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, filePath := range files {
		if strings.HasSuffix(filePath, "_test.go") {
			continue
		}
		parsed := parseGoFile(t, filePath)
		for _, declaration := range parsed.Decls {
			general, ok := declaration.(*ast.GenDecl)
			if !ok || general.Tok != token.CONST {
				continue
			}
			for _, specification := range general.Specs {
				values, ok := specification.(*ast.ValueSpec)
				if !ok {
					continue
				}
				for position, expression := range values.Values {
					literal, ok := expression.(*ast.BasicLit)
					if !ok || literal.Kind != token.STRING || position >= len(values.Names) {
						continue
					}
					value := strings.Trim(literal.Value, "\"")
					if strings.Contains(value, "/") {
						result[value] = values.Names[position].Name
					}
				}
			}
		}
	}

	return result
}

func findProductionSymbol(t *testing.T, filePath, symbol string) ast.Node {
	t.Helper()
	if strings.HasSuffix(filePath, "_test.go") {
		t.Fatalf("manifest consumer must not be a test file: %s", filePath)
	}
	if _, err := os.Stat(filePath); err != nil {
		return nil
	}
	parsed := parseGoFile(t, filePath)
	for _, declaration := range parsed.Decls {
		switch current := declaration.(type) {
		case *ast.FuncDecl:
			if current.Name.Name == symbol {
				return current
			}
		case *ast.GenDecl:
			for _, specification := range current.Specs {
				switch typed := specification.(type) {
				case *ast.ValueSpec:
					for _, name := range typed.Names {
						if name.Name == symbol {
							return typed
						}
					}
				case *ast.TypeSpec:
					if typed.Name.Name == symbol {
						return typed
					}
				}
			}
		}
	}

	return nil
}

func nodeReferencesSelector(node ast.Node, receiver, selected string) bool {
	found := false
	ast.Inspect(node, func(current ast.Node) bool {
		selector, ok := current.(*ast.SelectorExpr)
		if !ok || selector.Sel.Name != selected {
			return true
		}
		identifier, ok := selector.X.(*ast.Ident)
		if ok && identifier.Name == receiver {
			found = true
			return false
		}
		return true
	})

	return found
}

func parseGoFile(t *testing.T, filePath string) *ast.File {
	t.Helper()
	parsed, err := parser.ParseFile(token.NewFileSet(), filePath, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", filePath, err)
	}

	return parsed
}
