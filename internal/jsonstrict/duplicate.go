// Package jsonstrict provides deterministic strict validation that the standard library encoding/json does not offer directly.
package jsonstrict

import (
	"bytes"
	"encoding/json"
	"fmt"
	"unicode"
	"unicode/utf8"
)

// DuplicateKeyError means a duplicate field appeared inside one JSON object.
//
// Conflicts carries the complete path of the earlier spelling only when the two field names differ in bytes but are equal under case
// folding; for a byte-identical duplicate it is empty, and the error text keeps its original format.
type DuplicateKeyError struct {
	Path      string
	Conflicts string
}

func (e *DuplicateKeyError) Error() string {
	if e == nil {
		return ""
	}
	if e.Conflicts != "" {
		return fmt.Sprintf(
			"JSON字段重复: %s (与 %s 仅大小写不同)",
			e.Path,
			e.Conflicts,
		)
	}
	return fmt.Sprintf(
		"JSON字段重复: %s",
		e.Path,
	)
}

// RejectDuplicateKeys checks any level of a JSON object for byte-identical duplicate fields.
//
// This function reports only duplicate fields. When the JSON itself has a syntax error it returns nil, and the caller's existing strict
// decoder goes on to give the syntax, type, unknown-field, or trailing-content error, so no second complete parser forms.
//
// Field names differing only in case are not duplicates: when the decode target is a map, encoding/json tells keys apart by byte,
// and two such keys are legal distinct entries. When the decode target is a struct, use RejectDuplicateKeysFold.
func RejectDuplicateKeys(data []byte) error {
	return rejectDuplicateKeys(
		data,
		false,
	)
}

// RejectDuplicateKeysFold checks any level of a JSON object for duplicate fields that are byte-identical or differ only
// in case.
//
// encoding/json matches JSON fields to struct fields case-insensitively (even with
// DisallowUnknownFields enabled), so {"path":"a","Path":"b"} lets the latter override the former and
// lands in one struct field without any error. A caller whose decode target is a struct must close
// that hole with this function. Two field names equal under strings.EqualFold count as duplicates, which is exactly the
// simple Unicode case folding encoding/json uses to match struct fields.
//
// The scanner cannot see the shape of the decode target: map-typed fields inside a struct are checked by the folding rule too.
// If some artifact's map field legitimately carries keys differing only in case, that caller should keep using
// RejectDuplicateKeys. As with RejectDuplicateKeys, a syntax error returns nil.
func RejectDuplicateKeysFold(data []byte) error {
	return rejectDuplicateKeys(
		data,
		true,
	)
}

func rejectDuplicateKeys(
	data []byte,
	fold bool,
) error {
	decoder := json.NewDecoder(
		bytes.NewReader(data),
	)
	decoder.UseNumber()

	err := scanJSONValue(
		decoder,
		"",
		fold,
	)
	if duplicate, ok := err.(*DuplicateKeyError); ok {
		return duplicate
	}

	return nil
}

func scanJSONValue(
	decoder *json.Decoder,
	path string,
	fold bool,
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
		// seen indexes the complete field path of the first occurrence by the comparison key (folded or as it is).
		seen := map[string]string{}

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
			lookup := key
			if fold {
				lookup = foldKey(key)
			}
			if first, exists := seen[lookup]; exists {
				conflicts := ""
				if first != fieldPath {
					conflicts = first
				}
				return &DuplicateKeyError{
					Path:      fieldPath,
					Conflicts: conflicts,
				}
			}
			seen[lookup] = fieldPath

			if err := scanJSONValue(
				decoder,
				fieldPath,
				fold,
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
				fold,
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

// foldKey returns the canonical folded form of a field name: two field names fold the same if and only if
// strings.EqualFold judges them equal. The algorithm matches the folding encoding/json uses to match struct fields:
// ASCII lower-case letters become upper case, and every other rune takes the smallest value of its unicode.SimpleFold orbit,
// so one map lookup replaces an EqualFold comparison against every key already seen.
func foldKey(
	key string,
) string {
	for index := 0; index < len(key); index++ {
		c := key[index]
		if c >= utf8.RuneSelf || ('a' <= c && c <= 'z') {
			return string(appendFoldedKey(
				make([]byte, 0, len(key)),
				key,
			))
		}
	}
	return key
}

func appendFoldedKey(
	out []byte,
	in string,
) []byte {
	for index := 0; index < len(in); {
		if c := in[index]; c < utf8.RuneSelf {
			if 'a' <= c && c <= 'z' {
				c -= 'a' - 'A'
			}
			out = append(out, c)
			index++
			continue
		}
		r, size := utf8.DecodeRuneInString(in[index:])
		out = utf8.AppendRune(out, foldRune(r))
		index += size
	}
	return out
}

// foldRune returns the smallest rune of the SimpleFold orbit r belongs to; a rune with no folding relation returns itself.
func foldRune(
	r rune,
) rune {
	for {
		next := unicode.SimpleFold(r)
		if next <= r {
			return next
		}
		r = next
	}
}
