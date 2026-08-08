// 头部只读工具(v2.8 P2): 不读全文件即可获取索引头部(规范+标签字典)。
// 索引条目: tools_header.go(待补录,随本批入册)
//
// 定位: agent 需要字典时(起草标签/理解分层)此前只能读整个 aoci.txt,
// 大索引下浪费 token;本工具只返回头部区块(首个===段头之前),与
// index.ExtractHeader 同一边界定义(解析侧与编辑侧同指一段文本的
// 第三个消费方,绝不自写第二套边界判定)。
//
// source 为入参(MCP handler 传 agent,CLI 传 human)—— 初版曾用包级变量
// 承载缺省来源,并发不安全且账目会错标,双端接入即改为入参。
package mcptools

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/ledger"
	"github.com/aoci-spec/aoci-code/textassets"
)

// BuildHeaderText 返回索引头部原文(纯读,MCP 与 CLI 双端共用)。
// 头部为空(零#行)时返回空串由调用方给出提示文案;source 进 ledger 来源分级。
func BuildHeaderText(root, source string) (string, *Fail) {
	loaded, fail := loadCognitionCtx(root)
	if fail != nil {
		return "", fail
	}
	if deliveryFail := pendingCognitionDeliveryFail(root, loaded.set); deliveryFail != nil {
		return "", deliveryFail
	}
	if loaded.set.LayoutMode == cognition.LayoutVolumesV1 {
		ledger.Append(root, loaded.cfg.LedgerEnabled, ledger.Event{Op: "header_show", Source: source})
		return mcpMessage("mcp.volume.header_identity", loaded.set.Root.SHA256, loaded.set.Meta.SHA256) + string(loaded.set.Meta.Raw), nil
	}
	header, _ := index.ExtractHeader(string(loaded.set.Root.Raw))
	ledger.Append(root, loaded.cfg.LedgerEnabled, ledger.Event{Op: "header_show", Source: source})
	return header, nil
}

// registerHeaderTool 注册 aoci_header(挂载点在 server.go 的 RunStdio)。
// 零参数工具: In 取 any 令 go-sdk 生成空对象 schema。
func registerHeaderTool(
	srv *mcp.Server,
	root string,
	descriptions mcpToolDescriptions,
) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "aoci_header",
		Description: descriptions[textassets.ContractMCPHeaderDescription],
	}, func(ctx context.Context, req *mcp.CallToolRequest, in any) (*mcp.CallToolResult, any, error) {
		res := guard(func() *mcp.CallToolResult {
			header, fail := BuildHeaderText(root, "agent")
			if fail != nil {
				return failResult(fail)
			}
			if header == "" {
				if err := validateMCPContracts(textassets.ContractMCPHeaderEmptyMessage); err != nil {
					return errResult(errInternal, err.Error(), mcpMessage("mcp.asset.retry_hint"))
				}
				return textResult(mcpContract(textassets.ContractMCPHeaderEmptyMessage))
			}
			return textResult(header)
		})
		return res, nil, nil
	})
}
