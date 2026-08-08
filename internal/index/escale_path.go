// E规模参与路径判据。
//
// E规模描述静态源码或文本实体的实现规模，不描述AOCI治理资产自身的长度。
//
// 两类AOCI治理资产不参与E规模判定:
//
//   - 仓库根.aoci目录内的Ledger、Draft、锁、配置和审计资产；
//   - 仓库根aoci.txt索引本体。
//
// .aoci资产行数反映治理历史长度；aoci.txt行数会随条目数量自然增长。
// 若索引本体参与E档位检查，会形成“更新索引→索引跨档→再次更新索引”的
// 自指循环，因此必须由共享路径判据统一排除。
//
// 本文件是路径参与判据的唯一事实源。Score、单条回写、原子批量回写和
// Entries Check不得各自复制“.aoci/”或“aoci.txt”判断。
package index

import (
	"path/filepath"
	"strings"
)

// ShouldCheckEScalePath判断仓库相对路径是否应参与E规模校验。
//
// 输入通常已经过安全相对路径归一；本函数仍同时兼容正反斜杠和若干层“./”，
// 以保证Linux进程处理Windows形态字符串时同判。
//
// 返回false:
//   - 空路径或当前目录；
//   - 仓库根.aoci目录本身及其任意子路径；
//   - 仓库根aoci.txt索引本体。
//
// 返回true:
//   - .aoci2等同名前缀目录；
//   - nested/.aoci等非根部同名目录；
//   - nested/aoci.txt等非根部同名文件；
//   - aoci.txt.backup等相似名称；
//   - 普通仓库文件。
func ShouldCheckEScalePath(
	rel string,
) bool {
	normalized := normalizeEScalePath(
		rel,
	)

	if normalized == "" ||
		normalized == "." {
		return false
	}

	if normalized == "aoci.txt" {
		return false
	}

	return normalized != ".aoci" &&
		!strings.HasPrefix(
			normalized,
			".aoci/",
		)
}

// normalizeEScalePath把路径归一为仓库相对正斜杠形态。
//
// filepath.ToSlash只转换当前操作系统定义的Separator；Linux下反斜杠是普通
// 字符，因此仍须显式替换，保证Windows形态字符串在Linux测试中同判。
func normalizeEScalePath(
	rel string,
) string {
	normalized := strings.TrimSpace(
		filepath.ToSlash(rel),
	)

	normalized = strings.ReplaceAll(
		normalized,
		"\\",
		"/",
	)

	for strings.HasPrefix(
		normalized,
		"./",
	) {
		normalized = strings.TrimPrefix(
			normalized,
			"./",
		)
	}

	return strings.TrimRight(
		normalized,
		"/",
	)
}
