// aoci-code 进程入口
// 索引条目: main.go[Cmd.Root.9.T]
//
// 职责: 只调用 cli.Execute(),零业务逻辑。
// 铁律: 全仓任何日志都不许碰 stdout —— mcp 子命令下 stdout 是 JSON-RPC 协议流,
// 任何 println/日志残留都会毒化协议流导致 MCP client 静默断连。
// Go 标准库 log 默认输出即 stderr,此处显式设置一次,将该默认固化为不可依赖偶然的约定。
package main

import (
	"log"
	"os"

	"github.com/aoci-spec/aoci-code/internal/cli"
)

func main() {
	// 显式固定日志输出到 stderr(见上方铁律说明)
	log.SetOutput(os.Stderr)

	// 全部命令注册、参数解析、退出码映射均在 internal/cli 内完成
	cli.Execute()
}
