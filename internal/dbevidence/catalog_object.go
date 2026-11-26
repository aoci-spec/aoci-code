package dbevidence

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Keep catalog identifiers apart until display. Delimiters can also occur in
// the identifiers themselves, so a joined name cannot be split safely later.
type catalogObjectName struct {
	namespace string
	table     string
	member    string
}

func (name catalogObjectName) label(component func(string) string) string {
	label := component(name.namespace) + "." + component(name.table)
	if name.member != "" {
		label += " (" + component(name.member) + ")"
	}
	return label
}

// DisplayObject returns the object name for a single-line diagnostic.
func (err *SourceError) DisplayObject() string {
	if err.objectName.namespace != "" {
		return err.objectName.label(quoteCatalogIdentifier)
	}
	if err.Object == "" {
		return ""
	}
	return quoteCatalogIdentifier(err.Object)
}

// Quote escapes control characters as well as the quote and backslash
// characters that would otherwise be confused with the display syntax.
func quoteCatalogIdentifier(name string) string {
	if name == "" || !utf8.ValidString(name) || strings.IndexFunc(name, func(r rune) bool {
		return strings.ContainsRune(`.()"\`, r) || unicode.IsSpace(r) || !unicode.IsPrint(r)
	}) >= 0 {
		return strconv.Quote(name)
	}
	return name
}
