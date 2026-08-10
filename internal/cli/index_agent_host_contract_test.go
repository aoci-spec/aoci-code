// Windows Host-Agent命令绑定、Help、请求文件类型和Guide说明测试。
package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
)

func TestAgentCommandPrefixForWindowsAndPOSIX(
	t *testing.T,
) {
	windows, err := agentCommandPrefixFor(
		"windows",
		`C:\Program Files\AOCI\aoci.exe`,
	)
	if err != nil {
		t.Fatal(err)
	}
	if windows !=
		`& "C:/Program Files/AOCI/aoci.exe"` {
		t.Fatalf(
			"Windows命令前缀不符: %q",
			windows,
		)
	}

	posix, err := agentCommandPrefixFor(
		"linux",
		"/opt/aoci cli/build/aoci",
	)
	if err != nil {
		t.Fatal(err)
	}
	if posix !=
		"'/opt/aoci cli/build/aoci'" {
		t.Fatalf(
			"POSIX命令前缀不符: %q",
			posix,
		)
	}

	quoted, err := agentCommandPrefixFor(
		"linux",
		"/tmp/aoci's build/aoci",
	)
	if err != nil {
		t.Fatal(err)
	}
	if quoted !=
		`'/tmp/aoci'"'"'s build/aoci'` {
		t.Fatalf(
			"POSIX单引号转义不符: %q",
			quoted,
		)
	}
}

func TestResolveAgentExecutablePathFailsClosed(
	t *testing.T,
) {
	tests := []struct {
		name           string
		executablePath func() (string, error)
		absolutePath   func(string) (string, error)
	}{
		{
			name: "acquisition error",
			executablePath: func() (string, error) {
				return "", errors.New("unavailable")
			},
			absolutePath: filepath.Abs,
		},
		{
			name: "empty path",
			executablePath: func() (string, error) {
				return "  ", nil
			},
			absolutePath: filepath.Abs,
		},
		{
			name: "absolute path error",
			executablePath: func() (string, error) {
				return "aoci", nil
			},
			absolutePath: func(string) (string, error) {
				return "", errors.New("cannot resolve")
			},
		},
		{
			name: "resolver returned relative path",
			executablePath: func() (string, error) {
				return "aoci", nil
			},
			absolutePath: func(string) (string, error) {
				return "bin/aoci", nil
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path, err := resolveAgentExecutablePath(
				"linux",
				test.executablePath,
				test.absolutePath,
			)
			if err == nil || path != "" {
				t.Fatalf(
					"unsafe executable resolution did not fail closed: path=%q err=%v",
					path,
					err,
				)
			}
		})
	}
}

func TestAgentCommandPrefixRejectsUnsafeOrRelativePaths(
	t *testing.T,
) {
	tests := []struct {
		goos string
		path string
	}{
		{goos: "linux", path: ""},
		{goos: "linux", path: "aoci"},
		{goos: "linux", path: "bin/aoci"},
		{goos: "linux", path: "/opt/aoci\nbin/aoci"},
		{goos: "windows", path: `aoci.exe`},
		{goos: "windows", path: `C:\AOCI\$build\aoci.exe`},
		{goos: "windows", path: "C:\\AOCI\\`build\\aoci.exe"},
		{goos: "windows", path: `C:\AOCI\"build\aoci.exe`},
		{goos: "windows", path: `\\?\C:\AOCI\aoci.exe`},
	}

	for _, test := range tests {
		prefix, err := agentCommandPrefixFor(
			test.goos,
			test.path,
		)
		if err == nil || prefix != "" {
			t.Fatalf(
				"unsafe executable path produced a command: goos=%s path=%q prefix=%q err=%v",
				test.goos,
				test.path,
				prefix,
				err,
			)
		}
	}
}

func TestFinalizeAgentGuideFailsClosedWithoutAbsoluteExecutable(
	t *testing.T,
) {
	guide, err := buildAgentGuide(
		"codex",
		guideTestPlan(agentPlanStageHeaderRequired),
	)
	if err != nil {
		t.Fatal(err)
	}

	originalCommands := guide.Commands
	originalInstructions := append(
		[]string(nil),
		guide.Instructions...,
	)

	err = finalizeAgentGuideRuntimeContractFor(
		guide,
		"linux",
		"aoci",
	)
	if err == nil {
		t.Fatal("relative executable path should stop Guide finalization")
	}
	if guide.Commands != originalCommands {
		t.Fatalf(
			"failed Guide finalization mutated commands: before=%+v after=%+v",
			originalCommands,
			guide.Commands,
		)
	}
	if strings.Join(guide.Instructions, "\n") !=
		strings.Join(originalInstructions, "\n") {
		t.Fatalf(
			"failed Guide finalization emitted a contradictory runtime promise: %v",
			guide.Instructions,
		)
	}
}

func TestFinalizeVolumeAgentGuideBindsSupportedCommandsAndOmitsLegacyPlan(
	t *testing.T,
) {
	guide := &volumeAgentGuide{
		Commands: agentGuideCommands{
			Guide:      "aoci index agent guide --agent codex --json",
			HeaderShow: "aoci index header show",
			Verify:     "aoci verify --json",
		},
	}

	if err := finalizeVolumeAgentGuideRuntimeContractFor(
		guide,
		"linux",
		"/opt/aoci current/aoci",
	); err != nil {
		t.Fatal(err)
	}

	prefix := "'/opt/aoci current/aoci'"
	for name, command := range map[string]string{
		"guide":       guide.Commands.Guide,
		"header_show": guide.Commands.HeaderShow,
		"verify":      guide.Commands.Verify,
	} {
		if !strings.HasPrefix(command, prefix+" ") {
			t.Fatalf("Volumes %s command is not bound to the current executable: %q", name, command)
		}
	}
	if guide.Commands.Plan != "" {
		t.Fatalf("Volumes Guide exposed the Legacy-only Plan command: %q", guide.Commands.Plan)
	}

	rendered, err := json.Marshal(guide)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(rendered, []byte(`"plan"`)) ||
		bytes.Contains(rendered, []byte("index agent plan")) {
		t.Fatalf("Volumes Guide JSON exposed the Legacy-only Plan command: %s", rendered)
	}
}

func TestFinalizeAgentGuideBindsExecutableAndLimits(
	t *testing.T,
) {
	numeric := machinecontract.NumericText()
	entriesPlan := guideTestPlan(
		agentPlanStageEntriesRequired,
	)
	entriesPlan.AutomationMode =
		config.AutomationModeAuto
	entriesPlan.Targets = []agentPlanTarget{
		{
			Path: "x.go",
			Kind: "create",
			SourceSHA256: strings.Repeat(
				"a",
				64,
			),
		},
	}

	entriesGuide, err := buildAgentGuide(
		"codex",
		entriesPlan,
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := finalizeAgentGuideRuntimeContract(entriesGuide); err != nil {
		t.Fatal(err)
	}

	if strings.HasPrefix(
		entriesGuide.Commands.Guide,
		"aoci ",
	) ||
		strings.HasPrefix(
			entriesGuide.Commands.EntriesStage,
			"aoci ",
		) {
		t.Fatalf(
			"Guide命令不得保留裸aoci: %+v",
			entriesGuide.Commands,
		)
	}

	instructions := strings.Join(
		entriesGuide.Instructions,
		"\n",
	)
	for _, anchor := range []string{
		"绝对路径",
		"不要改回裸aoci",
		fmt.Sprintf("%d字节", numeric.EntriesMaxBodyBytes),
		numeric.EntriesMaxBodyHuman,
		fmt.Sprintf("最多%d条", numeric.EntriesMaxEntries),
		"auto_finalize.status=repair_required",
		"不得要求用户回复“继续”",
	} {
		if !strings.Contains(
			instructions,
			anchor,
		) {
			t.Fatalf(
				"Entries Guide缺少运行时合同%q:\n%s",
				anchor,
				instructions,
			)
		}
	}

	curationPlan := guideTestPlan(
		agentPlanStageCurationRequired,
	)
	curationPlan.CurationTargets =
		[]agentPlanCurationTarget{
			{
				Path: "marker.empty",
				SourceSHA256: strings.Repeat(
					"b",
					64,
				),
			},
		}

	curationGuide, err := buildAgentGuide(
		"codex",
		curationPlan,
	)
	if err != nil {
		t.Fatal(err)
	}

	if err := finalizeAgentGuideRuntimeContract(curationGuide); err != nil {
		t.Fatal(err)
	}

	curationInstructions := strings.Join(
		curationGuide.Instructions,
		"\n",
	)
	for _, anchor := range []string{
		fmt.Sprintf("%d字节", numeric.CurationMaxBodyBytes),
		numeric.CurationMaxBodyHuman,
		fmt.Sprintf("最多%d项", numeric.CurationMaxDecisions),
		"role和reason",
		"规范化为空格",
	} {
		if !strings.Contains(
			curationInstructions,
			anchor,
		) {
			t.Fatalf(
				"Curation Guide缺少合同%q:\n%s",
				anchor,
				curationInstructions,
			)
		}
	}
}

func TestAgentGuideCommandJSONUsesBoundCommands(
	t *testing.T,
) {
	root := buildAgentPlanMixedRepo(
		t,
		true,
		true,
	)

	oldRepo := flagRepo
	oldJSON := flagJSON
	flagRepo = root
	flagJSON = true

	t.Cleanup(func() {
		flagRepo = oldRepo
		flagJSON = oldJSON
	})

	command := newIndexAgentGuideCmd()
	command.SilenceUsage = true
	command.SilenceErrors = true
	command.SetArgs(
		[]string{
			"--agent",
			"codex",
		},
	)

	var output bytes.Buffer
	command.SetOut(
		&output,
	)
	command.SetErr(
		&output,
	)

	if err := command.Execute(); err != nil {
		t.Fatalf(
			"Guide命令失败: %v\n%s",
			err,
			output.String(),
		)
	}

	var guide agentGuide
	if err := json.Unmarshal(
		output.Bytes(),
		&guide,
	); err != nil {
		t.Fatalf(
			"Guide JSON不可解析: %v\n%s",
			err,
			output.String(),
		)
	}

	if guide.Commands.Guide == "" ||
		guide.Commands.EntriesStage == "" ||
		strings.HasPrefix(
			guide.Commands.Guide,
			"aoci ",
		) ||
		strings.HasPrefix(
			guide.Commands.EntriesStage,
			"aoci ",
		) {
		t.Fatalf(
			"生产Guide未绑定可执行路径: %+v",
			guide.Commands,
		)
	}
}

func TestLoadAgentRequestInputRejectsDirectory(
	t *testing.T,
) {
	directory := t.TempDir()

	_, _, err := loadAgentRequestInput(
		false,
		directory,
		nil,
		1024,
		"测试Stage",
	)

	var inputErr *agentRequestInputError
	if !errors.As(
		err,
		&inputErr,
	) ||
		inputErr.Code != ExitConfig ||
		!strings.Contains(
			err.Error(),
			"路径是目录",
		) ||
		!strings.Contains(
			err.Error(),
			"普通JSON文件",
		) {
		t.Fatalf(
			"目录型request-file错误不明确: %v",
			err,
		)
	}
}

func TestLoadAgentRequestInputReportsActualOversize(
	t *testing.T,
) {
	path := filepath.Join(
		t.TempDir(),
		"oversize.json",
	)
	if err := os.WriteFile(
		path,
		[]byte("12345"),
		0o644,
	); err != nil {
		t.Fatal(err)
	}

	_, _, err := loadAgentRequestInput(
		false,
		path,
		nil,
		4,
		"测试Stage",
	)

	var inputErr *agentRequestInputError
	if !errors.As(
		err,
		&inputErr,
	) ||
		inputErr.Code != ExitInvalid ||
		!strings.Contains(
			err.Error(),
			"上限4字节",
		) ||
		!strings.Contains(
			err.Error(),
			"实际5字节",
		) {
		t.Fatalf(
			"超限诊断未给出实际大小: %v",
			err,
		)
	}
}

func TestRootHelpDeclaresHostAgentLimits(
	t *testing.T,
) {
	numeric := machinecontract.NumericText()
	root := newRootCmd()

	// subCommands使用全局Cobra命令实例。测试结束必须解除临时Root的父子关系，
	// 否则后续直接执行init等子命令时会被Cobra提升为执行临时根命令。
	children := root.Commands()
	t.Cleanup(func() {
		root.RemoveCommand(
			children...,
		)
	})

	tests := []struct {
		path    []string
		anchors []string
	}{
		{
			path: []string{
				"index",
				"agent",
				"stage",
			},
			anchors: []string{
				fmt.Sprintf("%d字节", numeric.EntriesMaxBodyBytes),
				numeric.EntriesMaxBodyHuman,
				fmt.Sprintf("最多%d条", numeric.EntriesMaxEntries),
				"Stage先安全提交草稿",
				"automation.mode=auto",
				"内部完成Check、Diff、P-23与原子Apply",
				"候选内容错误返回repair_required",
				"只修失败项并自动重新Stage",
				"stopped结束当前尝试",
				"自动重新Plan、Resume、Rollback、用户动作或hard block",
				"review或legacy只保留草稿",
			},
		},
		{
			path: []string{
				"index",
				"agent",
				"header",
				"stage",
			},
			anchors: []string{
				fmt.Sprintf("%d字节", numeric.HeaderMaxBodyBytes),
				numeric.HeaderMaxBodyHuman,
				fmt.Sprintf("%d字节", numeric.HeaderMaxHeaderBytes),
				numeric.HeaderMaxHeaderHuman,
			},
		},
		{
			path: []string{
				"index",
				"agent",
				"curation",
				"stage",
			},
			anchors: []string{
				fmt.Sprintf("%d字节", numeric.CurationMaxBodyBytes),
				numeric.CurationMaxBodyHuman,
				fmt.Sprintf("最多%d项", numeric.CurationMaxDecisions),
				"规范化为空格",
			},
		},
		{
			path: []string{
				"index",
				"agent",
				"guide",
			},
			anchors: []string{
				"绝对路径",
				"PowerShell",
				"execute严格按当前阶段指令",
				"Entries Auto由Stage内部完成",
				"applied后只Verify并重新Guide",
				"repair_required只修findings中的失败项",
				"不得要求用户回复继续",
				"stopped时按现有正式写入与Recovery证据分类",
				"只有审批/外部动作边界或hard block才报告并结束",
				"Header与Curation按各自Guide",
			},
		},
	}

	for _, current := range tests {
		command, _, err := root.Find(
			current.path,
		)
		if err != nil ||
			command == nil {
			t.Fatalf(
				"未找到命令%v: %v",
				current.path,
				err,
			)
		}

		for _, anchor := range current.anchors {
			if !strings.Contains(
				command.Long,
				anchor,
			) {
				t.Fatalf(
					"命令%v Help缺少%q:\n%s",
					current.path,
					anchor,
					command.Long,
				)
			}
		}
	}

	entriesCommand, _, err := root.Find(
		[]string{
			"index",
			"agent",
			"stage",
		},
	)
	if err != nil ||
		entriesCommand == nil {
		t.Fatalf(
			"未找到Entries Stage命令: %v",
			err,
		)
	}

	if strings.Contains(
		entriesCommand.Long,
		"Stage不修改正式索引或Baseline",
	) {
		t.Fatalf(
			"Entries Stage Help仍含R65前旧合同:\n%s",
			entriesCommand.Long,
		)
	}

	guideCommand, _, err := root.Find(
		[]string{
			"index",
			"agent",
			"guide",
		},
	)
	if err != nil ||
		guideCommand == nil {
		t.Fatalf(
			"未找到Guide命令: %v",
			err,
		)
	}

	if strings.Contains(
		guideCommand.Long,
		"execute连续到Apply/Verify",
	) {
		t.Fatalf(
			"Guide Help仍含R65前旧合同:\n%s",
			guideCommand.Long,
		)
	}

	firstLong := guideCommand.Long
	decorateHostAgentHelp(
		root,
	)

	if guideCommand.Long != firstLong {
		t.Fatalf(
			"Help装饰必须幂等:\n第一次=%q\n第二次=%q",
			firstLong,
			guideCommand.Long,
		)
	}
}
