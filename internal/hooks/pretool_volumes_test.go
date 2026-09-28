package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/baseline"
	"github.com/aoci-spec/aoci-code/internal/config"
)

const volumesHookRoot = "#AOCI-ROOT-MANIFEST: 1\n#Format-Version: cognition-volumes/v1\n#Locale: en-US\n#Project: hook-fixture\n#Global-Invariants: -\n" +
	"#Volume: id=meta kind=meta path=aoci.meta.txt format=meta-v1 depends=- state=enabled\n" +
	"#Volume: id=code kind=code path=aoci.code.txt format=object-fras-v2 depends=meta state=enabled\n"

const volumesHookMeta = "#AOCI-META-VOLUME: 1\n#Object-Protocol: repository-cognition-object/v2\n#FRAS-Discipline: 2\n" +
	"#FRAS-v2-Limits-Authority: machine-contract\n#S-Admission: non-inferable-and-error-preventing\n" +
	"#S quota: C9-8≤600 C7-4≤200 C3-1≤50\n#Object-Kinds: code=file database=table\n" +
	"#Canonical-Tag-Authoring: compact A+B+C+[D]+E; dotted form is read compatibility only\n" +
	"#Code canonical identity example: code:path/to/file.go\n" +
	"#Code Entry example: file.go[CG7T]: F:Runs the example application | R:- | A:- | S:-\n" +
	"#[Tag dictionary: code]\n#A Layer: C-Code\n#B Module: G-General\n" +
	"#C Importance: 9-core 8-high-frequency 7-business 5-routine 3-supporting 1-edge\n" +
	"#E Scale: L-large>400 M-medium200-400 S-small100-200 T-tiny<100\n" +
	"#[Tag dictionary: database]\n#A Layer: D-Database\n#B Module: S-Schema\n" +
	"#C Importance: 9-core 8-high-frequency 7-business 5-routine 3-supporting 1-edge\n" +
	"#E Scale: L-large>400 M-medium200-400 S-small100-200 T-tiny<100\n"

// buildVolumesHookRepo is a Volumes v1 repository with one indexed source under
// src/, written the way init and a first authoring batch leave it.
func buildVolumesHookRepo(t *testing.T, strict bool) string {
	t.Helper()
	root := t.TempDir()
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(root, "src"), 0o755))
	must(os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("package src\n"), 0o644))
	must(os.WriteFile(filepath.Join(root, "aoci.txt"), []byte(volumesHookRoot), 0o644))
	must(os.WriteFile(filepath.Join(root, "aoci.meta.txt"), []byte(volumesHookMeta), 0o644))
	code := "#AOCI-CODE-VOLUME: 1\n===" + filepath.ToSlash(root) + "/src/===\n" +
		"a.go[CG5T]: F:Provides the fixture source | R:- | A:- | S:Volumes hook constraint\n"
	must(os.WriteFile(filepath.Join(root, "aoci.code.txt"), []byte(code), 0o644))
	cfg := config.DefaultConfig()
	cfg.HookStrict = strict
	must(config.Save(root, cfg))
	snap, _, err := baseline.Snapshot(root, cfg.WalkOptions())
	must(err)
	must(baseline.Save(root, baseline.NewBaseline(snap)))
	return root
}

// Until v0.1.0-rc15 the hook parsed aoci.txt as a Legacy index; under Volumes
// v1 that is a Root manifest with no sections, so an indexed file produced no
// text at all (#85). The Entry lives in the Code Volume.
func TestPretoolVolumesInjectsTheCodeVolumeEntry(t *testing.T) {
	root := buildVolumesHookRepo(t, false)
	res := HandlePreTool(root, "Edit", "src/a.go")
	if res.Block || !strings.Contains(res.Text, "Volumes hook constraint") || !strings.Contains(res.Text, "a.go[CG5T]") {
		t.Fatalf("Volumes v1 hook must inject the Code Volume Entry: %+v", res)
	}
	// A source without an Entry still gets the enrolment reminder.
	if err := os.WriteFile(filepath.Join(root, "src", "n.go"), []byte("package src\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res = HandlePreTool(root, "Write", "src/n.go")
	if res.Block || !strings.Contains(res.Text, "src/n.go") {
		t.Fatalf("unindexed Volumes source must get the reminder: %+v", res)
	}
}

func TestPretoolVolumesStaleAndStrictKeepTheirGuards(t *testing.T) {
	root := buildVolumesHookRepo(t, false)
	if err := os.WriteFile(filepath.Join(root, "src", "a.go"), []byte("package src\n\nvar changed = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := HandlePreTool(root, "Edit", "src/a.go")
	if res.Block || !strings.Contains(res.Text, "STALE") {
		t.Fatalf("stale Volumes Entry must warn without blocking by default: %+v", res)
	}
	strict := buildVolumesHookRepo(t, true)
	if err := os.WriteFile(filepath.Join(strict, "src", "a.go"), []byte("package src\n\nvar changed = true\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if res := HandlePreTool(strict, "Edit", "src/a.go"); !res.Block {
		t.Fatalf("strict mode must block a stale Volumes Entry: %+v", res)
	}
}

// A damaged Code Volume is a load failure, and the hook is not a gate: it
// must stay silent and fail open rather than block or crash the host.
func TestPretoolVolumesDamagedCodeVolumeFailsOpen(t *testing.T) {
	root := buildVolumesHookRepo(t, true)
	if err := os.WriteFile(filepath.Join(root, "aoci.code.txt"), []byte("#AOCI-CODE-VOLUME: 1\nnot an entry line\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res := HandlePreTool(root, "Edit", "src/a.go")
	if res.Block {
		t.Fatalf("damaged Volume must not block: %+v", res)
	}
}
