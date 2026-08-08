// Host-Agent重复JSON、SHA字段、批次差异和Agent审计标签测试。
package cli

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestAgentStageRequestsRejectDuplicateJSONKeys(
	t *testing.T,
) {
	t.Run(
		"entries_plan_id",
		func(t *testing.T) {
			_, err := readAgentStageRequest(
				bytes.NewBufferString(
					`{"version":1,"plan_id":"` +
						strings.Repeat("a", 64) +
						`","plan_id":"` +
						strings.Repeat("b", 64) +
						`","agent":"codex","entries":[]}`,
				),
			)
			if err == nil ||
				!strings.Contains(
					err.Error(),
					"plan_id",
				) {
				t.Fatalf(
					"Entries重复plan_id必须拒绝: %v",
					err,
				)
			}
		},
	)

	t.Run(
		"header_plan_id",
		func(t *testing.T) {
			_, err := readAgentHeaderStageRequest(
				bytes.NewBufferString(
					`{"version":1,"plan_id":"` +
						strings.Repeat("a", 64) +
						`","plan_id":"` +
						strings.Repeat("b", 64) +
						`","agent":"codex","header":"#x"}`,
				),
			)
			if err == nil ||
				!strings.Contains(
					err.Error(),
					"plan_id",
				) {
				t.Fatalf(
					"Header重复plan_id必须拒绝: %v",
					err,
				)
			}
		},
	)

	t.Run(
		"curation_decision",
		func(t *testing.T) {
			_, err := readAgentCurationStageRequest(
				bytes.NewBufferString(
					`{"version":1,"plan_id":"` +
						strings.Repeat("a", 64) +
						`","agent":"codex","decisions":[{` +
						`"path":"a.bin","source_sha256":"` +
						strings.Repeat("b", 64) +
						`","decision":"include","decision":"exclude",` +
						`"role":"x","reason":"y","confidence":98}]}`,
				),
			)
			if err == nil ||
				!strings.Contains(
					err.Error(),
					"decisions[0].decision",
				) {
				t.Fatalf(
					"Curation重复decision必须拒绝: %v",
					err,
				)
			}
		},
	)
}

func TestAgentRequestRawRequiredFields(
	t *testing.T,
) {
	tests := []struct {
		name      string
		input     string
		read      func(string) error
		wantError string
	}{
		{
			name:  "entries_missing_plan",
			input: `{"version":1,"agent":"codex","entries":[]}`,
			read: func(input string) error {
				_, err := readAgentStageRequest(
					strings.NewReader(input),
				)
				return err
			},
			wantError: "plan_id必须填写",
		},
		{
			name: "entries_null_source",
			input: `{"version":1,"plan_id":"` +
				strings.Repeat("a", 64) +
				`","agent":"codex","entries":[{"path":"a.go","source_sha256":null,"entry":"x"}]}`,
			read: func(input string) error {
				_, err := readAgentStageRequest(
					strings.NewReader(input),
				)
				return err
			},
			wantError: "entries[0].source_sha256不能为null",
		},
		{
			name:  "header_null_plan",
			input: `{"version":1,"plan_id":null,"agent":"codex","header":"#x"}`,
			read: func(input string) error {
				_, err := readAgentHeaderStageRequest(
					strings.NewReader(input),
				)
				return err
			},
			wantError: "plan_id不能为null",
		},
		{
			name: "curation_missing_decisions",
			input: `{"version":1,"plan_id":"` +
				strings.Repeat("a", 64) +
				`","agent":"codex"}`,
			read: func(input string) error {
				_, err := readAgentCurationStageRequest(
					strings.NewReader(input),
				)
				return err
			},
			wantError: "decisions必须填写",
		},
		{
			name: "curation_null_decisions",
			input: `{"version":1,"plan_id":"` +
				strings.Repeat("a", 64) +
				`","agent":"codex","decisions":null}`,
			read: func(input string) error {
				_, err := readAgentCurationStageRequest(
					strings.NewReader(input),
				)
				return err
			},
			wantError: "decisions不能为null",
		},
		{
			name: "curation_missing_source",
			input: `{"version":1,"plan_id":"` +
				strings.Repeat("a", 64) +
				`","agent":"codex","decisions":[{` +
				`"path":"a.bin","decision":"exclude","role":"x","reason":"y","confidence":98}]}`,
			read: func(input string) error {
				_, err := readAgentCurationStageRequest(
					strings.NewReader(input),
				)
				return err
			},
			wantError: "decisions[0].source_sha256必须填写",
		},
	}

	for _, current := range tests {
		t.Run(
			current.name,
			func(t *testing.T) {
				err := current.read(current.input)
				if err == nil ||
					!strings.Contains(
						err.Error(),
						current.wantError,
					) {
					t.Fatalf(
						"错误应包含%q: %v",
						current.wantError,
						err,
					)
				}
			},
		)
	}
}

func TestNormalizeRequiredSHA256AndSummary(
	t *testing.T,
) {
	value := "ABCDEF12" +
		strings.Repeat("0", 48) +
		"34567890"

	normalized, err := normalizeRequiredSHA256(
		"source_sha256",
		value,
	)
	if err != nil {
		t.Fatal(err)
	}
	if normalized != strings.ToLower(value) {
		t.Fatalf(
			"SHA应统一小写: %s",
			normalized,
		)
	}

	if got := shortAgentStageHash(normalized); got !=
		"abcdef12…34567890" {
		t.Fatalf(
			"摘要必须同时显示首尾: %s",
			got,
		)
	}

	for _, current := range []struct {
		value string
		part  string
	}{
		{
			value: "",
			part:  "必须填写",
		},
		{
			value: strings.Repeat("a", 63),
			part:  "当前长度63",
		},
		{
			value: "g" + strings.Repeat("a", 63),
			part:  "不是合法SHA-256",
		},
	} {
		if _, err := normalizeRequiredSHA256(
			"source_sha256",
			current.value,
		); err == nil ||
			!strings.Contains(
				err.Error(),
				current.part,
			) {
			t.Fatalf(
				"非法SHA应包含%q: %v",
				current.part,
				err,
			)
		}
	}
}

func TestAgentAuditLabelNormalization(
	t *testing.T,
) {
	planID := strings.Repeat("a", 64)

	entryRequest := agentStageRequest{
		Version: agentStageVersion,
		PlanID:  planID,
		Agent:   "  Codex  ",
		Entries: []agentStageEntry{
			{
				Path:         "a.go",
				SourceSHA256: strings.Repeat("b", 64),
				Entry:        "x",
			},
		},
	}
	if err := normalizeAndValidateAgentStageRequest(
		&entryRequest,
	); err != nil {
		t.Fatal(err)
	}
	if entryRequest.Agent != "codex" {
		t.Fatalf(
			"Entries agent未规范化: %q",
			entryRequest.Agent,
		)
	}

	headerRequest := agentHeaderStageRequest{
		Version: agentHeaderStageVersion,
		PlanID:  planID,
		Agent:   "  CLAUDE-CODE  ",
		Header:  "#x",
	}
	if err := normalizeAndValidateAgentHeaderStageRequest(
		&headerRequest,
	); err != nil {
		t.Fatal(err)
	}
	if headerRequest.Agent != "claude-code" {
		t.Fatalf(
			"Header agent未规范化: %q",
			headerRequest.Agent,
		)
	}

	curationRequest := agentCurationStageRequest{
		Version: agentCurationStageVersion,
		PlanID:  planID,
		Agent:   "  Cursor  ",
		Decisions: []agentCurationDecision{
			{
				Path:         "a.bin",
				SourceSHA256: strings.Repeat("b", 64),
				Decision:     "exclude",
				Role:         "x",
				Reason:       "y",
				Confidence:   98,
			},
		},
	}
	if err := normalizeAgentCurationRequest(
		&curationRequest,
	); err != nil {
		t.Fatal(err)
	}
	if curationRequest.Agent != "cursor" {
		t.Fatalf(
			"Curation agent未规范化: %q",
			curationRequest.Agent,
		)
	}
}

func TestCurationBatchDifferenceDetails(
	t *testing.T,
) {
	firstHash := strings.Repeat("a", 64)
	secondHash := strings.Repeat("b", 64)

	plan := &agentPlan{
		CurationTargets: []agentPlanCurationTarget{
			{
				Path:         "a.bin",
				SourceSHA256: firstHash,
			},
			{
				Path:         "b.bin",
				SourceSHA256: secondHash,
			},
		},
	}

	request := agentCurationStageRequest{
		Decisions: []agentCurationDecision{
			{
				Path:         "a.bin",
				SourceSHA256: firstHash,
				Decision:     "exclude",
				Role:         "a",
				Reason:       "a",
				Confidence:   98,
			},
			{
				Path:         "a.bin",
				SourceSHA256: firstHash,
				Decision:     "exclude",
				Role:         "a",
				Reason:       "a",
				Confidence:   98,
			},
			{
				Path:         "extra.bin",
				SourceSHA256: strings.Repeat("c", 64),
				Decision:     "exclude",
				Role:         "x",
				Reason:       "x",
				Confidence:   98,
			},
		},
	}

	_, err := prepareAgentCurationDecisions(
		request,
		plan,
	)
	if err == nil {
		t.Fatal("缺失、额外和重复路径必须整批拒绝")
	}

	message := err.Error()
	for _, part := range []string{
		"missing=[b.bin]",
		"extra=[extra.bin]",
		"duplicate=[a.bin]",
	} {
		if !strings.Contains(message, part) {
			t.Fatalf(
				"批次错误缺少%q: %s",
				part,
				message,
			)
		}
	}
}

func TestAgentRequestJSONRoundTripStillWorks(
	t *testing.T,
) {
	request := agentStageRequest{
		Version: agentStageVersion,
		PlanID:  strings.Repeat("a", 64),
		Agent:   "codex",
		Entries: []agentStageEntry{},
	}

	data, err := json.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := readAgentStageRequest(
		bytes.NewReader(data),
	); err != nil {
		t.Fatalf(
			"规范JSON仍应可解析: %v",
			err,
		)
	}
}
