// 策展Document的严格内存解码。
//
// Host-Agent Stage草稿与正式curation.json必须复用相同的重复字段、未知字段、
// 尾随对象和Decision字段校验，避免草稿和正式资产形成两套JSON接受口径。
package curation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/aoci-spec/aoci-code/internal/jsonstrict"
)

// DecodeDocument 严格解码一份策展Document。
//
// requireAudit=false用于Stage草稿，允许Agent和UpdatedAt暂空；
// requireAudit=true用于正式持久化资产。
func DecodeDocument(
	data []byte,
	requireAudit bool,
) (*Document, error) {
	if err := jsonstrict.RejectDuplicateKeys(
		data,
	); err != nil {
		return nil, fmt.Errorf(
			"策展JSON无效: %w",
			err,
		)
	}

	decoder := json.NewDecoder(
		bytes.NewReader(data),
	)
	decoder.DisallowUnknownFields()

	var document Document
	if err := decoder.Decode(
		&document,
	); err != nil {
		return nil, fmt.Errorf(
			"策展JSON解析失败: %w",
			err,
		)
	}

	var extra any
	if err := decoder.Decode(
		&extra,
	); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf(
				"策展JSON只能包含一个顶层对象",
			)
		}
		return nil, fmt.Errorf(
			"策展JSON存在尾随内容: %w",
			err,
		)
	}

	return NormalizeDocument(
		&document,
		requireAudit,
	)
}
