package prompt

import (
	"regexp"
	"strings"
	"testing"

	"github.com/aoci-spec/aoci-code/textassets"
)

var englishPromptHan = regexp.MustCompile(`[\p{Han}]`)

func TestEnglishHeaderAndEntryPromptsContainNoHanText(t *testing.T) {
	previous := textassets.ActiveLocale()
	if err := textassets.SetActiveLocale(textassets.DefaultLocale); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = textassets.SetActiveLocale(previous) })

	headerSystem, headerUser, err := BuildHeaderMessages(HeaderInput{
		ProjectName:   "demo",
		RepoRootSlash: "/work/demo/",
		TotalFiles:    1,
		Dirs:          []DirCount{{Dir: ".", Count: 1}},
		Exts:          []ExtCount{{Ext: ".go", Count: 1}},
		SampleFiles:   []string{"main.go"},
	})
	if err != nil {
		t.Fatal(err)
	}
	entrySystem, entryUser, err := BuildEntryMessages(EntryInput{
		RelPath:    "main.go",
		SourceText: "package main\nfunc main() {}\n",
		HeaderText: "#Locale: en-US\n#A Layer: X-Executable\n#B Module: App-Application\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	combined := strings.Join([]string{headerSystem, headerUser, entrySystem, entryUser}, "\n")
	if englishPromptHan.MatchString(combined) {
		t.Fatalf("English Prompt output contains Han text:\n%s", combined)
	}
	for _, required := range []string{"#Locale: en-US", "characters", "F:", "R:", "A:", "S:"} {
		if !strings.Contains(combined, required) {
			t.Fatalf("English Prompt output lacks %q", required)
		}
	}
}
