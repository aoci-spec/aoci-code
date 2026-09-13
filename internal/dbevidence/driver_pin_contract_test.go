// 数据库驱动版本表的机器闸。
//
// 真实经历: docs/supply-chain.md 的 "Database driver dependency audit" 表把三个驱动
// 模块的钉死版本写成给人看的四行 Markdown。表里没有任何东西读它, 于是 go.mod 被
// dependabot 提升之后, 表还停在旧版本上: rc11 切版前的对抗式审查才发现 pgx v5.10.0
// 与 go-sql-driver/mysql v1.10.0 已经过期(见提交 "docs: correct the driver audit
// versions and the README boundary claims"), 只能手工改表。supply-chain.md 是随 tag
// 发布出去、且被 tag 固定链接引用的文档, 所以它对外声明的版本就是发布声明的一部分;
// 一张没有闸的表迟早再次过期, 而这次它过期的位置恰好是被声明为供应链证据的文档。
//
// 本测试只比对"表里的版本"与"go.mod 要求的版本", 不判断版本"应该是"什么 —— 那是
// 依赖升级的决定。openGauss 行多一项: 它的模块身份来自 go.mod 的 require, 而实际
// 编译进二进制的字节来自 replace 指向的 third_party/ 携带副本, 因此两处都必须仍然
// 指向那个携带副本, 否则文档描述的"官方身份 + 本地已审补丁"就与实际构建不符。
package dbevidence

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

// driverAuditSectionHeading 是这四行表格所属的小节标题。
const driverAuditSectionHeading = "## Database driver dependency audit"

// carriedOpenGaussModule 是根模块以本地 replace 承载其已审补丁的驱动模块。
const carriedOpenGaussModule = "gitcode.com/opengauss/openGauss-connector-go-pq"

// carriedOpenGaussTree 是被 carriedOpenGaussModule 换入的仓库内路径。
const carriedOpenGaussTree = "third_party/openGauss-connector-go-pq"

// driverAuditTableRow 匹配表行 "| `module` | `version` ... |", 并按需去掉版本两侧
// 的反引号。openGauss 行的版本是 "`v1.0.8` + reviewed AOCI patch", 因此只锚定行首的
// "| `" 与模块名之后紧跟的 "`", 表格的列宽与列顺序变化不会让它静默失配。
var driverAuditTableRow = regexp.MustCompile("^\\|\\s*`([^`]+)`\\s*\\|\\s*`([^`]+)`")

// driverAuditRepoRoot 从本测试文件位置解析仓库根, 与本包其他契约测试一致; 测试路径
// 不依赖调用者的工作目录。
func driverAuditRepoRoot(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test file")
	}
	return filepath.Dir(filepath.Dir(filepath.Dir(currentFile)))
}

func readDriverAuditFile(t *testing.T, root, relative string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(relative)))
	if err != nil {
		t.Fatalf("%s: %v", relative, err)
	}
	return string(raw)
}

// driverAuditSection 返回小节标题到下一个二级标题之间的正文。小节被改名或挪走时返回
// 空串, 由调用方判定为"闸已经不再覆盖它", 而不是静默通过。
func driverAuditSection(document string) string {
	start := strings.Index(document, driverAuditSectionHeading)
	if start < 0 {
		return ""
	}
	remainder := document[start+len(driverAuditSectionHeading):]
	if end := strings.Index(remainder, "\n## "); end >= 0 {
		remainder = remainder[:end]
	}
	return remainder
}

// driverAuditTableEntries 返回表内每个模块的文档版本与它在文中出现的行号, 并校验
// 每个模块只有一行。
func driverAuditTableEntries(t *testing.T, document string) (map[string]string, map[string]int) {
	t.Helper()
	section := driverAuditSection(document)
	if section == "" {
		t.Fatalf("docs/supply-chain.md carries no %q section; this gate has stopped covering it",
			driverAuditSectionHeading)
	}
	versions := map[string]string{}
	lines := map[string]int{}
	for index, line := range strings.Split(section, "\n") {
		match := driverAuditTableRow.FindStringSubmatch(strings.TrimSpace(line))
		if match == nil {
			continue
		}
		module, version := match[1], match[2]
		if previous, seen := versions[module]; seen {
			t.Fatalf("the driver audit table lists %s twice (%s and %s)", module, previous, version)
		}
		versions[module] = version
		lines[module] = index + 1
	}
	if len(versions) == 0 {
		t.Fatalf("the %q table lists no module rows; the gate would pass while covering nothing",
			driverAuditSectionHeading)
	}
	return versions, lines
}

// goModGraph 是机器判据所用的 go.mod 事实: 每个 require 的已解析版本, 以及每个
// replace 的目标路径。它只读取文件文本, 不调用 go 命令, 因此在任何 Go 版本下与
// make fast / make full 的其他测试同样可执行。
type goModGraph struct {
	required map[string]string
	replaced map[string]string
}

func parseGoModGraph(t *testing.T, text string) goModGraph {
	t.Helper()
	graph := goModGraph{required: map[string]string{}, replaced: map[string]string{}}
	inRequireBlock := false
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(strings.SplitN(raw, "//", 2)[0])
		if line == "" {
			continue
		}
		if inRequireBlock {
			if line == ")" {
				inRequireBlock = false
				continue
			}
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				graph.required[fields[0]] = fields[1]
			}
			continue
		}
		if strings.HasPrefix(line, "replace ") {
			if module, target, err := parseReplaceDirective(line); err == nil {
				graph.replaced[module] = target
			}
			continue
		}
		if line == "require (" {
			inRequireBlock = true
			continue
		}
		if fields := strings.Fields(line); len(fields) >= 3 && fields[0] == "require" {
			graph.required[fields[1]] = fields[2]
		}
	}
	return graph
}

// parseReplaceDirective 解析 "replace <module> [<version>] => <target> [<version>]"。
// replace 目标里的版本只在换入另一个已发布模块时出现; 这里需要的是目标身份, 因此未
// 给出目标版本时返回空串而不是报错。
func parseReplaceDirective(line string) (string, string, error) {
	arrow := strings.Index(line, "=>")
	if arrow < 0 {
		return "", "", errors.New("replace directive carries no =>")
	}
	left := strings.Fields(strings.TrimSpace(strings.TrimPrefix(line, "replace")))
	right := strings.Fields(strings.TrimSpace(line[arrow+2:]))
	if len(left) == 0 || len(right) == 0 {
		return "", "", errors.New("replace directive names no module or no target")
	}
	return left[0], right[0], nil
}

// TestDocumentedDriverVersionsMatchGoMod 是这条闸本身。失败信息同时点名模块、表中
// 版本与 go.mod 版本, 使升级者不必自己去查是哪一边落后了。表里点名而 go.mod 完全不
// require 的模块同样是失败: 删掉一个驱动却把表行留下, 正是这条闸最初要抓的、rc11
// 切版前手工改表那类过期声明, 静默通过等于闸只覆盖了三个模块中的两个。
func TestDocumentedDriverVersionsMatchGoMod(t *testing.T) {
	root := driverAuditRepoRoot(t)
	versions, _ := driverAuditTableEntries(t, readDriverAuditFile(t, root, "docs/supply-chain.md"))
	graph := parseGoModGraph(t, readDriverAuditFile(t, root, "go.mod"))

	inspected := 0
	for module, documented := range versions {
		declared, ok := graph.required[module]
		if !ok {
			t.Errorf("the driver audit table lists %s, but go.mod does not require it\n"+
				"a driver removed from go.mod leaves the released supply-chain document claiming a dependency the module set no longer has",
				module)
			continue
		}
		inspected++
		if documented != declared {
			t.Errorf("the driver audit table pins %s at %s where go.mod requires %s\n"+
				"a dependency bump moves go.mod and the released supply-chain document keeps its old table row",
				module, documented, declared)
		}
	}
	if inspected == 0 {
		t.Fatal("the driver audit table named no module that go.mod requires; the gate covered nothing")
	}
}

// TestDocumentedOpenGaussDriverStillResolvesToTheCarriedTree 覆盖 openGauss 行多出的
// 那一项: go.mod 里 require 的身份是官方模块, 而实际编译的字节来自 replace 指向的
// third_party/ 携带副本。一旦 replace 被删掉或改指别处, 构建就会回到未打补丁的上游
// 行为, 而表里 "v1.0.8 + reviewed AOCI patch" 与文档的复审结论会同时变成不实描述。
func TestDocumentedOpenGaussDriverStillResolvesToTheCarriedTree(t *testing.T) {
	root := driverAuditRepoRoot(t)
	versions, _ := driverAuditTableEntries(t, readDriverAuditFile(t, root, "docs/supply-chain.md"))
	graph := parseGoModGraph(t, readDriverAuditFile(t, root, "go.mod"))

	if _, documented := versions[carriedOpenGaussModule]; !documented {
		t.Errorf("the driver audit table no longer names %s, so its version and its carried patch are unguarded",
			carriedOpenGaussModule)
	}
	declared, required := graph.required[carriedOpenGaussModule]
	if !required {
		t.Fatalf("go.mod no longer requires %s, but docs/supply-chain.md documents the carried patched module",
			carriedOpenGaussModule)
	}
	if documented, want := versions[carriedOpenGaussModule], declared; documented != "" && documented != want {
		t.Errorf("the driver audit table pins %s at %s where go.mod requires %s",
			carriedOpenGaussModule, documented, want)
	}
	target, replaced := graph.replaced[carriedOpenGaussModule]
	if !replaced {
		t.Fatalf("go.mod no longer replaces %s, so it resolves to unpatched upstream behavior while "+
			"docs/supply-chain.md still documents the reviewed AOCI patch carried under %s",
			carriedOpenGaussModule, carriedOpenGaussTree)
	}
	if want := "./" + carriedOpenGaussTree; strings.TrimSuffix(target, "/") != want {
		t.Errorf("go.mod replaces %s with %q, but the documented carried copy is %s",
			carriedOpenGaussModule, target, carriedOpenGaussTree)
	}
	if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(carriedOpenGaussTree), "go.mod")); err != nil {
		t.Fatalf("the replace target %s is not a readable module tree: %v", carriedOpenGaussTree, err)
	}
}
