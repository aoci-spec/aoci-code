// Host-Agent请求的共享严格字段契约。
package cli

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/jsonstrict"
)

// validateAgentRequestJSON拒绝任意层级的重复JSON字段。
//
// 语法、类型、未知字段与尾随内容继续由各协议既有严格解码器负责。
func validateAgentRequestJSON(
	data []byte,
) error {
	return jsonstrict.RejectDuplicateKeys(data)
}

// requireRawJSONString区分缺失、null、非字符串和真实空字符串。
func requireRawJSONString(
	raw json.RawMessage,
	field string,
) (string, error) {
	if len(raw) == 0 {
		return "", fmt.Errorf("%s", cliMessage("agent.field.required", field))
	}

	trimmed := bytes.TrimSpace(raw)
	if bytes.Equal(
		trimmed,
		[]byte("null"),
	) {
		return "", fmt.Errorf("%s", cliMessage("agent.field.null", field))
	}

	var value any
	decoder := json.NewDecoder(
		bytes.NewReader(trimmed),
	)
	decoder.UseNumber()

	if err := decoder.Decode(&value); err != nil {
		return "", fmt.Errorf("%s", cliMessage(
			"agent.field.string_decode_failed",
			field,
			localeSafeCLIDetail(err.Error()),
		))
	}

	text, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s", cliMessage("agent.field.string_required", field))
	}

	return text, nil
}

// requireRawJSONArray区分数组字段的缺失、null和错误类型。
func requireRawJSONArray(
	raw json.RawMessage,
	field string,
) error {
	if len(raw) == 0 {
		return fmt.Errorf("%s", cliMessage("agent.field.required", field))
	}

	trimmed := bytes.TrimSpace(raw)
	if bytes.Equal(
		trimmed,
		[]byte("null"),
	) {
		return fmt.Errorf("%s", cliMessage("agent.field.null", field))
	}

	var values []json.RawMessage
	if err := json.Unmarshal(
		trimmed,
		&values,
	); err != nil {
		return fmt.Errorf("%s", cliMessage("agent.field.array_required", field))
	}

	return nil
}

// normalizeRequiredSHA256规范化并分层校验必填SHA-256字段。
func normalizeRequiredSHA256(
	field,
	value string,
) (string, error) {
	value = strings.ToLower(
		strings.TrimSpace(value),
	)

	if value == "" {
		return "", fmt.Errorf("%s", cliMessage("agent.sha.required", field))
	}
	if len(value) != 64 {
		return "", fmt.Errorf("%s", cliMessage("agent.sha.length", field, len(value)))
	}
	if _, err := hex.DecodeString(value); err != nil {
		return "", fmt.Errorf("%s", cliMessage(
			"agent.sha.invalid",
			field,
			localeSafeCLIDetail(err.Error()),
		))
	}

	return value, nil
}

// normalizeAgentAuditLabel规范化非认证的宿主审计标签。
//
// agent字段不是身份认证结果；CLI无法证明调用者身份。保留开放标签以支持未来
// 宿主，但统一trim和小写，避免Codex/codex形成不同审计主体。
func normalizeAgentAuditLabel(
	value string,
) string {
	return strings.ToLower(
		strings.TrimSpace(value),
	)
}

// shortAgentStageHash同时展示摘要首尾，避免只改末位时显示为两个相同前缀。
func shortAgentStageHash(
	hash string,
) string {
	hash = strings.TrimSpace(hash)

	if len(hash) <= 16 {
		return hash
	}

	return hash[:8] +
		"…" +
		hash[len(hash)-8:]
}
