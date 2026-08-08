// Curation confidence原始JSON类型、必填语义和Guide无效占位合同测试。
package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func agentCurationConfidenceRequestJSON(
	confidenceField string,
) string {
	return `{"version":1,"plan_id":"` +
		strings.Repeat("a", 64) +
		`","agent":"codex","decisions":[{` +
		`"path":"docs/img/butterfly.png",` +
		`"source_sha256":"` +
		strings.Repeat("b", 64) +
		`","decision":"exclude",` +
		`"role":"文档品牌插图",` +
		`"reason":"语义由引用该图片的文档条目承载"` +
		confidenceField +
		`}]}`
}

func TestReadAgentCurationStageRequestConfidenceContract(
	t *testing.T,
) {
	tests := []struct {
		name       string
		field      string
		wantValue  int
		errorParts []string
	}{
		{
			name:      "integer_98",
			field:     `,"confidence":98`,
			wantValue: 98,
		},
		{
			name:      "integer_zero",
			field:     `,"confidence":0`,
			wantValue: 0,
		},
		{
			name:      "integer_hundred",
			field:     `,"confidence":100`,
			wantValue: 100,
		},
		{
			name:  "decimal",
			field: `,"confidence":0.98`,
			errorParts: []string{
				"decisions[0].confidence",
				"JSON整数",
				"0.98",
			},
		},
		{
			name:  "string",
			field: `,"confidence":"98"`,
			errorParts: []string{
				"decisions[0].confidence",
				"JSON整数",
				"字符串",
			},
		},
		{
			name:  "null",
			field: `,"confidence":null`,
			errorParts: []string{
				"decisions[0].confidence",
				"不能为null",
			},
		},
		{
			name:  "missing",
			field: "",
			errorParts: []string{
				"decisions[0].confidence",
				"必须填写",
			},
		},
		{
			name:  "negative_placeholder",
			field: `,"confidence":-1`,
			errorParts: []string{
				"decisions[0].confidence",
				"0至100",
				"-1",
			},
		},
		{
			name:  "over_hundred",
			field: `,"confidence":101`,
			errorParts: []string{
				"decisions[0].confidence",
				"0至100",
				"101",
			},
		},
		{
			name:  "boolean",
			field: `,"confidence":true`,
			errorParts: []string{
				"decisions[0].confidence",
				"JSON整数",
				"布尔值",
			},
		},
		{
			name:  "exponent",
			field: `,"confidence":9.8e1`,
			errorParts: []string{
				"decisions[0].confidence",
				"小数或指数",
				"9.8e1",
			},
		},
	}

	for _, current := range tests {
		t.Run(
			current.name,
			func(t *testing.T) {
				request, err :=
					readAgentCurationStageRequest(
						strings.NewReader(
							agentCurationConfidenceRequestJSON(
								current.field,
							),
						),
					)

				if len(current.errorParts) == 0 {
					if err != nil {
						t.Fatalf(
							"合法confidence不应失败: %v",
							err,
						)
					}
					if len(request.Decisions) != 1 ||
						request.Decisions[0].Confidence !=
							current.wantValue {
						t.Fatalf(
							"confidence解析结果不符: %+v",
							request.Decisions,
						)
					}
					return
				}

				if err == nil {
					t.Fatalf(
						"非法confidence应拒绝: %+v",
						request,
					)
				}

				message := err.Error()
				for _, part := range current.errorParts {
					if !strings.Contains(message, part) {
						t.Fatalf(
							"错误缺少%q: %s",
							part,
							message,
						)
					}
				}

				if strings.Contains(
					message,
					"agentCurationDecision",
				) {
					t.Fatalf(
						"错误不得暴露Go内部结构体名称: %s",
						message,
					)
				}
			},
		)
	}
}

func TestAgentCurationGuideUsesInvalidConfidencePlaceholder(
	t *testing.T,
) {
	root := buildPendingCurationRepo(t)

	cfg, document, indexPath :=
		agentPlanLoadDocument(
			t,
			root,
		)

	plan, err := buildAgentPlan(
		root,
		cfg,
		document,
		indexPath,
	)
	if err != nil {
		t.Fatal(err)
	}

	guide, err := buildAgentGuide(
		"codex",
		plan,
	)
	if err != nil {
		t.Fatal(err)
	}

	if guide.CurationStageRequest == nil ||
		len(guide.CurationStageRequest.Decisions) != 1 {
		t.Fatalf(
			"Guide缺少Curation请求模板: %+v",
			guide,
		)
	}

	if guide.CurationStageRequest.Decisions[0].Confidence != -1 {
		t.Fatalf(
			"Guide模板必须使用无效占位-1: %+v",
			guide.CurationStageRequest.Decisions[0],
		)
	}

	instructions := strings.Join(
		guide.Instructions,
		"\n",
	)

	for _, part := range []string{
		"confidence=-1",
		"无效占位",
		"逐项替换",
		"0至100",
		"JSON整数",
		"不得保留-1",
		"小数",
		"字符串",
		"null",
		"省略",
	} {
		if !strings.Contains(instructions, part) {
			t.Fatalf(
				"Guide缺少confidence合同%q:\n%s",
				part,
				instructions,
			)
		}
	}

	data, err := json.Marshal(
		guide.CurationStageRequest,
	)
	if err != nil {
		t.Fatal(err)
	}

	if !strings.Contains(
		string(data),
		`"confidence":-1`,
	) {
		t.Fatalf(
			"Guide JSON模板未输出无效占位-1: %s",
			string(data),
		)
	}
}
