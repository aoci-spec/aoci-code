// CI 契约: 每一处 action 引用都必须钉死到 40 位提交 SHA。
//
// 真实经历: release.yml 从一开始就是钉死的 —— 它签名、attest 并发布产物, 风险最
// 显眼。ci.yml / full-confidence.yml / release-rehearsal.yml 则一直用可移动的
// 标签, 而这三条流水线同样在检出源码、跑门禁、决定"绿不绿"。可移动标签意味着
// 上游一次改写就能改变本仓的判定, 而本仓的提交历史看不出任何变化。
//
// 这条测试是机器强制: 新增 job 时再引一个标签会在这里失败, 而不是等到某天
// 上游被劫持。发布链路已经证明钉死可行, 剩下的只是让纪律覆盖全部工作流。
package safety

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

var (
	workflowUsesPattern = regexp.MustCompile(`(?m)^\s*-?\s*uses:\s*(\S+)`)
	pinnedRefPattern    = regexp.MustCompile(`^[^@]+@[0-9a-f]{40}$`)
)

func ciWorkflowDir(t *testing.T) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("cannot locate this test file")
	}
	return filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(currentFile))), ".github", "workflows")
}

func TestEveryWorkflowActionIsPinnedToACommitSHA(t *testing.T) {
	dir := ciWorkflowDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	inspected := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, match := range workflowUsesPattern.FindAllStringSubmatch(string(raw), -1) {
			reference := match[1]
			// A local composite action is repository content, already covered by
			// the commit it lives in.
			if strings.HasPrefix(reference, "./") {
				continue
			}
			inspected++
			if !pinnedRefPattern.MatchString(reference) {
				t.Errorf("%s references %q by a movable tag; an upstream rewrite would change this "+
					"repository's verdict with nothing visible in its own history",
					entry.Name(), reference)
			}
		}
	}
	if inspected == 0 {
		t.Fatal("no workflow action references were inspected; the gate would pass while covering nothing")
	}
}

// A pinned SHA is unreadable on its own, so every pin must carry the version it
// stands for. Without it an upgrade cannot be reviewed, only diffed.
func TestEveryPinnedActionNamesItsVersion(t *testing.T) {
	dir := ciWorkflowDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	versionComment := regexp.MustCompile(`@[0-9a-f]{40}\s+#\s*v\S+`)
	inspected := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yml") {
			continue
		}
		raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		for _, line := range strings.Split(string(raw), "\n") {
			if !strings.Contains(line, "uses:") || !strings.Contains(line, "@") {
				continue
			}
			if strings.Contains(line, "./") {
				continue
			}
			inspected++
			if !versionComment.MatchString(line) {
				t.Errorf("%s: pinned reference carries no version comment: %s",
					entry.Name(), strings.TrimSpace(line))
			}
		}
	}
	// The sibling pin test has carried this guard from the start; this one did
	// not, so it passed vacuously over an empty set. A gate that reports success
	// while covering nothing is the same false green it exists to prevent.
	if inspected == 0 {
		t.Fatal("no workflow action references were inspected; the gate would pass while covering nothing")
	}
}
