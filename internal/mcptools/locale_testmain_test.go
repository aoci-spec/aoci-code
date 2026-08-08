package mcptools

import (
	"os"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/textassets"
)

func TestMain(m *testing.M) {
	if err := textassets.SetActiveLocale(textassets.LegacyLocale); err != nil {
		panic(err)
	}
	os.Exit(m.Run())
}

func legacyTestConfig() *config.Config {
	cfg := config.DefaultConfig()
	cfg.Locale = textassets.LegacyLocale
	return cfg
}
