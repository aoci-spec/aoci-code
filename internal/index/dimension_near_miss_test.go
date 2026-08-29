package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A spelling this parser does not accept must be reported, never accepted.
// Accepting it would turn a currently dark dictionary gate on for repositories
// whose header uses hyphens, so writes that succeed today would start failing on
// a binary swap — the exact upgrade wedge this release exists to remove.
func TestDimensionNameNearMissIsReportedNeverAccepted(t *testing.T) {
	for _, test := range []struct{ line, canonical, written string }{
		{"#S-quota: C9-8≤600", "S配额", "S-quota"},
		{"#S-Quota: C9-8≤600", "S配额", "S-Quota"},
		{"#s quota: C9-8≤600", "S配额", "s quota"},
		{"#S_quota: C9-8≤600", "S配额", "S_quota"},
		{"#S QUOTA: C9-8≤600", "S配额", "S QUOTA"},
		{"#A-Layer: C Code", "A层级", "A-Layer"},
		{"#a layer: C Code", "A层级", "a layer"},
		{"#B-Module: G General", "B模块", "B-Module"},
		{"#C-Importance: 9 8 7", "C重要度", "C-Importance"},
		{"#D-Trait: X", "D特征", "D-Trait"},
		{"#E-Scale: L M S T", "E规模", "E-Scale"},
		{"#e-scale: L M S T", "E规模", "e-scale"},
	} {
		canonical, written, near := DimensionNameNearMiss(test.line)
		if !near {
			t.Fatalf("%q was not reported as a near miss", test.line)
		}
		if canonical != test.canonical || written != test.written {
			t.Fatalf("%q reported canonical=%q written=%q, want %q / %q",
				test.line, canonical, written, test.canonical, test.written)
		}
		if _, _, ok := parseDictLine(test.line); ok {
			t.Fatalf("%q was ACCEPTED; a near miss must not change how any existing Meta parses", test.line)
		}
	}
}

// The single most important test here. The same header carries #F scope:,
// #R scope:, #A scope:, #S discipline: and #S-Admission:, so a predicate any
// looser than normalized equality tells every AOCI repository it misspelled a
// dimension. This pins the official assets against that.
func TestOfficialHeaderLinesAreNeverNearMisses(t *testing.T) {
	sources := map[string]string{}
	for _, path := range []string{
		"../../aoci.meta.txt",
		"../../examples/minimal-repository/README.md",
		"../../spec/public/aoci-index-format-v1.txt",
		"../../spec/public/s-field-discipline.txt",
		"../../spec/public/s-field-discipline.en.txt",
	} {
		raw, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		sources[filepath.Base(path)] = string(raw)
	}
	// Every accepted spelling, in every tolerated shape, must stay exact.
	sources["accepted-forms"] = strings.Join([]string{
		"#A Layer: C Code", "#A层级: C 代码", "#A层级(维名夹注): C 代码",
		"#B Module: G General", "#C Importance: 9 8 7", "#D Trait: X",
		"#E Scale: L M S T", "#S quota: C9-8≤600", "#S Quota: C9-8≤600",
		"#S配额: C9-8≤600", "#S配额：C9-8≤600",
	}, "\n")
	// Neighbouring header dimensions that are NOT tag dictionary dimensions.
	sources["neighbours"] = strings.Join([]string{
		"#F scope: what the object does", "#R scope: relations", "#A scope: api",
		"#S discipline: 2", "#S-Admission: non-inferable-and-error-preventing",
		"#F 口径: 职责", "#S 纪律: 2", "#Object-Kinds: code=file database=table",
		"#Canonical-Tag-Authoring: compact A+B+C+[D]+E",
	}, "\n")

	if len(sources) < 3 {
		t.Fatal("fixture precondition: expected to read at least the repository Meta and the specs")
	}
	for name, text := range sources {
		for _, line := range strings.Split(text, "\n") {
			if canonical, written, near := DimensionNameNearMiss(line); near {
				t.Fatalf("%s: %q was reported as a near miss for %s (written %q).\n"+
					"An official asset must never be told it misspelled a dimension; "+
					"the near-miss predicate has been widened too far.",
					name, strings.TrimSpace(line), canonical, written)
			}
		}
	}
}

// A near miss must not move any parsed value. Reporting is the whole change.
func TestNearMissChangesNoParsedValue(t *testing.T) {
	header := "#S-quota: C9-8≤100 C7-4≤100 C3-1≤100\n#E-Scale: A B C\n#A-Layer: Z\n"
	thresholds := ExtractSQuotaThresholds(header)
	if thresholds.HasQuotas() {
		t.Fatal("a rejected spelling supplied quota values")
	}
	if got := EffectiveSQuotaContract(header); got != EffectiveSQuotaContract("") {
		t.Fatalf("a rejected spelling changed the effective contract: %q", got)
	}
	dict := ExtractTagDict(header)
	if dict.HasObjectContract() {
		t.Fatal("a header made entirely of rejected spellings produced an object contract")
	}
}

// A stray misspelling beside a correct declaration changed nothing, so naming it
// would be noise. D is optional, so a Meta carrying only a misspelled D loads
// today and must keep loading without a new diagnostic. Both guards exist so the
// diagnostic can never fire on a repository that is in fact fine.
func TestNearMissIsRecordedOnlyWhereItCostTheOperatorSomething(t *testing.T) {
	for _, test := range []struct {
		name   string
		header string
		want   []string
	}{
		{"axis declared correctly beside a stray", "#E Scale: L M S T\n#E-Scale: A B\n", nil},
		{"axis only misspelled", "#E-Scale: A B\n", []string{"E-Scale"}},
		{"optional D only misspelled", "#A Layer: C\n#B Module: G\n#C Importance: 9\n#E Scale: L\n#D-Trait: X\n", nil},
		{"quota declared correctly beside a stray", "#S quota: C9-8≤600\n#S-quota: C9-8≤100\n", nil},
		{"quota only misspelled", "#S-quota: C9-8≤100\n", []string{"S-quota"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			got := []string{}
			for _, miss := range ExtractTagDict(test.header).UnrecognizedDimensionNames() {
				got = append(got, miss.Written)
			}
			if strings.Join(got, ",") != strings.Join(test.want, ",") {
				t.Fatalf("recorded %v, want %v", got, test.want)
			}
		})
	}
}

// The verdict an object contract produces must be identical with and without a
// near-miss line present. Recording is a diagnostic, never an input.
func TestNearMissNeverChangesAnObjectContractVerdict(t *testing.T) {
	complete := "#A Layer: C Code\n#B Module: G General\n#C Importance: 9 8 7\n#E Scale: L M S T\n"
	for _, stray := range []string{"", "#D-Trait: X\n", "#A-Layer: Z\n", "#S-quota: C9-8≤1\n"} {
		base := ExtractTagDict(complete)
		with := ExtractTagDict(complete + stray)
		if base.HasObjectContract() != with.HasObjectContract() {
			t.Fatalf("stray %q changed the object contract verdict", stray)
		}
		for axis, symbols := range map[string]map[string]bool{"A": base.A, "B": base.B, "C": base.C, "E": base.E} {
			other := map[string]map[string]bool{"A": with.A, "B": with.B, "C": with.C, "E": with.E}[axis]
			if len(symbols) != len(other) {
				t.Fatalf("stray %q changed axis %s: %v vs %v", stray, axis, symbols, other)
			}
		}
	}
}

// The S declaration has always had a three-state design: absent, present but
// unreadable, present and readable. A rejected spelling used to land in state
// one, which is how an operator's declared numbers silently became the machine
// defaults with no message at all. It belongs in state two.
func TestARejectedQuotaSpellingIsStateTwoNotStateOne(t *testing.T) {
	absent := ExtractSQuotaThresholds("#Object-Kinds: code=file\n")
	if absent.SawSQuotaLine() || absent.UnrecognizedName() != "" {
		t.Fatalf("a header with no S line is not state two: saw=%v name=%q",
			absent.SawSQuotaLine(), absent.UnrecognizedName())
	}

	rejected := ExtractSQuotaThresholds("#S-quota: C9-8≤100 C7-4≤100 C3-1≤100\n")
	if !rejected.SawSQuotaLine() {
		t.Fatal("a rejected spelling is still reported as no declaration at all")
	}
	if rejected.UnrecognizedName() != "S-quota" {
		t.Fatalf("the spelling was not reported as written: %q", rejected.UnrecognizedName())
	}
	if rejected.HasQuotas() {
		t.Fatal("a rejected spelling supplied values")
	}

	// A correct declaration alongside a rejected one is readable, and nothing is
	// reported: the operator has nothing to fix.
	both := ExtractSQuotaThresholds("#S-quota: C9-8≤100\n#S quota: C9-8≤600 C7-4≤500 C3-1≤50\n")
	if !both.HasQuotas() || both.UnrecognizedName() != "" {
		t.Fatalf("a readable declaration was reported as a problem: quotas=%v name=%q",
			both.HasQuotas(), both.UnrecognizedName())
	}
	if limit, _ := both.LimitForC(7); limit != 500 {
		t.Fatalf("the readable declaration did not govern: C7 = %d", limit)
	}
}
