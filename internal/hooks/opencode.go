// OpenCode V1 project-level MCP integration (opencode.json).
//
// This adapter intentionally supports only the stable V1 shape:
//
//	mcp.<server> = { type, command, enabled }
//
// OpenCode V2 uses mcp.servers.<server>. Mixing both shapes in one document is
// ambiguous, so V2, JSONC, and any pre-existing non-exact mcp.aoci definition
// fail closed without changing the target file.
package hooks

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"

	afs "github.com/aoci-spec/aoci-code/internal/fs"
	"github.com/aoci-spec/aoci-code/internal/jsonstrict"
)

const openCodeSchemaURL = "https://opencode.ai/config.json"

// openCodeMCPPlan is the read-only result shared by init preflight and the
// installer. Output is nil when the exact AOCI server is already configured.
// It is deliberately not a second transaction or persistent state object.
type openCodeMCPPlan struct {
	Path           string
	Output         []byte
	Existing       bool
	Current        bool
	ExpectedSHA256 string
}

func openCodeAOCIServer(binPath, repoRoot string) map[string]any {
	return map[string]any{
		"type": "local",
		"command": []any{
			toSlash(binPath),
			"--repo",
			toSlash(repoRoot),
			"mcp",
		},
		"enabled": true,
	}
}

func openCodeRuntimePaths(root string) (string, string, error) {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return "", "", err
	}
	binPath, err := os.Executable()
	if err != nil {
		return "", "", err
	}
	absoluteBin, err := filepath.Abs(binPath)
	if err != nil {
		return "", "", err
	}
	return absoluteRoot, absoluteBin, nil
}

func decodeOpenCodeObject(raw []byte) (map[string]any, error) {
	if err := jsonstrict.RejectDuplicateKeys(raw); err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return nil, errors.New("trailing JSON value")
		}
		return nil, err
	}
	object, ok := value.(map[string]any)
	if !ok {
		return nil, errors.New("root must be a JSON object")
	}
	return object, nil
}

// prepareOpenCodeMCP performs the complete read-only compatibility check and
// renders the deterministic V1 postimage. Callers may discard the plan when
// they need preflight only.
func prepareOpenCodeMCP(root string) (openCodeMCPPlan, error) {
	path := filepath.Join(root, "opencode.json")
	for _, alternate := range []string{
		filepath.Join(root, "opencode.jsonc"),
		filepath.Join(root, ".opencode", "opencode.json"),
		filepath.Join(root, ".opencode", "opencode.jsonc"),
	} {
		if _, err := os.Lstat(alternate); err == nil {
			return openCodeMCPPlan{}, errors.New(hookMessage("hook.opencode_alternate_unsupported", toSlash(alternate)))
		} else if !os.IsNotExist(err) {
			return openCodeMCPPlan{}, err
		}
	}

	absoluteRoot, absoluteBin, err := openCodeRuntimePaths(root)
	if err != nil {
		return openCodeMCPPlan{}, err
	}
	desired := openCodeAOCIServer(absoluteBin, absoluteRoot)

	document := map[string]any{
		"$schema": openCodeSchemaURL,
		"mcp":     map[string]any{"aoci": desired},
	}
	plan := openCodeMCPPlan{Path: path}
	info, statErr := os.Lstat(path)
	if statErr == nil && (info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular()) {
		return openCodeMCPPlan{}, errors.New(hookMessage("hook.opencode_shape_unsupported", "unsafe_target_type"))
	}
	if statErr != nil && !os.IsNotExist(statErr) {
		return openCodeMCPPlan{}, statErr
	}
	raw, readErr := os.ReadFile(path)
	if readErr == nil {
		plan.Existing = true
		digest := sha256.Sum256(raw)
		plan.ExpectedSHA256 = hex.EncodeToString(digest[:])
		document, err = decodeOpenCodeObject(raw)
		if err != nil {
			var duplicate *jsonstrict.DuplicateKeyError
			if errors.As(err, &duplicate) {
				return openCodeMCPPlan{}, errors.New(hookMessage("hook.opencode_duplicate", duplicate.Path))
			}
			return openCodeMCPPlan{}, errors.New(hookMessage("hook.opencode_invalid", err))
		}
		if _, ambiguous := document["mcpServers"]; ambiguous {
			return openCodeMCPPlan{}, errors.New(hookMessage("hook.opencode_shape_unsupported", "mcpServers"))
		}
		if schema, exists := document["$schema"]; exists {
			if schemaText, ok := schema.(string); !ok || schemaText != openCodeSchemaURL {
				return openCodeMCPPlan{}, errors.New(hookMessage("hook.opencode_shape_unsupported", "$schema"))
			}
		} else {
			document["$schema"] = openCodeSchemaURL
		}

		mcpValue, exists := document["mcp"]
		if !exists {
			mcpValue = map[string]any{}
			document["mcp"] = mcpValue
		}
		mcp, ok := mcpValue.(map[string]any)
		if !ok {
			return openCodeMCPPlan{}, errors.New(hookMessage("hook.opencode_shape_unsupported", "mcp"))
		}
		if _, v2 := mcp["servers"]; v2 {
			return openCodeMCPPlan{}, errors.New(hookMessage("hook.opencode_v2_unsupported"))
		}
		if current, exists := mcp["aoci"]; exists {
			if !reflect.DeepEqual(current, desired) {
				return openCodeMCPPlan{}, errors.New(hookMessage("hook.opencode_conflict"))
			}
			plan.Current = true
			return plan, nil
		}
		mcp["aoci"] = desired
	} else if !os.IsNotExist(readErr) {
		return openCodeMCPPlan{}, readErr
	}

	output, err := json.MarshalIndent(document, "", "  ")
	if err != nil {
		return openCodeMCPPlan{}, err
	}
	plan.Output = append(output, '\n')
	return plan, nil
}

// ValidateOpenCodeMCPInstall is the zero-write init preflight.
func ValidateOpenCodeMCPInstall(root string) error {
	_, err := prepareOpenCodeMCP(root)
	return err
}

// InstallOpenCodeMCP creates or strictly merges the project-level OpenCode V1
// configuration. Existing unrelated top-level keys and V1 servers survive the
// semantic merge; incompatible shapes are never overwritten.
func InstallOpenCodeMCP(root string) (string, error) {
	plan, err := prepareOpenCodeMCP(root)
	if err != nil {
		return "", err
	}
	if plan.Current {
		return hookMessage("hook.opencode_current") + "\n" + hookMessage("hook.opencode_host_load"), nil
	}
	if plan.Existing {
		if err := BackupThenWriteCAS(plan.Path, plan.Output, plan.ExpectedSHA256); err != nil {
			return "", err
		}
	} else {
		if err := afs.AtomicCreateCASMode(plan.Path, plan.Output, 0o644); err != nil {
			return "", err
		}
	}
	key := "hook.opencode_created"
	if plan.Existing {
		key = "hook.opencode_merged"
	}
	return hookMessage(key) + "\n" + hookMessage("hook.opencode_host_load"), nil
}
