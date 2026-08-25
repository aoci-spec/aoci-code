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
	for lineIndex, line := range strings.Split(indexText, "\n") {
		trimmed := strings.TrimSpace(line)
		if lineIndex == 0 {
			trimmed = strings.TrimPrefix(trimmed, "\ufeff")
		}
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

// ReplaceLocaleMarker changes only the machine-readable #Locale line. Legacy
// indexes without an explicit marker receive one at the start; every existing
// byte outside that one managed line is preserved.
func ReplaceLocaleMarker(raw []byte, locale string) ([]byte, error) {
	marker, err := LocaleMarker(locale)
	if err != nil {
		return nil, err
	}
	if _, _, err := DetectLocale(string(raw)); err != nil {
		return nil, err
	}

	markerStart, markerEnd := -1, -1
	for start := 0; start <= len(raw); {
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
		candidateStart := start
		if start == 0 && bytes.HasPrefix(raw, []byte{0xef, 0xbb, 0xbf}) {
			candidateStart = 3
		}
		if strings.HasPrefix(strings.TrimSpace(string(raw[candidateStart:contentEnd])), localeMarkerPrefix) {
			markerStart, markerEnd = candidateStart, contentEnd
			break
		}
		if end == len(raw) {
			break
		}
		start = end + 1
	}
	if markerStart >= 0 {
		result := make([]byte, 0, len(raw)-markerEnd+markerStart+len(marker))
		result = append(result, raw[:markerStart]...)
		result = append(result, marker...)
		result = append(result, raw[markerEnd:]...)
		return result, nil
	}

	prefix := 0
	if bytes.HasPrefix(raw, []byte{0xef, 0xbb, 0xbf}) {
		prefix = 3
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
