package prompt

import (
	"strings"

	"github.com/aoci-spec/aoci-code/textassets"
)

// loadPromptAsset从统一资源目录加载一个模型提示片段，并保持迁移前的尾换行
// 裁剪语义。失败由真正依赖该片段的Prompt路径返回，不缓存也不回退。
func loadPromptAsset(id textassets.ID) (string, error) {
	data, err := textassets.NumericTemplateData(textassets.ActiveLocale())
	if err != nil {
		return "", err
	}
	value, err := textassets.Render(
		textassets.ActiveLocale(),
		id,
		data,
	)
	if err != nil {
		return "", err
	}

	return trimAsset(value), nil
}

// ensurePromptTrailingNewline preserves existing content and adds exactly one
// newline only when the supplied evidence does not already end with one.
func ensurePromptTrailingNewline(
	value string,
) string {
	if strings.HasSuffix(
		value,
		"\n",
	) {
		return value
	}

	return value + "\n"
}
