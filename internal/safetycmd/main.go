package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/aoci-spec/aoci-code/internal/safety"
)

func main() {
	files, err := safety.PublicTextFiles(".", os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "safety: 枚举扫描文件失败: %v\n", err)
		os.Exit(2)
	}
	if len(files) == 0 {
		fmt.Println("safety: 扫描集为空，跳过")
		return
	}

	failed := false
	for _, candidate := range files {
		data, readErr := os.ReadFile(candidate)
		if readErr != nil {
			fmt.Fprintf(os.Stderr, "safety: 读取%s失败: %v\n", candidate, readErr)
			failed = true
			continue
		}
		lines := strings.Split(string(data), "\n")
		for _, hit := range safety.CheckForbiddenClaims(string(data)) {
			text := ""
			if hit.Line > 0 && hit.Line <= len(lines) {
				text = lines[hit.Line-1]
			}
			fmt.Printf("命中 %s [%s]: %d:%s\n", candidate, hit.Term, hit.Line, text)
			failed = true
		}
	}

	if failed {
		fmt.Println("safety: 存在禁区词或过度主张，词表见internal/machinecontract/lexical.go")
		os.Exit(1)
	}

	fmt.Printf("safety: 全部干净（扫描 %d 个文件）\n", len(files))
}
