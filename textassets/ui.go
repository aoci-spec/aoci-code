package textassets

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"unicode"
)

// ContractUIMessages is the single keyed catalog for short runtime messages
// whose structure is shared but whose natural-language text varies by locale.
const ContractUIMessages ID = "contracts/ui/messages"

var (
	uiMessageCatalogOnce     sync.Once
	uiMessageCatalogs        map[string]map[string]string
	uiMessageRelocalizations map[string]map[string]exactRelocalization
	uiMessageCatalogErr      error
)

type exactRelocalization struct {
	target         string
	firstKey       string
	conflictingKey string
}

// uiMessageCatalog returns one immutable decoded embedded bundle. Keeping the
// decoded maps process-local preserves the same fail-closed load boundary while
// avoiding a full JSON decode and asset scan for every rendered message.
func uiMessageCatalog(locale string) (map[string]string, error) {
	uiMessageCatalogOnce.Do(func() {
		manifest, err := embeddedManifest()
		if err != nil {
			uiMessageCatalogErr = err
			return
		}
		catalogs := make(map[string]map[string]string, len(manifest.OfficialLocales))
		for _, officialLocale := range manifest.OfficialLocales {
			source, loadErr := Load(officialLocale, ContractUIMessages)
			if loadErr != nil {
				uiMessageCatalogErr = loadErr
				return
			}
			messages, decodeErr := decodeMessageBundle([]byte(source))
			if decodeErr != nil {
				uiMessageCatalogErr = fmt.Errorf(
					"decode UI message bundle for %s: %w",
					officialLocale,
					decodeErr,
				)
				return
			}
			catalogs[officialLocale] = messages
		}
		relocalizations := make(map[string]map[string]exactRelocalization, len(catalogs))
		for targetLocale, targetMessages := range catalogs {
			byCurrentValue := map[string]exactRelocalization{}
			for _, sourceMessages := range catalogs {
				for key, current := range sourceMessages {
					if len(formatSignature(current)) != 0 {
						continue
					}
					target, exists := targetMessages[key]
					if !exists {
						uiMessageCatalogErr = fmt.Errorf("UI message key %q is missing from locale %s", key, targetLocale)
						return
					}
					matched := byCurrentValue[current]
					if matched.firstKey == "" {
						byCurrentValue[current] = exactRelocalization{target: target, firstKey: key}
						continue
					}
					if matched.target != target && matched.conflictingKey == "" {
						matched.conflictingKey = key
						byCurrentValue[current] = matched
					}
				}
			}
			relocalizations[targetLocale] = byCurrentValue
		}
		uiMessageCatalogs = catalogs
		uiMessageRelocalizations = relocalizations
	})
	if uiMessageCatalogErr != nil {
		return nil, uiMessageCatalogErr
	}
	messages, exists := uiMessageCatalogs[locale]
	if !exists {
		return nil, fmt.Errorf("unsupported text asset locale %q", locale)
	}
	return messages, nil
}

// Message formats one locale-bound runtime message. Unknown keys, malformed
// bundles, and argument-schema mismatches fail explicitly; there is no fallback.
func Message(locale, key string, args ...any) (string, error) {
	messages, err := uiMessageCatalog(locale)
	if err != nil {
		return "", err
	}
	format, exists := messages[key]
	if !exists {
		return "", fmt.Errorf("UI message key %q is not declared for locale %s", key, locale)
	}
	signature := formatSignature(format)
	if len(args) != len(signature) {
		return "", fmt.Errorf(
			"UI message %q expects %d format arguments, received %d",
			key,
			len(signature),
			len(args),
		)
	}
	result := fmt.Sprintf(format, args...)
	if strings.Contains(result, "%!") {
		return "", fmt.Errorf("UI message %q formatting failed", key)
	}
	return result, nil
}

// ValidateMessageKeys confirms that every compiled consumer key exists in the
// selected Locale bundle. Catalog-wide shape and format-signature parity are
// enforced separately by ValidateRuntime.
func ValidateMessageKeys(locale string, keys []string) error {
	source, err := Load(locale, ContractUIMessages)
	if err != nil {
		return err
	}
	messages, err := decodeMessageBundle([]byte(source))
	if err != nil {
		return err
	}
	for _, key := range keys {
		if _, exists := messages[key]; !exists {
			return fmt.Errorf("UI message key %q is not declared for locale %s", key, locale)
		}
	}
	return nil
}

// DiagnosticFacts extracts only machine-oriented tokens that remain useful
// when an untranslated component diagnostic must be suppressed. Natural prose
// is discarded; quoted fields, paths, identifiers, flags, API-like names,
// hashes, numbers, Locale names, and assignment-shaped facts remain byte-exact.
func DiagnosticFacts(detail string) string {
	facts := make([]string, 0, 8)
	seen := map[string]struct{}{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, exists := seen[value]; exists {
			return
		}
		seen[value] = struct{}{}
		facts = append(facts, value)
	}

	for index := 0; index < len(detail); index++ {
		quote := detail[index]
		if quote != '\'' && quote != '"' {
			continue
		}
		start := index
		index++
		for index < len(detail) {
			if detail[index] == '\\' {
				index++
			} else if detail[index] == quote {
				add(detail[start : index+1])
				break
			}
			index++
		}
	}

	fields := strings.FieldsFunc(detail, func(character rune) bool {
		if unicode.IsSpace(character) {
			return true
		}
		return unicode.IsPunct(character) && !strings.ContainsRune(`./\_-#=@:`, character)
	})
	for _, field := range fields {
		token := strings.Trim(field, "\t\r\n,;()[]{}<>")
		token = strings.TrimSuffix(token, ":")
		if diagnosticMachineToken(token) {
			add(token)
		}
		if len(facts) >= 16 {
			break
		}
	}
	return strings.Join(facts, " ")
}

func diagnosticMachineToken(token string) bool {
	if token == "" || strings.ContainsAny(token, "\"'") {
		return false
	}
	if strings.ContainsFunc(token, func(character rune) bool { return unicode.Is(unicode.Han, character) }) {
		return false
	}
	if strings.HasPrefix(token, "--") || strings.ContainsAny(token, `/\\_#=@`) {
		return true
	}
	if strings.Contains(token, ".") {
		return strings.HasPrefix(token, ".") || !strings.HasSuffix(token, ".")
	}
	hasDigit := false
	upperCount := 0
	lowerCount := 0
	internalUpper := false
	for index, character := range token {
		switch {
		case character >= '0' && character <= '9':
			hasDigit = true
		case character >= 'A' && character <= 'Z':
			upperCount++
			if index > 0 {
				internalUpper = true
			}
		case character >= 'a' && character <= 'z':
			lowerCount++
		}
	}
	return hasDigit || internalUpper || (upperCount >= 2 && lowerCount == 0)
}

// RelocalizeMessageExact maps an already-rendered zero-argument catalog value
// from any official locale to the same key in targetLocale. It supports command
// metadata constructed during Go package initialization without creating a
// second command tree or translation catalog.
func RelocalizeMessageExact(targetLocale, current string) (string, bool, error) {
	if _, err := uiMessageCatalog(targetLocale); err != nil {
		return "", false, err
	}
	matched, exists := uiMessageRelocalizations[targetLocale][current]
	if !exists {
		return "", false, nil
	}
	if matched.conflictingKey != "" {
		return "", false, fmt.Errorf("UI message value is ambiguous between keys %q and %q", matched.firstKey, matched.conflictingKey)
	}
	return matched.target, true, nil
}

func decodeMessageBundle(data []byte) (map[string]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if delimiter, ok := token.(json.Delim); !ok || delimiter != '{' {
		return nil, fmt.Errorf("top-level value must be an object")
	}
	result := map[string]string{}
	for decoder.More() {
		keyToken, tokenErr := decoder.Token()
		if tokenErr != nil {
			return nil, tokenErr
		}
		key, ok := keyToken.(string)
		if !ok || strings.TrimSpace(key) == "" || key != strings.TrimSpace(key) {
			return nil, fmt.Errorf("message key is invalid")
		}
		if _, duplicate := result[key]; duplicate {
			return nil, fmt.Errorf("duplicate message key %q", key)
		}
		var value string
		if decodeErr := decoder.Decode(&value); decodeErr != nil {
			return nil, fmt.Errorf("message %q must be a string: %w", key, decodeErr)
		}
		if value == "" {
			return nil, fmt.Errorf("message %q is empty", key)
		}
		result[key] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, fmt.Errorf("message bundle contains a trailing JSON value")
		}
		return nil, fmt.Errorf("message bundle contains trailing data: %w", err)
	}
	return result, nil
}

func formatSignature(format string) []byte {
	arguments := []byte{}
	nextArgument := 1
	record := func(index int, kind byte) {
		if index < 1 {
			return
		}
		for len(arguments) < index {
			arguments = append(arguments, 0)
		}
		if arguments[index-1] == 0 {
			arguments[index-1] = kind
		} else if arguments[index-1] != kind {
			arguments[index-1] = '!'
		}
	}
	for position := 0; position < len(format); position++ {
		if format[position] != '%' {
			continue
		}
		position++
		if position >= len(format) || format[position] == '%' {
			continue
		}

		pendingIndex := 0
		if index, next, ok := parseFormatIndex(format, position); ok {
			pendingIndex = index
			position = next
		}
		for position < len(format) && strings.ContainsRune("+-# 0", rune(format[position])) {
			position++
		}
		if pendingIndex > 0 && position < len(format) && format[position] == '*' {
			record(pendingIndex, '*')
			nextArgument = pendingIndex + 1
			pendingIndex = 0
			position++
		} else if position < len(format) && format[position] == '*' {
			record(nextArgument, '*')
			nextArgument++
			position++
		} else {
			for position < len(format) && format[position] >= '0' && format[position] <= '9' {
				position++
			}
		}

		if position < len(format) && format[position] == '.' {
			position++
			precisionIndex := 0
			if index, next, ok := parseFormatIndex(format, position); ok {
				precisionIndex = index
				position = next
			}
			if position < len(format) && format[position] == '*' {
				if precisionIndex == 0 {
					precisionIndex = nextArgument
				}
				record(precisionIndex, '*')
				nextArgument = precisionIndex + 1
				position++
			} else {
				for position < len(format) && format[position] >= '0' && format[position] <= '9' {
					position++
				}
			}
		}

		valueIndex := pendingIndex
		if index, next, ok := parseFormatIndex(format, position); ok {
			valueIndex = index
			position = next
		}
		if position >= len(format) {
			break
		}
		verb := format[position]
		if !strings.ContainsRune("vTtbcdoOqxXUeEfFgGsxXpaAw", rune(verb)) {
			continue
		}
		if valueIndex == 0 {
			valueIndex = nextArgument
		}
		record(valueIndex, verb)
		nextArgument = valueIndex + 1
	}
	return arguments
}

func parseFormatIndex(format string, position int) (int, int, bool) {
	if position >= len(format) || format[position] != '[' {
		return 0, position, false
	}
	index := 0
	current := position + 1
	start := current
	for current < len(format) && format[current] >= '0' && format[current] <= '9' {
		index = index*10 + int(format[current]-'0')
		current++
	}
	if current == start || current >= len(format) || format[current] != ']' || index < 1 {
		return 0, position, false
	}
	return index, current + 1, true
}
