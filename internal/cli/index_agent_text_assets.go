package cli

import (
	"github.com/aoci-spec/aoci-code/textassets"
)

// renderGuideText和renderGuideLines是Guide领域构造器的无panic读取桥。
// buildAgentGuide已预检当前分支的精确资源集合；内部测试若绕过入口，读取失败
// 也只生成带ID的诊断，不会panic或回退到源码副本。
func renderGuideText(locale string, id textassets.ID, data any) string {
	value, err := textassets.RenderScalar(locale, id, data)
	if err != nil {
		return cliMessage("guide.asset_load_failed", id, localeSafeCLIDetail(err.Error()))
	}

	return value
}

func renderGuideLines(locale string, id textassets.ID, data any) []string {
	value, err := textassets.RenderLines(locale, id, data)
	if err != nil {
		return []string{cliMessage("guide.asset_load_failed", id, localeSafeCLIDetail(err.Error()))}
	}

	return value
}
