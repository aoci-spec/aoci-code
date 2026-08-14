// 服务二进制身份自检。
//
// 替换磁盘字节不会改变正在运行的进程: 升级后旧进程会继续用旧规则服务, 直到宿主
// 重启它。收据里的 mcp_service_version 能在认知层揭穿这件事, 但工具行为层没有
// 任何信号 —— 宿主与模型都看不见"磁盘上已经躺着一个不同的二进制"。
//
// 这里在服务启动时记录自身可执行文件的低成本身份(大小 + 修改时间), 之后由治理
// 面在响应里以 service_binary_replaced_on_disk 的形式暴露漂移事实。它是纯咨询
// 信号: 不阻塞任何工具、不参与任何身份推导, 只把"该重启了"从人工排查变成机器
// 事实。探测失败一律静默视为未漂移, 自检永远不能成为服务不可用的原因。
package mcptools

import (
	"os"
	"sync"
	"time"
)

type serviceBinaryIdentity struct {
	path    string
	size    int64
	modTime time.Time
}

var serviceBinaryState struct {
	sync.Mutex
	recorded *serviceBinaryIdentity
}

// RecordServiceBinaryIdentity 在服务启动时记录自身二进制的磁盘身份。
// path 为空或不可探测时不记录, 自检保持关闭。
func RecordServiceBinaryIdentity(path string) {
	if path == "" {
		return
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return
	}
	serviceBinaryState.Lock()
	defer serviceBinaryState.Unlock()
	serviceBinaryState.recorded = &serviceBinaryIdentity{path: path, size: info.Size(), modTime: info.ModTime()}
}

// serviceBinaryReplacedOnDisk 报告磁盘上的服务二进制是否已不同于启动时记录的
// 身份。未记录时恒为 false; 文件消失同样视为已替换 —— 磁盘上已经没有正在运行
// 的这份字节了。
func serviceBinaryReplacedOnDisk() bool {
	serviceBinaryState.Lock()
	recorded := serviceBinaryState.recorded
	serviceBinaryState.Unlock()
	if recorded == nil {
		return false
	}
	info, err := os.Stat(recorded.path)
	if err != nil {
		return true
	}
	return info.Size() != recorded.size || !info.ModTime().Equal(recorded.modTime)
}

// resetServiceBinaryIdentityForTest 清空记录, 仅供测试恢复全局状态。
func resetServiceBinaryIdentityForTest() {
	serviceBinaryState.Lock()
	defer serviceBinaryState.Unlock()
	serviceBinaryState.recorded = nil
}
