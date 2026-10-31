package index

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/aoci-spec/aoci-code/textassets"
)

const localeMarkerPrefix = "#Locale:"

// DetectLocale returns the explicit machine-readable index locale. An index
// created before rc9 has no marker and deterministically remains zh-CN.
func DetectLocale(indexText string) (string, bool, error) {
	locale := ""
	explicit := false
	for _, line := range strings.Split(indexText, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, localeMarkerPrefix) {
			continue
		}
		value := strings.TrimSpace(strings.TrimPrefix(trimmed, localeMarkerPrefix))
		if value == "" {
			return "", true, fmt.Errorf("index Locale marker is empty")
		}
		if explicit {
			return "", true, fmt.Errorf("index contains duplicate Locale markers")
		}
		locale = value
		explicit = true
	}
	if !explicit {
		return textassets.LegacyLocale, false, nil
	}
	if !textassets.IsOfficialLocale(locale) {
		return "", true, fmt.Errorf("index declares unsupported Locale %q", locale)
	}
	return locale, true, nil
}

// LocaleMarker returns the canonical Header line for an official locale.
func LocaleMarker(locale string) (string, error) {
	if !textassets.IsOfficialLocale(locale) {
		return "", fmt.Errorf("unsupported index Locale %q", locale)
	}
	return localeMarkerPrefix + " " + locale, nil
}

// ReplaceLocaleMarker returns raw with only its machine-readable #Locale
// line replaced by the canonical marker for locale. Every other byte is
// preserved, including a UTF-8 BOM and CRLF line endings; a document without
// a marker gains one as its first line. It is the one edit the Locale switch
// transaction makes to a Volumes Root, so the Root's project prose, layout
// declarations, and invariants never move with the Locale.
func ReplaceLocaleMarker(raw []byte, locale string) ([]byte, error) {
	marker, err := LocaleMarker(locale)
	if err != nil {
		return nil, err
	}
	if _, _, err := DetectLocale(string(raw)); err != nil {
		return nil, err
	}
	bom := []byte{0xef, 0xbb, 0xbf}
	prefix := 0
	if bytes.HasPrefix(raw, bom) {
		prefix = len(bom)
	}
	for start := prefix; start <= len(raw); {
		end := bytes.IndexByte(raw[start:], '\n')
		if end < 0 {
			end = len(raw)
		} else {
			end += start
		}
		contentEnd := end
		if contentEnd > start && raw[contentEnd-1] == '\r' {
			contentEnd--
		}
		if strings.HasPrefix(strings.TrimSpace(string(raw[start:contentEnd])), localeMarkerPrefix) {
			result := make([]byte, 0, len(raw)-(contentEnd-start)+len(marker))
			result = append(result, raw[:start]...)
			result = append(result, marker...)
			result = append(result, raw[contentEnd:]...)
			return result, nil
		}
		if end == len(raw) {
			break
		}
		start = end + 1
	}
	lineEnding := []byte{'\n'}
	if newline := bytes.IndexByte(raw[prefix:], '\n'); newline > 0 && raw[prefix+newline-1] == '\r' {
		lineEnding = []byte{'\r', '\n'}
	}
	result := make([]byte, 0, len(raw)+len(marker)+len(lineEnding))
	result = append(result, raw[:prefix]...)
	result = append(result, marker...)
	result = append(result, lineEnding...)
	result = append(result, raw[prefix:]...)
	return result, nil
}
