// Fail-closed tests of the Makefile format quality gate.
package safety

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestMakeFmtCheckFailsClosed verifies that any gofmt tool fault cannot be misreported as a pass,
// while locking the existing success and unformatted output under a normal environment.
func TestMakeFmtCheckFailsClosed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fmt-check 使用 POSIX shell，由 Linux CI 覆盖")
	}

	makePath, err := exec.LookPath("make")
	if err != nil {
		t.Skip("当前环境没有 make；格式质量闸由 Linux CI 覆盖")
	}

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatalf("resolve repository root: %v", err)
	}

	tests := []struct {
		name            string
		gofmt           string
		mode            os.FileMode
		wantSuccess     bool
		wantOutput      string
		forbiddenOutput string
	}{
		{
			name:        "formatted tree",
			gofmt:       "#!/bin/sh\nexit 0\n",
			mode:        0o755,
			wantSuccess: true,
			wantOutput:  "fmt-check: 全部文件符合 gofmt",
		},
		{
			name:       "unformatted file",
			gofmt:      "#!/bin/sh\nprintf '%s\\n' internal/example.go\nexit 0\n",
			mode:       0o755,
			wantOutput: "fmt-check: 以下文件未通过 gofmt:\ninternal/example.go",
		},
		{
			name:            "gofmt unavailable",
			forbiddenOutput: "fmt-check: 全部文件符合 gofmt",
		},
		{
			name:            "gofmt cannot execute",
			gofmt:           "#!/bin/sh\nexit 0\n",
			mode:            0o644,
			forbiddenOutput: "fmt-check: 全部文件符合 gofmt",
		},
		{
			name:            "gofmt returns nonzero",
			gofmt:           "#!/bin/sh\nexit 7\n",
			mode:            0o755,
			forbiddenOutput: "fmt-check: 全部文件符合 gofmt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			binDir := t.TempDir()
			if tt.gofmt != "" {
				gofmtPath := filepath.Join(binDir, "gofmt")
				if err := os.WriteFile(gofmtPath, []byte(tt.gofmt), tt.mode); err != nil {
					t.Fatalf("write fake gofmt: %v", err)
				}
			}

			command := exec.Command(
				makePath,
				"--no-print-directory",
				"fmt-check",
				"VERSION=test",
				"COMMIT=test",
				"DATE=test",
				"STATICCHECK=/missing/staticcheck",
				"GOFMT_BIN="+filepath.Join(binDir, "gofmt"),
			)
			command.Dir = repoRoot
			command.Env = append(os.Environ(), "PATH="+binDir)

			output, runErr := command.CombinedOutput()
			text := string(output)
			if tt.wantSuccess && runErr != nil {
				t.Fatalf("fmt-check should pass: %v\n%s", runErr, text)
			}
			if !tt.wantSuccess && runErr == nil {
				t.Fatalf("fmt-check should fail:\n%s", text)
			}
			if tt.wantOutput != "" && !strings.Contains(text, tt.wantOutput) {
				t.Fatalf("fmt-check output missing %q:\n%s", tt.wantOutput, text)
			}
			if tt.forbiddenOutput != "" && strings.Contains(text, tt.forbiddenOutput) {
				t.Fatalf("fmt-check reported a false success:\n%s", text)
			}
		})
	}
}
