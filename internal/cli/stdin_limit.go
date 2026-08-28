// 有界 stdin 读取 —— CLI 输入的唯一上界入口
// 索引条目: stdin_limit.go[CG7T]
//
// 动机: hook.go 与 update_entry.go 都用 io.ReadAll(os.Stdin) 无界读取。仓库
// 别处早已是有界的 —— index_agent_stage_protocol.go 与 index_agent_curation_protocol.go
// 都走 LimitReader(max+1) 再比长度。这两条 CLI 通道漏掉了同一个纪律。
//
// max+1 是刻意的: 恰好读到 max+1 字节才能把"正好达到上限"与"超出上限"区分开,
// 少读一字节就分不出来。
package cli

import "io"

// hookInputMaxBytes 刻意小: hook JSON 只携带工具名与路径,而 hook 基础设施
// 必须失败放行,绝不能为一份不可信的编辑器载荷分配缓冲。
const hookInputMaxBytes int64 = 64 << 10

// readLimitedInput 读至多 maxBytes 字节。oversize 为真表示输入超限,
// 此时不返回内容 —— 调用方各自决定是放行还是报错。
func readLimitedInput(reader io.Reader, maxBytes int64) (data []byte, oversize bool, err error) {
	data, err = io.ReadAll(io.LimitReader(reader, maxBytes+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(data)) > maxBytes {
		return nil, true, nil
	}
	return data, false, nil
}
