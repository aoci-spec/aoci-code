// aoci hook —— hook runtime 隐藏子命令(供各 agent 的 shell 模板调用)
// 索引条目: hook.go[CHK8S]
//
// 契约: shell 模板零逻辑只 exec 本命令;JSON 解析与判断全在 Go 内可测。
// 输入双通道:
//
//	--path 显式传参(通用/测试通道);
//	--stdin-json 从 stdin 读 agent 的 hook JSON(Claude Code 通道:
//	  {"tool_name":"Edit","tool_input":{"file_path":"..."}}),取 tool_name 与 file_path。
//
// 退出码映射:
//
//	放行 = 0;
//	阻断(strict 且条目 STALE)= 通用 4;--agent claude 时映射为 2 且文本走 stderr
//	(Claude Code 契约: exit 2 = 阻断并将 stderr 回喂模型)。
//
// 环境异常一律放行退出 0 —— hook 绝不能因自身故障卡死用户工作流。
package cli

import (
	"encoding/json"
	"fmt"

	"github.com/aoci-spec/aoci-code/internal/config"
	"github.com/aoci-spec/aoci-code/internal/hooks"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/spf13/cobra"
)

// ExitHookBlock 通用阻断退出码;ExitHookBlockClaude 为 Claude Code 契约码
const (
	ExitHookBlock       = 4
	ExitHookBlockClaude = 2
)

// claudeHookInput Claude Code hook stdin JSON 的最小解析结构
type claudeHookInput struct {
	ToolName  string `json:"tool_name"`
	ToolInput struct {
		FilePath string `json:"file_path"`
	} `json:"tool_input"`
}

func init() {
	var agent, tool, path string
	var stdinJSON bool

	pretool := &cobra.Command{
		Use:   "pretool",
		Short: cliMessage("cli.short.hook"),
		RunE: func(cmd *cobra.Command, args []string) error {
			// 仓库定位失败直接放行(hook 容错纪律)
			root, err := config.FindRepoRoot(".", flagRepo)
			if err != nil {
				return nil
			}

			// stdin-json 通道: 解析失败放行(容错),成功则覆盖 tool/path
			if stdinJSON {
				data, tooLarge, rerr := readLimitedInput(cmd.InOrStdin(), machinecontract.HeaderRequestMaxBytes)
				if rerr == nil && !tooLarge && len(data) > 0 {
					var in claudeHookInput
					if json.Unmarshal(data, &in) == nil {
						if in.ToolName != "" {
							tool = in.ToolName
						}
						if in.ToolInput.FilePath != "" {
							path = in.ToolInput.FilePath
						}
					}
				}
			}
			if path == "" {
				return nil // 无路径: 无事可做,放行
			}

			// Claude 传绝对路径属正常(其 file_path 为绝对);内核只认相对路径,先行换算
			path = toRepoRel(root, path)

			res := hooks.HandlePreTool(root, tool, path)
			if res.Block {
				if agent == "claude" {
					// Claude 契约: exit 2 + stderr 回喂模型
					return &ExitError{Code: ExitHookBlockClaude, Msg: res.Text}
				}
				if res.Text != "" {
					fmt.Println(res.Text)
				}
				return &ExitError{Code: ExitHookBlock}
			}
			if res.Text != "" {
				fmt.Println(res.Text)
			}
			return nil
		},
	}
	pretool.Flags().StringVar(&agent, "agent", "", cliMessage("cli.flag.hook_agent"))
	pretool.Flags().StringVar(&tool, "tool", "", cliMessage("cli.flag.hook_tool"))
	pretool.Flags().StringVar(&path, "path", "", cliMessage("cli.flag.hook_path"))
	pretool.Flags().BoolVar(&stdinJSON, "stdin-json", false, cliMessage("cli.flag.hook_stdin"))

	cmd := &cobra.Command{
		Use:    "hook",
		Short:  cliMessage("cli.short.hook_runtime"),
		Hidden: true, // 人用 help 不展示,属机器通道
	}
	cmd.AddCommand(pretool)
	registerCommand(cmd)
}

// toRepoRel 绝对路径落在仓库根下时换算为相对路径(正斜杠);其余原样返回交内核裁决。
// Windows 盘符大小写归一(C: 与 c: 视为同盘;NTFS 路径不区分大小写,盘符字母是
// 最常见的大小写漂移点 —— 审查修正,归一前 c:\repo 与 C:\repo 前缀比对会失配,
// 导致注入静默失效;仅归一盘符不归一其余路径段,避免误合 POSIX 上真正区分大小写的路径)。
func toRepoRel(root, p string) string {
	if len(p) == 0 || (p[0] != '/' && !(len(p) >= 2 && p[1] == ':')) {
		return p // 已是相对路径
	}
	norm := func(s string) string {
		out := make([]byte, len(s))
		for i := 0; i < len(s); i++ {
			if s[i] == '\\' {
				out[i] = '/'
			} else {
				out[i] = s[i]
			}
		}
		// 盘符归一为大写(仅当形如 X: 开头)
		if len(out) >= 2 && out[1] == ':' && out[0] >= 'a' && out[0] <= 'z' {
			out[0] = out[0] - 'a' + 'A'
		}
		return string(out)
	}
	np, nr := norm(p), norm(root)
	for len(nr) > 0 && nr[len(nr)-1] == '/' {
		nr = nr[:len(nr)-1]
	}
	if np == nr {
		return p
	}
	if len(np) > len(nr)+1 && np[:len(nr)] == nr && np[len(nr)] == '/' {
		return np[len(nr)+1:]
	}
	return p
}
