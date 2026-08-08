// 头部草稿Prompt编译器: 把“当前头部 + 仓库画像 + 格式纪律”编译为可发送给
// 用户端点的system和user两段消息文本。
// 索引条目: header.go[WPM8M]
//
// 定位(D28冷启动顺序铁律): 索引头是所有条目的全局坐标系，header bootstrap
// 先于一切条目生成。本包只负责“把事实与纪律编译成文本”，不发起任何网络调用。
//
// 依赖铁律(D23加严): 本包保持确定性纯函数:
//   - 不import internal/llm，消息以裸字符串返回，由workflow层翻译为llm.Message；
//   - 不import config/indexgen，输入由调用方翻译为本包纯数据结构HeaderInput；
//   - 不读文件、不落盘、不走网络；同输入必得同输出。
//     Dirs、Exts、SampleFiles按调用方给定顺序原样渲染，排序与截断由调用方负责。
//
// Prompt文档化(D70,v2.8): 纪律文本位于textassets/zh-CN/prompts/*.txt，
// 经统一textassets目录go:embed编入二进制，保持单二进制、离线和零运行时
// 文件依赖。BuildHeaderMessages只按稳定资源ID加载并拼装，不保留正文副本。
// 拼装逻辑保持确定性，锚点测试锁定关键纪律。
//
// 承诺与闸门对齐纪律: 输出规则中声称“机器校验”的条款必须存在真实机器闸。
// 无法机器化的语言、语义质量和人工校准示例保留要求，应明确作为语义纪律。
//
// 通用性纪律: 头部不要求独立的抽象边界板块。系统边界写入【系统】，
// 文件特有必要事实写入S，文件收录与排除由Curation治理，三者不得混写。
//
// 正确示例纪律: Header Prompt固定提供两条人工校准的真实Go索引示例。
// 示例只传达FRAS分工与颗粒度，不要求目标仓库采用Go技术栈或示例标签。
//
// Prompt三条硬输入:
//   - D37: 事实以当轮注入内容为依据；
//   - R20: 禁止声称“已核对”，不得虚构画像之外的内容；
//   - D40: 字典纪律立约，B/D符号禁数字，取值只覆盖画像可推断范围。
package prompt

import (
	"errors"
	"strings"

	"github.com/aoci-spec/aoci-code/textassets"
)

// trimAsset剥除embed资产的尾换行，保证拼装结果稳定。
func trimAsset(asset string) string {
	return strings.TrimRight(
		asset,
		"\n",
	)
}

// DirCount描述某个目录下的文件数量。
type DirCount struct {
	Dir   string
	Count int
}

// ExtCount描述某种扩展名的文件数量。
type ExtCount struct {
	Ext   string
	Count int
}

// HeaderInput包含编译头部草稿Prompt所需的全部输入。
type HeaderInput struct {
	// ProjectName是项目名；空值渲染为“未命名项目”。
	ProjectName string

	// RepoRootSlash是使用正斜杠表示的仓库根绝对路径。
	RepoRootSlash string

	// CurrentHeader是现有头部原文；非空时在其基础上完善。
	CurrentHeader string

	// TotalFiles是仓库纳入扫描的文件总数。
	TotalFiles int

	// Dirs是目录分布，调用方负责排序和截断。
	Dirs []DirCount

	// Exts是扩展名分布，调用方负责排序和截断。
	Exts []ExtCount

	// SampleFiles是代表性文件的仓库相对路径样本。
	SampleFiles []string
}

type headerUserTemplateData struct {
	ProjectName      string
	RepoRootSlash    string
	HasCurrentHeader bool
	CurrentHeader    string
	TotalFiles       int
	Dirs             []DirCount
	Exts             []ExtCount
	SampleFiles      []string
}

// BuildHeaderMessages编译头部草稿的system与user消息。
func BuildHeaderMessages(
	input HeaderInput,
) (
	system string,
	user string,
	err error,
) {
	if strings.TrimSpace(
		input.RepoRootSlash,
	) == "" {
		return "", "", errors.New(
			"HeaderInput.RepoRootSlash不能为空" +
				"(头部需要明确的绝对路径基准)",
		)
	}

	var systemBuilder strings.Builder
	for position, assetID := range []textassets.ID{
		textassets.PromptHeaderRole,
		textassets.PromptHeaderOutputRules,
		textassets.PromptHeaderContentBlocks,
		textassets.PromptHeaderDictRules,
	} {
		value, loadErr := loadPromptAsset(assetID)
		if loadErr != nil {
			return "", "", loadErr
		}
		if position > 0 {
			systemBuilder.WriteString("\n\n")
		}
		systemBuilder.WriteString(value)
	}

	system = systemBuilder.String()

	name := strings.TrimSpace(
		input.ProjectName,
	)
	if name == "" {
		name, err = textassets.Message(
			textassets.ActiveLocale(),
			"prompt.unnamed_project",
		)
		if err != nil {
			return "", "", err
		}
	}

	total := input.TotalFiles
	if total < 0 {
		total = 0
	}

	hasCurrentHeader := strings.TrimSpace(
		input.CurrentHeader,
	) != ""

	currentHeader := ""
	if hasCurrentHeader {
		currentHeader = ensurePromptTrailingNewline(
			input.CurrentHeader,
		)
	}

	user, err = textassets.Render(
		textassets.ActiveLocale(),
		textassets.PromptHeaderUser,
		headerUserTemplateData{
			ProjectName:      name,
			RepoRootSlash:    input.RepoRootSlash,
			HasCurrentHeader: hasCurrentHeader,
			CurrentHeader:    currentHeader,
			TotalFiles:       total,
			Dirs:             input.Dirs,
			Exts:             input.Exts,
			SampleFiles:      input.SampleFiles,
		},
	)
	if err != nil {
		return "", "", err
	}

	return system, user, nil
}
