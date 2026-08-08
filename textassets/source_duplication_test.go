package textassets

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const exactSourceGuardMinimumBytes = 96

type sourceGuardAsset struct {
	id    string
	text  string
	group string
}

func TestMigratedContractsHaveNoProductionGoSourceCopy(t *testing.T) {
	assets := sourceGuardAssets(t)
	sources := readProductionGoSources(t, "..")
	findings, err := detectSourceDuplicates(assets, sources)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) > 0 {
		t.Fatalf("migrated text assets re-entered production Go source:\n%s", strings.Join(findings, "\n"))
	}

	for sourcePath, source := range sources {
		if strings.Contains(source, "github.com/aoci-spec/aoci-code/prompts") {
			t.Fatalf("obsolete prompts package is still imported by %s", sourcePath)
		}
	}
}

func TestSourceGuardDetectsSplitLongContract(t *testing.T) {
	long := strings.Repeat("稳定长合同段落", 20)
	middle := len(long) / 2
	source := "package sample\nconst first = " + strconv.Quote(long[:middle]) +
		"\nconst second = " + strconv.Quote(long[middle:]) +
		"\nvar duplicated = first + second\n"
	findings, err := detectSourceDuplicates(
		[]sourceGuardAsset{{id: "contracts/long", text: long}},
		map[string]string{"internal/sample.go": source},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 1 {
		t.Fatalf("split and rejoined long contract must be detected: %v", findings)
	}
}

func TestSourceGuardAllowsOneOrdinaryShortPhrase(t *testing.T) {
	assets := []sourceGuardAsset{
		{id: "contracts/maintain/actions/a", text: "继续下一项检查", group: "maintain-actions"},
		{id: "contracts/maintain/actions/b", text: "检查已经完成", group: "maintain-actions"},
		{id: "contracts/maintain/actions/c", text: "等待宿主确认", group: "maintain-actions"},
	}
	findings, err := detectSourceDuplicates(
		assets,
		map[string]string{
			"internal/sample.go": "package sample\nvar message = \"继续下一项检查\"\n",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(findings) != 0 {
		t.Fatalf("one ordinary short phrase must not trigger a group duplicate: %v", findings)
	}
}

func sourceGuardAssets(t *testing.T) []sourceGuardAsset {
	t.Helper()
	manifest, err := ReadManifest()
	if err != nil {
		t.Fatal(err)
	}
	result := make([]sourceGuardAsset, 0, len(manifest.Assets))
	for _, asset := range manifest.Assets {
		text, err := Load(LegacyLocale, ID(asset.ID))
		if err != nil {
			t.Fatal(err)
		}
		guard := sourceGuardAsset{id: asset.ID, text: strings.TrimSpace(text)}
		switch {
		case strings.HasPrefix(asset.ID, "contracts/maintain/actions/"):
			guard.group = "maintain-actions"
		case strings.HasPrefix(asset.ID, "contracts/mcp/"):
			guard.group = "mcp-contracts"
		}
		if len(guard.text) >= exactSourceGuardMinimumBytes || guard.group != "" {
			result = append(result, guard)
		}
	}

	return result
}

func detectSourceDuplicates(
	assets []sourceGuardAsset,
	sources map[string]string,
) ([]string, error) {
	findings := []string{}
	for sourcePath, source := range sources {
		values, err := evaluateSourceStrings(sourcePath, source)
		if err != nil {
			return nil, err
		}
		groupMatches := map[string]map[string]struct{}{}
		for _, asset := range assets {
			if asset.text == "" {
				continue
			}
			matched := false
			for _, value := range values {
				if strings.TrimSpace(value) == asset.text {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			if len(asset.text) >= exactSourceGuardMinimumBytes {
				findings = append(findings, fmt.Sprintf("%s contains complete asset %q", sourcePath, asset.id))
				continue
			}
			if asset.group != "" {
				if groupMatches[asset.group] == nil {
					groupMatches[asset.group] = map[string]struct{}{}
				}
				groupMatches[asset.group][asset.id] = struct{}{}
			}
		}
		for group, matches := range groupMatches {
			// 一句常用短语允许正常出现；同一文件出现三项稳定动作才表明
			// 整组合同正在重新形成第二事实源。
			if len(matches) >= 3 {
				findings = append(findings, fmt.Sprintf(
					"%s contains %d short assets from group %q",
					sourcePath,
					len(matches),
					group,
				))
			}
		}
	}

	return findings, nil
}

func evaluateSourceStrings(sourcePath, source string) ([]string, error) {
	parsed, err := parser.ParseFile(token.NewFileSet(), sourcePath, source, 0)
	if err != nil {
		return nil, fmt.Errorf("parse production Go source %s: %w", sourcePath, err)
	}

	bindings := map[string]string{}
	assignments := map[string][]ast.Expr{}
	ast.Inspect(parsed, func(node ast.Node) bool {
		switch current := node.(type) {
		case *ast.ValueSpec:
			for position, name := range current.Names {
				if position < len(current.Values) {
					assignments[name.Name] = append(assignments[name.Name], current.Values[position])
				}
			}
		case *ast.AssignStmt:
			for position, left := range current.Lhs {
				name, ok := left.(*ast.Ident)
				if ok && position < len(current.Rhs) {
					assignments[name.Name] = append(assignments[name.Name], current.Rhs[position])
				}
			}
		}
		return true
	})
	for pass := 0; pass <= len(assignments); pass++ {
		changed := false
		for name, expressions := range assignments {
			if len(expressions) != 1 {
				continue
			}
			if value, ok := evaluateStringExpression(expressions[0], bindings); ok && bindings[name] != value {
				bindings[name] = value
				changed = true
			}
		}
		if !changed {
			break
		}
	}

	values := []string{}
	ast.Inspect(parsed, func(node ast.Node) bool {
		expression, ok := node.(ast.Expr)
		if ok {
			if value, evaluated := evaluateStringExpression(expression, bindings); evaluated {
				values = append(values, value)
			}
		}
		return true
	})

	return values, nil
}

func evaluateStringExpression(expression ast.Expr, bindings map[string]string) (string, bool) {
	switch current := expression.(type) {
	case *ast.BasicLit:
		if current.Kind != token.STRING {
			return "", false
		}
		value, err := strconv.Unquote(current.Value)
		return value, err == nil
	case *ast.ParenExpr:
		return evaluateStringExpression(current.X, bindings)
	case *ast.Ident:
		value, exists := bindings[current.Name]
		return value, exists
	case *ast.BinaryExpr:
		if current.Op != token.ADD {
			return "", false
		}
		left, leftOK := evaluateStringExpression(current.X, bindings)
		right, rightOK := evaluateStringExpression(current.Y, bindings)
		return left + right, leftOK && rightOK
	case *ast.CallExpr:
		selector, ok := current.Fun.(*ast.SelectorExpr)
		if !ok {
			return "", false
		}
		packageName, packageOK := selector.X.(*ast.Ident)
		if !packageOK || packageName.Name != "strings" || selector.Sel.Name != "Join" || len(current.Args) != 2 {
			return "", false
		}
		list, ok := current.Args[0].(*ast.CompositeLit)
		if !ok {
			return "", false
		}
		separator, ok := evaluateStringExpression(current.Args[1], bindings)
		if !ok {
			return "", false
		}
		parts := make([]string, 0, len(list.Elts))
		for _, element := range list.Elts {
			part, ok := evaluateStringExpression(element, bindings)
			if !ok {
				return "", false
			}
			parts = append(parts, part)
		}
		return strings.Join(parts, separator), true
	}

	return "", false
}

func readProductionGoSources(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	for _, directory := range []string{"cmd", "internal"} {
		walkRoot := filepath.Join(root, directory)
		err := filepath.WalkDir(walkRoot, func(
			filePath string,
			entry fs.DirEntry,
			walkErr error,
		) error {
			if walkErr != nil {
				return walkErr
			}
			if entry.IsDir() || filepath.Ext(filePath) != ".go" ||
				strings.HasSuffix(filePath, "_test.go") {
				return nil
			}
			data, err := os.ReadFile(filePath)
			if err != nil {
				return err
			}
			if strings.Contains(string(data[:min(len(data), 256)]), "Code generated") {
				return nil
			}
			result[filePath] = string(data)
			return nil
		})
		if err != nil {
			t.Fatalf("scan production Go sources: %v", err)
		}
	}

	return result
}
