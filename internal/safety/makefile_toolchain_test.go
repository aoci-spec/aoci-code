// Boundary tests of the base toolchain precheck.
//
// The precheck translates "the base Go is behind go.mod" from an error disguised as a supply-chain fault into plain words. Its
// judgement must be >= rather than ==: a newer base satisfies every GOTOOLCHAIN=local pin by itself,
// and blocking it would lock out a whole batch of contributors. These boundaries were previously verified by hand with a stub toolchain
// in a single session, leaving no automated evidence.
package safety

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// stubToolchain writes an executable that answers only "go version".
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
		// The version go.mod declares must itself pass, or no base could ever pass.
		{"exact", required, true},
		// A newer base satisfies the rule Go enforces at every pin; blocking it is a pure false report.
		{"newer patch", bumpPatch(t, required), true},
		{"newer minor", bumpMinor(t, required), true},
		// An older base is exactly the one the precheck must block.
		{"older patch", dropPatch(t, required), false},
		// A bare minor version is read as .0, so it is below any requirement with a patch number.
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
			// The refusal must name both versions, or the operator still has to find out which side is behind.
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
	// A GO_BIN that cannot run must never be read as "the version is fine", which would silently disable the gate.
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
