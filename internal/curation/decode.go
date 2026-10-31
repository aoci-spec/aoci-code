// Strict in-memory decoding of the curation Document.
//
// A Host Agent Stage draft and the formal curation.json must reuse the same duplicate-field, unknown-field,
// trailing-object, and Decision-field validation, so drafts and formal assets do not form two JSON acceptance readings.
package curation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"

	"github.com/aoci-spec/aoci-code/internal/jsonstrict"
)

// DecodeDocument strictly decodes one curation Document.
//
// requireAudit=false serves a Stage draft and allows Agent and UpdatedAt to be empty for now;
// requireAudit=true serves the formally persisted asset.
func DecodeDocument(
	data []byte,
	requireAudit bool,
) (*Document, error) {
	if err := jsonstrict.RejectDuplicateKeysFold(
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
