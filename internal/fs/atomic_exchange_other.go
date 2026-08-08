//go:build !linux && !darwin && !windows

// 其他平台缺少标准库可用的原子路径交换原语；安全方向是明确不可用，绝不退回
// 到“先比较再rename”的伪CAS。
package fs

import "fmt"

func exchangeAtomicPaths(_, _ string) error {
	return fmt.Errorf("当前平台不支持原子路径交换")
}

func normalizeAtomicExchangeArtifacts(string) error { return nil }
