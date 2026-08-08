// Package jsonstrict 提供标准库encoding/json未直接提供的确定性严格校验。
package jsonstrict

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// DuplicateKeyError 表示同一JSON对象内出现重复字段。
type DuplicateKeyError struct {
	Path string
}

func (e *DuplicateKeyError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf(
		"JSON字段重复: %s",
		e.Path,
	)
}

// RejectDuplicateKeys 检查任意层级JSON对象中的重复字段。
//
// 本函数只报告重复字段。JSON本身存在语法错误时返回nil，由调用方既有严格
// 解码器继续给出语法、类型、未知字段或尾随内容错误，避免形成第二套完整解析器。
func RejectDuplicateKeys(data []byte) error {
	decoder := json.NewDecoder(
		bytes.NewReader(data),
	)
	decoder.UseNumber()

	err := scanJSONValue(
		decoder,
		"",
	)
	if duplicate, ok := err.(*DuplicateKeyError); ok {
		return duplicate
	}

	return nil
}

func scanJSONValue(
	decoder *json.Decoder,
	path string,
) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}

	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}

	switch delimiter {
	case '{':
		seen := map[string]bool{}

		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}

			key, ok := keyToken.(string)
			if !ok {
				return fmt.Errorf(
					"JSON对象字段名不是字符串",
				)
			}

			fieldPath := joinJSONPath(
				path,
				key,
			)
			if seen[key] {
				return &DuplicateKeyError{
					Path: fieldPath,
				}
			}
			seen[key] = true

			if err := scanJSONValue(
				decoder,
				fieldPath,
			); err != nil {
				return err
			}
		}

		_, err = decoder.Token()
		return err

	case '[':
		position := 0

		for decoder.More() {
			itemPath := fmt.Sprintf(
				"%s[%d]",
				path,
				position,
			)
			if err := scanJSONValue(
				decoder,
				itemPath,
			); err != nil {
				return err
			}
			position++
		}

		_, err = decoder.Token()
		return err

	default:
		return nil
	}
}

func joinJSONPath(
	parent,
	key string,
) string {
	if parent == "" {
		return key
	}
	return parent + "." + key
}
