package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
)

func TestStatusRejectsUnreadableBaseline(t *testing.T) {
	for _, directory := range []bool{false, true} {
		for _, output := range []string{"text", "json", "quiet"} {
			name := output + "/corrupt"
			if directory {
				name = output + "/directory"
			}
			t.Run(name, func(t *testing.T) {
				root := t.TempDir()
				if err := config.Save(root, config.DefaultConfig()); err != nil {
					t.Fatal(err)
				}
				path := filepath.Join(root, ".aoci", "baseline.json")
				if directory {
					if err := os.Mkdir(path, 0o755); err != nil {
						t.Fatal(err)
					}
				} else if err := os.WriteFile(path, []byte("{broken"), 0o644); err != nil {
					t.Fatal(err)
				}
				args := []string{"--repo", root, "status"}
				if output != "text" {
					args = append(args, "--"+output)
				}
				var stdout, stderr bytes.Buffer
				if code := executeCLI(args, &stdout, &stderr); code != ExitInvalid {
					t.Fatalf("unreadable Baseline: exit=%d stdout=%s stderr=%s", code, &stdout, &stderr)
				}
				if output == "json" {
					var payload map[string]any
					if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
						t.Fatalf("expected one JSON error envelope: %v\n%s", err, &stdout)
					}
					if _, ok := payload["baseline_exists"]; ok || payload["message"] == "" {
						t.Fatalf("expected a failure, not an absent Baseline: %s", &stdout)
					}
					if payload["ok"] != false || payload["exit_code"] != float64(ExitInvalid) || payload["error_code"] != "invalid" {
						t.Fatalf("unexpected error envelope: %s", &stdout)
					}
				} else if stdout.Len() != 0 || !strings.Contains(strings.ToLower(stderr.String()), "baseline") {
					t.Fatalf("expected an error on stderr: stdout=%s stderr=%s", &stdout, &stderr)
				}
				if !directory {
					data, err := os.ReadFile(path)
					if err != nil || string(data) != "{broken" {
						t.Fatalf("status changed the corrupt Baseline: %q, %v", data, err)
					}
				}
			})
		}
	}
}

func TestStatusDistinguishesPresentAndMissingBaseline(t *testing.T) {
	for _, exists := range []bool{false, true} {
		name := "missing"
		if exists {
			name = "present"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			if err := config.Save(root, config.DefaultConfig()); err != nil {
				t.Fatal(err)
			}
			if exists {
				if err := baseline.Save(root, baseline.NewBaseline(map[string]baseline.Fingerprint{})); err != nil {
					t.Fatal(err)
				}
			}
			var stdout, stderr bytes.Buffer
			if code := executeCLI([]string{"--repo", root, "status", "--json"}, &stdout, &stderr); code != ExitOK {
				t.Fatalf("status failed: exit=%d stdout=%s stderr=%s", code, &stdout, &stderr)
			}
			var report statusReport
			if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
				t.Fatal(err)
			}
			if report.BaselineExists != exists {
				t.Fatalf("baseline_exists=%v, want %v", report.BaselineExists, exists)
			}
		})
	}
}
