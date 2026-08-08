// 文件行数统计(E 档位闸的行数事实源)
// 索引条目待补: countlines.go
//
// 行数语义与 inventory 画像一致: 末行无换行也计 1 行;空文件 0 行。
// 流式统计不整读进内存(任意大文件安全);刻意不做二进制嗅探 —— 调用方
// (E 档位闸)对二进制条目至多产生一条无害警告,为其新增嗅探不值一份副本
// (嗅探逻辑在 indexgen 与 workflow 已有两份,合并归 P2 卫生账,不添第三份)。
package fs

import (
	"bytes"
	"io"
	"os"
)

// CountFileLines 流式统计文件行数。
// 语义: 空文件返回 0;以换行结尾时行数=换行数;末行无换行时再+1。
func CountFileLines(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	buf := make([]byte, 32*1024)
	count := 0
	lastByte := byte('\n') // 哨兵: 空文件不触发末行+1
	empty := true
	for {
		n, rerr := f.Read(buf)
		if n > 0 {
			empty = false
			count += bytes.Count(buf[:n], []byte{'\n'})
			lastByte = buf[n-1]
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return 0, rerr
		}
	}
	if empty {
		return 0, nil
	}
	if lastByte != '\n' {
		count++
	}
	return count, nil
}
