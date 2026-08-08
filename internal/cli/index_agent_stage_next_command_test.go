// Host-Agent Stage成功导航必须继续使用当前AOCI绝对可执行路径。
package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
)

func TestAgentStageAutoNextCommandIsBound(
	t *testing.T,
) {
	root := buildAgentPlanMixedRepo(
		t,
		true,
		true,
	)

	r65ConfigureHostAgentMode(
		t,
		root,
		config.AutomationModeAuto,
	)

	request := r65HostAgentRequest(
		t,
		root,
		"new.go",
		"new.go[XAP7T]: F:绑定导航 | R:- | A:- | S:-",
	)

	output, err := r65RunAgentStageDirect(
		t,
		root,
		request,
	)
	if err != nil {
		t.Fatalf(
			"Auto Stage应成功: %v\n%s",
			err,
			output,
		)
	}

	var result agentStageResult
	if err := json.Unmarshal(
		[]byte(output),
		&result,
	); err != nil {
		t.Fatalf(
			"Stage JSON不可解析: %v\n%s",
			err,
			output,
		)
	}

	if result.NextCommand == "" ||
		!strings.Contains(
			result.NextCommand,
			"verify --json",
		) ||
		strings.HasPrefix(
			result.NextCommand,
			"aoci ",
		) {
		t.Fatalf(
			"Stage下一命令未绑定当前可执行路径: %q",
			result.NextCommand,
		)
	}
}
