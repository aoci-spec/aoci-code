package authoringcontract

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/internal/cognition"
	"github.com/aoci-spec/aoci-code/internal/index"
	"github.com/aoci-spec/aoci-code/internal/machinecontract"
	"github.com/aoci-spec/aoci-code/textassets"
)

func TestOldFormalMetaBuildsCompleteCodeAndDatabaseContractsWithoutByteChanges(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "volumes", "compat-52bc4af", "aoci.meta.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "Entry example") || strings.Contains(string(raw), "#S quota:") {
		t.Fatal("compatibility fixture unexpectedly contains the current authoring additions")
	}
	for _, locale := range []string{textassets.DefaultLocale, textassets.LegacyLocale} {
		t.Run(locale, func(t *testing.T) {
			output, buildErr := Build(raw, []string{cognition.ScopeDatabase, cognition.ScopeCode}, locale)
			if buildErr != nil {
				t.Fatal(buildErr)
			}
			if output.AuthoringMeta != string(raw) {
				t.Fatal("assembler changed the old formal Meta bytes")
			}
			assertDeliveredExamples(t, raw, output)
			codeIndex, databaseIndex := instructionIndex(output.Instructions, output.Examples[cognition.ScopeCode]), instructionIndex(output.Instructions, output.Examples[cognition.ScopeDatabase])
			if codeIndex < 0 || databaseIndex <= codeIndex {
				t.Fatalf("combined contract order is not Code then Database: code=%d database=%d instructions=%#v", codeIndex, databaseIndex, output.Instructions)
			}
		})
	}
}

func TestFreshMetaContractExamplesComeFromActualAssembly(t *testing.T) {
	for _, locale := range []string{textassets.DefaultLocale, textassets.LegacyLocale} {
		locale := locale
		t.Run(locale, func(t *testing.T) {
			template, err := textassets.Render(locale, textassets.TemplateVolumeMeta, machinecontract.NumericText())
			if err != nil {
				t.Fatal(err)
			}
			output, err := Build([]byte(template), []string{cognition.ScopeCode, cognition.ScopeDatabase}, locale)
			if err != nil {
				t.Fatal(err)
			}
			if output.AuthoringMeta != template {
				t.Fatal("assembler changed the fresh formal Meta bytes")
			}
			assertFormalMetaHighImportanceDashExample(t, template)
			assertSoftSAuthoringPolicy(t, locale, output.Instructions)
			assertDeliveredExamples(t, []byte(template), output)
		})
	}
}

func assertSoftSAuthoringPolicy(t *testing.T, locale string, instructions []string) {
	t.Helper()
	combined := strings.Join(instructions, "\n")
	required := map[string][]string{
		textassets.DefaultLocale: {
			"C6-C9 objects",
			"actively look for evidence-backed S constraints",
			"cannot be inferred from F/R/A",
			"affects system understanding or modification",
			"Keep S:- when no qualifying constraint exists",
		},
		textassets.LegacyLocale: {
			"C6-C9对象应优先识别有证据支持的S约束",
			"无法由F/R/A推导",
			"影响系统理解或修改",
			"不存在合格约束时保持S:-",
		},
	}
	for _, anchor := range required[locale] {
		if !strings.Contains(combined, anchor) {
			t.Fatalf("%s authoring instructions lack soft-S policy anchor %q: %q", locale, anchor, combined)
		}
	}
	if count := strings.Count(combined, "C6-C9"); count != 2 {
		t.Fatalf("%s authoring instructions must carry the soft-S policy for both Code and Database, got %d copies", locale, count)
	}
}

func assertFormalMetaHighImportanceDashExample(t *testing.T, meta string) {
	t.Helper()
	const prefix = "#Code Entry example: "
	for _, line := range strings.Split(meta, "\n") {
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		entry, ok := index.ParseEntryLine(strings.TrimPrefix(line, prefix), 1)
		if !ok {
			t.Fatalf("formal Meta example is not a valid Entry: %q", line)
		}
		importance, err := strconv.Atoi(entry.TagsParsed["C"])
		if err != nil || importance < 6 || entry.S != "-" {
			t.Fatalf("formal Meta must retain its compatible high-C S:- example: %#v", entry)
		}
		return
	}
	t.Fatal("formal Meta lacks its Code Entry example")
}

func assertDeliveredExamples(t *testing.T, raw []byte, output Output) {
	t.Helper()
	for _, domain := range []string{cognition.ScopeCode, cognition.ScopeDatabase} {
		example := output.Examples[domain]
		if example == "" || instructionIndex(output.Instructions, example) < 0 {
			t.Fatalf("%s example was not delivered through actual instructions: %#v", domain, output)
		}
		entry, ok := index.ParseEntryLine(example, 1)
		if !ok || entry.FullLine != example {
			t.Fatalf("%s example is not one complete physical Entry: %q", domain, example)
		}
		importance, err := strconv.Atoi(entry.TagsParsed["C"])
		if err != nil || importance < 6 {
			t.Fatalf("%s example must calibrate a high-C object: %q", domain, example)
		}
		if strings.TrimSpace(entry.S) == "" || entry.S == "-" {
			t.Fatalf("%s example must demonstrate a qualifying S constraint: %q", domain, example)
		}
		if entry.R != "-" {
			t.Fatalf("%s calibration example must remain portable across repositories and use R:-: %q", domain, example)
		}
		dictionary := index.ExtractScopedTagDict(string(raw), domain)
		if findings := cognition.ValidateVolumeAuthoringExample(domain, example, dictionary); len(findings) != 0 {
			t.Fatalf("%s delivered example failed formal validation: %#v", domain, findings)
		}
	}
}

func instructionIndex(instructions []string, wanted string) int {
	for index, instruction := range instructions {
		if instruction == wanted {
			return index
		}
	}
	return -1
}
