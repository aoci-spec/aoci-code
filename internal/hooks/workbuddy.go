// WorkBuddy 适配: 机器级用户 MCP 配置(~/.workbuddy/mcp.json)
// 索引条目: workbuddy.go[Hook.WorkBuddy.8.S]
//
// 纪律:
//   - WorkBuddy 只有**机器级**一个 MCP 入口(用户文件 ~/.workbuddy/mcp.json),
//     读面是 UserMcpProvider,写面是同一文件;没有项目级配置面。因此本文件
//     不写仓库内任何文件,initAgentCandidatePaths 对 workbuddy 返回 nil,
//     init 也不会为它写 .gitignore —— 宿主配置在仓库外,无入库问题;
//   - JSON 合并写入,绝不覆盖用户既有 servers;写前 BackupThenWrite 备份;
//   - 幂等判据单一事实源: 判据端 IsWorkBuddyMCPInstalled(status.go)与写入端
//     复用 workbuddyServerMatches —— 写入端与判据端绝不各持一份逻辑副本
//     (status.go 判据失配事故的教训);
//   - 键名退让: 该文件被**所有项目**共享,而 MCP server 硬绑 --repo。已有 aoci
//     键指向别的仓库时绝不覆盖,改用 aoci-<项目名> 新增一条;两个键名都被别的
//     仓库占用时报错交人工,不做任何静默改写;
//   - hook: WorkBuddy 没有写前生命周期 hook 接入面,故不实现 --hooks 分支,
//     `init --hooks --agent workbuddy` 静默忽略而不是假装安装。
package hooks

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// workbuddyServerKey 是默认 server 键名,与其它宿主保持一致。
const workbuddyServerKey = "aoci"

// workbuddyConfigPath 返回 ~/.workbuddy/mcp.json。
// 用户目录不可解析时返回可操作错误,绝不猜路径。
func workbuddyConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return "", errors.New(hookMessage("hook.workbuddy_home_missing"))
	}
	return filepath.Join(home, ".workbuddy", "mcp.json"), nil
}

// workbuddyServerSpec 构造指向当前仓库的 server 定义(正斜杠路径由 TplData 保证)。
func workbuddyServerSpec(data TplData) map[string]any {
	return map[string]any{
		"command": data.BinPath,
		"args":    []string{"--repo", data.RepoRoot, "mcp"},
	}
}

// workbuddyServerMatches 判定某个已解析条目是否就是"当前仓库的 aoci server"。
// 单条判据的唯一实现:写入端选键前用它识别"同仓库已有条目",判据端用它做
// 全量匹配 —— 两侧共用,任一侧改动另一侧自动跟随。
func workbuddyServerMatches(entry any, data TplData) bool {
	server, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	if command, _ := server["command"].(string); command != data.BinPath {
		return false
	}
	rawArgs, ok := server["args"].([]any)
	if !ok || len(rawArgs) != 3 {
		return false
	}
	want := []string{"--repo", data.RepoRoot, "mcp"}
	for i, expected := range want {
		// json 解析出的元素是 any;逐位比较,类型不符即判不匹配。
		value, isString := rawArgs[i].(string)
		if !isString || value != expected {
			return false
		}
	}
	return true
}

// workbuddyKeySuffix 把项目名收敛成可安全用作 server 键名的片段。
func workbuddyKeySuffix(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	suffix := strings.Trim(b.String(), "-")
	if suffix == "" {
		suffix = "repo"
	}
	return suffix
}

// workbuddyTargetKey 选定本次写入使用的键名。
//   - aoci 键不存在 → 用它;
//   - aoci 键已是当前仓库 → 用它(写入端幂等早退由调用方先行处理);
//   - aoci 键绑别的仓库 → aoci-<项目名>;该键空闲就用它,也被别的仓库占用则报错。
func workbuddyTargetKey(servers map[string]any, data TplData) (string, error) {
	existing, taken := servers[workbuddyServerKey]
	if !taken || workbuddyServerMatches(existing, data) {
		return workbuddyServerKey, nil
	}
	scoped := workbuddyServerKey + "-" + workbuddyKeySuffix(data.ProjectName)
	if occupant, used := servers[scoped]; used && !workbuddyServerMatches(occupant, data) {
		return "", errors.New(hookMessage("hook.workbuddy_key_taken", scoped, data.RepoRoot))
	}
	return scoped, nil
}

// readWorkBuddyDocument 读取并解析用户 MCP 整份文件;文件缺失返回空文档。
// 返回整份文档而不是只有 mcpServers —— 写入必须原样保留用户文档里的其它
// 顶层字段(与 claude.go 同一纪律:只动 mcpServers,其余字节不碰)。
// JSON 损坏返回可操作错误,由调用方原样上报,绝不覆盖用户的坏文件。
func readWorkBuddyDocument(path string) (map[string]any, error) {
	doc := map[string]any{}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return doc, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return doc, nil
	}
	if jerr := json.Unmarshal(raw, &doc); jerr != nil {
		return nil, errors.New(hookMessage("hook.workbuddy_mcp_invalid", path, jerr))
	}
	return doc, nil
}

// workbuddyServersOf 取出文档里的 mcpServers 表;缺失或类型不符则新建一张。
func workbuddyServersOf(doc map[string]any) map[string]any {
	if servers, ok := doc["mcpServers"].(map[string]any); ok {
		return servers
	}
	return map[string]any{}
}

// InstallWorkBuddyMCP 合并写入机器级 ~/.workbuddy/mcp.json 的 aoci server。
// 幂等早退复用判据端 IsWorkBuddyMCPInstalled(单一事实源)。
func InstallWorkBuddyMCP(root string) (string, error) {
	path, err := workbuddyConfigPath()
	if err != nil {
		return "", err
	}
	data := NewTplData(root)

	// 幂等早退:当前仓库已在任一 aoci 条目里配置好(判据端同一实现)。
	if IsWorkBuddyMCPInstalled(root) {
		return hookMessage("hook.workbuddy_mcp_current", path), nil
	}

	doc, err := readWorkBuddyDocument(path)
	if err != nil {
		return "", err
	}
	servers := workbuddyServersOf(doc)
	key, err := workbuddyTargetKey(servers, data)
	if err != nil {
		return "", err
	}
	servers[key] = workbuddyServerSpec(data)
	doc["mcpServers"] = servers

	out, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return "", err
	}
	if err := BackupThenWrite(path, append(out, '\n')); err != nil {
		return "", err
	}
	return hookMessage("hook.workbuddy_mcp_written", path, key), nil
}
