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
			assertExampleTag(t, output.Examples[cognition.ScopeCode], "CG7T")
			assertExampleTag(t, output.Examples[cognition.ScopeDatabase], "DS7T")
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
			assertExampleTag(t, output.Examples[cognition.ScopeCode], "EG7T")
			assertExampleTag(t, output.Examples[cognition.ScopeDatabase], "EI7T")
			assertFormalMetaHighImportanceDashExample(t, template)
			assertUnifiedLocaleAuthoringPolicy(t, locale, output.Instructions)
			assertSoftSAuthoringPolicy(t, locale, output.Instructions)
			assertStarterClassificationPolicy(t, locale, output.Instructions)
			assertDeliveredExamples(t, []byte(template), output)
		})
	}
}

func assertUnifiedLocaleAuthoringPolicy(t *testing.T, locale string, instructions []string) {
	t.Helper()
	combined := strings.Join(instructions, "\n")
	required := map[string][]string{
		textassets.DefaultLocale: {"configured project Locale " + locale, "new or genuinely updated Entry", "not translation targets"},
		textassets.LegacyLocale:  {"配置参数指定的项目Locale " + locale, "新建或真实更新Entry", "不会仅因语言不同而成为批量翻译目标"},
	}
	for _, anchor := range required[locale] {
		if !strings.Contains(combined, anchor) {
			t.Fatalf("%s authoring instructions lack prospective Locale policy %q: %q", locale, anchor, combined)
		}
	}
}

func assertStarterClassificationPolicy(t *testing.T, locale string, instructions []string) {
	t.Helper()
	combined := strings.Join(instructions, "\n")
	required := map[string][]string{
		textassets.DefaultLocale: {"official starter dictionary", "genuinely cross-domain", "insufficient evidence is not Z", "never enter S", "Under the official starter dictionary, a test file", "B=Q only for test or quality infrastructure", "Custom formal Meta meanings remain authoritative"},
		textassets.LegacyLocale:  {"官方初始字典", "真正跨域", "证据不足不属于Z", "绝不写入S", "在官方初始字典中，测试文件", "只有测试或质量基础设施本身才使用B=Q", "自定义正式Meta的含义始终具有权威"},
	}
	for _, anchor := range required[locale] {
		if !strings.Contains(combined, anchor) {
			t.Fatalf("%s authoring instructions lack starter classification policy %q: %q", locale, anchor, combined)
		}
	}
}

func TestCustomMetaLetterReuseDoesNotAcquireStarterMeanings(t *testing.T) {
	raw := []byte("#AOCI-META-VOLUME: 1\n" +
		"#Object-Protocol: repository-cognition-object/v2\n" +
		"#FRAS-Discipline: 2\n#FRAS-v2-Limits-Authority: machine-contract\n" +
		"#S-Admission: non-inferable-and-error-preventing\n#Object-Kinds: code=file database=table\n" +
		"#[Tag dictionary: code]\n#A Layer: E-Engine A-Application\n#B Module: G-Gateway B-Business\n" +
		"#C Importance: 7-high\n#E Scale: T-tiny<100\n" +
		"#[Tag dictionary: database]\n#A Layer: E-Event A-Archive\n#B Module: I-Integration B-Business\n" +
		"#C Importance: 7-high\n#E Scale: T-tiny<100\n")
	output, err := Build(raw, []string{cognition.ScopeCode, cognition.ScopeDatabase}, textassets.DefaultLocale)
	if err != nil {
		t.Fatal(err)
	}
	assertExampleTag(t, output.Examples[cognition.ScopeCode], "AB7T")
	assertExampleTag(t, output.Examples[cognition.ScopeDatabase], "AB7T")
	if output.AuthoringMeta != string(raw) {
		t.Fatal("assembler changed custom formal Meta bytes")
	}
}

func assertExampleTag(t *testing.T, example, want string) {
	t.Helper()
	entry, ok := index.ParseEntryLine(example, 1)
	if !ok {
		t.Fatalf("calibration example is not a valid Entry: %q", example)
	}
	if entry.TagsRaw != want {
		t.Fatalf("unexpected calibration tag: got=%q want=%q example=%q", entry.TagsRaw, want, example)
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
