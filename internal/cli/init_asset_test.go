// Init稳定输出资产迁移的字节级兼容测试。
package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
)

func readInitGolden(
	t *testing.T,
	name string,
) string {
	t.Helper()

	data, err := os.ReadFile(
		filepath.Join(
			"..",
			"..",
			"testdata",
			"golden",
			name,
		),
	)
	if err != nil {
		t.Fatalf(
			"读取Init Golden %s失败: %v",
			name,
			err,
		)
	}

	return string(data)
}

func assertInitGolden(
	t *testing.T,
	name string,
	actual string,
) {
	t.Helper()

	expected := readInitGolden(
		t,
		name,
	)

	if actual+"\n" != expected {
		t.Fatalf(
			"Init稳定输出与Golden不一致 %s:\n%s",
			name,
			actual,
		)
	}
}

func mustInitMessage(t *testing.T, load func() (string, error)) string {
	t.Helper()
	value, err := load()
	if err != nil {
		t.Fatal(err)
	}

	return value
}

func TestInitStableOutputMatchesGoldenByteForByte(
	t *testing.T,
) {
	assertInitGolden(
		t,
		"init_next_step.txt",
		mustInitMessage(t, initNextStepMessage),
	)

	assertInitGolden(
		t,
		"init_full_index_auto.txt",
		mustInitMessage(t, func() (string, error) {
			return initFullIndexMessage("codex", config.AutomationModeAuto)
		}),
	)

	assertInitGolden(
		t,
		"init_header_dictionary.txt",
		mustInitMessage(t, initHeaderDictionaryMessage),
	)
}

func TestInitAutomationHintsMatchGoldenByteForByte(
	t *testing.T,
) {
	tests := []struct {
		mode   string
		golden string
	}{
		{
			mode:   config.AutomationModeAuto,
			golden: "init_automation_auto.txt",
		},
		{
			mode:   config.AutomationModeReview,
			golden: "init_automation_review.txt",
		},
		{
			mode:   config.AutomationModeLegacy,
			golden: "init_automation_legacy.txt",
		},
		{
			mode:   config.AutomationModeOff,
			golden: "init_automation_off.txt",
		},
	}

	for _, current := range tests {
		t.Run(
			current.mode,
			func(t *testing.T) {
				assertInitGolden(
					t,
					current.golden,
					mustInitMessage(t, func() (string, error) {
						return agentAutomationInitHint(current.mode)
					}),
				)
			},
		)
	}
}

func TestInitAutomationInvalidFallbackUnchanged(
	t *testing.T,
) {
	got := mustInitMessage(t, func() (string, error) {
		return agentAutomationInitHint("wild")
	})

	want :=
		"automation.mode 无效，请先修复团队 config.json"

	if got != want {
		t.Fatalf(
			"非法automation提示不符: got=%q want=%q",
			got,
			want,
		)
	}
}
