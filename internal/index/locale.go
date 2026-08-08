package index

import (
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
