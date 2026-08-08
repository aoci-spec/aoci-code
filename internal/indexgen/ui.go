package indexgen

import (
	"fmt"

	"github.com/aoci-spec/aoci-code/textassets"
)

func indexgenMessage(key string, args ...any) string {
	value, err := textassets.Message(textassets.ActiveLocale(), key, args...)
	if err != nil {
		return fmt.Sprintf("[text asset %q failed: %v]", key, err)
	}
	return value
}
