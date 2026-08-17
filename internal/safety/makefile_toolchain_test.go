// 底座工具链预检的边界测试。
//
// 预检把"底座 Go 落后于 go.mod"从一句伪装成供应链故障的错误翻译成人话。它的
// 判定必须是 >= 而不是 ==：更新的底座本来就满足每个 GOTOOLCHAIN=local 钉点,
// 拦住它会把整批贡献者挡在门外。这些边界此前只在一次会话里用桩工具链手工验过,
// 没有留下任何自动化证据。
package safety

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// stubToolchain 写出一个只回答 "go version" 的可执行文件。
func stubToolchain(t *testing.T, dir, version string) string {
	t.Helper()
	path := filepath.Join(dir, "go")
	script := "#!/bin/sh\n[ \"$1\" = version ] && echo \"go version go" + version +
		" linux/amd64\" || exit 3\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestToolchainPreflightAdmitsEveryToolchainGoRequires(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("预检是 POSIX shell 配方，由 Linux CI 覆盖")
	}
	makePath, err := exec.LookPath("make")
	if err != nil {
		t.Skip("当前环境没有 make；预检由 Linux CI 覆盖")
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	required := goDirective(t, filepath.Join(repoRoot, "go.mod"))

	for _, test := range []struct {
		name    string
		version string
		admit   bool
	}{
		// go.mod 声明的版本本身必须放行，否则没有任何底座能通过。
		{"exact", required, true},
		// 更新的底座满足 Go 在每个钉点执行的规则，拦住它是纯粹的误报。
		{"newer patch", bumpPatch(t, required), true},
		{"newer minor", bumpMinor(t, required), true},
		// 落后的底座正是预检要拦的那一个。
		{"older patch", dropPatch(t, required), false},
		// 裸次版本按 .0 处理，因此低于任何带补丁号的要求。
		{"bare minor", bareMinor(t, required), false},
	} {
		t.Run(test.name, func(t *testing.T) {
			binary := stubToolchain(t, t.TempDir(), test.version)
			command := exec.Command(makePath, "GO_BIN="+binary, "toolchain-preflight")
			command.Dir = repoRoot
			output, runErr := command.CombinedOutput()
			admitted := runErr == nil
			if admitted != test.admit {
				t.Fatalf("base %s admitted=%v, want %v: %s", test.version, admitted, test.admit, output)
			}
			if test.admit {
				return
			}
			// 拒绝必须同时点名两个版本，否则操作者还得自己去查是哪一边落后。
			for _, wanted := range []string{test.version, required} {
				if !strings.Contains(string(output), wanted) {
					t.Fatalf("拒绝未点名 %s: %s", wanted, output)
				}
			}
		})
	}
}

func TestToolchainPreflightRefusesAnUnusableGoBinary(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("预检是 POSIX shell 配方，由 Linux CI 覆盖")
	}
	makePath, err := exec.LookPath("make")
	if err != nil {
		t.Skip("当前环境没有 make；预检由 Linux CI 覆盖")
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	broken := filepath.Join(t.TempDir(), "go")
	if err := os.WriteFile(broken, []byte("#!/bin/sh\nexit 3\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	command := exec.Command(makePath, "GO_BIN="+broken, "toolchain-preflight")
	command.Dir = repoRoot
	// 一个跑不起来的 GO_BIN 绝不能被读成"版本没问题"，那会让门禁静默失效。
	if output, runErr := command.CombinedOutput(); runErr == nil {
		t.Fatalf("预检把无法执行的 GO_BIN 当成通过: %s", output)
	}
}

func goDirective(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[0] == "go" {
			return fields[1]
		}
	}
	t.Fatal("go.mod 没有 go 指令")
	return ""
}

func splitVersion(t *testing.T, version string) []string {
	t.Helper()
	parts := strings.Split(version, ".")
	if len(parts) < 3 {
		t.Skipf("go 指令 %q 没有补丁号，边界用例不适用", version)
	}
	return parts
}

func bumpPatch(t *testing.T, version string) string {
	parts := splitVersion(t, version)
	return parts[0] + "." + parts[1] + "." + incr(t, parts[2])
}

func bumpMinor(t *testing.T, version string) string {
	parts := splitVersion(t, version)
	return parts[0] + "." + incr(t, parts[1]) + ".0"
}

func dropPatch(t *testing.T, version string) string {
	parts := splitVersion(t, version)
	return parts[0] + "." + parts[1] + "." + decr(t, parts[2])
}

func bareMinor(t *testing.T, version string) string {
	parts := splitVersion(t, version)
	return parts[0] + "." + parts[1]
}

func incr(t *testing.T, value string) string {
	t.Helper()
	number := 0
	for _, char := range value {
		if char < '0' || char > '9' {
			t.Skipf("版本分量 %q 不是纯数字", value)
		}
		number = number*10 + int(char-'0')
	}
	return itoa(number + 1)
}

func decr(t *testing.T, value string) string {
	t.Helper()
	number := 0
	for _, char := range value {
		if char < '0' || char > '9' {
			t.Skipf("版本分量 %q 不是纯数字", value)
		}
		number = number*10 + int(char-'0')
	}
	if number == 0 {
		t.Skip("补丁号为 0，无法构造更旧的底座")
	}
	return itoa(number - 1)
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := []byte{}
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}
