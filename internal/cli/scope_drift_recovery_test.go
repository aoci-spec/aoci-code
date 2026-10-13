package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/config"
)

// An out-of-band formal Volume edit is not a policy change. The refusal must
// retain its guard and explain recovery rather than send the caller to a
// Legacy Baseline refresh that cannot run under Managed Scope.
func TestScopeFormalVolumeDriftExplainsRecoveryWithoutWrites(t *testing.T) {
	for _, locale := range []string{"en-US", "zh-CN"} {
		for _, jsonMode := range []bool{false, true} {
			t.Run(locale+map[bool]string{false: "/text", true: "/json"}[jsonMode], func(t *testing.T) {
				root := scopeActivationFixture(t)
				cfg, err := config.LoadBase(root)
				if err != nil {
					t.Fatal(err)
				}
				cfg.Locale = locale
				if err := config.Save(root, cfg); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(root, "aoci.code.txt")
				original, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, append(original, []byte("\n# synthetic out-of-band edit\n")...), 0o644); err != nil {
					t.Fatal(err)
				}
				before := activationFiles(t, root, "aoci.txt", "aoci.meta.txt", "aoci.code.txt", ".aoci/config.json", ".aoci/baseline.json")
				for _, command := range [][]string{{"scope", "activate"}, {"baseline", "scope", "plan"}} {
					args := []string{"--repo", root}
					if jsonMode {
						args = append(args, "--json")
					}
					var out, errs bytes.Buffer
					code := executeCLI(append(args, command...), &out, &errs)
					if code != ExitInvalid {
						t.Fatalf("%v: expected guarded refusal, code=%d out=%s err=%s", command, code, &out, &errs)
					}
					message := errs.String()
					if jsonMode {
						var envelope struct {
							Code    string `json:"error_code"`
							Message string `json:"message"`
						}
						if err := json.Unmarshal(out.Bytes(), &envelope); err != nil {
							t.Fatalf("invalid error envelope: %v: %s", err, &out)
						}
						wantCode := "managed_scope_invalid"
						if command[0] == "baseline" {
							wantCode = "baseline_scope_invalid"
						}
						if envelope.Code != wantCode {
							t.Fatalf("machine code changed: %+v", envelope)
						}
						message = envelope.Message
					}
					if !strings.Contains(message, "docs/troubleshooting.md#formal-volume-baseline-drift") {
						t.Fatalf("%v lacks the recovery route: %s", command, message)
					}
					if command[0] == "scope" && !strings.Contains(message, "managed_scope_formal_volume_baseline_drift: aoci.code.txt") {
						t.Fatalf("lost the original drift diagnostic: %s", message)
					}
					assertActivationFiles(t, root, before)
				}
			})
		}
	}
}

func TestScopeUnrelatedFailureDoesNotSuggestVolumeRecovery(t *testing.T) {
	err := managedScopeExitError(errors.New("managed_scope_index_source_stale: main.go"))
	if strings.Contains(err.Error(), "formal-volume-baseline-drift") {
		t.Fatalf("source drift is not formal Volume drift: %v", err)
	}
}
