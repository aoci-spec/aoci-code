package authoringcontract

import (
	"os"
	"path/filepath"
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
		template, err := textassets.Render(locale, textassets.TemplateVolumeMeta, machinecontract.NumericText())
		if err != nil {
			t.Fatal(err)
		}
		output, err := Build([]byte(template), []string{cognition.ScopeCode, cognition.ScopeDatabase}, locale)
		if err != nil {
			t.Fatal(err)
		}
		assertDeliveredExamples(t, []byte(template), output)
	}
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
