// 禁区扫描正反用例
// 索引条目: safety_test.go[Test.Claims.8.IP.S]
package safety

import (
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

// TestForbiddenAndOverclaim 逐词正用例
func TestForbiddenAndOverclaim(t *testing.T) {
	for _, term := range machinecontract.PublicTextTerms() {
		hits := CheckForbiddenClaims("prefix " + term.Text + " suffix")
		if len(hits) != 1 || hits[0].Term != term.Text || hits[0].Kind != term.Kind {
			t.Errorf("机器词表项未按声明命中: term=%+v hits=%+v", term, hits)
		}
		if term.Mode == machinecontract.TextMatchSubstringFold {
			hits = CheckForbiddenClaims(strings.ToUpper(term.Text))
			if len(hits) != 1 || hits[0].Term != term.Text {
				t.Errorf("大小写不敏感词表项未命中: term=%+v hits=%+v", term, hits)
			}
		}
	}
}

// TestTriesWordBoundary TRIES 词边界: 专名命中,entries/retries/小写不误伤
func TestTriesWordBoundary(t *testing.T) {
	if hits := CheckForbiddenClaims("K字段 TRIES 扩展"); len(hits) == 0 {
		t.Fatal("TRIES 专名应命中")
	}
	clean := "Use aoci_get_entries to read entries; the parser retries nothing; tries lowercase."
	if hits := CheckForbiddenClaims(clean); len(hits) != 0 {
		t.Fatalf("entries/retries/小写 tries 不得误伤: %+v", hits)
	}
}

// TestCleanText 合规文案零命中 + FormatHits 空渲染
func TestCleanText(t *testing.T) {
	text := "AOCI 提供文件级意图/约束/负空间,与结构图工具互补不替代。"
	hits := CheckForbiddenClaims(text)
	if len(hits) != 0 {
		t.Fatalf("合规文案不应命中: %+v", hits)
	}
	if FormatHits("x", hits) != "" {
		t.Fatal("空命中应渲染为空串")
	}
}
